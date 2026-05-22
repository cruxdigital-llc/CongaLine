# Protocol Notes — Telegram in OpenClaw v2026.5.18

Surfaced during `/glados:spec-feature` Phase 1 (protocol confirmation).
All references are to `ghcr.io/openclaw/openclaw:2026.5.18`.

## Plugin shape

- **Bundled, NOT externalized.** `/app/extensions/telegram/openclaw.plugin.json`
  exists; `openclaw plugins list` shows `telegram (stock) … disabled`.
  Activated when `channels.telegram` is set in config and
  `plugins.entries.telegram.enabled: true`.
- **No "receive forwarded events" mode.** Schema (`/app/dist/bundled-channel-config-schema-*.js`)
  has `accounts: record(...)` for multi-account-per-gateway, but no
  pattern where one gateway acts as a webhook receiver for events
  forwarded by an external router. The plugin always either
  long-polls or registers a webhook with `api.telegram.org`.

## Webhook mode wiring

- `channels.telegram.webhookUrl` — **public HTTPS URL** registered with
  Telegram during `setWebhook`. Telegram POSTs to this URL.
- `channels.telegram.webhookSecret` — required when `webhookUrl` is set.
  Telegram returns it in `X-Telegram-Bot-Api-Secret-Token`; the plugin
  verifies on inbound POSTs.
- `channels.telegram.webhookPath` — local route, **default `/telegram-webhook`**.
- `channels.telegram.webhookHost` — local bind, **default `127.0.0.1`**.
- `channels.telegram.webhookPort` — local bind, **default 8787**.

The webhook listener is **a separate HTTP server inside the gateway
process**, distinct from the main 18789 API listener. Operator fronts
that local listener with a public reverse proxy / Tailscale Funnel /
similar, and sets `webhookUrl` to the public URL.

## Failover

`If setWebhook fails with a recoverable network error during polling
startup, OpenClaw continues into long polling instead of making another
pre-poll control-plane call. A still-active webhook surfaces as a
getUpdates conflict; OpenClaw then rebuilds the Telegram transport and
retries webhook cleanup.` (from `/app/dist/monitor-polling.runtime-*.js`)

Translation: the plugin cannot be talked into "don't talk to
api.telegram.org at all". It will always attempt long-poll or webhook
registration. There is no third-party-injection mode.

## Token

`channels.telegram.botToken` — required (or `tokenFile`, or
`TELEGRAM_BOT_TOKEN` env for the default account only).

## What this means for our three topologies

### Option A — Slack-style router fanout (what plan.md recommended)

**Not feasible.** The OpenClaw telegram plugin has no mode where a
channel sits idle and waits for an external router to inject events.
Even if we wire the router to POST to `http://conga-<name>:8787/telegram-webhook`,
the agent's own plugin would simultaneously try to long-poll Telegram
(or register its own webhook), creating a `getUpdates` conflict against
the router's connection.

Workarounds considered and rejected:
- Set `webhookHost: "0.0.0.0"` so the router can reach the webhook
  listener: doesn't solve the conflict — the plugin still calls
  `setWebhook` against Telegram with whatever `webhookUrl` we set, and
  Telegram only allows ONE bot connection at a time (whichever called
  `setWebhook` or `getUpdates` last wins).
- Set `webhookUrl: ""` so it stays in long-poll mode: now the agent
  long-polls directly. The router's connection conflicts with it.
- Disable telegram plugin entirely on the agent: agent has no telegram
  config, can't process events, no point in routing to it.

The Slack equivalent (`channels.slack.mode: "http"`) puts the channel
in pure receiver mode — no outbound connection to Slack. **Telegram has
no such mode.** This is a fundamental design difference between the
two upstream platforms' OpenClaw integrations.

### Option B — Per-agent direct

**Technically works.** Each agent has its own bot token, configured via
the per-agent secret store (not the shared one we use for Slack). Each
agent either long-polls or registers its own webhook URL with Telegram.

Operator UX cost:
- N bots in BotFather (one per agent that needs telegram)
- N per-agent secrets (each agent's `telegram-bot-token` secret)
- For webhook mode: N public HTTPS endpoints (or N Tailscale Funnel
  routes, or N reverse-proxy entries)

This is materially heavier than Slack's "one bot, many channels via
router". Onboarding a new telegram-bound agent goes from "operator
binds an existing Slack channel" to "operator creates a new bot,
provisions a token, sets up ingress."

### Option C — Hermes-only (the de-facto current state)

**The existing router is already Hermes-shaped.** It forwards Telegram
events to `/v1/chat/completions` on port 8642 (Hermes API). Make this
explicit by:

1. Gating `conga channels add telegram` to only work for clusters with
   a Hermes runtime.
2. Documenting OpenClaw + Telegram as **unsupported** until / unless
   Option B is built out.
3. Annotating `pkg/channels/telegram/telegram.go` to make this clear
   (we already added this in `e7aa46e`; could go further by failing
   loudly when the runtime is OpenClaw).

This matches reality and saves the work of building Option B for a
use case no operator has asked for.

## Recommendation

**Option C now, with Option B specced and parked.**

Rationale:
- Production fleet doesn't use Telegram + OpenClaw.
- Option B is genuine work (channel config rewrite + N-secrets-per-agent
  + per-agent webhook UX); it's premature without an operator who needs
  it.
- Option A (which `plan.md` originally recommended) is **not feasible**
  given the v2026.5.18 plugin design — keeping that recommendation in
  the plan would mislead the next operator.
- Option C is mostly **documentation + a small CLI gate**: low effort,
  high clarity.

The spec phase output should make these trade-offs explicit and let the
operator pick. If they pick Option B, the spec describes the work; if
Option C, the spec describes the gate + docs.
