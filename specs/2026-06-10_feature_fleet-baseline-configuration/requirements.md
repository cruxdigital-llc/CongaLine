# Requirements — Fleet Baseline (+ Per-Agent Declarative) Configuration

## Goal

Feature #30 (Infrastructure-Only Simplification) gave admins a per-agent, **on-host**,
drift-friendly escape hatch (`agent-custom.json`) for custom OpenClaw config. That's great
for one-off tweaks, but it's the wrong shape for two real fleet-maintenance needs:

1. **"Every agent should have a baseline set of config"** (e.g. a standard MCP server / skill
   set) — today you'd hand-edit N host files, new agents start empty, and baseline changes
   don't propagate.
2. **"Configure an MCP server for an agent in code"** — today the only home is the on-host
   `agent-custom.json` (manual, SSM on AWS, not version-controlled). `agent.yaml` is
   strict-keyed (model + subagents only) and rejects `mcp`/`skills`/`tools`.

This feature makes custom OpenClaw config **declarative and version-controlled in the repo**, at
two granularities — **fleet** (all agents) and **per-agent** (`agents/<name>/`) — deployed by
Conga and composed via OpenClaw's `$include`, while preserving the on-host `agent-custom.json`
as the admin-drift layer.

## Success criteria

1. **Fleet baseline applies everywhere** — a committed fleet config (e.g. an MCP server) lands on
   **every** agent (existing + new) after `conga refresh`/provision, with **no per-agent edits**.
2. **Per-agent declarative config** — config placed in `agents/<name>/` is deployed to that agent
   and survives restarts/refresh; this is the supported "configure MCP in code" path.
3. **Propagation** — editing the fleet (or a per-agent) committed file + `conga refresh` updates
   the running agent(s); no `rebaseline`, no host-side editing.
4. **Composition + precedence is well-defined** — fleet < per-agent-declarative < on-host admin
   drift (`agent-custom.json`); and Conga-owned keys in the managed root (gateway/channels/auth)
   still win over all custom layers. Verified empirically.
5. **Fleet config without a binary release** — `openclaw-defaults.json` is no longer embedded;
   fleet defaults are an editable host/S3-synced file (de-embed folded in).
6. **Security preserved** — every custom layer is still forbidden from declaring Conga-owned keys
   (`channels`/`gateway`/`plugins`/`$include`); egress + secrets work fleetwide.
7. **Three-provider parity** (local, remote, AWS).

## Current state (grounding)

- **Fleet baseline already half-exists**: `openclaw-defaults.json` is unmarshalled as the base of
  every managed `openclaw.json` (`pkg/runtime/openclaw/config.go:19`) — but it's **`//go:embed`'d**
  (`config.go:14`), so changing it needs a rebuild + provider release, and it's authoritative
  (regenerated, non-driftable).
- **Per-agent config homes today**: `agents/<name>/agent.yaml` (strict-keyed: model + subagents —
  no `mcp`) and `agents/<name>/*.md` (prompts). Plus the on-host `agent-custom.json` (#30:
  free-form, admin-owned, never overwritten, root:root 0444 on AWS).
- **`$include` verified (feature #30)**: merges, survives restart/hot-reload, fails closed on
  owned-writes, **root wins on scalar conflicts**, objects union. Accepts an **array** of files
  "deep-merged in order" (array-order precedence NOT yet live-verified).
- **Behavior-file deploy precedent**: `agents/<name>/*.md` and `agents/_defaults/` are already
  deployed to the agent workspace by a deploy mechanism — a model for deploying custom-config files.

## Scope

**In scope**
- A fleet-level committed custom-config source (applies to all agents).
- A per-agent committed custom-config source under `agents/<name>/`.
- Conga deploying both as `$include` layers, composing with on-host `agent-custom.json`.
- De-embedding `openclaw-defaults.json` to a host/S3-synced file.
- Precedence/merge definition + live verification; integrity treatment of managed vs admin layers.
- Three-provider parity; fleet egress/secrets story.

**Non-goals**
- A typed schema for the custom config (it stays free-form OpenClaw JSON, like `agent-custom.json`).
  We are NOT modeling `mcp`/`skills`/etc. as first-class Conga fields.
- Changing `agent.yaml`'s strict model/subagents schema.
- Replacing the on-host `agent-custom.json` drift hatch (it stays — highest precedence).
- Application code.

## Constraints

- **Security boundary**: custom layers must not declare `channels`/`gateway`/`plugins`/`$include`
  (the #30 reserved-key guard extends to all layers). Channel allowlist stays Conga-owned.
- **Secrets**: never in custom config files (Issue #9627); fleet MCP auth → shared secret.
- **Egress**: a fleet/per-agent MCP endpoint must be in the relevant egress allowlist
  (global `allowed_domains` is additive per `egress-controls.md`).
- **`pkg/` change → provider release.** **De-embed must not break the air-gapped/first-boot path**
  (defaults must still be present at boot — via S3 sync like other bootstrap assets).
- **Backward compatible** with #30: existing `agent-custom.json` files keep working, unchanged.
- Honor the config taxonomy (`standards/config-taxonomy.md`) — this adds a new declarative layer;
  the doc must be updated.
