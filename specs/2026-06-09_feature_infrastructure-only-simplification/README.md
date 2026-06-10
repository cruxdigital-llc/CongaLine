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

- **2026-06-09** — `/glados:spec-feature` started (branch `plan/infrastructure-only-simplification`, PR #57). Resolving the deferred decisions (root ownership, re-baseline UX, migration, integrity) before drafting `spec.md`.

- **2026-06-09** — Drafted `spec.md`. Pre-spec, ran two more isolated probes on `aaron` to settle load-bearing assumptions: (probe3) on **conflicting scalar** keys the **root wins** (Conga-owned values can't be overridden); (probe4) on **objects, deep-merge unions** — an include CAN **add** `channels.*` entries / new channel sections. The union result is a security finding (channel allowlist is a declared boundary). All probes isolated via `OPENCLAW_CONFIG_PATH`; aaron untouched, probes cleaned up.

- **2026-06-09** — `/glados:implement-feature` started. Capabilities: in-container `openclaw config validate`/`get` (for the §9 validation hook + tests), conga MCP `container_exec` + AWS SSM (live verify on AWS fleet). No UI/DB tools relevant. Created `tasks.md` breakdown for review before coding.

- **2026-06-09** — Impl P1+P2 landed. **C1 verified on `aaron`** (isolated): a missing `$include` target invalidates the whole config → helper must self-heal on every root write. Files: `pkg/runtime/runtime.go` (+`CustomConfigFileName()`), `pkg/runtime/openclaw/{config.go,container.go}` ($include injection + const + method), `pkg/runtime/hermes/config.go` (method→""), `pkg/provider/localprovider/{provider.go,channels.go}` (helper + 3 calls), `pkg/provider/remoteprovider/{provider.go,channels.go}` (helper + 3 calls), `pkg/provider/awsprovider/channels.go` (create-if-absent + root:root 0444 re-protect). Tests: `config_test.go` `TestGenerateConfig_IncludesAdminCustomFile`. Build/vet/gofmt clean; runtime+local+remote suites pass.

- **2026-06-09** — Impl P3 (security) + P4 (rebaseline) + docs landed (PR #57, commits through `cb9cd23`). P3: `common.ValidateAgentCustomConfig` forbids the include from declaring Conga-owned keys (`$include`/`channels`/`gateway`/`plugins`) — the load-bearing channel-allowlist control — wired into local+remote `RunIntegrityCheck` (JSON5-safe: surfaces `ErrCustomConfigUnparseable`, no unsafe comment-stripping). P4: `Provider.ResetAgentCustomConfig` (3 impls) + `conga agent rebaseline` (CLI+JSON+MCP). Docs: `config-taxonomy.md` gains the `agent-custom.json` locus (resolves gate `should` warning). **Remaining**: T3.4 AWS `check-config-integrity.sh` (tftpl jq), T2.6 AWS bootstrap `$include`+include creation (tftpl), T5.2 first-refresh advisory, T6.1/6.2 integration tests, T6.3 live verify (→ `/glados:verify-feature` + post-impl security gate). Full Go suite + vet clean.

- **2026-06-09** — AWS portion landed (T2.6 + T3.4, `user-data.sh.tftpl`). Bootstrap now injects `$include` via `jq` into the data-dir `openclaw.json`, creates `agent-custom.json` (root:root 0444), and re-baselines the integrity hash from the post-`$include` file. `check-config-integrity.sh` gained the reserved-key guard (jq: ALERT on `$include/channels/gateway/plugins`, WARN on invalid JSON). jq fragments verified locally. No `${}` interpolation hazards introduced; bare `$VAR` per tftpl convention. tftpl change → no provider release. **All providers now at parity.** Remaining: T5.2 advisory, T6.1/6.2 integration tests, T6.3 live verify (→ `/glados:verify-feature` + security re-audit).

## Spec Review & Standards Gate (pre-implementation)

### Persona Review
- **Architect** — APPROVE (post-amendment). Caught two `must` gaps now fixed: missing **Data Safety** section (added §11a) and **Interface Parity** for `conga agent rebaseline` (now CLI+JSON+MCP, §5.4). No new external deps; uses OpenClaw's native `$include` + existing CLI; embodies the "Own the box, not the behavior" principle; agent record unchanged.
- **Product Manager** — APPROVE. Why/Who clear; acceptance criteria testable (add MCP → refresh → survives, §11); scope guarded (typed `mcp:` schema explicitly out). Note (non-blocking): the in-container `config set` fail-closed + edit-the-include workflow is an operator UX change → release notes/docs.
- **QA** — APPROVE (post-amendment). Edge cases covered (§10: missing include, invalid JSON5, override-attempt, hot-reload race). Reinforced the deep-merge-union channel-injection unhappy path; required a **security regression test** (added §11) asserting the effective-allowlist check fires on an injected channel.

### Standards Gate
| Standard | Severity | Verdict |
|---|---|---|
| security.md — channel allowlist = security boundary (Principle 1/2) | must | ❌→✅ **RESOLVED** — effective-allowlist validation (§5.5) + `agent-custom.json` read-only-to-agent (§12) |
| security.md — secrets via env, never config (#9627) | must | ✅ PASSES |
| architecture.md — Agent Data Safety | must | ❌→✅ **RESOLVED** — Data Safety section added (§11a) |
| architecture.md — Interface Parity | must | ❌→✅ **RESOLVED** — rebaseline CLI+JSON+MCP (§5.4) |
| architecture.md — Provider contract (all 3 providers) | must | ✅ PASSES (§5.2) |
| architecture.md — Channel abstraction (platform-agnostic) | must | ✅ PASSES (allowlist check keys off agent record bindings) |
| egress-controls.md — admin MCP endpoints need allowlisting | must | ✅ PASSES (§12 documents; mirror overlay egress-gap warning) |
| config-taxonomy.md — per-agent config split | should | ⚠️ WARNING — new locus (`agent-custom.json`) must be added to the taxonomy doc during implement |

**Gate decision**: all `must` items RESOLVED via spec amendments; one `should` warning logged (taxonomy doc sync). **PROCEED** to `/glados:implement-feature`. Note: the live security/effective-allowlist control should be re-audited at the post-implementation gate.

## Key Decisions (this phase)

1. **Feature framing** — "infrastructure only" = Conga owns infra + a one-time baseline; ongoing runtime-config ownership moves to the administrator. Name kept as given.
2. **Approach C (recommended, validated)** — `$include` layering, live-confirmed on `aaron`/`2026.5.26`: merges, validates, survives restart + hot-reload, fails closed (never flattens). Conga owns the root `openclaw.json`; admin owns an `$include`'d file edited directly. Remaining decision for spec: confirm root ownership + document the in-container `config set` trade-off.
3. **`openclaw` CLI: validation, not mutation** — `config patch` is validated/version-correct but strips admin comments and needs in-container exec (§5c). Use `config validate`/`schema` (read-only) to check Conga's generated file against the exact image; keep file-templating for ownership.
4. **Security-relevant** — changes the config-integrity monitor's contract; `product-knowledge/standards/security.md` review required before implementation.

## Files Created

- [requirements.md](./requirements.md)
- [plan.md](./plan.md)
- [research-openclaw-config.md](./research-openclaw-config.md) — full config-surface map + Conga footprint
- [spec.md](./spec.md) — detailed technical specification (Approach C; security-gated)

## Next Step

`/glados:implement-feature` — implement `spec.md` §5 across `pkg/runtime/openclaw`, the three
providers, the CLI (`conga agent rebaseline`), and integrity (incl. the §5.5 effective-allowlist
check). Land tests per §11, then `/glados:verify-feature` + the post-implementation security gate.
Reminder: `pkg/` change → `terraform-provider-conga` release.
