# Plan: DM Agent Routing

## Overview

Add LLM-classified DM routing to the Slack event router so that users with access to multiple agents can DM the bot and have the right agent respond transparently. The classifier uses Haiku for fast, cheap intent classification. When confidence is low, the system asks the user to clarify. Thread replies stay pinned to the routed agent.

## Architecture

```
User DM → Slack Socket Mode → Router
                                 ├─ No dmRouting entry? → fall through to members map (unchanged)
                                 ├─ dmRouting with 1 agent? → forward directly (no classifier)
                                 ├─ dmRouting with 2+ agents, thread reply? → forward to cached agent
                                 └─ dmRouting with 2+ agents, new message?
                                      ├─ Classifier confident → forward to chosen agent
                                      └─ Classifier uncertain → ephemeral message asking user to pick
```

## Phases

### Phase 1: Go Data Model Extensions

**Files:**
- `pkg/provider/provider.go` — Add `Description string` and `DMAccess []string` to `AgentConfig`
- `pkg/common/routing.go` — Add `DMRouting map[string]DMRoutingEntry` to `RoutingConfig`, new `DMRoutingEntry` and `DMRoutingAgent` types
- `pkg/common/routing.go` — Extend `GenerateRoutingJSON()` to populate `dmRouting`

**`AgentConfig` changes:**
```go
Description string   `json:"description,omitempty"`  // agent purpose — used by classifier
DMAccess    []string `json:"dm_access,omitempty"`     // user IDs who can DM this team agent
```

**`RoutingConfig` extension:**
```go
DMRouting map[string]DMRoutingEntry `json:"dmRouting,omitempty"`
```
```go
type DMRoutingEntry struct {
    Default string           `json:"default,omitempty"` // fallback URL
    Agents  []DMRoutingAgent `json:"agents"`
}
type DMRoutingAgent struct {
    Name        string `json:"name"`
    Description string `json:"description"`
    URL         string `json:"url"`
}
```

**`GenerateRoutingJSON` logic:**
1. Build `channels` and `members` as today (no change to existing logic)
2. Second pass: collect all team agents with `DMAccess` entries. For each enrolled user ID, find their personal agent URL (from `members`, if any) and all team agent URLs they're enrolled in.
3. Build `dmRouting` entries:
   - User has 1 agent total (single team, no personal): create entry with 1 agent. Router will direct-forward.
   - User has 2+ agents: create entry with all agents. `default` = personal agent URL if exists, else first team agent.
   - User has only a personal agent (no DM enrollments): no `dmRouting` entry — routes via `members` as today.
4. Omit `dmRouting` entirely if empty.

**Tests:** `pkg/common/routing_test.go` — new test cases:
- Personal-only user: no `dmRouting` entry
- Personal + 1 team: `dmRouting` with 2 agents
- Team-only user, single team: `dmRouting` with 1 agent
- Team-only user, multiple teams: `dmRouting` with N agents
- Paused team agent excluded from `dmRouting`
- Empty `DMAccess` on team agent: no `dmRouting` entries

### Phase 2: Team Agent DM Acceptance

**Files:**
- `pkg/runtime/openclaw/config.go` — Post-process Slack channel config for team agents with `DMAccess`

**Logic:** After `ch.OpenClawChannelConfig()` returns (line 34), if agent is team type with `DMAccess`:
```go
if string(params.Agent.Type) == "team" && len(params.Agent.DMAccess) > 0 {
    if slackSection, ok := channelsCfg["slack"].(map[string]any); ok {
        slackSection["dmPolicy"] = "allowlist"
        slackSection["allowFrom"] = params.Agent.DMAccess
        slackSection["dm"] = map[string]any{"enabled": true}
    }
}
```

This overrides `dmPolicy: "disabled"` set by `pkg/channels/slack/slack.go` without changing the `Channel` interface. The override happens in the runtime config generator which has access to the full `AgentConfig`.

**Tests:** `pkg/runtime/openclaw/config_test.go` — verify team agent with DMAccess produces `dmPolicy: "allowlist"` in output JSON. Verify team agent without DMAccess keeps `dmPolicy: "disabled"`.

### Phase 3: Enrollment CLI

**Files:**
- `internal/cmd/channels.go` — New `enroll` and `unenroll` subcommands

**Commands:**
```
conga channels enroll <team-agent> <user-id>
conga channels unenroll <team-agent> <user-id>
```

