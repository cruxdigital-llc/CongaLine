# Phase 0 — Upstream Changelog Review

**Window**: `v2026.3.12` → `v2026.5.18`, inclusive on both ends.
**Source**: `https://raw.githubusercontent.com/openclaw/openclaw/v2026.5.18/CHANGELOG.md`
**Lines audited**: 7,173 (window starts at original line 5, ends at original line 7177).
**Releases in window**: 35+ (including several `-beta.N` and `-1` sub-releases).

## Gate Decision

**✅ PASSES.**

- **Blocking entries**: 0. No required `openclaw.json` schema change, no required
  env-var rename/removal, no required on-disk layout change inside the data
  directory that affects our deployment.
- **Adjacent entries**: ~15 worth surfacing for the Phase 3 verification
  scenarios. Catalogued below by scenario.
- **Single explicit `### Breaking` section in the window** (v2026.5.14): the
  BlueBubbles channel surface for iMessage was removed. We don't use the
  iMessage channel — Slack and gateway-only mode are the only channels we
  exercise. **Irrelevant.**

Phase 1 may proceed.

## How "Blocking" was checked

For each of the four blocking rubric items in `spec.md` §"Phase 0":

### 1. Required `openclaw.json` schema change?

**No.** Every schema-touching entry in the window is either:
- **Additive** — new optional fields (e.g. `channels.slack.unfurlLinks`,
  `channels.slack.replyBroadcast`, `agents.defaults.models["provider/*"].agentRuntime`,
  `serviceTier` for Bedrock). Our rendered config doesn't set any of these and is
  unaffected.
- **`doctor --fix` migrations** — apply only when an operator runs `openclaw
  doctor --fix` against a stale config. Our config is generated fresh by
  `pkg/runtime/openclaw/config.go` on every refresh/cycle, so it's never
  "legacy" from OpenClaw's perspective. Specific migrations seen
  (Codex compaction provider, Brave web-search config, Telegram streaming
  progress, BlueBubbles → iMessage, `tools.web.search.apiKey` → `plugins.entries.brave.config.webSearch.apiKey`,
  Discord channel-create `type` → `channelType`, etc.) — none touch fields we
  set.
- **Removal of fields we never set** — e.g. `agent-model timeoutMs` keys
  removed via doctor (v2026.5.x — we never wrote those keys); the ambiguous
  legacy `main` agent dir helper removed (we don't reference `main`).

### 2. Required env-var change?

**No.** Many new env vars added (`OPENCLAW_WORKSPACE_DIR`,
`OPENCLAW_IMAGE_APT_PACKAGES`, `OPENCLAW_TRAJECTORY_FLUSH_TIMEOUT_MS`,
`OPENCLAW_ACPX_RUNTIME_STARTUP_PROBE`, `OPENCLAW_HEAVY_CHECK_LOCK_SCOPE`,
`OPENCLAW_INCLUDE_ROOTS`, …) — all opt-in with documented defaults. None
of the env vars we set today (`SLACK_BOT_TOKEN`, `SLACK_SIGNING_SECRET`,
`GOOGLE_CLIENT_ID`, `GOOGLE_CLIENT_SECRET`, per-agent secrets,
`NODE_OPTIONS=--max-old-space-size=1536`) are renamed, removed, or have
their semantics changed.

### 3. Required on-disk layout change inside `/home/node/.openclaw/`?

**No.** One relevant Docker-startup fix (v2026.4.5):

> Docker: pre-create `/home/node/.openclaw` with node ownership and private
> permissions so first-run Docker Compose named volumes no longer fail
> startup with EACCES.

This makes the image robust against EACCES on first start with named
volumes — strictly additive. Our deployment already creates
`/opt/conga/data/<name>/` on the host with uid 1000 ownership and bind-mounts
it onto `/home/node/.openclaw` (see `terraform/modules/infrastructure/user-data.sh.tftpl`
and `pkg/provider/{local,remote,aws}provider/...`). The fix doesn't change
the inside-container path or required permissions.

### 4. Required host-side migration?

**No.** All operator migrations in the window are gated by `openclaw doctor
--fix`, which is operator-initiated, never automatic. We do not invoke
`doctor --fix` in our bootstrap or refresh paths. (We could not, even if we
wanted to: doctor edits `openclaw.json` in place, which is incompatible with
our "regenerate config every refresh" model.)

## Adjacent entries (verification focus areas)

Listed by the Phase 3 scenario each maps to. Format: short summary + line
reference into the upstream changelog.

### S1 / S2 — Slack inbound (user + team agent)

The whole reason we're bumping. Notable Slack changes after the fix (which
itself landed in v2026.3.22):

