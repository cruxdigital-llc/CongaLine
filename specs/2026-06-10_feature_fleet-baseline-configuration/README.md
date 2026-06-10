# Feature: Fleet Baseline (+ Per-Agent Declarative) Configuration

**Trace Log** — GLaDOS `plan-feature` workflow

- **Created**: 2026-06-10
- **Owner**: Aaron Stone
- **Status**: Planning
- **Spec dir**: `specs/2026-06-10_feature_fleet-baseline-configuration/`
- **Builds on**: `specs/2026-06-09_feature_infrastructure-only-simplification/` (the `$include` layering + `agent-custom.json` it shipped)

## One-line

Make custom OpenClaw config (MCP servers, skills, tools, …) **declarative and
version-controlled in the repo** at two granularities — a **fleet baseline**
applied to every agent, and **per-agent** config under `agents/<name>/` — deployed
by Conga via `$include` layering, composing with the existing on-host
admin-drift `agent-custom.json`.

## Scope reframe (operator, 2026-06-10)

The trigger was "every agent should have a baseline set," but the operator widened
it: *"the fleet baseline is ONE use case, but we may want agent-specific
configuration in the `agents/{agent}/` folders."* So this is really about a
**declarative custom-config layer in the repo** (the "configure MCP in code"
answer), with fleet + per-agent levels — not just a single fleet file.

## Active Personas
- **Architect** — config-layering model, `$include`-array precedence, where each layer is sourced/synced/deployed, embed→file, three-provider parity.
- **Product Manager** — scope vs. the existing config taxonomy, operator value, success criteria.
- **QA** — merge/precedence edge cases, fleet propagation correctness, egress/secrets fleetwide, integrity of managed vs admin layers.

## Active Capabilities
- **GitHub** (`gh`), **conga MCP** (now on v0.0.28), **AWS SSM** — for live validation of `$include`-array precedence (the load-bearing unknown), mirroring how feature #30 was empirically driven.
- No UI/DB tools relevant.

## Key decisions (this phase)
1. **Scope = fleet baseline + per-agent declarative config**, both in the repo (`agents/_defaults/…` and `agents/<name>/…`), layered via `$include`.
2. **Fold in de-embedding `openclaw-defaults.json`** so fleet defaults are a host/S3 file editable without a binary rebuild + provider release (long-standing logged debt).
3. Anchor on the `$include`-array mechanism (extends feature #30's verified single-include `$include`).

## Session log
- **2026-06-10** — Session start. Personas (all 3). Scope reframed to fleet + per-agent declarative config; de-embed folded in. Confirmed `openclaw-defaults.json` is `//go:embed`'d at `pkg/runtime/openclaw/config.go:14`.

## Files
- [requirements.md](./requirements.md)
- [plan.md](./plan.md)

## Next step
`/glados:spec-feature` — resolve the layering precedence (esp. `$include`-array order, live-verified), the file/deploy model, and integrity treatment.
