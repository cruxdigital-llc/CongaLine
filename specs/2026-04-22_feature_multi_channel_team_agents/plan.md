# Plan: Multi-Channel Team Agents

## Overview

Lift the "one binding per platform per agent" constraint so a single team agent can serve multiple channels on the same platform (primary target: Slack). The change is mostly about removing a guard and iterating where the code currently assumed a singular binding. Router fan-out works unchanged — it already routes events by channel ID, we just populate more IDs into its map.

## Architecture

```
Admin                                         Router (routing.json)
┌──────────────────────────┐                 ┌──────────────────────────┐
│ conga channels bind      │                 │ channels: {              │
│   acme slack:C1          │ ──►             │   "C1": "http://acme...",│
│ conga channels bind      │ regenerates     │   "C2": "http://acme...",│
│   acme slack:C2          │                 │   "C3": "http://acme..." │
│ conga channels bind      │                 │ }                        │
│   acme slack:C3          │                 └──────────────────────────┘
└──────────────────────────┘                           │
                                                       │ fan-out
Agent container (openclaw.json)                        ▼
┌──────────────────────────┐              message in C1, C2, or C3
│ channels.slack:          │ ◄─────────── all reach same agent webhook
│   groupPolicy: allowlist │
│   channels: {C1,C2,C3}   │
└──────────────────────────┘

Unbound channel path:
┌──────────────────────────┐
│ message in C9            │
│ (bot invited, not bound) │ ──► Router sees unknown ID →
└──────────────────────────┘     chat.postEphemeral to sender:
                                 "I'm not configured for this channel.
                                  Ask an admin to run
                                  `conga channels bind <agent> slack:C9`."
                                 (rate-limited per (channel, user) per 24h)
```

## Phases

### Phase 1: Go Data Model — Plural Bindings Helper

**Files:**
- `pkg/provider/provider.go` — Add `ChannelBindings(platform string) []channels.ChannelBinding` method alongside the existing singular `ChannelBinding(platform string)` (lines 38-46).

**New method:**
```go
// ChannelBindings returns all bindings for the given platform.
// Empty slice if none are bound.
func (a *AgentConfig) ChannelBindings(platform string) []channels.ChannelBinding {
    var out []channels.ChannelBinding
    for _, b := range a.Channels {
        if b.Platform == platform {
            out = append(out, b)
        }
    }
    return out
}
```

The singular `ChannelBinding(platform)` helper stays for callers that genuinely want "any one binding" (e.g. display code, existence checks). It continues to return the first match.

**Tests:** `pkg/provider/provider_test.go` — `TestChannelBindings_ReturnsAllMatches`, `TestChannelBindings_EmptyWhenNone`, `TestChannelBinding_UnchangedForSingle`.

### Phase 2: Bind Guard — Dedupe on (platform, id)

**Files:**
- `pkg/provider/localprovider/channels.go:187-191`
- `pkg/provider/remoteprovider/channels.go:164-168`
- `pkg/provider/awsprovider/channels.go:186-190`

**Current guard (all three):**
```go
if a.ChannelBinding(binding.Platform) != nil {
    return fmt.Errorf("agent %q already has a %s binding: %w",
        agentName, binding.Platform, provider.ErrBindingExists)
}
```

**Replace with:**
```go
for _, existing := range a.Channels {
    if existing.Platform == binding.Platform && existing.ID == binding.ID {
        // Idempotent: same binding already present
        return nil
    }
}
```

Idempotent re-bind of the exact same `(platform, id)` returns `nil` — no error, no duplicate entry. Different IDs on the same platform are now allowed.

**Tests:** For each provider:
- `TestBind_MultipleChannelsSamePlatform_Succeeds`
- `TestBind_DuplicateExactBinding_Idempotent`
- `TestBind_DifferentLabelSameID_NoDuplicate`

### Phase 3: Routing Generation — Fan-Out Entries

**Files:**
- `pkg/common/routing.go:62-71`

