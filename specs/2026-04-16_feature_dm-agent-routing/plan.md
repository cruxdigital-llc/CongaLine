# Plan: DM Agent Routing

## Overview

Add LLM-classified DM routing to the Slack event router so users with access to multiple agents can DM the bot and have the right agent respond transparently. DM access is derived automatically from Slack channel membership — no manual enrollment. The classifier defaults to Haiku but accepts any OpenAI-compatible endpoint (e.g. self-hosted via Ollama) for privacy-sensitive deployments. When confidence is low, the router sends an ephemeral Block Kit picker. Thread replies stay pinned to the routed agent.

## Architecture

```
User DM → Slack Socket Mode → Router
                                 ├─ User not in any bound channel? → personal agent via members map (unchanged)
                                 ├─ User in 1 channel (+ optional personal)? → 1 agent: forward directly
                                 ├─ User in 2+ channels, thread reply? → forward to cached agent
                                 └─ User in 2+ channels, new message?
                                      ├─ Classifier confident → forward to chosen agent
                                      └─ Classifier uncertain → ephemeral picker

Membership resolution:
  Router startup → conversations.members for each Slack-bound channel → in-memory maps
  Steady state  → member_joined_channel / member_left_channel events → update maps
  Safety net    → re-poll every 30 minutes
```

## Phases

### Phase 0: Validation Spike

Before committing to the data-model and Slack manifest changes downstream, verify three load-bearing assumptions against the pinned `ghcr.io/openclaw/openclaw:2026.3.11` image and the `@slack/socket-mode` v2 router.

**Verify:**

1. **OpenClaw empty-allowlist DM acceptance.** Configure a team agent with `dmPolicy: "allowlist"`, `allowFrom: []`, `dm.enabled: true`. Send a router-forwarded DM event to its `/slack/events` endpoint. Confirm OpenClaw processes the DM rather than rejecting it as unallowed. If empty `allowFrom` denies, document the required form (drop `allowFrom` entirely, use a sentinel, or have the router populate it from the membership map and refresh).
2. **Socket Mode interactive delivery.** Confirm whether `block_actions` payloads arrive via the existing `client.on('slack_event')` catch-all or require a separate `client.on('interactive')` handler in `@slack/socket-mode` v2. Document which.
3. **Socket Mode member events.** Confirm `member_joined_channel` and `member_left_channel` arrive through the catch-all once subscribed in the app manifest.

**Slack app manifest updates needed (regardless of outcome):**

- Event Subscriptions: add `member_joined_channel`, `member_left_channel`
- Interactivity & Shortcuts: enabled (no Request URL — Socket Mode delivers)

**Output:** a short findings note in this spec dir that locks the empty-allowlist form (feeds Phase 2) and the interactive-handler shape (feeds Phase 6). If any assumption fails, revise the dependent phase before proceeding.

**Effort:** S. Single-agent dev environment, no provider release, no router code change beyond a logging hook.

### Phase 1: Go Data Model — `routing.json` Schema Restructure

**Files:**
- `pkg/provider/provider.go` — Add `Description string` to `AgentConfig`
- `pkg/common/routing.go` — Restructure `RoutingConfig` and extend `GenerateRoutingJSON()`

**`AgentConfig` change:**
```go
Description string `json:"description,omitempty"` // agent purpose — used by classifier
```

DM access is NOT stored on `AgentConfig`. It is resolved at runtime by the router from Slack channel membership.

**`RoutingConfig` restructure (wire-format change):**

```go
type RoutingConfig struct {
    Agents   map[string]AgentRouting `json:"agents"`
    Channels map[string]string       `json:"channels"` // platform channel id → agent name
    Members  map[string]string       `json:"members"`  // platform member id → agent name
}

type AgentRouting struct {
    URL         string              `json:"url"`
    Type        string              `json:"type"` // "user" | "team"
    Description string              `json:"description,omitempty"`
    ChannelIDs  map[string][]string `json:"channelIds,omitempty"` // platform → ids
    MemberIDs   map[string][]string `json:"memberIds,omitempty"`  // platform → ids
}
```

