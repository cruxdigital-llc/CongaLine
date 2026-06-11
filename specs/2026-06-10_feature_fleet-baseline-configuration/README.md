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

- **Done + committed + green**: P1 (generator 3-layer `$include`), P3 (`ResolveCustomConfigSources`), **P4 Go paths** (local/remote/AWS-regenerate deploy `fleet-custom.json` + `agent-managed-custom.json`), **P2 Go de-embed** (`openclaw-defaults.json` loader + embedded fallback; file lives at `agents/_defaults/openclaw/openclaw-defaults.json`, reuses fleet-custom's sync — no new terraform). The feature works via `conga refresh` today; `$include`-array precedence is live-verified (root > admin-drift > per-agent > fleet).
- **Done + committed + green (this session)**: **T4.4** AWS bash fresh-deploy path — `deploy-agents.sh` deploys fleet-custom.json + agent-managed-custom.json from S3-synced sources (root:root 0444, openclaw-gated); 3-element `$include` jq at all 3 config-write sites (add-user/add-team/user-data.tftpl). **P5** integrity — reserved-key guard on all 3 include layers + hash-verify the 2 managed layers vs deployed baseline, across local Go + remote Go + AWS (`check-config-integrity.sh` loop, `deploy-agents.sh` baselines). New runtime method `ManagedCustomConfigFiles()`; `common` refactored to filename-generic `ValidateCustomConfigKeys`/`ClassifyIncludeValidation`. Tests across all paths.
- **P6 done this session**: pre-deploy fail-closed validation (`ValidateManagedConfigSources` — reserved-key violation in fleet/per-agent aborts the deploy before any write, all 3 paths) + custom-config egress-gap warnings (`WarnCustomConfigEgressGaps` over `mcp.servers.*.url`, all 6 provision/refresh sites). Tests green.
- **P7 done this session**: `conga agent show-config` — **layered view** (operator-chosen: 4 deployed layers read live via ContainerExec, labeled by precedence/role/owner; no synthesized merge). CLI + `--output json` + MCP `conga_agent_show_config`. Shared `common.EffectiveConfigSpecs`/`BuildConfigLayers`. Tests green.
- **P8 done this session**: T8.1 migration is automatic (generator always emits the 3-element array; deploy always creates empty managed files → first refresh rewrites #30's 1-element form, agent-custom.json untouched). T8.2 `config-taxonomy.md` updated — declarative vs admin-drift split, 4-layer precedence subsection, de-embed note, Example 6 rewrite.
- **P9 (deterministic) done this session**: `TestDeployManagedCustomConfig` — fleet+per-agent deployed from sources (or `{}`), re-synced each call, admin-drift untouched, bad fleet source fails closed (no partial write). All `go test ./...` green; `go vet` clean.
- **Implementation phase COMPLETE.** All code/docs/unit+integration(Go) tasks done across P1–P8 + P9-deterministic. **Next: `/glados:verify-feature`** for T9.2 (live fleet propagation + override precedence + integrity + show-config on a real agent) and the post-implementation security re-audit. **Post-merge: R1** `terraform-provider-conga` release (this PR touches `pkg/`).
- **Still open (intentionally, not blockers for this PR)**: **T2.4** AWS bash-boot-path de-embed unification (tracked follow-up, operator-flagged); **T9.2** live verify (verify-feature); **R1** provider release (post-merge).
- **Tracked follow-up (do NOT lose — operator flagged)**: **T2.4** unify the AWS bash boot/provision path to consume the de-embedded `openclaw-defaults.json` (the conga Go binary never runs on-host, so the bash heredocs still hardcode defaults inline; a fresh AWS boot reflects edits only after the first `conga refresh`). Likely pairs naturally with T4.4 (same bash files).
- **Merge gate cleared**: P5 (security guard) + P6 (blast-radius pre-deploy validation) are both in. PR #61 can merge once P7–P9 + the verify/security gates pass.
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

- **2026-06-10** — **Code review (pr-review-toolkit) + fixes.** Reviewer (diff `279a4b4..HEAD`) confirmed the security controls (reserved-key guard on all 3 layers × 3 providers, pre-deploy fail-closed, hash scope, terraform escaping, gitignore) are correct. Fixed 3 findings: **(BLOCKER)** AWS Go refresh (`regenerateAgentConfigOnInstance`) re-uploaded the managed layers but only rewrote the *root* baseline — now writes the `<name>-<file>.sha256` managed-include baselines too, restoring hash symmetry with `deploy-agents.sh` + local/remote (without it, every content-changing `conga refresh` on AWS would trip a false integrity violation). **(should)** local provider read the #31 sources + runtime defaults from the `~/.conga` snapshot — switched the 5 sites to `overlayBehaviorDir()` (live repo) so edits propagate on `conga refresh` without re-running `admin setup`, matching `agent.yaml` + the spec §5 flow + the codebase-config operator preference. **(should)** AWS `add-user`/`add-team` wrote the 3-element `$include` but created the managed targets only if `deploy-agents.sh` was present — added a `{}` fallback (root:root 0444) so a missing target can never invalidate the config. Nit #4 (ContainerExec stdout/stderr) verified non-issue (`dockerRun` returns stdout only). Added a scripts render assertion for the fallback. All `go test ./...` green, `go vet` clean.

- **2026-06-10** — `/glados:implement-feature` resumed (session 2). **Drove P2 → P9 to completion.** Order: P2 (de-embed) → T4.4 (AWS bash fresh-deploy) → P5 (integrity guard+hash) → P6 (pre-deploy fail-closed + egress warnings) → P7 (show-config) → P8 (docs) → P9 (deterministic tests). Six feature commits (9012f34, 368b02b, 1201db7, 7e0771b, 2ca8cf8, 51fcfa3) + this trace. All `go test ./...` green, `go vet` clean throughout. **Full file inventory (since resume point 279a4b4):**
  - **Generator/runtime**: `pkg/runtime/runtime.go` (ConfigParams.RuntimeDefaults + ManagedCustomConfigFiles iface), `pkg/runtime/openclaw/config.go` (de-embed loader+fallback, ManagedCustomConfigFiles), `pkg/runtime/hermes/config.go` (ManagedCustomConfigFiles nil), `pkg/runtime/openclaw/config_test.go`.
  - **common**: `config.go` (thread RuntimeDefaults), `custom_config.go` (ResolveRuntimeDefaults, ValidateCustomConfigKeys/ClassifyIncludeValidation/ValidateManagedConfigSources), `egress_check.go` (CheckCustomConfigEgress/WarnCustomConfigEgressGaps), `config_layers.go` (EffectiveConfigSpecs/BuildConfigLayers) + tests for each.
  - **providers**: local/remote `integrity.go` (guard all 3 + hash managed 2 + baselines), local/remote `provider.go` + aws `channels.go`/`provider.go` (RuntimeDefaults wiring, pre-deploy fail-closed, egress warnings), `localprovider/customconfig_test.go`.
  - **AWS bash**: `scripts/deploy-agents.sh.tmpl` (deploy managed layers + baselines), `scripts/add-user.sh.tmpl`/`add-team.sh.tmpl` + `terraform/.../user-data.sh.tftpl` (3-element $include; integrity loop over 3 layers + hash managed 2), `scripts/scripts_test.go`.
  - **CLI/MCP**: `internal/cmd/agent_behavior.go` (show-config), `internal/mcpserver/tools_config.go` + `tools.go` + `server_test.go` (conga_agent_show_config).
  - **seed/docs**: `agents/_defaults/openclaw/openclaw-defaults.json` (committed seed), `product-knowledge/standards/config-taxonomy.md`, `product-knowledge/observations/observed-philosophies.md` (pattern-observer: "show source-of-truth, don't re-implement upstream semantics").

- **2026-06-10** — `/glados:implement-feature` resumed. **P2 Go de-embed landed + green.** Two operator scope decisions: (1) Go de-embed now, AWS bash boot-path unification deferred to tracked follow-up T2.4 (conga binary never runs on-host per `user-data.sh.tftpl:1367`, so the embed is Go-only); (2) editable file at `agents/_defaults/openclaw/openclaw-defaults.json` reusing fleet-custom's existing S3/snapshot sync — no new terraform (resolves checkpoint C2). Changes: `ConfigParams.RuntimeDefaults` field; `GenerateConfig` prefers it (valid JSON) else embed, malformed→warn+embed; `common.ResolveRuntimeDefaults`; wired 3 Go gen sites (local ×2, `RuntimeGenerateAgentFilesWithOverlay` for remote+AWS); seeded committed source file; unit tests T2.3. Modified: `pkg/runtime/runtime.go`, `pkg/runtime/openclaw/config.go`, `pkg/common/custom_config.go`, `pkg/common/config.go`, `pkg/provider/localprovider/provider.go`, `agents/_defaults/openclaw/openclaw-defaults.json` (new), `pkg/runtime/openclaw/config_test.go`, `pkg/common/custom_config_test.go`. Files changes logged here per trace requirement.

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
