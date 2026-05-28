# PR #53 Comprehensive Review — Aggregated Findings

5 specialized agents reviewed PR #53 (`feat/delegation-routing` → `main`) in
parallel. Below is the consolidated report. Source agent outputs are in the
session trace.

## TL;DR

- **No blocking correctness bugs.** All 4 substantive agents converged on "no
  regressions found"; the v1 byte-equality guard
  (`TestGenerateConfig_V2NoSubagentsBlock_IdenticalToV1`) is sufficient.
- **Provider parity: confirmed.** AWS/local/remote `RefreshAgent` paths are
  semantically equivalent post-change. One error-fatality divergence is
  intentional and matches existing semantics (documented in followups).
- **Test coverage gaps are the principal concern.** Critical: the new
  `RefreshAgent` multi-step behavior + `UnpauseAgent` self-heal have **no
  regression guard at any layer**. Two test files promised in the spec
  were never delivered.
- **One NEW silent failure introduced**, two latent failures inherited.
- **No type-design or comment-hygiene blockers.** Comments on new
  abstractions are genuinely high-quality.

Aaron's verdict-recommendation across all 5 agents: **CONDITIONAL APPROVE
pending test gaps + the silent-failure fix.**

---

## Verdict by agent

| Agent | Verdict | Blocking issues |
|---|---|---|
| code-reviewer | APPROVE (no blockers) | 0 |
| pr-test-analyzer | CONDITIONAL APPROVE | 4 critical test gaps |
| silent-failure-hunter | 1 NEW silent failure | 1 needs fix |
| type-design-analyzer | APPROVE | 0 |
| comment-analyzer | Minor cleanup needed | 0 |

---

## Critical issues (must fix before merge)

### CRIT-1 — `redeployEgressDuringRefresh` silently collapses all policy-load errors → deny-all

`pkg/provider/awsprovider/provider.go:542-577` (the new function I added).

`policy.Load(policyPath)` returns `err != nil` for **all** of:
- file missing (expected: deny-all is correct)
- YAML typo (broken: should fail loudly)
- permission denied / I/O error (broken: should fail loudly)

All three collapse to `pf = nil` → deny-all Envoy config gets pushed to every
refreshed agent. A typo in `conga-policy.yaml` would silently regress every
agent on every refresh.

**Source**: silent-failure-hunter #2.

**Fix**: distinguish `os.IsNotExist` from other errors. Only proceed with
`pf = nil` on not-exist; abort refresh (or at minimum log the verbatim
error) on any other failure.

---

### CRIT-2 — `RefreshAgent` 4-step semantics has zero regression guard

`pkg/provider/awsprovider/provider.go:479` — the new flow does (1) regen
openclaw.json, (2) refresh-user.sh, (3) `regenerateRoutingOnInstance` +
`restartRouterOnInstance`, (4) `redeployEgressDuringRefresh`.

**Steps 3 and 4 are the followups #6 + #9 fixes. They have ZERO tests at
any layer.** A future refactor that drops either step would pass CI green.
The non-fatal failure semantics (the new `Warning + continue` pattern) are
also untested.

**Source**: pr-test-analyzer C1.

**Fix**: unit tests in `pkg/provider/awsprovider/provider_test.go` — stub
the helpers, assert both steps are called, assert step-3 failure is
non-fatal (RefreshAgent still returns nil), assert step-4 still runs after
step-3 failure.

---

### CRIT-3 — Missing test files the spec explicitly promised

Two test files documented in `spec.md` § "Test plan summary" do not exist
on the branch:

- `internal/mcpserver/tools_lifecycle_test.go` — promised "~2 new test
  cases" for the MCP `conga_provision_agent` tool's new `role` parameter.
  **Doesn't exist.**
- `internal/cmd/json_schema_test.go` — promised "~2 new test cases" for
  the JSON schema `role` field. **Doesn't exist.**

**Source**: pr-test-analyzer C3 + C4.

**Fix**: create both files with the promised coverage. At minimum: happy
path + type-mismatch error parity with the CLI.

---

### CRIT-4 — `UnpauseAgent` self-heal has no integration coverage

`pkg/provider/awsprovider/provider.go:432` — followups #5 fix
(clear paused → `RefreshAgent` recreates missing systemd unit). Existing
`TestAgentLifecycle/unpause` in `internal/cmd/integration_test.go` was NOT
updated — `git log main..HEAD -- internal/cmd/integration_test.go` is
empty.

**Source**: pr-test-analyzer C2.

**Fix**: add a subtest that (a) pauses, (b) manually deletes the unit
file, (c) calls unpause, (d) asserts the unit + container are recreated.

---

### CRIT-5 — Stderr warnings invisible under MCP for the followups fixes themselves

Multiple sites in `RefreshAgent` use `fmt.Fprintf(os.Stderr, "Warning: …")`
+ `continue`. The MCP server returns only `mcp.NewToolResult*` strings —
stderr is dropped. Concretely:

