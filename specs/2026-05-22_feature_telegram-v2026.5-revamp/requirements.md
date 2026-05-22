# Requirements: Telegram v2026.5 Revamp

## Goal

Wire up Telegram as a fully-supported messaging channel for **OpenClaw**
agents running on `ghcr.io/openclaw/openclaw:v2026.5.18+`. After this
feature lands, an operator should be able to:

1. `conga channels add telegram` — register the Telegram bot token.
2. `conga admin add-user <name> --channel telegram:<user-id>` — provision
   a user agent bound to a Telegram user.
3. `conga admin add-team <name> --channel telegram:<group-chat-id>` —
   provision a team agent bound to a Telegram group.
4. DM the bot / mention the bot in a group → agent receives the message
   end-to-end, responds via Telegram.

This is the parity-with-Slack outcome: the same lifecycle, the same
channel-abstraction interface, just on a different upstream platform.

## Motivation

Three things converged to make this worth doing:

1. **v2026.5.18 made it broken-by-default.** The new image's strict schema
   rejects our current Telegram channel config (`mode: "http"` is not a
   valid telegram key, `channels` should be `groups`, `allow: true` was
   migrated to `requireMention`/`allowFrom`). Even if an operator tried to
   enable Telegram today, it would fail at gateway startup.
2. **The router is for Hermes, not OpenClaw.** The codebase already has a
   `router/telegram/src/index.js` but it routes to Hermes' OpenAI-compatible
   endpoint, not to OpenClaw's webhook receiver. The OpenClaw-on-Telegram
   delivery path doesn't exist.
3. **Hidden assumptions about OpenClaw + Telegram.** Documentation and
   onboarding flow suggest Telegram works for any runtime; we should either
   make that real or be explicit that it's Hermes-only and gate the
   `conga channels add telegram` command accordingly.

This spec picks (1) over (3) — make it real.

## In Scope

1. **Topology decision** — pick one of:
   - **Router-fanout** (Slack-style): one shared router process holds the
     bot, fans out events to per-agent webhook receivers via signed HTTP
     POST. Matches the existing `pkg/channels/slack` pattern.
   - **Per-agent direct** (closer to upstream OpenClaw idiom): each agent
     gets its own bot token via `channels.telegram.accounts.<id>` and
     either long-polls Telegram directly or exposes its own webhook URL.
2. **`pkg/channels/telegram/telegram.go` rewrite** to emit the v2026.5.18
   canonical shape per the chosen topology.
3. **Router rewrite or removal** depending on topology. If router-fanout:
   port `router/telegram/src/index.js` from "POST to /v1/chat/completions
   on Hermes" to "signed POST to /telegram-webhook (or whatever OpenClaw
   v2026.5.18 expects) on each agent's gateway". If per-agent-direct: delete
   the router and remove the `conga-router` connection from telegram
   agents' Docker networks.
4. **AWS bash heredocs + add-team/add-user templates** lifted into the same
   migration: `setup_user_agent` / `setup_team_agent` heredocs in
   `terraform/modules/infrastructure/user-data.sh.tftpl`, and the
   `scripts/add-team.sh.tmpl` / `scripts/add-user.sh.tmpl` paths. These all
   currently bake in the Slack-shaped channel config.
5. **Plugin enablement**: OpenClaw v2026.5.18 ships telegram bundled but
   `disabled` by default. Generator must emit
   `plugins.entries.telegram.enabled: true` (already does) AND verify
   nothing else is gating activation.
6. **Tests**: extend `scripts_test.go` with telegram-shape assertions
   (positive: `groups` key, `requireMention`, etc.; negative: `channels`,
   `allow: true`, `mode: "http"`). Add `slack_test.go`-equivalent coverage
   for `pkg/channels/telegram/telegram_test.go` (today it likely doesn't
   cover the v2026.5.18 shape yet).
7. **Documentation**: update `agents/_example/agent.yaml.example` if any
   per-agent telegram config moves to the overlay layer. Update the channel
   abstraction docs in `product-knowledge/standards/architecture.md`.

## Out of Scope

- Multi-account Telegram (multiple bots per gateway). v2026.5.18 supports
  `channels.telegram.accounts.<id>` but matching that surface to our
  one-bot-per-channel-platform abstraction is a separate decision.
