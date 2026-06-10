# Research — The `openclaw.json` Config Surface & Conga's Footprint

> Purpose: stop reasoning from the single MCP example. Map the *entire* `openclaw.json`
> config surface, mark exactly what Conga owns today, and identify what administrators
> will legitimately want to modify outside our purview. This grounds the owned-key-set
> decision in `plan.md`.

## Sources & version caveat

- **Upstream schema**: OpenClaw `docs/gateway/configuration-reference.md` +
  `configuration-examples.md` (via Context7 `/openclaw/openclaw`, High reputation, and a
  raw-doc fetch of `main`). Indexed versions ran to ~v2026.4.x; our deployed pin is
  **`ghcr.io/openclaw/openclaw:2026.5.26`**. Treat the *structure* as authoritative; verify
  exact key spellings against the pinned image before implementation.
- **Conga footprint**: exhaustive code inventory of `pkg/runtime/openclaw/config.go`,
  `openclaw-defaults.json`, channel `OpenClaw*Config()` impls, and the overlay appliers
  (captured in `requirements.md` §Current State and expanded below).

## 1. Full top-level section catalog (authoritative)

OpenClaw's config has **~26 top-level sections**. Conga writes into a small minority.

| Section | Controls | Conga footprint |
|---|---|---|
| `gateway` | HTTP/WS server, auth, TLS, bind, control UI, reload mode | **Owned** — bind/port/mode, `controlUi.allowedOrigins`, `auth.{mode,token}` |
| `agents` | Agent defaults, multi-agent `list[]`, session/delivery, **sandbox**, `memorySearch` | **Partial** — only `defaults.{model,models,subagents}` + ships static defaults (`workspace,heartbeat,contextPruning,compaction`). `list[]`, `sandbox`, `memorySearch`, `userTimezone`, `imageModel`, per-agent thinking/reasoning = **untouched** |
| `channels` | Per-channel (slack/discord/telegram/whatsapp/matrix/imessage) | **Partial** — writes `slack.*` from bindings; many slack sub-keys (`slashCommand`, per-channel `requireMention`, streaming knobs) are set to fixed values or untouched |
| `models` | Provider defs, allowlists, pricing, custom providers, `mode:"merge"` | **Overlay-only** — written *only* when an `agent.yaml` model/subagents overlay exists |
| `mcp` | **MCP servers** (`mcp.servers.<name>`), session TTL | **Untouched** — no path today (the motivating gap) |
| `tools` | `allow`/`deny`, `exec`, `elevated`, `media` (audio/video transcription) | **Partial** — ships `profile:"coding"`; team agents append `alsoAllow:["message"]`. `allow/deny/elevated/media` = untouched |
| `skills` | Bundled/installed skill allowlists, load paths, install prefs | **Partial** — ships `install.nodeManager:"pnpm"`; allowlists untouched |
| `plugins` | Plugin discovery/enablement, hooks trust flags, subagent overrides | **Partial** — writes `entries.<channel>.enabled`; trust flags untouched |
| `browser` | Automation profiles, **SSRF policy**, CDP | **Untouched** |
| `hooks` | Webhook ingress (Gmail etc.), transforms | **Untouched** (distinct from `agents.defaults.hooks.internal` we ship) |
| `cron` | Scheduled jobs, retry, failure alerts | **Untouched** |
| `ui` | Control-UI branding (name, avatar, accent) | **Untouched** |
| `env` | Env vars, `.env` import, shell env | **Untouched** in JSON (we use a separate `.env` file) |
| `auth` | OAuth profiles, credential storage, cooldowns | **Untouched** |
| `secrets` | Secret providers (env/file/exec) | **Untouched** |
| `logging` | Levels, files, redaction | **Untouched** |
| `diagnostics` | Instrumentation, OpenTelemetry | **Untouched** |
| `update` | Release channel, auto-update | **Owned (static)** — ships `checkOnStart:false`, `auto.enabled:false` |
| `acp` | Claude agent spawning/runtime backend | **Untouched** |
| `commitments` | Inferred follow-up memory | **Untouched** |
| `discovery` | mDNS/DNS-SD | **Untouched** |
| `cli` | Banner tagline | **Untouched** |
| `wizard` | Setup-flow metadata | **Untouched** |
| `talk` | Real-time voice mode | **Untouched** |
| `messages` | Delivery, TTS, markdown, `groupChat` | **Partial (team)** — `groupChat.visibleReplies:"message_tool"` for team agents only |
| `session` | Lifecycle, reset, maintenance, sendPolicy | **Partial (static)** — ships `dmScope`; reset/maintenance/sendPolicy untouched |
| `permissions` | RBAC (largely expressed via `gateway.auth`/`gateway.nodes`) | **Untouched** |

