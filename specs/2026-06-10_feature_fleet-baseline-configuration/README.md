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
- [spec.md](./spec.md) — detailed spec (4-layer model, verified precedence, de-embed, deploy/integrity)

- **2026-06-10** — `/glados:spec-feature` started. **Live-verified the `$include`-array precedence** on `aaron`/`2026.5.26` (isolated copy via `OPENCLAW_CONFIG_PATH`, driven through `aws ssm`/`docker exec` because the MCP server held stale SSO creds): **later-in-array wins** (per-agent over fleet), **includes union** (distinct keys from all layers compose), and the **managed root still wins over all includes** (`gateway.port` stayed 18789). The 4-layer model is viable as planned: root > admin-drift > per-agent > fleet. Drafted `spec.md`.

## Spec Review & Standards Gate (pre-implementation)

### Persona Review
- **Architect** — APPROVE. Reuses #30's verified `$include` + the now-verified array precedence; fits the config taxonomy as a new declarative layer; de-embed-with-embedded-fallback is sound; parity covered (all 3 providers + AWS tftpl + provision scripts). Concern: 4 layers is a lot of cognitive load → recommends the **effective-config view (§3.5) ship *in* this feature**, not deferred.
- **Product Manager** — APPROVE. Serves both use cases (fleet baseline + "MCP in code"); criteria testable; scope bounded (free-form, no typed schema). Note: operator mental model needs the `config-taxonomy.md` update.
- **QA** — APPROVE with required tests (in §9): **fleet blast-radius** (bad fleet file rejected pre-deploy), **fleet propagation** (one file → all agents), **per-agent overrides fleet / admin overrides per-agent**, and the **de-embed fallback** (absent file → embedded).

### Standards Gate (pre-implementation)
| Standard | Severity | Verdict |
|---|---|---|
| security.md — reserved-key guard on every layer (channel allowlist boundary) | must | ✅ PASS (§3.4; root-wins verified) |
| security.md — **fleet blast radius** (one file → all agents) | must | ✅ PASS *given* pre-deploy validation (fail closed) + staged rollout (§3.3/§11) |
| security.md — de-embed defaults integrity + safe fallback | must | ✅ PASS (embedded fallback retained; synced file integrity-covered, §11) |
| security.md — secrets via env, egress additive | must | ✅ PASS (§11) |
| architecture.md — Agent Data Safety | must | ✅ PASS (§10) |
| architecture.md — Interface Parity | must | ⚠️ CONDITIONAL — *if* `conga agent show-config` ships, it must be CLI+JSON+MCP (§3.5). No new command otherwise. |
| architecture.md — Provider contract (all 3) | must | ✅ PASS (§7) |
| config-taxonomy.md — document the new layers | should | ⚠️ WARNING — taxonomy doc update required during implement (new fleet/per-agent layers). |

**Gate decision: PASS.** No blocking `must` violations. Two items to honor during implementation: the config-taxonomy doc update (should), and Interface Parity *if* the effective-config view ships. Re-audit the fleet blast-radius + reserved-key controls at the post-implementation gate.

## Next step
`/glados:implement-feature` — generator `$include` array, de-embed `openclaw-defaults.json` with embedded fallback, source resolver + per-provider deploy (all 3 #30 write paths incl. AWS tftpl + provision scripts), integrity extension to all layers, tests per spec §9. Then `/glados:verify-feature` + security re-audit. `pkg/` change → provider release.