**Flow (same pattern as `bind`/`unbind`):**
1. Load agent → validate type=team
2. Validate user ID format (`^U[A-Z0-9]{8,12}$` via `slack.ValidateBinding`)
3. Add/remove user ID from `agent.DMAccess` (deduplicate on add)
4. Save agent config
5. Regenerate team agent's `openclaw.json` (picks up `dmPolicy` change)
6. Regenerate `routing.json` (picks up `dmRouting` section)
7. Refresh agent container + restart router

**Also add:**
- MCP tools: `conga_enroll_dm`, `conga_unenroll_dm` in `internal/mcpserver/tools_channels.go`
- JSON schemas in `internal/cmd/json_schema.go`

### Phase 4: Agent Descriptions

**Files:**
- `internal/cmd/admin_provision.go` — Add `--description` flag
- New subcommand or flag on existing `conga agent` command for updating descriptions post-hoc

**Behavior:**
- Description stored in `AgentConfig.Description`
- Default if empty: `"{name} ({type} agent)"` — generated at routing config time, not stored
- Descriptions appear in `routing.json` `dmRouting.agents[].description`
- The classifier prompt quality depends directly on description quality

### Phase 5: Router Classifier Module

**Files:**
- `router/slack/src/classifier.js` (new)

**Classifier:**
- `createClassifier(apiKey, botToken)` — returns `{ classify, getCachedThread, cacheThread }` or `null` if no API key
- `classify(messageText, dmRoutingEntry)` — calls Anthropic Messages API with Haiku:
  - System prompt: list agents with descriptions, instruct to return JSON `{"agent": "name", "confident": true/false}`
  - User message: the DM text
  - 3-second timeout via `AbortController`
  - Validate returned agent name matches a known agent
  - Return `{ agent: DMRoutingAgent, confident: boolean }` or `null` on failure

**Clarification flow (low confidence):**
- When `confident: false`, router sends an ephemeral Slack message to the user via `chat.postEphemeral` (requires bot token + `chat:write` scope — already in recommended scopes)
- Ephemeral message includes Block Kit buttons, one per agent: "Which assistant can help? [Personal] [Project1] [Project2]"
- Router listens for `block_actions` interactive events matching a known action ID prefix
- On button click: forward the original message to the chosen agent, cache the thread
- Pending messages held in memory with 60-second TTL (if user ignores, forward to default)

**Thread cache:**
- `Map<thread_ts, { agentUrl, expiry }>`
- 4-hour TTL, max 2000 entries, lazy eviction
- On DM thread reply: check cache before classifying

**No new npm dependencies** — uses native `fetch` for Anthropic API and existing `@slack/web-api` (already in `package.json`, currently unused) for ephemeral messages.

### Phase 6: Router Integration

**Files:**
- `router/slack/src/index.js` — Modified `resolveTarget` and event handler

**Modified `resolveTarget`:**
```javascript
function resolveTarget(payload) {
  const channel = extractChannel(payload);

  if (channel && channel.startsWith('D')) {
    const userId = extractUser(payload);
    const dmRoute = config.dmRouting?.[userId];

    if (dmRoute) {
      // Single agent: direct forward, no classification
      if (dmRoute.agents.length === 1) {
        return { target: dmRoute.agents[0].url, reason: `dm-direct:${userId}` };
      }
      // Multi-agent: needs classification (async)
      return { target: null, dmRoute, userId, reason: `dm-classify:${userId}` };
    }

    // Fallback: single personal agent (existing behavior)
    if (userId && config.members[userId]) {
      return { target: config.members[userId], reason: `dm:${userId}` };
    }
  }

  // ... rest unchanged ...
}
```

**Modified event handler** — async classification path:
1. If `route.target` is set, forward immediately (unchanged fast path)
2. If `route.dmRoute` exists (needs classification):
   a. Check thread cache first (thread reply → cached agent)
   b. Call `classifier.classify(text, dmRoute)`
   c. If confident → forward, cache thread
   d. If not confident → send ephemeral picker, hold message in pending map
   e. On failure → forward to `dmRoute.default`, cache thread

**Interactive handler** — button clicks from clarification:
- Match action ID prefix `dm-route-pick:`
- Look up pending message
- Forward to chosen agent
- Cache thread, clear pending entry

### Phase 7: Router Secret

