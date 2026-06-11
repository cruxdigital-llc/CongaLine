# Fix Plan — Router host-networking (eliminate per-agent bridge attach)

**Status:** Planned (not implemented). Output of `/glados/plan-fix`.
**Bug dir:** `specs/2026-06-11_bugfix_router-host-networking/`
**Persona:** Architect

## 1. Bug summary & impact

`conga-router` (the shared Slack event router) fans out Slack events to per-agent
containers by HTTP POST to `http://conga-<agent>:18789/slack/events`, reached
over each agent's isolated Docker bridge network. To do that the router is
hot-attached to every `conga-<agent>` network (`docker network connect`).

On the AWS host (Docker **25.0.16**, kernel **6.1.174-217.345.amzn2023.aarch64**)
that attach now fails:

```
docker network connect conga-aaron conga-router
Error: cannot program address 172.18.0.3/16 in sandbox interface because it
conflicts with existing route {Dst: 0.0.0.0/0 Gw: 172.17.0.1 Table: 254}
```

Reproduced even against a **fresh throwaway network** → the router cannot attach
to **any** `/16` bridge network while it holds the default-bridge default route.
Result: router receives events but every forward logs `fetch failed`; **Slack
never reaches the agents** (agents themselves are healthy). A live manual
workaround is in place on prod (see §7) and is the behavior this plan formalizes.

## 2. Root cause (5 Whys)

1. **Why no Slack to agents?** Router can't forward — `fetch failed` (can't resolve/reach `conga-<agent>`).
2. **Why?** The router isn't attached to the agents' networks.
3. **Why not?** `docker network connect` fails with a route conflict.
4. **Why now?** A reboot activated a kernel update (`6.1.170` → `6.1.174`); Docker 25.0.16 libnetwork can't program a `/16` connected route on a container that already has a default route, on this kernel. It worked at the 06:14 boot on the prior kernel.
5. **Why is the system exposed to this at all?** **The router-fanout architecture depends on attaching the router to every per-agent bridge network** so it can address containers by DNS name. That multi-bridge-attach pattern is the fragile dependency the Docker/kernel change broke.

**Root cause:** an architectural dependency on hot-attaching one container (router) to N isolated `/16` bridge networks. The fix removes that dependency — it does **not** chase the libnetwork conflict.

## 3. Fix strategy (Architect)

**Chosen fix — host networking + loopback delivery (real fix, not band-aid):**
- Run `conga-router` with `--network host`.
- Deliver to each agent's **published loopback port** instead of container DNS:
  `http://127.0.0.1:<hostPort>/slack/events`, where `<hostPort>` is the agent's
  host-side `GatewayPort` (every agent already publishes `-p 127.0.0.1:<hostPort>:18789`).
- Remove the per-agent `docker network connect <net> conga-router` (now both
  unnecessary and impossible under host networking).

This eliminates the root cause (no router↔bridge attach at all) rather than
working around the route conflict, so it is robust across Docker/kernel versions.

**Band-aid alternatives rejected:**
- *Pin/downgrade Docker* off 25.0.16 — fragile, host-wide blast radius, doesn't
  address the architectural dependency, breaks on the next kernel bump.
- *Recreate agent networks / reboot* — proven not to fix it (conflict is daemon+kernel level, networks persist).
- *Smaller agent subnets (/24)* — speculative, still relies on multi-bridge attach.

## 4. Concrete changes

