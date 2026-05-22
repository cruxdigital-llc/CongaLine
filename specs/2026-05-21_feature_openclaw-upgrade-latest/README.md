# Feature Trace: OpenClaw Upgrade Latest

**Feature**: `openclaw-upgrade-latest`
**Started**: 2026-05-21
**Status**: Planning
**Lead**: TBD (pending persona selection)

## Purpose

Bump the deployed OpenClaw image pin from `v2026.3.11` → the current upstream
stable (`v2026.5.20` at time of writing), now that the Slack socket-mode
regression that held the pin back has been resolved upstream.

This is the deferred Phase 1 of the Local Model Routing feature
(spec `2026-05-19_feature_local-model-routing`), promoted into its own
spec so the pin bump lands as a discrete, bisectable change ahead of any
future feature that depends on the newer OpenClaw schema or runtime
behavior.

## Upstream Status Check (per `project-openclaw-pin-revisit` memory)

- **Holdback issue**: [openclaw/openclaw#45311](https://github.com/openclaw/openclaw/issues/45311)
- **State**: CLOSED (2026-04-25)
- **Fix release**: `v2026.3.22` via PR #45953 (Slack Bolt import-interop hardening)
- **Confirmation**: Reporter validated end-to-end on `v2026.3.22`; maintainer
  closed citing release-note evidence and on-main regression coverage in
  `extensions/slack/src/monitor/provider.interop.test.ts`.
- **Current latest stable**: `v2026.5.20` (released 2026-05-21).
- **Conclusion**: Pin holdback is no longer justified — proceed with bump.

## Active Personas

- **Architect** — schema/runtime compatibility, provider parity (local/remote/AWS), rollback strategy
- **QA** — verification plan covering Slack inbound, gateway, per-agent model overlay, egress proxy interaction post-bump

## Session Log

- **2026-05-21**: Session started; spec dir scaffolded.
- **2026-05-21**: Verified upstream Slack regression #45311 is closed; fix
  shipped in `v2026.3.22`; pin bump unblocked.
- **2026-05-21**: Personas selected — Architect, QA.
- **2026-05-21**: Requirements drafted. Scope = pin bump only; target =
  `v2026.5.18` (3-day soak, named as deferred Phase 1 target by sibling
  Local Model Routing spec). Success criteria: Slack inbound (user +
  team), gateway-only, model overlay, egress proxy, three-provider
  parity smoke, CLAUDE.md + memory hygiene.
- **2026-05-21**: Plan drafted. 6 phases — changelog review → single
  code-change commit → provider parity audit → 5-scenario verification
  per provider → opt-in per-environment rollout → memory/docs hygiene.
  Pin lives in 6 in-repo locations (3 Terraform, 2 Go provider, 1 JSON
  schema example) plus CLAUDE.md; two `:latest` fallbacks are explicitly
  out of scope.

## Persona Review (Spec Phase)

### Architect

**Verdict**: ✅ APPROVE.

- **No new dependencies**, no data-model change, no API contract change.
  An image-tag bump touched in six pre-existing locations.
- **Three-provider parity**: spec correctly identifies the AWS
  `refresh-all`-vs-`cycle-host` asymmetry (the SSM-baked-into-systemd-
  unit path) and routes the operator down the right command per
  provider. ✓
- **Bisectability**: single commit, six-line diff plus one paragraph in
  `CLAUDE.md`. Revert is trivial. ✓
- **Schema-change gate**: Phase 0 (upstream changelog audit) is the
  right guard against silent breakage. The artifact requirement
  (`changelog-review.md`) gives implementation a paper trail. ✓

**Notes for follow-up (not blocking)**:
- The `refresh-all`-vs-`cycle-host` asymmetry on AWS is documented in
  Phase 2 but knowingly not fixed. Worth a small follow-up spec to
  unify the rollout command across providers.
- The two `:latest` fallback paths
  (`pkg/runtime/openclaw/container.go:23`,
  `pkg/provider/remoteprovider/provider.go:255, :651`) are correctly
  out of scope. Latent risk noted; not introduced by this bump.
- GHCR tag immutability assumption: OpenClaw releases are immutable in
  practice; not codified in policy. Acceptable risk.

### QA

**Verdict**: ✅ APPROVE with three requests.

- **Unhappy path coverage** present: pull failure, mid-rollout
  container failure, mixed-version fleet, wrong-command operator
  mistake. ✓
- **Verifiable outputs** defined for all five scenarios. Each scenario
  has a concrete observable (HTTP 200, reply within 30s, `docker
  inspect` shows new tag, etc.). ✓
- **Rollout ordering** (local → remote → AWS) is correct: smallest
  blast radius first, gated on prior passes.

**Requests (folded into spec)**:
1. **S5 (Egress) should run on remote too**, not AWS-only. Remote runs
   Envoy in enforce mode per the egress feature spec; the iptables
   half is AWS-only, but the proxy half is verifiable on remote.
2. **Define a soak window**. A concrete "≥7 days of normal use on at
   least one production agent" gate before any further bumps or
   building features on top of `v2026.5.18`.
3. **Mid-rollout rollback rehearsal on AWS**: before
   `conga admin cycle-host` on production, do one deliberate rollback
   round-trip on a non-critical agent (or a clone host) to confirm the
   revert-SSM + restart-image-refresh path works as described.

**Note**: no automated tests added by this bump. Acceptable for a
one-time pin change; flagged as a recombobulate-worthy observation
(integration tests against the real runtime image catch this class of
bug earlier — already on the radar from the `2026-05-19` Local Model
Routing spec).

## Standards Gate Report (Pre-implementation)

| Standard | Scope | Severity | Verdict |
|---|---|---|---|
| `architecture.md` — Agent Data Safety | all (lifecycle ops) | must | ✅ PASSES — image bump does not touch volume mounts, data directories, or refresh-data paths. The "Refresh operations rebuild config, not data" rule is preserved (refresh-all and cycle-host both leave `/opt/conga/data/<name>/` untouched). |
| `architecture.md` — Interface Parity | all (cli/json/mcp) | must | ✅ PASSES — no new CLI flag, JSON field, or MCP parameter introduced. The change is purely in default values; existing surfaces unchanged. |
| `architecture.md` — Module Structure | pkg/internal split | must | ✅ PASSES — no code moves between `pkg/` and `internal/`. Edits are localized to one `internal/` file (`json_schema.go`) and one `pkg/` file (`remoteprovider/setup.go`); both already existed in their respective trees. |
| `architecture.md` — Provider Contract is API Boundary | all providers | should | ✅ PASSES — bump applies uniformly to all three providers via their respective config-storage mechanisms (SSM/local-config.json/remote-config.json). |
| `architecture.md` — Config Format Boundary | config files | should | ✅ PASSES — no new config files or formats. |
| `architecture.md` — Channel Abstraction (no deeper Slack coupling) | new code | should | ✅ PASSES — no Slack-specific logic introduced. The bump benefits Slack handling (clears #45311) but the change itself is platform-agnostic. |
| `config-taxonomy.md` — Decision rule for new per-agent concerns | per-agent config | must | ✅ PASSES — the image tag is global infrastructure config (already in tfvars on AWS), not a per-agent concern. No taxonomy slot is created or violated. |
| `egress-controls.md` — iptables active in ALL modes; defense-in-depth layers | egress | must | ✅ PASSES — no change to egress proxy, iptables rules, or DOCKER-USER chain. Verification scenario S5 explicitly re-tests the egress controls on the new image. |
| `security.md` — Pinned image baseline | universal | must | ⚠️ **WARNING → resolved by spec**. The security standard at line 41 hardcodes the old tag in the rationale. The spec's Phase 1 catalog (entry B6) updates this file in the same commit, so the standard stays in sync with reality. Without the standards-gate catch, this would have drifted. |
| `security.md` — Secrets via env vars, never config | universal | must | ✅ PASSES — no secret-handling change. |
| `security.md` — Config integrity monitoring | universal | must | ✅ PASSES — the integrity baseline is recomputed when `openclaw.json` regenerates as part of the bump. No change to the integrity-check mechanism. |
| `security.md` — Pinned to known-good version (rationale rewrite) | universal | should | ✅ PASSES — the spec rewrites the rationale from "avoid v2026.3.12 Slack regression" to "stability/bisectability against arbitrary upstream drift". More durable framing; reviewed and approved as part of B1/B6. |

**Result**: ✅ Gate PASSES. No `must` violations remain after spec
adjustments (the one `must` warning on `security.md:41` is closed by
spec entry B6). One blocking gap caught — nine additional hardcoded
version-string locations the initial enumeration missed (B2–B7, C2–C6)
— is fully absorbed into the spec's Phase 1 catalog.

## Spec adjustments from review

Folded into `spec.md`:
- Scenario S5 extended to include remote provider (proxy half only;
  iptables enforcement still AWS-only).
- Phase 5 expanded with a "soak window" item: ≥7 days at
  `v2026.5.18` on at least one production agent before any further
  bumps or dependent feature work.
- Phase 4 (AWS rollout) prepended with a "rehearsal" sub-step.
- **Phase 1 catalog expanded** (caught by standards gate): from 6
  files to 14 files / ~20 single-line edits, broken into three
  categories: A. in-repo defaults (5), B. docs + standards (7),
  C. tests + CI (6). The original 6 were correct but incomplete —
  README/TECH_STACK/security.md docs, integration test const,
  manifest test fixture, and CI cache key were all missed.

## Decisions

- **Scope**: pin bump only — no defaults reconciliation, no schema
  migration, no dependent feature work. (Memory: bisectable.)
- **Target version**: `v2026.5.18` over `v2026.5.20` (latest) for
  soak time, and over `v2026.3.22` (minimum-viable) for currency.
- **Stop condition**: any "blocking" changelog entry between
  `v2026.3.12` and `v2026.5.18` pauses this spec and spawns a separate
  migration spec.
- **Rollback**: `git revert` of pin commit + per-env `conga admin setup`
  re-pin. No data migration.
- **AWS rollout**: `cycle-host`, not `refresh-all` — image is baked
  into per-agent systemd ExecStart lines and only the boot-time
  `conga-image-refresh.service` rewrites them. `refresh-all` refreshes
  env files only (pre-existing asymmetry; not fixed here).

## Session Log (continued)

- **2026-05-21**: Spec drafted. Persona review (Architect + QA) passed
  with three QA asks folded into spec. Standards gate caught nine
  additional hardcoded version-string locations beyond the initial
  six — total Phase 1 catalog is now 14 files / ~20 line edits, split
  into A. defaults (5), B. docs/standards (7), C. tests/CI (6).
- **2026-05-21**: PROJECT_STATUS.md updated to reflect spec'd /
  reviewed / gate-passed state.