**Current (singular):**
```go
case "team":
    binding := a.ChannelBinding("slack")
    if binding == nil {
        continue
    }
    cfg.Channels[binding.ID] = url
```

**Replace with:**
```go
case "team":
    for _, binding := range a.ChannelBindings("slack") {
        cfg.Channels[binding.ID] = url
    }
```

For an agent with N Slack bindings, `cfg.Channels` gets N entries all pointing at the same `url`.

**Tests:** `pkg/common/routing_test.go`:
- `TestGenerateRoutingJSON_TeamAgentSingleChannel` — regression; byte-identical output to current.
- `TestGenerateRoutingJSON_TeamAgentMultipleChannels` — N entries, all same URL.
- `TestGenerateRoutingJSON_TeamAgentMixedWithOtherAgents` — no cross-contamination.

### Phase 4: Slack Policy Generation — Allowlist Expansion via Interface Extension

**Files:**
- `pkg/channels/channels.go` — Add new `MultiBindingChannel` interface that embeds `Channel`.
- `pkg/channels/slack/slack.go:73-80` — Implement the new method.
- `pkg/runtime/openclaw/config.go` — Call site: prefer multi path when the channel satisfies `MultiBindingChannel`.

**Interface extension (Go embedding pattern):**
```go
// Channel (existing, unchanged) — every platform implements this.
type Channel interface {
    OpenClawChannelConfig(binding ChannelBinding) map[string]any
    // ... existing methods
}

// MultiBindingChannel — platforms that support multiple bindings per agent
// extend the base interface. Callers type-assert to discover support.
type MultiBindingChannel interface {
    Channel
    OpenClawChannelConfigMulti(bindings []ChannelBinding) map[string]any
}
```

Slack implements `MultiBindingChannel`. Telegram and any future channel that only supports 1:1 binding continue to implement plain `Channel` — no change required. The runtime config generator type-asserts:

```go
if multi, ok := ch.(channels.MultiBindingChannel); ok {
    bindings := agent.ChannelBindings(platform)
    cfg := multi.OpenClawChannelConfigMulti(bindings)
    // merge cfg into channelsCfg[platform]
} else {
    // existing singular path, unchanged
    cfg := ch.OpenClawChannelConfig(binding)
}
```

This keeps the existing `Channel` contract stable for all implementers, adds capability for platforms that need it, and gives the call site a clean way to opt in.