**Files:**
- `pkg/channels/slack/slack.go` — Add to `SharedSecrets()`:
  ```go
  {Name: "anthropic-api-key", EnvVar: "ANTHROPIC_API_KEY",
   Prompt: "Anthropic API key for DM routing classifier (optional, sk-ant-...)",
   Required: false, RouterOnly: true}
  ```
- `pkg/channels/slack/slack.go` — Add to `RouterEnvVars()`:
  ```go
  if v := sv["anthropic-api-key"]; v != "" {
      vars["ANTHROPIC_API_KEY"] = v
  }
  ```
- Also pass `SLACK_BOT_TOKEN` to router env (needed for ephemeral messages in clarification flow):
  ```go
  if v := sv["slack-bot-token"]; v != "" {
      vars["SLACK_BOT_TOKEN"] = v
  }
  ```

**Router initialization:**
```javascript
const classifier = process.env.ANTHROPIC_API_KEY
  ? createClassifier(process.env.ANTHROPIC_API_KEY, process.env.SLACK_BOT_TOKEN)
  : null;
```

When `classifier` is `null`: `dmRouting` entries with 1 agent still direct-forward. Multi-agent entries fall back to `dmRoute.default`. No classification, no ephemeral messages.

### Phase 8: Tests

**Unit tests:**
- `pkg/common/routing_test.go` — All `dmRouting` generation scenarios (Phase 1)
- `pkg/runtime/openclaw/config_test.go` — Conditional `dmPolicy` (Phase 2)
- `pkg/channels/slack/slack_test.go` — No changes needed (slack.go unchanged)
- `internal/cmd/channels_test.go` — Enroll/unenroll validation

**Integration tests:**
- Provision personal + team agent, enroll user, verify `routing.json` has `dmRouting`
- Verify team agent's `openclaw.json` has `dmPolicy: "allowlist"` with enrolled user

**E2E verification:**
- DM the Slack app → verify correct agent responds
- Reply in thread → verify same agent responds
- Ambiguous message → verify clarification ephemeral appears
- Remove API key → verify fallback to default agent
- Team-only user → verify DM reaches team agent

## Persona Review Checklist

### Architect
- [ ] `RoutingConfig` extension is additive and backward compatible
- [ ] `Channel` interface is NOT changed — DM policy override is in runtime config generator
- [ ] No circular import introduced (`provider` ← `channels` boundary preserved)
- [ ] `GenerateRoutingJSON` handles all user/agent combinations without panic
- [ ] Router async path doesn't block the Slack ack (ack happens before classification)
- [ ] Thread cache doesn't leak memory (TTL + max size + lazy eviction)

### QA
- [ ] Classifier timeout (3s) prevents hung requests
- [ ] Ephemeral clarification has 60s TTL — pending messages don't leak
- [ ] Thread cache eviction works correctly at boundary (2000 entries)
- [ ] Duplicate events handled: dedup fires before classification (existing dedup is sufficient)
- [ ] Enrollment validation rejects invalid user IDs
- [ ] Unenroll from all team agents removes user from `dmRouting` entirely

### PM
- [ ] Zero-syntax UX for end users — no prefixes, no commands
- [ ] Clarification flow is non-blocking — user can ignore and default fires after 60s
- [ ] Admin enrollment is explicit and auditable
- [ ] Feature is fully opt-in (no API key = no change)
- [ ] Success metrics: classify accuracy (log chosen vs. clarification rate)

## Terraform Provider Impact

Changes to `pkg/provider/provider.go` (`Description`, `DMAccess` fields) and `pkg/common/routing.go` (new types) require a Terraform provider release. These are additive optional fields — backward compatible. Follow release flow in CLAUDE.md.

## Implementation Order

| Phase | Depends on | Risk | Effort |
|-------|-----------|------|--------|
| 1. Go data model | — | Low | S |
| 2. DM acceptance | Phase 1 | Medium | S |
| 3. Enrollment CLI | Phase 1 | Low | M |
| 4. Agent descriptions | Phase 1 | Low | S |
| 5. Router classifier | — | Medium | M |
| 6. Router integration | Phase 5 | Medium | L |
| 7. Router secret | Phase 3 | Low | S |
| 8. Tests | All | — | M |

Phases 1-4 (Go) and Phase 5 (router classifier module) can be developed in parallel.
Phase 6 (router integration) depends on Phase 5.
Phase 7 (router secret) depends on Phase 3 (enrollment changes how secrets flow).