- `pkg/provider/awsprovider/provider.go:521,523,532,548` — followups #6 +
  #9's new warnings.
- `pkg/common/egress_check.go::WarnOverlayEgressGaps` — called from all
  three providers' `ProvisionAgent` paths.

**This is inconsistent with the explicit project principle** stated at
`pkg/provider/awsprovider/channels.go:687-694`: *"Emitting a warning and
proceeding is unsafe under MCP because stderr is invisible to the
operator."* The principle was applied to the worktree resolver (which
hard-fails) but not to the new warnings.

The followups #6 + #9 fixes — the whole point of which is to surface
issues operators couldn't see before — are themselves silently failing
under MCP. MCP is the most common operator path in this project.

**Source**: silent-failure-hunter #1 + #3 + #8 + code-reviewer #7 variant.

**Fix**: return accumulated warnings as part of the MCP tool result.
Minimum: errors.Join the non-fatal warnings into `RefreshAgent`'s return
so the MCP server's tool handler can surface them.

---

## Important issues (should fix, not strictly blocking)

### IMP-6 — AWS egress check reads the wrong source of truth

`pkg/provider/awsprovider/provider.go:158` (provision-time
`WarnOverlayEgressGaps`) and `:543` (`redeployEgressDuringRefresh`) both
load the policy from `~/.conga/conga-policy.yaml` on the operator's
laptop. On AWS the canonical egress allowlist is in
`terraform.tfvars` (`agents.<name>.egress_allowed_domains`).

