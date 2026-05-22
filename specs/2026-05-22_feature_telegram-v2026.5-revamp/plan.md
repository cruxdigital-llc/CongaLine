# Plan: Telegram v2026.5 Revamp

**Target**: Telegram as a first-class messaging channel for OpenClaw agents
on `ghcr.io/openclaw/openclaw:v2026.5.18+`, parity with the existing Slack
integration.

## Approach

The work splits cleanly into a **decision phase** (Phase 0) and a
**delivery phase** (Phases 1–4). Phase 0 is where the architecture choice
gets made; everything downstream is mechanical once that decision is locked.

The high-risk piece is **not** the channel config schema — that's a
mechanical migration we already understand from the Slack v2026.5.x work
in PR #51. The high-risk piece is choosing between Slack-style router
fanout vs per-agent direct connections, because each has cascading
consequences for secrets, routing, multi-agent isolation, and rollout.

## Phase 0 — Architecture decision (BLOCKING; topology pick)

Owner: Architect.

Two viable topologies. Pick one. Implement one. Document why.

### Option A — Slack-style router fanout

- One shared `conga-router-telegram` process per host. Holds the Telegram
  bot token via `TELEGRAM_BOT_TOKEN` env (already wired through
  `pkg/channels/telegram.RouterEnvVars`).
- Router does long polling against `api.telegram.org` (or sets a webhook
  URL pointed at itself).
- On receiving an update, router signs and POSTs to the bound agent's
  webhook receiver inside the per-agent Docker network.
- Each agent's OpenClaw config sets `channels.telegram.webhookUrl` to
  `http://localhost:18789/telegram-webhook` (or whatever the receiver path
  is — Phase 1 verifies) so the agent gateway accepts the forwarded POST.
- Agent containers do NOT get `TELEGRAM_BOT_TOKEN` (mirror current
  behavior; only router has it).

Pros:
- Reuses the existing channel-abstraction pattern (`router/slack/`
  cognate). Minimal delta to the surrounding codebase.
- One outbound long-poll connection per host vs N per agent. Less load on
  `api.telegram.org`.
- Operator manages one bot token, not N.

Cons:
- v2026.5.18's first-class Telegram is per-account (long polling or its
  own webhook); the "central router fans out to agent webhook receivers"
  pattern is not how OpenClaw expects Telegram to be wired upstream.
- Replacing the existing Hermes-shaped router potentially breaks any
  Hermes + Telegram consumer.
- Need to verify whether OpenClaw's `channels.telegram` actually accepts a
  "I'm just a webhook receiver, don't try to long-poll yourself" mode.
  The Slack equivalent is `channels.slack.mode: "http"` (which Phase 1
  will need to confirm has a Telegram analog).

### Option B — Per-agent direct

- Each agent has its own bot token via `channels.telegram.accounts.<id>`
  (or a single-account `channels.telegram.botToken`).
- Each agent independently long-polls `api.telegram.org` OR sets its own
  `webhookUrl` pointing at a public ingress.
- No shared router; the `conga-router-telegram` container is removed for
  Telegram agents.
- Operator must create N bot tokens in BotFather (one per agent) and
  manage them as per-agent secrets.

