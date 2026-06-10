# Requirements — Infrastructure-Only Simplification

## Goal

Conga's configuration generation has become too prescriptive. It treats `openclaw.json`
as a fully-derived, disposable artifact: every provision, refresh, restart, channel
bind/unbind, and (after a manual refresh) secret change **regenerates the whole file from
scratch**, discarding anything an administrator added by hand.

This blocks legitimate per-agent customization. Concrete motivating case: an operator wants
to add the **Linear MCP server** to one agent's OpenClaw config. They edit `openclaw.json`,
it works — until the next restart/refresh regenerates the file and wipes the `mcpServers`
section.

The goal is to **let Conga deploy a standard baseline once, then get out of the way** so
administrators can customize each agent for the users and workloads it serves, with those
customizations persisting across restarts.

## Success Criteria

1. **Baseline on first deploy** — provisioning a new agent still produces a complete, correct,
   version-controllable standard `openclaw.json` (no regression vs. today).
2. **Drift survives restart** — after an administrator edits `openclaw.json` (e.g. adds an
   `mcpServers.linear` entry), restarting the container and running `conga refresh` **preserves
   the edit**. The added MCP server is still present and functional.
3. **Conga-managed concerns still update** — operations Conga is responsible for (channel
   bind/unbind, gateway port/bind, `allowedOrigins`, secret/token wiring, model/subagent
   overlays) continue to take effect on refresh **without** clobbering admin-owned sections.
4. **Re-baseline is possible but explicit** — an operator can deliberately reset an agent back
   to the generated baseline (opt-in, not the silent default).
5. **Three-provider parity** — behavior is identical across local, remote, and AWS providers.
6. **Integrity story remains coherent** — the config-integrity monitor no longer treats every
   admin edit as a tamper/violation, while still detecting corruption of Conga-managed keys
   (security review signs off).

## Current State (as-explored, 2026-06-09)

Grounding facts from a code sweep — see `plan.md` for how the design responds.

- **Generation is stateless full-file**: `pkg/runtime/openclaw/config.go:GenerateConfig()`
  builds the entire JSON from embedded defaults + channel bindings + `agent.yaml` overlays
  (model v1 / subagents v2) + team channel discipline. No on-disk state is read or merged.
- **Written/overwritten on**: ProvisionAgent, RefreshAgent/RefreshAll, channel bind/unbind/remove,
  and (post-manual-refresh) secret changes — in all three providers
  (`localprovider/provider.go`, `remoteprovider/provider.go`,
  `awsprovider/channels.go:regenerateAgentConfigOnInstance`). On AWS, refresh runs on the host
  and the write is atomic with a single `.bak`.
- **Integrity monitor** (`localprovider/integrity.go`, `remoteprovider/integrity.go`, and the
  AWS `check-config-integrity.sh` systemd timer using `config_check_interval_minutes`):
  SHA-256 of the **whole file** vs. a stored `<agent>.sha256` baseline. On mismatch — local/remote
  regenerate the gateway token instead of preserving it; AWS only logs to
  `/var/log/conga-integrity.log` (no auto-revert). The baseline is recomputed after every write.
- **No custom-config injection path exists**: `agent.yaml` overlay schema is strict-keyed
  (model, subagents only); unknown keys fail loudly. There is **no** `mcpServers` path today.
- **Persistence split**: stable agent records live in `~/.conga/agents/<name>.json`,
  `/opt/conga/agents/<name>.json`, or SSM `/conga/agents/<name>`; secrets live in a separate
  store; `openclaw.json` is purely computed and not authoritative for anything.
- **`.last-good` / `.bak.N`**: observed on the AWS host but **not** created by the integrity
  logic found — likely OpenClaw's own hot-reload/rotation. Origin to be confirmed in spec.

## Scope

**In scope**
- Changing the refresh/restart write path so admin edits to `openclaw.json` persist.
- Defining the **Conga-owned key set** and a merge strategy that preserves everything else.
- Adapting the integrity monitor to the new ownership boundary.
- An explicit re-baseline / reset affordance.
- Parity work across local, remote, AWS.

**Non-goals**
- A typed `mcpServers:` schema in `agent.yaml`. (The whole point is to *stop* requiring Conga
  to model every config concern. MCP becomes admin-owned free-form config, not a Conga overlay.
  A typed overlay could be a separate future feature.)
- Changing how stable agent records or secrets are stored.
- Multi-file / layered OpenClaw config that depends on unverified upstream merge behavior
  (noted as a rejected approach in `plan.md`).
- Application code — this remains infrastructure-as-code only.

## Constraints

- **OpenClaw hot-reload**: the config dir and file must stay writable by uid 1000; cannot be made
  read-only (existing constraint, do not regress).
- **No `${VAR}` substitution in `openclaw.json`** (Issue #9627) — preserved.
- **Security**: relaxing whole-file integrity is security-relevant — requires
  `product-knowledge/standards/security.md` review (Agent Data Safety, Interface Parity musts).
- **`pkg/` change → provider release**: any change under `pkg/` requires a
  `terraform-provider-conga` release per `CLAUDE.md`.
- **Backward compatibility**: existing deployed agents must migrate cleanly (first refresh after
  upgrade must not wipe a config an operator may already have hand-edited in the field).