In practice today (Aaron's production fleet has a local policy file), this
works. But operators who manage egress purely via Terraform get either
spurious "missing host" warnings or no useful output.

**Source**: code-reviewer #1 + #4.

**Fix**: either wire the tfvars-derived SSM reader, or document the
limitation prominently. Acceptable to defer if scope-bounded.

---

### IMP-7 — `roleSlug` is unvalidated before `filepath.Join`

`pkg/common/role_package.go:36-41` + `internal/mcpserver/tools_lifecycle.go:109,115`.

`roleSlug="../../../etc/passwd"` traverses upward. Not exploitable today
(upstream `agentName` validation + role.meta read failure block it), but
defense-in-depth gap.

**Source**: code-reviewer #2.

**Fix**: apply `[a-z0-9-]+` validation after normalization. ~5 LoC.

---

### IMP-8 — Egress pre-flight fires only on Provision, not Refresh

All three providers call `WarnOverlayEgressGaps` only from
`ProvisionAgent`. The most common iteration flow is: operator edits
`agents/<name>/agent.yaml` (changes a `subagents.model.base_url`) → runs
`conga refresh`. The warning never fires for this case.

**Source**: code-reviewer #3.

**Fix**: add the same call to each provider's `RefreshAgent` after
overlay load. ~3 LoC per provider.

---

### IMP-9 — Hermes role packages carry useless `delegation_mode: prefer`

`agents/_defaults/hermes/role-code-dev/agent.yaml:22` and
`role-writing/agent.yaml`. Files are byte-identical to OpenClaw siblings,
including `delegation_mode: prefer`. Per
`pkg/runtime/hermes/config.go:103`, the field is silently dropped on
Hermes. An operator copying the Hermes package and tuning that line is
misled.

**Source**: comment-analyzer #1.

**Fix**: either drop the line from the Hermes role files OR add a comment
that it's ignored on Hermes.

---

### IMP-10 — 10 identical role READMEs

`agents/_defaults/{openclaw,hermes}/role-*/README.md` are byte-identical
between runtimes. Hermes versions reference `openclaw-defaults.json`
which doesn't have a Hermes analog.

**Source**: comment-analyzer #2.

**Fix**: consolidate or differentiate. Possibly: one README per role with
runtime-specific subsections.

---

### IMP-11 — `followups.md #N` cross-references will rot

5 sites in provider code: `pkg/provider/awsprovider/provider.go:431,469,
474,541`, `pkg/provider/{local,remote}provider/provider.go`. Numbered
list items in `followups.md` are renumber-prone — closing one item shifts
all subsequent numbers.

**Source**: comment-analyzer #6.

**Fix**: inline-expand the load-bearing rationale at each site; keep one
orienting reference in `UnpauseAgent`'s godoc.

---

### IMP-12 — Hermes generator silently ignores `Overlay.Model`

`pkg/runtime/hermes/config.go:37-87` honors `params.Model` (setup-time
global) + `params.Overlay.Subagents` (this PR) but never reads
`params.Overlay.Model`. So a v2 overlay's primary `model:` block is a
no-op on Hermes. **Pre-existing** (not this PR), but this PR cements v2
contract expectations.

**Source**: code-reviewer #5.

**Fix**: either document the Hermes limitation in `agent.yaml.example` or
add `applyModelOverlay` for Hermes. Defer-acceptable.

---

### IMP-13 — `regenerateRoutingOnInstance` on AWS uses nil webhook resolver

`pkg/provider/awsprovider/channels.go:552` calls
`common.GenerateRoutingJSON(agents, nil)`. Harmless today for the
OpenClaw + Slack fleet (slack default `/slack/events` is hardcoded), but
breaks when Hermes or Telegram agents land on AWS.

**Source**: code-reviewer #6.

**Fix**: pass an appropriate resolver. ~3 LoC.

---

## Suggestions (polish, not required)

- **SUG-14**: Type-design: promote `DelegationMode` to named type
  (`type DelegationMode string`); replace inline `role.meta` parser with
  a `RoleMeta` struct. (type-design-analyzer #1-#5)
- **SUG-15**: Add primary-preservation assertion to
  `TestGenerateConfig_SubagentsOverlay_Basic`. (code-reviewer #8)
- **SUG-16**: `UnpauseAgent` rollback uses `%v` not `%w` for the rollback
  error, breaking `errors.Is/As` chains. (silent-failure-hunter #6)
- **SUG-17**: `ApplyRolePackage` partial-failure recoverability (no
  rollback, half-populated dir). (silent-failure-hunter #5)
- **SUG-18**: `findRepoRoot` test helper may skip silently if test runs
  from unusual cwd; add `t.Fatal` guard. (pr-test-analyzer I3)
- **SUG-19**: CLI/MCP `applyRolePackage` duplication — consolidate.
  (code-reviewer #7, comment-analyzer #18)
- **SUG-20**: Various WHY-vs-WHAT comment violations; minor restating-
  the-code comments. (comment-analyzer #11-#14)
- **SUG-21**: Document the `hermesKnownProviderHosts` refresh policy
  (when does the list need updating). (comment-analyzer #15)
- **SUG-22**: Mention bare-`slack.com` requirement at the slack-channel
  layer, not just in `egress-controls.md`. (pr-test-analyzer I2)
- **SUG-23**: terraform-provider-conga cascade warning guidance —
  document in `terraform.tfvars.example` so new operators don't panic
  on the stale-pkg/ warnings. (silent-failure-hunter #9)

---

## No-regression sign-off

✅ **No regressions detected.** Verified by:

- code-reviewer: `TestGenerateConfig_V2NoSubagentsBlock_IdenticalToV1`
  guard verified against every existing v1 codepath in
  `pkg/runtime/openclaw/config_test.go`. v1 documents continue to
  produce byte-identical openclaw.json.
- pr-test-analyzer: sibling-parity table for all new exported symbols
  matches the existing siblings' test coverage at unit level.
- silent-failure-hunter: pre-existing silent failures documented and not
  re-introduced. One **new** silent failure was introduced (CRIT-1
  above).
- All providers (AWS/local/remote) confirmed semantically equivalent
  post-change across egress redeploy, routing reconcile, and error
  handling (one intentional divergence in error fatality is correct).

---

## Provider parity sign-off

| Concern | AWS | Local | Remote |
|---|---|---|---|
| Egress proxy redeploy on refresh | ✅ via `redeployEgressDuringRefresh` (#6 fix) | ✅ via stop+start | ✅ via stop+start |
| `routing.json` reconcile on refresh | ✅ via `regenerateRoutingOnInstance` + restart router | ✅ via `regenerateRouting` | ✅ via `regenerateRouting` |
| Error handling fatality | non-fatal for routing+egress | non-fatal routing; fatal proxy | non-fatal routing; fatal proxy |
| Pre-flight egress warning | ✅ on Provision only ⚠ (IMP-8) | ✅ on Provision only ⚠ (IMP-8) | ✅ on Provision only ⚠ (IMP-8) |
| Unpause self-heal via Refresh | ✅ (followups #5) | ✅ pre-existing | ✅ pre-existing |

**Parity confirmed.** Two gaps applied uniformly to all three providers
(IMP-8 above is universal). The error-fatality divergence in row 3 is
intentional and documented.

---

## Recommended action plan

**Before merge — must fix (5 items)**:
1. CRIT-1 — fix `redeployEgressDuringRefresh` error handling
2. CRIT-2 — add `RefreshAgent` regression-guard tests
3. CRIT-3 — create the two missing test files
4. CRIT-4 — add `UnpauseAgent` self-heal integration test
5. CRIT-5 — surface MCP-side warnings via tool result

**Before merge — strongly suggested (4 items)**:
6. IMP-7 — `roleSlug` validation
7. IMP-8 — refresh-time egress pre-flight parity
8. IMP-9 — Hermes role files: drop `delegation_mode` or comment
9. IMP-11 — inline-expand `followups.md #N` cross-references

**Defer to follow-up issues**:
- IMP-6, IMP-10, IMP-12, IMP-13 — real but bounded; bookmark for next PR
- All SUG-* items — polish

**Total in-PR effort**: ~5 commits, mostly small additions. None of the
critical findings require architectural changes.