Pros:
- Matches v2026.5.18's first-class shape exactly. No translation layer.
- No shared mutable state across agents (one agent's restart doesn't
  affect another's Telegram connection).
- Webhook mode is straightforward — each agent's `webhookUrl` is its own
  public ingress.

Cons:
- N outbound long-poll connections to `api.telegram.org`. Within Telegram's
  per-bot rate limit but multiplies host-egress load.
- Operator UX: N bot tokens, N BotFather setups. Less ergonomic than
  Slack's "one app, many channels" pattern.
- Breaks the channel-abstraction symmetry with Slack: Slack uses a router,
  Telegram doesn't. Two patterns to maintain.

### Decision criteria

- **If operator UX wins**: Option A. One bot token, fewer moving parts.
- **If correctness-with-upstream wins**: Option B. Matches v2026.5.18 idiom.
- **If we want to keep the Hermes path intact**: Option B (the new
  OpenClaw path coexists with the existing Hermes router); or Option A
  with a router rewrite that supports both routing destinations.

Recommendation (pending architect review): **Option A**, because the
channel-abstraction precedent in `pkg/channels/slack` is what the rest of
the codebase is built around, and breaking that symmetry would require
parallel paths in every provider's channels.go / config gen. The router
rewrite from Hermes-shape to OpenClaw-shape is the bulk of the work, but
it's isolated to `router/telegram/`.

**Deliverable**: a single Markdown decision doc inside this spec dir
(`topology-decision.md`) recording the choice with rationale and any
constraints discovered along the way. Phases 1+ proceed only after this
exists and is reviewed.

## Phase 1 — Confirm the v2026.5.18 wire protocol

Owner: Architect.

Before any code lands, verify a few protocol questions empirically against
the pinned image (`ghcr.io/openclaw/openclaw:v2026.5.18`):

1. **What HTTP path does the OpenClaw gateway expose as a Telegram webhook
   receiver?** Slack's is `/slack/events` (and `channels.slack.webhookPath`
   makes it configurable). The Telegram docs mention `webhookPath`
   defaulting to `/telegram-webhook`, but whether that route is served by
   the OpenClaw gateway itself or by some other process is what we need to
   confirm.
2. **What's the signing scheme?** Slack uses HMAC-SHA256 over the request
   body with a timestamp header. Telegram uses a different scheme
   (`X-Telegram-Bot-Api-Secret-Token` header equal to the configured
   secret). Verify the OpenClaw plugin's expectations.
3. **Multi-account vs single-account**: confirm whether the
   `channels.telegram.accounts.<id>` shape is required for our use case or
   if the simpler top-level `botToken` works.

Deliverable: a short `protocol-notes.md` inside the spec dir summarizing
the answers, with file/line references into the OpenClaw v2026.5.18 image
source under `/app/extensions/telegram/`.

## Phase 2 — Code: channel config generator

Owner: Architect.

Rewrite `pkg/channels/telegram/telegram.go::OpenClawChannelConfig` to emit
the v2026.5.18 canonical shape per the Phase 0 topology choice. Specifically:

- Drop `"mode": "http"` (not a valid telegram key).
- Rename `cfg["channels"]` → `cfg["groups"]` for team agents.
- Replace `{"allow": true}` per-group binding with `{"requireMention": true}`
  (or `false`, depending on operator preference — match Slack's
  `requireMention: false` for consistency).
- Add `webhookUrl`/`webhookSecret`/`webhookPath` fields IF topology is
  router-fanout (Option A) and the OpenClaw plugin accepts those for
  receiver-mode operation. Otherwise emit `botToken` + leave webhook
  fields unset.
- Verify `dmPolicy: "allowlist"` + `allowFrom: ["<user-id>"]` still
  matches the v2026.5.18 schema.

Update `pkg/channels/telegram/telegram_test.go` (create if missing) to
mirror the slack test structure with positive + negative assertions.

## Phase 3 — Code: bash templates

Owner: Architect.

The four bash JSON heredocs that bake telegram channel config:

- `terraform/modules/infrastructure/user-data.sh.tftpl` setup_user_agent
- `terraform/modules/infrastructure/user-data.sh.tftpl` setup_team_agent
- `scripts/add-user.sh.tmpl`
- `scripts/add-team.sh.tmpl`

Today these only emit Slack. If we want telegram to work on AWS fresh
deploys, each needs a conditional emission path (or a separate
`channels.telegram` block emitted alongside `channels.slack`).