`agents` is the source of truth for agent metadata. `channels` and `members` become string→name indexes. The platform-keyed `ChannelIDs`/`MemberIDs` lets the Slack router filter to Slack IDs only (and the Telegram router do the same for its IDs) without colliding when the file is shared.

`GenerateRoutingJSON()` populates all three maps atomically from the agent list:
- Description defaults to `"{name} ({type} agent)"` when empty
- Paused agents are excluded from `agents`, `channels`, and `members`
- Per-binding `Platform` flows into `ChannelIDs[platform]` / `MemberIDs[platform]`

**Tests:** `pkg/common/routing_test.go` — new cases:
- Agent name appears in `agents` with correct URL and type
- `channels[id]` and `members[id]` point to agent name (not URL)
- `ChannelIDs["slack"]` populated correctly across bindings
- Description fallback when empty; explicit description honored
- Paused agent excluded from all three maps

**Deployment note:** this is a wire-format break. `conga admin refresh-all` regenerates `routing.json` before the router reloads (the router watches the file and reloads on change). Same-repo coupling between writer and reader means we ship them together.

### Phase 2: Team Agent DM Acceptance (in Slack channel)

**Files:**
- `pkg/channels/slack/slack.go` — Change the `case "team"` branch of `OpenClawChannelConfig` to accept router-forwarded DMs

The DM-policy decision belongs in the Slack channel, not the runtime layer — `pkg/runtime/openclaw/config.go` iterates bindings generically and shouldn't carry Slack-specific behavior.

**Change (subject to Phase 0 findings on the exact `allowFrom` form):**

```go
case "team":
    cfg["groupPolicy"] = "allowlist"
    cfg["dmPolicy"] = "allowlist"
    cfg["allowFrom"] = []string{} // router gates access via channel membership
    cfg["dm"] = map[string]any{"enabled": true}
    if binding.ID != "" {
        cfg["channels"] = map[string]any{
            binding.ID: map[string]any{"allow": true, "requireMention": false},
        }
    }
```

The router gates who can reach the team agent via channel membership; OpenClaw just needs to accept DMs the router forwards. If Phase 0 shows empty `allowFrom` is treated as deny-all, adopt whatever form Phase 0 documents (e.g. omit `allowFrom`, or have the router populate it).

**Tests:** `pkg/channels/slack/slack_test.go` — verify team agent produces `dmPolicy: "allowlist"` and `dm.enabled: true`; verify user agent unchanged.

### Phase 3: Channel Membership Resolution (Router)

**Files:**
- `router/slack/src/membership.js` (new)

**Bootstrap (on router startup):**
1. For each agent in `config.agents` with type `"team"`, iterate `agent.channelIds["slack"]`. Call `conversations.members` per channel (Tier 4 — 100 req/min, trivial for typical fleets).
2. Build in-memory maps:
   - `channelId → Set<userId>`
   - `userId → [{ agentName, url, description }]` — agents the user can DM (channel-derived + personal from `config.members`)
3. Personal agent (from `config.members[userId]` → `config.agents[name]`) added to each user's agent list when present.

**Bootstrap-failure policy:** if `conversations.members` fails for a channel (bot not in channel, rate limit, transport error), log a warning and skip that channel. Do not block router startup. Affected users fall back to personal-only DM routing until the next safety-net re-poll. Slack API outage at startup ≠ router refusing to start.

**Steady-state (event-driven):**
- `member_joined_channel` / `member_left_channel` arrive via Socket Mode (delivery path confirmed in Phase 0). Update both maps.

**Safety net:**
- Re-poll `conversations.members` every 30 minutes to recover from dropped events.

**Exports:** `{ bootstrap, getUserAgents, handleMemberJoin, handleMemberLeave }`.

### Phase 4: Agent Descriptions

**Files:**
- `internal/cmd/admin_provision.go` — add `--description` flag
- `internal/cmd/agent.go` (or equivalent) — add a way to update description post-hoc (e.g. `conga agent set <name> --description "..."`)

**Behavior:**
- Description stored in `AgentConfig.Description`
- Default at routing-config generation time: `"{name} ({type} agent)"`
- Surfaces in `routing.json` as `agents[name].description`
- Classifier prompt quality scales with description quality — `conga agent show <name>` should display it