| Entry | Risk to us | Mapping |
|---|---|---|
| **v2026.3.22 — `Slack/startup: harden @slack/bolt import interop`** | None — this IS the fix for #45311. | Confirms S1/S2 should now succeed. |
| Slack Socket Mode native reconnect kept enabled (#77933, v2026.5.x area) | Low — only improves connection resilience. | Confirm reconnects survive transient losses during S1/S2. |
| Slack assistant thread lifecycle support (#80787, v2026.5.18) | Low — additive. We don't currently use Slack's "assistant view"; no change to plain DM/mention flow. | If S1/S2 work, this entry adds capability we may want later. |
| DM thread replies kept on the main DM session (#82390, v2026.5.18) | **Low–medium** — changes routing of in-thread DM replies to stay on the agent's primary DM session. For user agents (`dmPolicy: allowlist`), this is desirable behavior. | Verify S1 includes an in-thread reply and confirm it lands as expected. |
| `unfurlLinks` default flipped off (v2026.5.14 area, #82123) | None — visual only. | No action. |
| Mention metadata preserved (v2026.5.14 area, #79025) | None — agents can now distinguish direct vs thread-wake mentions. | No action. |

### S3 — Gateway-only mode

| Entry | Risk to us | Mapping |
|---|---|---|
| Control UI allowed-origin migration for `0.0.0.0` (v2026.5.18, #83286) | **Medium-low**. The migration runs through `doctor --fix`, which we don't invoke — but the underlying validation logic now exists. Our config sets `allowedOrigins: ["localhost:18789", "localhost:<hostPort>"]` and `gateway.bind: 0.0.0.0`. The new validator may emit warnings about the `0.0.0.0` bind even though we explicitly need it. | Verify S3 succeeds with no startup error and `conga logs <agent>` is free of `allowedOrigins`-related warnings. |
| Gateway WS protocol v4 restored (v2026.5.18, #82882) | None — internal, our config doesn't pin a protocol. | Implicit in S3 passing. |
| Gateway/auth same-host trusted-proxy password fallback re-allowed (v2026.5.18, #82607) | None — we use token auth via `conga connect`. | No action. |
| Restart drain of pending replies during shutdown (v2026.5.18, #69121) | None — affects clean shutdown, not first-start. | No action. |

### S4 — Per-agent model overlay

| Entry | Risk to us | Mapping |
|---|---|---|
| Doctor warns when per-agent `model` config omits `fallbacks` and global is non-empty (v2026.5.4 area, #79369) | **None** — our overlay loader emits `fallbacks: []` explicitly (Local Model Routing spec finding). The warning will NOT fire for our agents. | Verify the warning is absent in S4 logs as confirmation our overlay shape is current. |
| `/model provider/model` clarified as exact session route (docs only, line 1244 area) | None. | No action. |
| `agents.defaults.models["provider/*"].agentRuntime` accepted (v2026.5.18, #82243) | None — we don't set per-provider runtime policy. | No action. |
| Subagent model `timeoutMs` key removal (v2026.5.18, #83291) | None — we never set this. | No action. |
| Gemini 3 Pro Preview normalization (several entries) | None — we use Anthropic. | No action. |

### S5 — Egress proxy

OpenClaw does not own the egress proxy — it's our Envoy sidecar with iptables
DROP rules in DOCKER-USER (egress feature spec). Nothing in the changelog
window touches our enforcement layer. The only intersection: container env
vars that include `HTTPS_PROXY`/`HTTP_PROXY` are still honored by OpenClaw
network calls (no change). **No action; standard S5 run.**

## Items intentionally not flagged

Things that look adjacent at first glance but aren't, for the record:

- **`OPENCLAW_NIX_MODE`** changes — we don't deploy Nix.
- **macOS Mac app changes** — we deploy Docker on Linux.
- **Codex / OpenAI Codex auth profile migrations** — we use Anthropic via
  configured model; no Codex.
- **WhatsApp / iMessage / Discord / Telegram / Matrix / Mattermost / Tlon
  channel changes** — we use Slack + gateway-only. The router sidecar is
  Slack-only.
- **Plugin install/repair churn** (much of v2026.4.x) — we deploy the stock
  image without installing additional plugins.

## Recommended belt-and-suspenders for Phase 3

Two adjacent entries above are not "blocking" but are worth a direct check
during verification:

1. **S3 — `allowedOrigins` doctor seeding for `0.0.0.0` (#83286)**:
   `docker logs conga-<agent> | grep -iE 'allowed.{0,3}origin|0\.0\.0\.0'`
   should show our two configured origins recognized and no warning that
   would suggest the gateway is auto-amending the list. (We can't enforce
   the runtime decision, but we can detect drift.)

2. **S4 — fallbacks warn (#79369)**:
   `docker logs conga-<agent> | grep -i fallbacks` should be empty (or only
   show our explicit `fallbacks: []` echoed back during config load). If it
   warns about a missing key, that's a config-generator bug to fix, not a
   bump blocker.

## Decision logged

Phase 0 clears. Implementation proceeds to Phase 1 (the 14-file commit).