Phase 4 of the openclaw-upgrade-latest PR (PR #51) already migrated all
four for Slack. This phase applies the same pattern for Telegram.

Extend `scripts/scripts_test.go` `assertOpenClawV5Shape` (or a parallel
helper) with telegram-specific positive + negative assertions.

## Phase 4 — Router (rewrite or remove)

Owner: Architect.

Depends on Phase 0 topology:

- **Option A (router-fanout)**: rewrite `router/telegram/src/index.js` to
  drop the Hermes `/v1/chat/completions` POST path and replace with a
  signed POST to each bound agent's OpenClaw telegram-webhook receiver.
  Reuse the dedup-by-update-id, group-vs-DM routing, and reply-via-bot
  patterns already in the file.
- **Option B (per-agent direct)**: delete `router/telegram/` entirely.
  Update `pkg/channels/telegram/telegram.go::RouterEnvVars` to return nil
  (no shared router). Remove `conga-router-telegram` Docker network
  connection logic from each provider's lifecycle.

Either way, update the Docker network plumbing accordingly: under Option A
the router needs to join each telegram-bound agent's network; under
Option B it doesn't exist.

## Phase 5 — Live validation

Owner: QA.

Five-scenario verification, mirroring the Slack S1–S5 in the openclaw-upgrade
spec:

- **T1 — DM, user agent**. Provision a user agent with `--channel telegram:<id>`.
  DM the bot. Confirm reply.
- **T2 — Group mention, team agent**. Provision a team agent with a group
  chat binding. Mention the bot. Confirm reply in-group.
- **T3 — Multi-agent isolation**. Two telegram agents on the same host.
  User A's DM does not reach agent B.
- **T4 — Gateway clean boot**. `docker logs conga-<agent>` shows no
  `must NOT have additional properties` errors and `openclaw status`
  reports Telegram OK.
- **T5 — Restart resilience**. Restart the gateway / cycle-host. Telegram
  reconnects without duplicate-delivery or message loss (test by sending
  a message while restarting).

## Phase 6 — Documentation

Owner: Architect + QA.

- Update `agents/_example/agent.yaml.example` if any per-agent telegram
  config moves to the overlay layer.
- Update `product-knowledge/standards/architecture.md` Channel Abstraction
  section to reflect any new conventions discovered during this work.
- Add a `## Telegram` section to the main README.

## Risks & Mitigations

| Risk | Likelihood | Mitigation |
|---|---|---|
| Phase 0 picks the wrong topology and we burn rework | Medium | Make Phase 1 (protocol notes) precede Phase 2. The protocol notes will surface most topology constraints. |
| OpenClaw v2026.5.18 telegram plugin has undocumented quirks | Medium | Phase 1 tests against the actual image; Phase 5 catches anything Phase 1 missed. |
| Breaking Hermes + Telegram if the router is rewritten | Low (operator says no production Hermes-on-Telegram users) | Confirm with operator in Phase 0; if there are users, keep parallel routing paths. |
| v2026.5.20+ migrates telegram further before this spec lands | Low | The spec is targeted at v2026.5.18 specifically. If a future bump (e.g., openclaw-upgrade-latest-2) needs migration, that's a separate spec on top of this one. |

## Rollback

Each commit on this spec's branch is bisectable. Channel config changes
(Phase 2) are reverted by `git revert`; bash heredoc changes (Phase 3)
the same. Router rewrite (Phase 4) under Option A is the highest-risk
commit — keep it small and isolated.

If telegram causes problems post-rollout, operator sets
`plugins.entries.telegram.enabled: false` in each agent's config and
restarts. Telegram disables, gateway boots clean.

## Open Questions Carried Into Implementation

(See requirements.md §"Open Questions for Spec Phase" for the full list.
Re-listed here for convenience:)

1. Topology choice — Option A (router fanout) or Option B (per-agent direct)?
2. What's the OpenClaw v2026.5.18 telegram webhook receiver path?
3. Are there production Hermes + Telegram users that constrain the router
   rewrite?
4. Forum-topic / channel-with-topics support — defer or in scope?