| # | File | Change |
|---|------|--------|
| 1 | `pkg/common/routing.go` | `GenerateRoutingJSON` (line 64) **hardcodes** `http://conga-%s:%d`. Add a `Host` field to `WebhookTarget` (default `""` ⇒ `conga-<agent>` + `BaseGatewayPort`, preserving today's behavior) and have the resolver be able to emit `Host="127.0.0.1"` + `Port=<agent host GatewayPort>`. The resolver signature must gain access to the agent (it currently takes only `(runtime, platform)`) so it can return the agent's host port — or `GenerateRoutingJSON` applies the host-port when a loopback resolver is selected. |
| 2 | `pkg/provider/awsprovider/channels.go` | `restartRouterOnInstance`: `docker run --network host …` (drop default-bridge implicit attach); **remove** the trailing per-agent `docker network connect` loop. Extend `TestRouterRestartScriptUsesSlackPath` to assert `--network host` and absence of the connect loop. |
| 3 | `terraform/modules/infrastructure/user-data.sh.tftpl` | `conga-router.service` ExecStart → add `--network host`. Remove `connect-router-networks.sh` invocation + the `conga-router-networks.service` companion unit. Remove the agent units' `ExecStartPost=-/usr/bin/docker network connect conga-$AGENT conga-router`. (Terraform/bootstrap file — deployed via apply/S3 sync, no provider release for this file alone.) |
| 4 | provider routing callers (AWS, local, remote) | Pass the loopback `WebhookTargetResolver` so routing.json emits loopback URLs. |
| 5 | `pkg/provider/localprovider`, `pkg/provider/remoteprovider` | Apply the same router-run change (`--network host`) for consistency (see §5). |

## 5. Cross-provider analysis (side effects — the load-bearing question)

`GenerateRoutingJSON` lives in `pkg/common` and is shared. All three providers
are **single-host** topologies (all agents + the router run on one Docker host),
and all publish agent gateways to `127.0.0.1:<hostPort>`. Therefore host-net +
loopback delivery is valid for **AWS, local, and remote** alike — the bug could
hit any of them on the same Docker/kernel combo.

**Recommendation: make host-networking + loopback the standard router topology
for all providers**, rather than an AWS-only branch. Rationale: it's strictly
more robust, removes the brittle multi-bridge-attach from every provider, and
avoids divergent router code paths. Do **not** gate on Docker/kernel detection
(fragile, and the new topology is a superset-safe improvement regardless).

Backward-compat: routing.json schema is unchanged (only URL values change); the
router code just reads URLs. The `WebhookTarget.Host` default preserves the old
behavior for any caller that doesn't opt into loopback, so the change is additive
at the `pkg/common` contract level.

## 6. New risks & mitigations

- **`--network host` reduces the router's network isolation.** Mitigation: the
  router holds no inbound listeners (Slack Socket Mode is outbound; forwards are
  outbound POSTs), keep the existing hardening (`cap-drop ALL`,
  `no-new-privileges`, `--read-only`, `--user 1000:1000`, `--tmpfs /tmp`), and the
  host is a zero-ingress VPC. Net exposure increase is minimal and bounded to the
  router, which already holds the Slack tokens.
- **Loopback delivery requires agents to publish to `127.0.0.1:<hostPort>`** —
  already true on all providers (`pkg/common/ports.go` + the `-p` bindings).
- **Resolver signature change** ripples to all `GenerateRoutingJSON` callers —
  contained, compile-time caught, covered by `routing_test.go`.

## 7. Relationship to the live prod override

The prod host (`i-024bf3a55563f9e88`) currently has this fix applied **by hand**
(router unit `--network host`; routing.json rewritten to loopback ports). It is
**unmanaged drift** and any `conga refresh`/`policy deploy`/re-bootstrap reverts
it → Slack outage. After this fix ships and a `terraform apply` runs, the
**managed** config will match the override, making it durable. Until then: **do
not refresh/apply against this host** (see memory `project_router_bridge_route_conflict`).

## 8. Release & rollout

- `pkg/` changes (`routing.go`, `channels.go`) ⇒ **terraform-provider-conga release** required (memory `reference_provider_release_flow`): tag congaline `v0.0.30` → bump provider go.mod → release `v0.1.8` → bump the `version` pin in tfvars/module.
- `user-data.sh.tftpl` change ships via `terraform apply` (S3-synced bootstrap), no provider release for that file.
- Sequencing to avoid a fresh outage on the prod host: land the change, release the provider, then apply during a window — the apply will rewrite routing.json + the router unit to the (now-correct) managed values, matching the live override, so the router stays connected throughout.

## 9. Verification

- **Unit:** `routing_test.go` — loopback resolver emits `http://127.0.0.1:<hostPort>/slack/events` with the agent's host port; default (nil/empty Host) still emits `conga-<agent>:18789`. `channels_test.go` — `routerRestartScript` contains `--network host` and **no** `docker network connect` loop.
- **Integration / live:** after apply, `docker inspect conga-router -f '{{.HostConfig.NetworkMode}}'` = `host`; `routing.json` URLs are loopback; a Slack DM/channel message reaches the agent (router log shows `message → …` with **no** `Forward error`).

## 10. Handoff

Run `/glados/implement-fix` to implement against this plan. Implementation must
NOT touch the live prod host (output is code + bootstrap template + tests; deploy
via the provider-release + apply window).

## 11. Implementation notes (divergences from this plan)

Implemented 2026-06-11. Divergences from the plan as written, all deliberate:

- **§4 #1 — `Loopback bool`, not a `Host` field.** The plan offered two options
  ("add a `Host` field … _or_ `GenerateRoutingJSON` applies the host-port when a
  loopback resolver is selected"). Took the second: `WebhookTarget` gains
  `Loopback bool`, and `GenerateRoutingJSON` substitutes `127.0.0.1` +
  `a.GatewayPort` when it's set. A bare `Host` field would have been dead config
  (the per-agent host port can't come from a `(runtime, platform)` resolver
  anyway). The resolver signature was therefore left unchanged.
- **§4 #4 — shared `common.LoopbackWebhookResolver(globalDefaultRuntime)`.** One
  runtime-aware helper used by all three providers, rather than per-provider
  resolver code. Local's `webhookTargetResolver` now delegates to it.
- **Bridge-attach removal was broader than §4 #2/#3.** Beyond `routerRestartScript`
  and the bootstrap units, the per-agent `docker network connect … conga-router`
  was also removed from AWS `BindChannel` and from every local/remote
  connect/disconnect site (the helpers + the underlying `connect/disconnectNetwork`
  funcs are now deleted). Necessary for correctness: a host-networked router
  cannot be attached to a bridge, so any surviving attach call would error.
- **Scope — OpenClaw-only loopback (operator decision).** Loopback delivers to the
  published gateway port, valid only when the webhook is served there
  (`WebhookPort()==0` = OpenClaw). Hermes' separate, unpublished webhook port
  (8644) is a documented follow-up (would require publishing that port to a host
  loopback port + `GatewayPort` bookkeeping). The router JS forwards the
  routing.json URL verbatim, so no router-source change was needed.