### Phase 5: Router Classifier Module

**Files:**
- `router/slack/src/classifier.js` (new)

**Endpoint selection:**

```javascript
// Default: Anthropic Haiku (Messages API)
// Self-hosted: set CLASSIFIER_URL to any OpenAI-compatible /v1/chat/completions endpoint
const classifierUrl = process.env.CLASSIFIER_URL || null;
const anthropicKey  = process.env.ANTHROPIC_API_KEY || null;
```

**Two code paths, one prompt.** Anthropic Messages and OpenAI Chat Completions differ in non-trivial ways — the spec previously called this "same prompt, same JSON response parsing," which understated it:

| | Anthropic (default) | OpenAI-compatible (`CLASSIFIER_URL`) |
|---|---|---|
| Auth | `x-api-key` + `anthropic-version` headers | `Authorization: Bearer` (often optional for self-hosted) |
| System prompt | Top-level `system` field | First message with `role: "system"` |
| Response text | `content[0].text` | `choices[0].message.content` |

The **prompt content** is shared. Both expect the model to return JSON `{"agent": "name", "confident": true|false}` inside the assistant text; both code paths extract that text and parse it identically downstream.

**Classifier surface:**
- `createClassifier(config)` — returns `{ classify, getCachedThread, cacheThread }`, or `null` if neither endpoint nor key configured
- `classify(messageText, agents)`:
  - System prompt lists each agent's name + description and instructs JSON-only output
  - User message is the DM text
  - 3-second timeout via `AbortController`
  - Validates returned agent name against the known set
  - Returns `{ agent, confident }` or `null` on failure

**Clarification flow (low confidence):**
- Router sends an ephemeral message via `chat.postEphemeral` with Block Kit buttons (one per agent). Requires `SLACK_BOT_TOKEN` + `chat:write` (already in recommended scopes).
- Action IDs prefixed `dm-route-pick:` so the interactive handler can match them.
- On click: forward original message to chosen agent, cache thread, clear pending entry.
- Pending messages held in-memory with 60-second TTL. On TTL expiry: forward to default agent (first in list).

**Thread cache:**
- `Map<thread_ts, { agentUrl, expiry }>`
- 4-hour TTL, max 2000 entries, lazy eviction on insert
- On DM thread reply: check cache before classifying

**No new npm dependencies** — native `fetch` covers both endpoints.

### Phase 6: Router Integration

**Files:**
- `router/slack/src/index.js` — modified `resolveTarget`, event dispatch, interactive handler

**Modified `resolveTarget` (Option C indirection):**

```javascript
function resolveTarget(payload) {
  const channel = extractChannel(payload);

  if (channel && channel.startsWith('D')) {
    const userId = extractUser(payload);
    const agents = membership.getUserAgents(userId); // [{ agentName, url, description }]

    if (agents && agents.length > 0) {
      if (agents.length === 1) {
        return { target: agents[0].url, reason: `dm-direct:${userId}` };
      }
      return { target: null, agents, userId, reason: `dm-classify:${userId}` };
    }

    // Fallback: personal agent via members index (existing behavior)
    if (userId && config.members[userId]) {
      const name = config.members[userId];
      const url  = config.agents[name]?.url;
      if (url) return { target: url, reason: `dm:${userId}` };
    }
  }

  if (channel && config.channels[channel]) {
    const name = config.channels[channel];
    const url  = config.agents[name]?.url;
    if (url) return { target: url, reason: `channel:${channel}` };
  }

  // ... user fallback for app_home etc., resolved through agents ...
}
```

**Async dispatch** for the multi-agent DM case:
1. Check thread cache → forward to cached agent if hit
2. Call `classifier.classify(text, route.agents)`
3. Confident → forward + cache
4. Not confident → post ephemeral picker + hold in pending map (60s TTL)
5. Classifier failure → forward to first agent (default) + cache

**Interactive handler** — shape determined by Phase 0:
- Match action ID prefix `dm-route-pick:`
- Look up pending message → forward → cache thread → clear pending