**Slack's `OpenClawChannelConfigMulti` behavior:**
- `allowlist`: union of all binding IDs.
- `channels`: map keyed by each binding ID (one entry per binding), values as in the existing singular path.
- For `len(bindings) == 0`: returns nil (caller skips — matches the current "no Slack section if no binding" behavior).
- For `len(bindings) == 1`: output is byte-identical to the singular `OpenClawChannelConfig` path (regression guarantee in Success Criteria #8).

**Tests:** `pkg/channels/slack/slack_test.go`:
- `TestOpenClawChannelConfigMulti_SingleBinding_MatchesSingular` — output byte-identical to the singular path.
- `TestOpenClawChannelConfigMulti_MultipleBindings` — allowlist and `channels` map contain all IDs.
- `TestOpenClawChannelConfigMulti_Empty` — returns nil; no panics.
- `TestMultiBindingChannel_SlackImplements` — compile-time assertion via `var _ channels.MultiBindingChannel = (*slack.Slack)(nil)`.

### Phase 5: CLI UX — Additive Bind, Targeted Unbind, List All

**Files:**
- `internal/cmd/channels.go`
- `internal/mcpserver/tools_channels.go`

**`channels bind` (line 231 `channelsBindRun`):**
- No argument shape change. Now succeeds for N bindings; idempotent on exact match.
- Update help text: "Binds a channel to an agent. An agent may have multiple bindings on the same platform."

**`channels unbind` (line 262 `channelsUnbindRun`):**
- Argument syntax remains `<agent> <platform>:<id>`. The `<id>` was previously technically optional (since only one binding existed); if any call sites accepted just `<platform>`, they must now error with a helpful message when multiple bindings exist for that platform.
- Add explicit error: `"agent %q has N %s bindings; specify which to remove (e.g. %s:%s)"` with the first ID as an example.

**`channels list` (line 188 `channelsListRun`):**
- Output format: one row per binding. Same agent appears multiple times across rows when it has multiple bindings.
- Example table:
  ```
  AGENT   PLATFORM  ID            LABEL
  acme    slack     C0123456789   #legal
  acme    slack     C0234567890   #sales
  acme    slack     C0345678901   #procurement
  ```

**MCP parity:** Update the three MCP tool handlers at `internal/mcpserver/tools_channels.go:109-223` to match CLI behavior. Tool descriptions updated to note multi-binding support.

**Tests:**
- `internal/cmd/channels_test.go` — table-driven tests for each subcommand, multi-binding cases.
- `internal/mcpserver/tools_channels_test.go` — MCP parity.

### Phase 6: Router — Unbound-Channel Ephemeral Notice

**Files:**
- `router/slack/src/index.js` — `resolveTarget` (lines 61-83), event handler (lines 163+).
- `router/slack/src/unbound-notice.js` (new) — small module for rate-limited ephemeral dispatch.

**`resolveTarget` diff:** currently returns `null` when the channel ID isn't in `config.channels`. Extend the return shape so callers can distinguish "unknown channel" from "no route at all":
```javascript
return { target: null, reason: `unbound:${channel}`, channelId: channel, userId: extractUser(payload) };
```

**Event handler:** when `route.target` is null and `route.reason` starts with `unbound:`, call `unbound.notify(route.channelId, route.userId, payload)` which:
1. Checks an in-memory `Map<(channelId|userId), expiryTs>` to enforce the 24h rate limit.
2. Looks up a "suggested agent name" — for v1, omits the agent name from the message (see PM open question in requirements.md). Message text:
   > "I'm not configured for this channel yet. Ask your Conga admin to run: `conga channels bind <agent> slack:{channelId}`"
3. POSTs `chat.postEphemeral` with `channel: channelId`, `user: userId`, `text: <message>`.
4. Swallows errors from `chat.postEphemeral` (e.g. bot not in channel yet, user_id invalid) — log and drop.

**`unbound.notify` rate limit:**
- Key: `${channelId}:${userId}`
- TTL: 24 hours
- Max map size: 5000 entries, lazy eviction (prune expired on write).
- Router restart clears the map — acceptable per requirements.

**Safety:** skip notification if:
- The event is a `bot_message` or subtype we already drop (line 169).
- The channel ID doesn't start with `C` or `G` (private channels, DMs, etc. — DMs use a different path).
- The user ID is missing or starts with `B` (bot).

**Tests:** `router/slack/src/unbound-notice.test.js`:
- First event in unbound channel → one `chat.postEphemeral` call.
- Second event from same user in same channel within 24h → zero calls.
- Second event from different user → one call.
- After 24h → one call.
- Missing user ID → zero calls.

### Phase 7: Tests & Integration

**Unit tests** — covered in phases 1-6 above.

**Integration tests:**
- `test/integration/local/multi_channel_test.go` (new) — provision one team agent locally, bind 3 Slack channels, verify:
  - `routing.json` has 3 entries for that agent
  - `openclaw.json` allowlist has 3 IDs
  - Unbind one → 2 entries, 2 IDs
  - Unbind all → 0 entries, agent's `channels.slack` section no longer present (or has empty allowlist — match current single-binding behavior when allowlist is zeroed by removing the only binding).
- Remote and AWS parity covered via existing provider integration harness; add one multi-binding case to each.

**E2E verification (manual):**
- Provision team agent, bind three real Slack channels, post in each → same container receives all three.
- Invite bot to an unbound channel, post a message → ephemeral notice appears to sender with the exact bind command.
- Post again in the unbound channel → no second ephemeral within 24h.
- Unbind one channel while active → routing and allowlist update within the router's `fs.watch` reload window.

## Persona Review Checklist

### Architect
- [ ] `ChannelBindings` is additive; `ChannelBinding` singular helper unchanged for existing callers.
- [ ] Bind guard change is behavior-preserving for the single-binding case (idempotent add returns nil, matching current error-free path when the exact binding doesn't exist).
- [ ] Routing generation diff is a pure for-loop over the existing singular case — no new branches in the type switch.
- [ ] `OpenClawChannelConfigMulti` added without modifying the existing `Channel` interface shape; old path remains.
- [ ] Provider parity preserved across local, remote, aws — identical guard change in all three files.
- [ ] No interaction hazard with dm-agent-routing's `AgentDescriptions` addition — different field in `RoutingConfig`.

### QA
- [ ] Idempotent bind covered: same `(platform, id)` twice → no error, no dup.
- [ ] Unbind error message when multiple bindings exist and no ID specified is helpful and names a concrete example.
- [ ] Byte-identical output for single-binding agents verified against a snapshot test.
- [ ] Unbound-channel rate limit tested at boundary: first hit, second hit within 24h, after TTL, different user, bot message skipped.
- [ ] Router map max size (5000) exercised; lazy eviction path hit.
- [ ] `channels list` sort order is stable (alphabetical by agent, then platform, then ID).

### PM
- [ ] Admin experience for binding a second channel is a single CLI call — no ceremony.
- [ ] Unbound-channel message text includes the exact CLI command; user can copy-paste to their admin.
- [ ] Open question: does the ephemeral message name the agent? Trade-off: discoverability vs. information leakage about internal agent names. **Default for v1: does not name the agent** — the admin knows which agent should own which channels. Message says "ask your Conga admin" without specifying an agent name. Revisit based on user feedback.
- [ ] `channels list` output is readable — one row per binding, agent name repeated, clear that one agent owns multiple channels.
- [ ] No surprise behavior for existing single-binding agents.

## Terraform Provider Impact

Changes to `pkg/provider/provider.go` (additive `ChannelBindings` method) and `pkg/channels/slack/slack.go` (additive `OpenClawChannelConfigMulti`) require a Terraform provider release. All changes are additive — backward compatible. Follow the release flow in CLAUDE.md § Terraform Provider:

1. Tag congaline after merge.
2. `go get` + `go mod tidy` in the provider repo.
3. Tag the provider; GoReleaser publishes to registry.

No Terraform resource schema changes — `AgentConfig.Channels` was already a slice.

## Implementation Order

| Phase | Depends on | Risk | Effort |
|-------|-----------|------|--------|
| 1. Plural bindings helper | — | Low | S |
| 2. Bind guard change (idempotent) | Phase 1 | Low | S |
| 3. Routing fan-out | Phase 1 | Low | S |
| 4. Allowlist via interface extension | Phase 1 | Medium | M |
| 5. `UnbindChannel` signature change + `ErrAmbiguousUnbind` | — | Medium | M (breaking API — three impls + three callers) |
| 6. CLI + MCP UX | Phases 2, 3, 4, 5 | Low | M |
| 7. Router ephemeral notice | — | Medium | M |
| 8. Tests & integration | All | — | M |
| 9. **Terraform provider coordinated update** | Phases 1-5 tagged | Medium-High | M (state migration, ID scheme change, deprecated import format) |

**Two PRs across two repos.**
- **PR #1 (congaline, single):** Phases 1-8 ship together. Router changes and Go changes land in one PR so the feature is coherent and the ephemeral notice isn't live without the binding support.
- **PR #2 (terraform-provider-conga):** Phase 9. Opens after the congaline tag is published.

**Why this can't ship as CLI-only first:** the `Provider` interface change (Phase 5) is a hard compile break against terraform-provider-conga. The provider must be updated in the same release cycle or operators using Terraform will be unable to build/run the provider.

Congaline tag → provider `go get` + tidy → provider tag → operator upgrade. Full sequence in spec.md §7.1.