- Telegram-specific features beyond DM + group mention: forum topics,
  reactions, voice notes, media groups, inline keyboards. These are
  follow-on once the base channel works.
- Bot pairing UX (`openclaw pairing approve telegram <code>`) integration.
  Telegram's default `dmPolicy: "pairing"` adds an approval step that
  doesn't map cleanly to our `--channel telegram:<id>` provisioning flow;
  this spec defaults agents to `dmPolicy: "allowlist"` with the binding ID
  pre-approved.

## Success Criteria

**Functional** — verified end-to-end against a real Telegram bot on at
least one provider (AWS preferred since that's production-shaped):

1. **DM works (user agent)**. Operator provisions a user agent with a
   Telegram binding. DMs the bot. Agent replies in the same DM thread.
2. **Group mention works (team agent)**. Operator provisions a team agent
   with a group chat binding. Bot is added to the group. User mentions the
   bot with `@<bot> ping`. Agent replies in-group.
3. **Multi-agent isolation**. Two telegram-bound agents on the same
   gateway don't cross-contaminate (user A's DM only reaches agent A's
   container).
4. **Gateway boots cleanly**. No `must NOT have additional properties` or
   similar config-rejection errors on startup; `openclaw status` reports
   `Telegram | ON | OK | accounts 1` (or whatever the v2026.5.18 healthy
   status indicator is).

**Structural**:
5. **All three providers smoked**. Local, remote, AWS — interface parity
   is a Must standard. AWS bash heredocs + Go generator + Go provider
   refresh paths all emit equivalent config.
6. **Channel abstraction holds**. No telegram-specific logic outside
   `pkg/channels/telegram/` or `router/telegram/` (per
   `product-knowledge/standards/architecture.md` Channel Abstraction
   section).
7. **Tests catch regressions**. `scripts_test.go` assertions guard against
   any of the bash templates drifting back to pre-v2026.5.18 shape; a
   `telegram_test.go` covers the Go generator's emission.

## Constraints (carried in)

- **No breaking changes to Hermes + Telegram**. The existing Hermes-shaped
  router serves at least one downstream user (or did when it was written).
  If we replace the router with an OpenClaw-shaped one, Hermes + Telegram
  either becomes unsupported (documented) or is wired in parallel via a
  runtime-routed forwarder.
- **Strict-keyed YAML for any new overlay fields**. If telegram surfaces
  new per-agent operator-authored config, it goes in `agents/<name>/agent.yaml`
  under a versioned schema bump, per `product-knowledge/standards/config-taxonomy.md`.
- **Long polling vs webhook trade-off**. v2026.5.18 defaults to long
  polling; webhook mode is opt-in. Long polling is simpler but couples each
  gateway to outbound reachability of `api.telegram.org`. Webhook mode
  needs ingress on a public URL (Tailscale Funnel, reverse proxy, etc.).
  The spec phase will pick one as the primary supported topology and
  document the other as "advanced".

## Open Questions for Spec Phase

1. **Router-fanout vs per-agent direct** — which topology? Slack-style
   router is what the existing codebase already does for Slack; replicating
   it for Telegram is the smallest delta. Per-agent direct is closer to how
   v2026.5.18 docs describe single-account setups.
2. **What's the OpenClaw v2026.5.18 webhook receiver path on the agent
   container?** The Slack equivalent is `/slack/events` (configurable via
   `channels.slack.webhookPath`). The Telegram docs reference
   `webhookPath` defaulting to `/telegram-webhook`. Confirm the OpenClaw
   gateway actually serves this route when in `mode: "http"` equivalent —
   or whether `webhookUrl` set on the gateway-side means "Telegram POSTs
   here" (operator-supplied public URL).
3. **Does the existing Hermes + Telegram flow have any production
   consumers?** If yes, the router rewrite needs to preserve Hermes
   delivery as a parallel path. If no, simpler to replace.
4. **Are the existing telegram `userIDPattern` / `chatIDPattern` regexes
   still accurate?** v2026.5.18 references supergroup IDs starting with
   `-100…` which are matched, but newer Telegram features (channels with
   topics, forum chats) use `chatId:topic:topicId` form per the docs. Not
   in scope for the MVP, but worth noting.
