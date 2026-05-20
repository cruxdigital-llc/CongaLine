# Trace Log: DM Agent Routing

**Feature**: DM Agent Routing
**Spec Directory**: `specs/2026-04-16_feature_dm-agent-routing/`
**Started**: 2026-04-16
**Status**: Planning

## Active Personas
- **Architect** — System design review, data model impact, pattern consistency
- **QA** — Edge cases, failure modes, test strategy
- **PM** — User value, enrollment UX, scope control

## Active Capabilities
- **MCP Tools**: `conga_*` tools for runtime verification (status, policy, logs)
- **Playwright**: Available for gateway UI testing if needed
- **GitHub**: PR creation and review

## Session Log

### 2026-04-16 — Planning Session
- **Context**: User requested ability for Slack DM messages to route to the correct agent when a user has access to multiple agents (personal + team, or team-only)
- **Key constraint identified**: Not every team member has a personal agent. Some users only have team agent access.
- **Decision**: LLM classifier (Haiku) in the router for transparent routing. Team agent responds directly (no mediation).
- **Decision**: Explicit admin enrollment via CLI (not inferred from Slack channel membership)
- **Artifacts**:
  - [requirements.md](requirements.md)
  - [plan.md](plan.md)
- **Prior design work**: `/Users/aaronstone/.claude/plans/serialized-swimming-narwhal.md` (detailed architectural exploration)
- **Decision**: Low-confidence classifier results trigger ephemeral Slack message asking user to pick agent (60s TTL, then default)
- **Decision**: Team-only users (no personal agent) supported — `dmRouting` with 1 agent = direct forward, 2+ = classify
- **Decision**: Per-user enrollment via CLI for v1; batch/auto-infer from channel membership deferred
- **Files created**:
  - [requirements.md](requirements.md) — 5 user scenarios, 8 success criteria, non-goals
  - [plan.md](plan.md) — 8-phase implementation plan with persona review checklist

### 2026-04-16 — Plan Revision (Spark infrastructure session)
- **Decision reversal**: Replace manual enrollment with automatic channel membership resolution
  - DM access derived from Slack channel membership via `conversations.members` API
  - Router maintains membership maps via `member_joined_channel` / `member_left_channel` events
  - Eliminates `conga channels enroll/unenroll` CLI commands and `DMAccess` field
  - Rationale: manual enrollment drifts from reality, channel membership is the source of truth
- **Decision**: Configurable classifier endpoint — defaults to Anthropic Haiku, supports any OpenAI-compatible endpoint via `CLASSIFIER_URL` env var
  - Enables self-hosted models (e.g. Ollama on DGX Spark) for privacy-sensitive deployments
  - DM content stays on controlled infrastructure when using self-hosted classifier
  - CongaLine users get Haiku by default — no extra infrastructure required
- **Files updated**:
  - [requirements.md](requirements.md) — revised enrollment model, classifier model, success criteria
  - [plan.md](plan.md) — Phase 3 replaced (enrollment CLI → membership resolution), Phase 5/7 updated for configurable endpoint

### 2026-05-19 — Pre-implementation Review Pass
- **Context**: Architect review against current codebase before starting Phase 1
- **Verified accurate**: `RoutingConfig` shape today, team `dmPolicy: "disabled"` location, `RouterEnvVars()` missing `SLACK_BOT_TOKEN`, router is single-file Socket Mode with catch-all listener
- **Issues found**:
  1. Phase 1's `agentDescriptions` map didn't carry the channel→agent linkage the router needs; spec self-contradicted (Phase 4 referenced an undefined `dmRouting` shape)
  2. Socket Mode delivery of `block_actions` and `member_joined_channel`/`member_left_channel` was assumed, not verified — and the Slack `SetupGuide()` documents zero event subscriptions or interactivity
  3. Phase 2's empty-`allowFrom` DM acceptance was a load-bearing assumption against the pinned `2026.3.11` OpenClaw image with no validation
  4. Phase 2 patched `pkg/runtime/openclaw/config.go` post-hoc, putting Slack-specific DM logic in the channel-agnostic runtime layer
  5. No router test runner — Phase 8 specified `.test.js` files but `router/slack/package.json` had no test infrastructure
  6. Phase 5 oversold Anthropic vs OpenAI-compatible as "same prompt, same JSON response parsing" — two distinct code paths
- **Decision: `routing.json` schema — Option C (single `agents` table)**
  - `agents` keyed by name with url/type/description and platform-keyed `channelIds`/`memberIds`
  - `channels`/`members` become name-indexed lookups (id → agent name)
  - Platform-keyed binding slices keep Slack/Telegram routers from cross-reading each other's IDs
  - Wire-format break, but writer (Go) and reader (router) ship from this repo together
- **Decision: Phase 0 validation spike added** — verify OpenClaw empty-allowlist DM acceptance + Socket Mode interactive/member event delivery against the pinned image before committing the dependent phases
- **Decision: Phase 2 relocated** into `pkg/channels/slack/slack.go`'s `case "team"` branch
- **Decision: `SetupGuide()` update is an explicit Phase 7 deliverable** (Event Subscriptions + Interactivity & Shortcuts)
- **Decision: Phase 8 adds `node:test` runner** via `router/slack/package.json` script (zero new npm deps)
- **Files updated**:
  - [plan.md](plan.md) — new Phase 0; Phase 1 schema rewritten; Phase 2 relocated; Phase 5 dual-path made honest; Phase 7 SetupGuide deliverable; Phase 8 test runner; new Operational Notes section; Implementation Order table updated
  - [requirements.md](requirements.md) — constraints tightened (scopes already satisfied; manifest events/interactivity are the real gap); new Operational Limitations section
  - [README.md](README.md) — this entry