## 2. Conga's owned footprint (precise)

Confirmed JSON paths Conga writes/mutates (file:line in `pkg/runtime/openclaw/config.go`):

- `gateway.port`, `gateway.mode`, `gateway.bind`, `gateway.controlUi.allowedOrigins`,
  `gateway.auth.{mode,token}` (token only when present) — `buildGatewayConfig()`.
- `channels.slack.*` (mode/enabled/botToken/signingSecret/webhookPath/userTokenReadOnly/
  streaming/{group,dm}Policy/allowFrom/channels[id]) — from bindings, slack `OpenClawChannelConfig()`.
- `plugins.entries.<channel>.enabled` — from bindings.
- `agents.defaults.model{,s}`, `agents.defaults.subagents.*`, `models.providers.*` — overlay
  appliers, **only when `agent.yaml` overlay present**.
- `messages.groupChat.visibleReplies`, `tools.alsoAllow` — **team agents only**.
- **Static defaults shipped once** (never re-mutated): `agents.defaults.{workspace,heartbeat,
  contextPruning,compaction}`, `tools.profile`, `commands.*`, `session.dmScope`,
  `hooks.internal.*`, `skills.install.*`, `update.*`.
- **Not in JSON at all**: secrets/tokens for the model providers and Slack bot flow through the
  `.env` file (`GenerateEnvFile`), not the config — deliberate, per Issue #9627.

## 3. What administrators will want to modify (outside our purview)

Concrete, realistic per-agent/per-deployment customizations Conga does **not** model:

1. **`mcp.servers.<name>`** — the motivating case (Linear, GitHub, fetch, custom HTTP MCPs).
   Note the path is `mcp.servers`, **not** `mcpServers`.
2. **`skills`** allowlists / install paths — enabling specific skills per agent.
3. **`tools.allow` / `tools.deny` / `tools.elevated`** — loosening or tightening the tool
   surface beyond the `coding` profile; `tools.media` for audio/video.
4. **`agents.defaults.sandbox`** — sandboxing tool execution (docker/ssh backends).
5. **`agents.defaults.memorySearch`** — embeddings provider + extra memory paths.
6. **`models.providers.*`** beyond our overlay — extra custom providers, aliases, pricing,
   `imageModel`.
7. **`channels.slack` sub-keys** we hardcode — `slashCommand`, per-channel `requireMention`,
   `groupChat.mentionPatterns`, streaming tuning.
8. **`messages`** tuning — `queue`, `ackReaction`, `responsePrefix`, markdown/TTS.
9. **`session`** policy — `reset`, `maintenance`, `sendPolicy`.
10. **`hooks`** (webhook ingress), **`cron`** (scheduled jobs), **`ui`** branding,
    **`browser.ssrfPolicy`**, **`logging`**, **`talk`** (voice).

This is the heart of the user's concern: the customizable surface is **far** larger than MCP.
Conga touches ~6 sections; ~20 are fully admin territory, plus many sub-keys within the
sections we partially touch.

## 4. Upstream mechanisms that reshape the design

These were not visible from our codebase and change the approach trade-offs:

### 4a. Native `$include` layering — **viable, fail-closed**
> "Single file replaces containing object; array of files deep-merged in order. Paths resolved
> relative to the including file, must stay inside the top-level config directory. **Root
> includes and arrays are read-only for OpenClaw-owned writes (fail closed rather than flatten).**"

This means **Approach C (layered files) is upstream-supported**, not speculative — contradicting
the original `plan.md` dismissal. Conga could own a managed base and reference admin files via
`$include`, and OpenClaw deep-merges them. The "read-only for OpenClaw-owned writes" rule
interacts with hot-reload `.tmp` writes — must be validated against the pinned image.