The Slack ack still fires before classification — async path doesn't block the 3-second ack budget.

### Phase 7: Router Secrets, Configuration, and SetupGuide

**Files:**
- `pkg/channels/slack/slack.go` — `SharedSecrets()`, `RouterEnvVars()`, `SetupGuide()`

**Secrets:**
```go
// SharedSecrets() additions:
{Name: "anthropic-api-key", EnvVar: "ANTHROPIC_API_KEY",
 Prompt: "Anthropic API key for DM routing classifier (optional, sk-ant-...)",
 Required: false, RouterOnly: true}
{Name: "classifier-url", EnvVar: "CLASSIFIER_URL",
 Prompt: "Custom classifier URL (optional, OpenAI-compatible endpoint for self-hosted models)",
 Required: false, RouterOnly: true}
```

**`RouterEnvVars()`** — pass `SLACK_BOT_TOKEN` (needed for `conversations.members` + `chat.postEphemeral`) plus the two new vars:
```go
if v := sv["slack-bot-token"]; v != "" { vars["SLACK_BOT_TOKEN"] = v }
if v := sv["anthropic-api-key"]; v != "" { vars["ANTHROPIC_API_KEY"] = v }
if v := sv["classifier-url"]; v != "" { vars["CLASSIFIER_URL"] = v }
```

**`SetupGuide()` updates** — the current guide documents scopes but says nothing about events or interactivity. Add:
- **Event Subscriptions**: subscribe to `member_joined_channel` and `member_left_channel`. (No Request URL needed — Socket Mode delivers.)
- **Interactivity & Shortcuts**: enable. (No Request URL needed — Socket Mode delivers `block_actions`.)
- Reaffirm that `channels:read`, `groups:read`, and `chat:write` are required (already documented).

**Router initialization:**
```javascript
const classifier = (process.env.CLASSIFIER_URL || process.env.ANTHROPIC_API_KEY)
  ? createClassifier({
      classifierUrl: process.env.CLASSIFIER_URL,
      anthropicKey:  process.env.ANTHROPIC_API_KEY,
      botToken:      process.env.SLACK_BOT_TOKEN,
    })
  : null;

await membership.bootstrap(config, process.env.SLACK_BOT_TOKEN);
```

**Classifier priority:**
1. `CLASSIFIER_URL` set → custom OpenAI-compatible endpoint
2. `ANTHROPIC_API_KEY` set → Anthropic Haiku
3. Neither → no classifier; single-agent DMs still route directly; multi-agent DMs fall back to first agent

### Phase 8: Tests

**Router test runner:**
- `router/slack/package.json` — add `"test": "node --test src/*.test.js"`
- Uses Node's built-in `node:test` runner — no new npm dependencies

**Unit tests (Go):**
- `pkg/common/routing_test.go` — Option C schema generation (Phase 1)
- `pkg/channels/slack/slack_test.go` — team agent DM acceptance (Phase 2)

**Router tests:**
- `router/slack/src/membership.test.js` — bootstrap with mocked `conversations.members`, join/leave event handling, periodic re-poll
- `router/slack/src/classifier.test.js` — Anthropic path, OpenAI-compatible path, timeout, agent-name validation, failure fallback

**Integration tests:**
- Provision personal + team agent → assert `routing.json.agents` has both with descriptions, URLs, and slack channelIds/memberIds
- Assert team agent's `openclaw.json` has `dmPolicy: "allowlist"` and `dm.enabled: true`

**E2E verification:**
- DM the Slack app → correct agent responds
- Reply in thread → same agent responds
- Ambiguous message → ephemeral picker appears; button click routes correctly
- No classifier configured → multi-agent DM falls back to default agent (not dropped)
- Team-only user → DM reaches team agent
- User joins bound channel → DM routing to that agent starts
- User leaves bound channel → DM routing to that agent stops
- `CLASSIFIER_URL` set → custom endpoint receives the request (not Anthropic)

## Persona Review Checklist

