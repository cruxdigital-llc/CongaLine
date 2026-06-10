# Feature: Infrastructure-Only Simplification

**Trace Log** — GLaDOS `plan-feature` workflow

- **Created**: 2026-06-09
- **Owner**: Aaron Stone
- **Status**: Planning
- **Spec dir**: `specs/2026-06-09_feature_infrastructure-only-simplification/`

## One-line

Narrow Conga's role to **infrastructure + initial baseline config only**: generate a
standard `openclaw.json` once at provision time, then let administrators customize it
(e.g. add an MCP server) with those changes **surviving restarts and refreshes**.

## Active Personas

- **Architect** — config-lifecycle redesign, Conga-owned vs admin-owned key boundary, three-provider parity.
- **Product Manager** — scope, operator value, success criteria, non-goals.
- **QA** — restart/refresh survival, drift edge cases, integrity-monitor interaction, test strategy.

## Active Capabilities

- **GitHub** (`gh`) — PRs, CI, release flow (in active use this session).
- **conga MCP** — live agent introspection (`get_status`, `get_logs`, `container_exec`) against the AWS fleet, useful for verifying restart-survival in the verify phase.
- **AWS SSM** — host inspection for AWS-provider behavior.
- _No browser/UI or DB tools relevant — this is a config-generation/infra feature, no UI surface._

## Session Log

- **2026-06-09** — Session start. Personas selected (Architect, PM, QA). Capabilities recorded.
- **2026-06-09** — Ran a very-thorough code exploration of the current config lifecycle (generation, regeneration call sites across all 3 providers, integrity/hash monitoring, `.bak`/`.last-good` artifacts, MCP-injection paths, generated-vs-persisted split). Findings captured in `requirements.md` §Current State.
- **2026-06-09** — Drafted `requirements.md` (goal, problem, success criteria, scope, constraints) and `plan.md` (high-level approach + key decisions deferred to spec).
- **2026-06-09** — Per operator request ("be circumspect about the OTHER things in openclaw.json, not just MCP"), ran a full config-surface research pass: exhaustive inventory of what Conga's generator writes (subagent) + authoritative upstream schema (Context7 `/openclaw/openclaw` + raw `configuration-reference.md`). Findings in `research-openclaw-config.md`. Two findings changed the design: (a) OpenClaw natively supports `$include` deep-merge with fail-closed read-only roots → Approach C is upstream-supported, not speculative; (b) config is JSON5 → Approach B (read-merge-write) would strip admin comments. Updated `plan.md` approaches + decisions accordingly.
- **2026-06-09** — Live-validated `$include` on the `aaron` production agent (image `2026.5.26`), isolated-copy first then live with byte-exact backup/restore. Confirmed: merges + validates; resolves top-level keys AND `mcp.servers`; survives restart + hot-reload; OpenClaw **fails closed (never flattens)** on owned-writes when root has `$include`; gateway does not owned-write at startup. aaron restored byte-identical (integrity sha256 re-matched baseline). Promoted **Approach C to recommended** (Conga owns root + admin-include); recorded the in-container `config set` trade-off. Findings in `research-openclaw-config.md` §5b; open questions #1/#3/#5 resolved or narrowed.
- **2026-06-09** — Operator asked whether to use the `openclaw` CLI instead of writing config directly. Evaluated **Approach D (Conga drives `openclaw config patch`)** live on `aaron` (isolated copy): `patch` does a validated, version-correct recursive merge with `null`-deletes and runs standalone — but **strips admin JSON5 comments** and needs per-change in-container execution. Verdict: **use the CLI for read-only validation (`config validate`/`schema`), not mutation; keep Approach C for ownership.** Captured in `research-openclaw-config.md` §5c; resolves open Q#4. Cleaned up all probe artifacts on aaron.

## Key Decisions (this phase)

1. **Feature framing** — "infrastructure only" = Conga owns infra + a one-time baseline; ongoing runtime-config ownership moves to the administrator. Name kept as given.
2. **Approach C (recommended, validated)** — `$include` layering, live-confirmed on `aaron`/`2026.5.26`: merges, validates, survives restart + hot-reload, fails closed (never flattens). Conga owns the root `openclaw.json`; admin owns an `$include`'d file edited directly. Remaining decision for spec: confirm root ownership + document the in-container `config set` trade-off.
3. **`openclaw` CLI: validation, not mutation** — `config patch` is validated/version-correct but strips admin comments and needs in-container exec (§5c). Use `config validate`/`schema` (read-only) to check Conga's generated file against the exact image; keep file-templating for ownership.
4. **Security-relevant** — changes the config-integrity monitor's contract; `product-knowledge/standards/security.md` review required before implementation.

## Files Created

- [requirements.md](./requirements.md)
- [plan.md](./plan.md)
- [research-openclaw-config.md](./research-openclaw-config.md) — full config-surface map + Conga footprint

## Next Step

`/glados:spec-feature` — turn the recommended approach into a detailed technical spec
(resolve the open decisions in `plan.md` §Key Decisions to Resolve).