### 4b. Tiered hot-reload
> `gateway.reload.mode`: `off|restart|hot|hybrid` (default hybrid). "Changes under `mcp.*`
> hot-apply by disposing cached session MCP runtimes. Most plugin/channel changes require
> gateway restart."

Implication: admin-added MCP servers can hot-apply without a full restart — *if* their edit
survives. Strengthens the case for not clobbering on refresh.

### 4c. Config is **JSON5**, not strict JSON
Comments + trailing commas allowed. **Our generator emits strict JSON.** A read-merge-write
(Approach B) would have to parse admin-authored JSON5 (comments, trailing commas) and would
**strip comments** on rewrite — a real downside. Approach C (separate files) sidesteps this:
Conga never parses the admin file.

### 4d. `${VAR}` substitution exists upstream — but we forbid it
Upstream supports `${VAR}` (uppercase) in config strings. Our CLAUDE.md **prohibits** it in
`openclaw.json` (Issue #9627 writes secret values to disk). Admin docs we publish must steer
operators toward the `.env`/secrets mechanisms, not inline `${VAR}`.

## 5. Implications for the owned-key set

- The owned-key set is **small and stable**: `gateway.*`, `channels.<bound>.*`,
  `plugins.entries.*`, `agents.defaults.{model,models,subagents}`, and the team-discipline keys.
  Everything else is admin territory by default.
- **Re-evaluate Approach C vs B.** `$include` makes a clean separation possible: Conga owns a
  managed file (full authority, freely regenerated), admin owns an included overlay (never
  touched). This avoids the JSON5-merge and comment-stripping problems of Approach B entirely.
  The cost is depending on `$include` semantics holding under hot-reload + the read-only-root rule.
- **Integrity monitor** can hash only the *Conga-managed file* (Approach C) rather than the
  whole config — a cleaner security boundary than re-scoping a whole-file hash (Approach B).

## 5b. Empirical validation on `aaron` (2026-06-09, image `2026.5.26`)

Live-tested `$include` against the pinned image on the production `aaron` agent (isolated copy
first via `OPENCLAW_CONFIG_PATH`, then live with backup + byte-exact restore). Results:

- **`$include` works.** An isolated config with `"$include": ["inc.json"]` **validates**
  (`openclaw config validate` → "Config valid"), and both a top-level key
  (`logging.level` → `debug`) and the real use case (`mcp.servers.congaProbe.url`) **resolve
  from the included file** via `openclaw config get`. Merge is deep.
- **Survives restart + hot-reload.** On the live agent, adding `$include` triggered
  `[reload] config change detected; evaluating reload (logging)` — the gateway saw the merged
  section. After a full `systemctl restart`, `$include` was **still on disk**, `logging.level`
  still resolved to `debug`, config still valid, and startup logs were clean (gateway reached
  `ready`, no errors).
- **OpenClaw fails CLOSED — never flattens.** Every owned-write via `openclaw config set` while a
  root `$include` is present was **refused**, regardless of whether the target key lived in the
  include, the main file, or was new:
  > `Error: Config write would flatten $include-owned config at <root>; edit that include file
  > directly or remove the $include first.`
  The directive is preserved; the include is never inlined.
- **The gateway does not owned-write at startup.** Logs explicitly:
  *"auto-enabled plugins for this runtime **without writing config**."* So the fail-closed rule
  does **not** break normal runtime operation — it only affects interactive `openclaw config
  set`/`configure`/wizard flows run inside the container.
- **Integrity is whole-file today.** Byte-exact restore re-matched `aaron.sha256` (the monitor
  hashes the whole file), confirming any include-based design must re-scope the integrity target.

**Design consequence (sharpens decision #1):** Approach C is viable and clean, but the
fail-closed-on-owned-writes behavior makes **root ownership** a real decision. The natural fit:
**Conga owns the root `openclaw.json`** (regenerated wholesale — Conga never uses OpenClaw's
owned-write path, so it's unaffected by the rule) with `$include` → an admin-owned file; admins
edit the include directly (exactly what OpenClaw's error message instructs). Integrity then hashes
only the Conga-managed root. **Trade-off**: operators lose in-container `openclaw config
set`/`configure` for root keys (must edit the include or use Conga CLI) — acceptable for an
infra-managed deployment, but must be documented.

## 5c. Should Conga use the `openclaw` CLI instead of writing files? (Approach D)

The image ships a capable config CLI (`openclaw config get/set/patch/unset/validate/schema`,
`openclaw channels …`). Worth a real evaluation: **Approach D = Conga drives the CLI** (esp.
`config patch`) rather than templating `openclaw.json`. Tested `config patch` on `aaron` (isolated
copy, 2026-06-09):

- ✅ **Validated recursive merge** — preserved an admin `mcp.servers.linear` block while updating
  only the patched keys.
- ✅ **Validates before writing, fail-safe** — rejected an invalid `session.dmScope` enum with **no
  partial write** (also corrected stale docs: valid values are
  `main|per-peer|per-channel-peer|per-account-channel-peer`).
- ✅ **`null` deletes a path cleanly** — removed `tools.profile` without touching siblings (answers
  the collection-delete open question).
- ✅ **Runs standalone** — no gateway required; reports "Restart the gateway to apply."
- ❌ **Strips JSON5 comments** — a `//` comment was gone after the write. The CLI round-trips
  through a serializer, so it carries the **same comment-loss** problem as a hand-rolled merge (B).

### Verdict: use the CLI for **validation**, not **mutation**

| | C (Conga owns root file + `$include`) | D (Conga drives `config patch`) |
|---|---|---|
| Admin comments/JSON5 | **preserved** (admin file never touched) | **stripped** on every patch |
| Schema correctness | we maintain our small managed schema | **CLI-validated, version-correct** |
| Provisioning model | deterministic file write (host/SSH/SSM upload, **no container exec**) | must **exec the openclaw binary** per change (container/`docker run`; AWS = SSM+docker round-trip) |
| Collection deletes | we define semantics | **`null` built-in** |
| `openclaw config set` for admins | fails closed (edit include directly) | works (no `$include`) |
| Determinism / diffability | high | lower (stateful mutation) |

For *this feature's goal* (let admins customize and have it survive), **C wins on the thing that
matters most — admin edits and comments are never touched** — and keeps the simpler, container-free
provisioning path. D's comment-stripping actively works against the goal, and in-container execution
adds real cross-provider complexity.

**But the CLI is still valuable — for read-only validation.** Conga should shell out to
`openclaw config validate` / `openclaw config schema` (no write → no fail-closed conflict, no
comment loss) to verify its generated managed file against the **exact image version**, eliminating
the hand-maintained-key-spelling risk (open Q#4) without adopting D's write path. Recommendation:
**Approach C for ownership + CLI for validation.**

## 6. Open questions for `/glados:spec-feature`

1. ~~Does `$include` hold under hot-reload on 2026.5.26?~~ **RESOLVED (§5b)** — yes; merges,
   validates, survives restart + hot-reload, fails closed (never flattens).
2. **Root ownership** — confirm Conga-owns-root + admin-include (the §5b recommendation) vs the
   inverse. Decision now data-informed; needs a final call + the owned-write trade-off documented.
3. ~~JSON5 / Approach B comment-stripping?~~ **Largely moot** if we adopt Approach C (Conga never
   parses the admin file). Keep only as a fallback consideration.
4. ~~Confirm exact 2026.5.26 key spellings for every owned path?~~ **Approach decided (§5c)** —
   adopt `openclaw config validate`/`schema` as a read-only CI/runtime check of Conga's generated
   managed file, so key-spelling correctness is enforced against the exact image, not hand-maintained.
5. ~~`.last-good`/`.bak.N` origin / recovery path?~~ **Partially resolved** — integrity is
   whole-file today (§5b) and must be re-scoped to the Conga-managed root file. Confirm the
   `.bak.N`/`.last-good` rotation owner during spec (still open).
6. **Operator UX** — document that in-container `openclaw config set`/`configure` is blocked for
   root keys once `$include` is present; provide the supported edit path (edit the include / use
   Conga CLI).