### Architect
- [ ] `RoutingConfig` restructure is wire-format-coordinated: writer (Go) and reader (router) ship together; refresh writes new format before router reload
- [ ] `agents` is the single source of truth; `channels`/`members` are name-indexed lookups
- [ ] Platform-keyed `ChannelIDs`/`MemberIDs` keep Slack and Telegram routers from cross-reading each other's IDs
- [ ] Phase 2 keeps Slack DM policy in `pkg/channels/slack` — runtime layer stays channel-agnostic
- [ ] No circular import introduced (`provider` ← `channels` boundary preserved)
- [ ] Membership module is resilient to Slack API failures (skip-and-warn at bootstrap, periodic re-poll covers missed events)
- [ ] Async classification path doesn't block the Slack ack
- [ ] Thread cache and pending map are TTL- and size-bounded
- [ ] Classifier dual-path is honest about the two code paths (auth, system-prompt placement, response shape)

### QA
- [ ] Phase 0 findings recorded; dependent phases adjusted if assumptions failed
- [ ] Classifier timeout (3s) prevents hung requests
- [ ] Ephemeral clarification 60s TTL — pending messages don't leak
- [ ] Thread cache eviction works at boundary (2000 entries)
- [ ] Existing dedup fires before classification
- [ ] Membership re-poll catches missed join/leave events
- [ ] Bot-not-in-channel → log warning, skip channel, don't block startup
- [ ] Router restart is recoverable: membership rebuilt from API; thread/pending state is best-effort and documented as ephemeral

### PM
- [ ] Zero-syntax UX for end users — no prefixes, no commands
- [ ] Zero admin overhead — DM access follows channel membership automatically
- [ ] Clarification flow is non-blocking — user can ignore and default fires after 60s
- [ ] Feature is opt-in (no classifier configured = no behavior change)
- [ ] Self-hosted classifier option for privacy-sensitive deployments
- [ ] Setup guide reflects new Slack manifest requirements (events + interactivity)
- [ ] Success metrics: classify accuracy (log chosen vs. clarification-required rate)

## Terraform Provider Impact

Changes to `pkg/provider/provider.go` (`Description` field) and `pkg/common/routing.go` (`RoutingConfig` restructure, new `AgentRouting` type) require a Terraform provider release. The `Description` field is additive; the `RoutingConfig` restructure is a wire-format change. Provider consumers don't read `routing.json` directly — they generate it via the Go API — so the provider release exposes the new shape via the same `GenerateRoutingJSON` they already call.

Follow the release flow in `CLAUDE.md`.

Note: `DMAccess` is NOT added to `AgentConfig`. DM access is resolved at runtime by the router from Slack channel membership.

## Implementation Order

| Phase | Depends on | Risk | Effort |
|-------|-----------|------|--------|
| 0. Validation spike | — | Low | S |
| 1. Go data model | Phase 0 (allowlist form, if it affects schema) | Low | S |
| 2. DM acceptance | Phase 0 (allowlist form) | Low | S |
| 3. Membership resolution | Phase 0 (event delivery), Phase 1 (schema) | Medium | M |
| 4. Agent descriptions | Phase 1 | Low | S |
| 5. Router classifier | — | Medium | M |
| 6. Router integration | Phase 0 (interactive shape), Phases 3, 5 | Medium | L |
| 7. Router secrets / SetupGuide | — | Low | S |
| 8. Tests | All | — | M |

Phase 0 unblocks the riskiest assumptions. Phases 1–2 (Go) and Phases 3, 5 (router modules) can be developed in parallel once Phase 0 is recorded. Phase 6 depends on Phases 3 and 5. Phase 7 is independent — just env-var and SetupGuide wiring.

## Operational Notes

- **Thread cache and pending-clarification map are per-router-process.** A router restart (config reload, container restart, host cycle) wipes them. Recovery is graceful: new DMs in an existing thread re-classify on next message; pending picks past TTL fall back to the default agent. Acceptable for v1.
- **Membership state is rebuilt on router startup** via `conversations.members`. Slack API outage at startup means affected users temporarily fall back to personal-only routing; full state recovers on the next 30-minute re-poll.
- **`routing.json` wire-format change** ships in lockstep with router code from this repo. `conga admin refresh-all` writes the new format before the router file-watcher reloads.
