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

## ⏸️ RESUME HERE (next session)

Implementation is **partially complete on branch `plan/fleet-baseline-configuration` (PR #61, NOT merged)**. Re-run `/glados:implement-feature` and continue from `tasks.md`.

- **Done + committed + green**: P1 (generator 3-layer `$include`), P3 (`ResolveCustomConfigSources`), **P4 Go paths** (local/remote/AWS-regenerate deploy `fleet-custom.json` + `agent-managed-custom.json`). The feature works via `conga refresh` today; `$include`-array precedence is live-verified (root > admin-drift > per-agent > fleet).
- **Remaining (in order)**: **P2** de-embed `openclaw-defaults.json` (loader+embedded fallback; touches terraform + every boot path; embed is at `pkg/runtime/openclaw/config.go:14`) → **T4.4** AWS boot tftpl + `add-user.sh.tmpl`/`add-team.sh.tmpl` deploy the layers from S3-synced sources → **P5** extend reserved-key guard + hashing to all managed layers (local/remote Go + AWS `check-config-integrity.sh`) → **P6** pre-deploy validation (fleet blast-radius fail-closed) + egress-gap warnings → **P7** `conga agent show-config` (CLI+JSON+MCP) → **P8** `config-taxonomy.md` docs → **P9** tests + live verify. Then review + verify gates → merge → `terraform-provider-conga` release → deployed-path verification.
- **Do NOT merge PR #61 until P5+P6 land** — the new layers are deployed but their security guard (P5) + blast-radius validation (P6) aren't in yet.
- **Gotchas**: `git checkout plan/fleet-baseline-configuration` first (work is not on main). For live AWS work, the conga MCP server holds stale SSO creds → restart it (or use a freshly-built `bin/conga` + `aws ssm` directly); re-`aws sso login --profile openclaw` when the token expires.

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

- **2026-06-10** — `/glados:implement-feature` started. Capabilities: in-container `openclaw config validate/get`, conga MCP (needs restart to clear stale SSO from earlier — use freshly-built `bin/conga` + `aws ssm` directly meanwhile), AWS SSM for live verify. Created `tasks.md` (9 phases) for review before coding.

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
