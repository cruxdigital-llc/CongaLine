# Bugfix: conga-router host networking

**Status: VERIFIED (code complete) — DEPLOY PENDING.** The fix is implemented,
regression-tested, and standards-reconciled (see Trace). It is NOT yet on prod:
ship via terraform-provider-conga release → `terraform apply` in a window. Until
that apply lands, prod host `i-024bf3a55563f9e88` carries the unmanaged manual
override — **do not refresh/apply against it** (memory
`project_router_bridge_route_conflict`).

Router cannot attach to per-agent bridge networks on Docker 25.0.16 + kernel
6.1.174 (`docker network connect` → `conflicts with existing route 0.0.0.0/0`),
so Slack events never reach agents (`Forward error … fetch failed`). Architectural
dependency on hot-attaching the router to N isolated `/16` bridge networks.

## Trace

- **2026-06-11 — Identified & reproduced (prod):** root cause isolated to a
  Docker/libnetwork + kernel route-programming conflict, exposed after a reboot
  activated kernel `6.1.170 → 6.1.174`. Reproduced against a fresh throwaway
  network. Full detail in memory `project_router_bridge_route_conflict`.
- **2026-06-11 — Live mitigation (manual, unmanaged):** router → `--network host`;
  `routing.json` → loopback published ports (`http://127.0.0.1:<hostPort>/slack/events`).
  Verified: aaron replied over Slack. NOTE: reverted by any provider refresh/apply.
- **2026-06-11 — Plan-fix (this dir):** `plan.md`. **Chosen strategy:** make
  host-networking + loopback delivery the **standard router topology for all
  providers** (single-host AWS/local/remote), removing the multi-bridge-attach
  dependency entirely. Real fix, not a libnetwork workaround.
- **2026-06-11 — Implemented (`/glados/implement-fix`):** code + bootstrap +
  tests landed (NOT deployed — prod host untouched). Scope decisions:
  loopback is **OpenClaw-only** for now (Hermes' separate, unpublished webhook
  port 8644 is a documented follow-up), applied to **all providers**.
  Changes:
  - `pkg/common/routing.go`: `WebhookTarget` gains `Loopback bool`;
    `GenerateRoutingJSON` emits `http://127.0.0.1:<agent host GatewayPort><path>`
    when a loopback target is selected (default conga-`<name>`:BaseGatewayPort
    preserved for nil/non-loopback resolvers). New shared
    `common.LoopbackWebhookResolver(globalDefaultRuntime)` (runtime-aware path).
  - Providers wired to the loopback resolver: local (`webhookTargetResolver`
    now delegates to the shared helper), AWS (`regenerateRoutingOnInstance`),
    remote (`regenerateRouting`) — previously passed `nil`.
  - Routers switched to `--network host` and **all** per-agent bridge attaches
    removed: AWS `routerRestartScript` + the `BindChannel` connect call; local
    `runRouterContainer` + `ensureRouter`/`ensureTelegramRouter` + the
    `connect/disconnectRoutersToNetwork` helpers and their 6 call sites (helpers
    deleted, plus now-unused `connect/disconnectNetwork`); remote
    `runRouterContainer` + `ensureRouter`/`BindChannel` + the 5 connect/disconnect
    sites and the `connect/disconnectNetwork` methods.
  - `terraform/modules/infrastructure/user-data.sh.tftpl`: `conga-router.service`
    ExecStart → `--network host`; removed its `connect-router-networks.sh`
    `ExecStartPost`, the `conga-router-networks.service` companion unit, the
    `connect-router-networks.sh` script + boot invocation, and the agent units'
    `ExecStartPost=… docker network connect conga-$AGENT conga-router`.
  - Tests: `routing_test.go` (`TestGenerateRoutingJSON_Loopback`,
    `TestLoopbackWebhookResolver`); AWS `channels_test.go`
    `TestRouterRestartScriptUsesSlackPath` extended to assert `--network host`
    and the absence of any `docker network connect`.
  - Docs: CLAUDE.md Slack-architecture + bootstrap-conventions notes updated.
  **Verification:** `go build ./...`, `go vet ./...`, `go test ./...` all green.
  Bridge form still covered by the existing nil-resolver tests; loopback form
  covered by the two new tests.
- **2026-06-11 — Verified (`/glados/verify-fix`):** regression + retrospection.
  - **Regression:** `go build`/`go vet`/`go test ./...` green (22 pkgs); gofmt
    clean on all touched files. No golangci-lint configured.
  - **Side-effect sweep:** no dangling refs to removed helpers
    (`connect/disconnectRoutersToNetwork`, `connect/disconnectNetwork`,
    `connect-router-networks.sh`, `conga-router-networks.service`). Confirmed
    `router/slack/src/index.js` forwards the routing.json URL verbatim
    (`fetch(target,…)`), so loopback URLs flow through with no router-source
    change. Agent containers stay on their per-agent bridge + published
    `127.0.0.1:<hostPort>` (unchanged) — egress/iptables paths untouched.
  - **Architect review:** core contract additive (nil/non-loopback resolvers
    unchanged). Loopback URL uses `a.GatewayPort`; every channel-bound agent is
    provisioned with a port, so the prior fixed-`BaseGatewayPort` invariant holds.
  - **Spec retrospection:** divergences from `plan.md` recorded in plan.md §11
    (`Loopback bool` instead of a `Host` field; shared resolver; broader
    bridge-attach removal; OpenClaw-only scope). Standards audit found one stale
    reference — `product-knowledge/standards/architecture.md` line 228 described
    bridge-form delivery (`http://conga-{name}:{port}/…`); updated to the
    loopback form with a pointer to this spec.
  - **Test sync:** no stale imports; new public `LoopbackWebhookResolver` has a
    test; loopback path now has parallel coverage to the bridge path. No fakes
    involved (pure function + string-assert on the systemd script).
  - **Status:** code VERIFIED. Operationally OPEN until provider release + apply.

## Key constraints

- `GenerateRoutingJSON` (`pkg/common/routing.go`) is shared by all providers and
  hardcodes the `conga-<agent>` host — the fix extends the `WebhookTarget`/resolver
  contract to emit loopback + host-port.
- `pkg/` change ⇒ terraform-provider-conga release (memory `reference_provider_release_flow`).
- Implementation must not touch the live prod host; deploy via provider-release + a `terraform apply` window (which reconciles the live manual override into managed config).

## Next

Implementation complete (above). Then: `/glados/verify-fix` → release
terraform-provider-conga (tag congaline → bump provider go.mod → release →
bump version pin) → `terraform apply` in a window (reconciles the live manual
override into managed config; router stays connected throughout).
