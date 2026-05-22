# Tasks — Delegation Routing

Derived from `spec.md` § "Phased implementation contract." Each phase is
a small, independently-verifiable commit. Within a phase, sub-tasks are
sequential unless marked parallel.

---

## Phase 1 — Schema bump + types

**Goal**: Schema v2 type + validation, no behavior change in generators.

- [x] **1.1** `pkg/runtime/overlay.go`:
  - bump `CurrentOverlaySchemaVersion` 1 → 2
  - add `SubagentsOverlay` struct with fields `Model`, `DelegationMode`,
    `MaxConcurrent`, `MaxSpawnDepth`
  - add `AgentOverlay.Subagents *SubagentsOverlay` (omitempty yaml tag)
  - add `(s *SubagentsOverlay) validate()`:
    - `Model` required when block present
    - Model goes through existing `ModelOverlay.validate()`
    - Reject `Model.Provider == anthropic` (the v2 scope-narrow rule)
    - Reject same-provider conflict with primary (primary + subagent
      both use same provider key but different `BaseURL`)
    - `DelegationMode` enum: `""`, `suggest`, `prefer`
    - `MaxConcurrent >= 0`, sane cap at e.g. 128 (typo guard)
    - `MaxSpawnDepth` in `0..3` (0 = absent / use runtime default)
  - extend `(o *AgentOverlay) Validate()` to call `Subagents.validate()`
    and to detect the same-provider conflict (needs both Model and
    Subagents to compare)
- [x] **1.2** `pkg/common/overlay_agent.go`:
  - **Implementation note**: no edits needed. `subagents` was not in
    `reservedTopLevelKeys` (verified). The friendly v1-with-subagents
    error is emitted at `AgentOverlay.Validate()` time and reaches
    the operator via `LoadAgentOverlay`'s existing
    `fmt.Errorf("%s: %w", path, err)` wrapping. No pre-pass needed —
    the validation-time path produces the same operator UX.
- [x] **1.3** Tests `pkg/runtime/overlay_test.go`:
  - v1 doc with only `model:` still parses (regression)
  - v2 doc with valid `subagents:` parses
  - v2 doc with `subagents:` missing inner `model:` → error
  - v2 doc with `subagents.model.provider: anthropic` → error
  - v2 doc with same-provider primary + subagent + different base_urls
    → error
  - v2 doc with valid `delegation_mode: prefer` → ok
  - v2 doc with invalid `delegation_mode: foo` → error
  - v2 doc with `max_concurrent: -1` → error
  - v2 doc with `max_spawn_depth: 4` → error (out of 0..3 range)
- [x] **1.4** Tests `pkg/common/overlay_agent_test.go`:
  - v1 doc with `subagents:` key → loader error mentioning "bump to
    version: 2"
  - v2 doc with valid subagents → loads, overlay.Subagents is non-nil
  - v2 doc with unknown subagent inner key (e.g. `subagent.modal:`) →
    strict-key parse error
  - bonus: v2 doc with primary + subagent (role-code-dev shape)
  - bonus: v2 same-provider-conflict
  - bonus: v3 still rejected (replaces the deleted v2-rejected test)
- [x] **1.5** `go test ./pkg/runtime/... ./pkg/common/...` green;
  full suite green (regression); `go vet ./...` clean;
  `gofmt -l pkg/runtime/ pkg/common/` clean.

**Phase 1 acceptance**: validation surface complete; generators
unchanged; full test suite green. ✅

---

## Phase 2 — OpenClaw generator

**Goal**: A v2 overlay produces a correctly-shaped `openclaw.json`
section under `agents.defaults.subagents` + extended models allowlist
+ extended `models.providers`. v1 overlay output unchanged.

- [x] **2.1** `pkg/runtime/openclaw/config.go`:
  - new helper `applySubagentsOverlay(config map[string]any,
    s *runtime.SubagentsOverlay) error`
  - emits `agents.defaults.subagents.model = "<provider>/<name>"`
  - emits `delegationMode` and `maxConcurrent` only when set
  - **does NOT emit** `max_spawn_depth` (Hermes-only knob — implementation
    note added during Phase 2)
  - merges `<provider>/<name>` into `agents.defaults.models` (additive)
  - merges subagent provider config into `models.providers.<id>`:
    creates a new entry when the provider isn't there; appends the
    subagent model to the existing `models[]` array when the provider
    matches the primary; rejects same-provider + different-base_url
    as defense-in-depth (validation normally catches this first)
  - called from `GenerateConfig` right after `applyModelOverlay`
- [x] **2.2** Tests `pkg/runtime/openclaw/config_test.go` — 7 new
  test functions:
  - `TestGenerateConfig_SubagentsOverlay_Basic` — subagent-only overlay
    (no primary block) emits the expected shape
  - `TestGenerateConfig_SubagentsOverlay_DelegationModeAndConcurrent`
    — `prefer` + `4` emitted as `delegationMode` + `maxConcurrent`
  - `TestGenerateConfig_SubagentsOverlay_MaxSpawnDepthNotEmitted` —
    Hermes-only knob filtered out (regression guard)
  - `TestGenerateConfig_V2NoSubagentsBlock_IdenticalToV1` — v2 doc
    without subagents block is byte-identical to v1 (Feature #27
    regression guard)
  - `TestGenerateConfig_SubagentsOverlay_AllowlistMergePreservesPrimary`
    — primary + subagent + runtime default all in allowlist
  - `TestGenerateConfig_SubagentsOverlay_SameProviderAppendsToModelsArray`
    — primary + subagent on same provider + same base_url → single
    provider entry with both models in `models[]`
  - `TestGenerateConfig_SubagentsOverlay_SameProviderConflictDefense`
    — programmatic AgentOverlay bypassing Validate still hits a
    generator-level conflict error
- [x] **2.3** Full test suite green. `go vet ./...` clean. `gofmt -l
  pkg/runtime/ pkg/common/` clean.

**Phase 2 acceptance**: OpenClaw generator emits the documented shape. ✅

---

## Phase 3 — Hermes generator

**Goal**: A v2 overlay against Hermes produces a `delegation:` block,
with degraded-mode behavior for unsupported providers.

- [x] **3.1** `pkg/runtime/hermes/config.go`:
  - new helper `applySubagentsOverlay(cfg, s, agentName)` emitting the
    `delegation:` YAML block
  - `delegation.model: "<provider>/<name>"`
  - `delegation.max_concurrent_children` only when set
  - `delegation.max_spawn_depth` only when set (Hermes-specific knob)
  - `delegation_mode` filtered out (OpenClaw-only — Hermes generator
    must not emit it; covered by a regression test)
  - degraded-mode path: `openai` provider + base_url not matching any
    `hermesKnownProviderHosts` entry → emit `delegation.model` only
    (omit `delegation.provider`) + one-time stderr warning using a
    `sync.Map`-based dedup mirror of the `overlayWarningOnce` pattern
  - `stderrWriter` indirection (var `func() *os.File`) so tests can
    pipe stderr without touching `os.Stderr` directly
- [x] **3.2** Tests `pkg/runtime/hermes/config_test.go` (new file —
  Hermes previously had no test files) — 9 new test functions:
  - `TestGenerateConfig_HermesNoOverlay` — baseline: no delegation block
  - `TestGenerateConfig_HermesV2NoSubagents_IdenticalToBaseline` —
    v2 doc without subagents block is byte-identical to baseline
  - `TestGenerateConfig_HermesSubagents_OllamaInherit` — ollama →
    model emitted, no provider, no warning (transparent inheritance)
  - `TestGenerateConfig_HermesSubagents_DegradedNoProvider` — openai +
    custom base_url → model only, warning logged
  - `TestGenerateConfig_HermesSubagents_KnownAdapterHostNoWarning` —
    openai + openrouter base_url → no warning
  - `TestGenerateConfig_HermesSubagents_MaxConcurrentEmittedAsHermesKey`
    — overlay `max_concurrent` emits as Hermes' `max_concurrent_children`
    (NOT the OpenClaw `maxConcurrent`)
  - `TestGenerateConfig_HermesSubagents_MaxSpawnDepthEmitted` —
    Hermes-specific knob actually appears in output
  - `TestGenerateConfig_HermesSubagents_DelegationModeFiltered` —
    OpenClaw-only `delegation_mode` filtered out
  - `TestGenerateConfig_HermesSubagents_WarningEmittedOnce` — dedup
    across two GenerateConfig calls
- [x] **3.3** Full test suite green. `go vet ./...` clean. `gofmt -l
  pkg/runtime/ pkg/common/` clean.

**Phase 3 acceptance**: Hermes generator produces the documented shape;
degraded path is logged, not silent. ✅

---

## Phase 4 — Egress check helper

**Goal**: Provisioning detects subagent endpoints missing from the
agent's effective egress allowlist; emits a clear warning; does NOT
block.

- [x] **4.1** `pkg/common/egress_check.go` (new):
  - `CheckOverlayEgress(overlay, allowlist) []string` — extracts
    hostnames from `Model.BaseURL` and `Subagents.Model.BaseURL`,
    skips empty hosts and unparseable URLs, dedups, matches against
    allowlist using exact + `*.suffix` wildcard semantics
    (mirroring `policy.MatchDomain` — copied locally to avoid
    common→policy import per the architecture standards)
  - `FormatEgressGapWarning(agentName, host) string` — multi-line
    operator-facing warning matching the spec format
  - `WarnOverlayEgressGaps(w io.Writer, overlay, allowlist, name)`
    — one-shot wrapper for provider use; writes one warning per gap
- [x] **4.2** Tests `pkg/common/egress_check_test.go` (new file) — 17
  test functions:
  - nil overlay, no base_urls, hosted openai (no base_url) all return nil
  - primary missing / subagent missing / both present / mixed
  - case-insensitive matching (host AND allowlist entry)
  - wildcard match for subdomains, wildcard does NOT match bare host
  - port stripped from host
  - duplicate hosts deduped (single warning when primary + subagent
    share an endpoint)
  - insertion order: primary first, then subagent
  - malformed URLs skipped (defense-in-depth)
  - `FormatEgressGapWarning` shape (agent name + host + both file
    paths in the message)
  - `WarnOverlayEgressGaps` no-output when no gaps, one warning per
    gap
- [x] **4.3** Provider wiring — added `WarnOverlayEgressGaps` call to
  all three providers' `ProvisionAgent` flows after overlay+policy
  load:
  - `pkg/provider/localprovider/provider.go` (line ~306) — overlay
    + policy already in scope; one-line addition
  - `pkg/provider/remoteprovider/provider.go` (line ~285) — same
    pattern
  - `pkg/provider/awsprovider/provider.go` (line ~170) — AWS
    `ProvisionAgent` did NOT previously load the overlay (only
    `RefreshAgent` did); added a best-effort overlay load via
    `resolveAWSBehaviorDir()` for the check only. If the local
    `agents/` overlay dir isn't resolvable (operator outside the
    repo) the check skips silently.
- [x] **4.4** No new integration tests added. Rationale: the helper
  unit tests cover the behavior thoroughly (17 cases); the provider
  wiring is a one-line call. The existing
  `TestAgentLifecycle/add-user` (integration) exercises the
  ProvisionAgent path with real Docker — that path remains green
  apart from a pre-existing Docker-port collision on Aaron's machine
  (port 18789 already bound), unrelated to this phase. Phase 8 live
  smoke will exercise the warning end-to-end with a real overlay.
- [x] **4.5** Full test suite green. `go vet ./...` clean. `gofmt -l
  pkg/` clean.

**Phase 4 acceptance**: warnings visible at provision; provisioning
itself unaffected. ✅

---

## Phase 5 — Role packages (catalog)

**Goal**: Ten directories under `agents/_defaults/<runtime>/role-*/`,
each with SOUL.md, AGENTS.md, USER.md.tmpl, agent.yaml, role.meta.

- [x] **5.1** Created `agents/_defaults/openclaw/role-ops/`:
  - `role.meta` (`type: user`)
  - `SOUL.md` — calm, data-driven ops persona; sub-agent-aware framing
  - `AGENTS.md` — triaging alerts, status report workflow, red lines
  - `USER.md.tmpl` — single-user ops specialist on Qwen
  - `agent.yaml` — v2, `model.provider: openai`, base_url placeholder
    `https://litellm.internal/v1`, NO subagents block
  - `README.md` — post-provision customization steps + egress note
- [x] **5.2** Created `agents/_defaults/openclaw/role-data/` (Qwen, no
  subagent). Personality: methodical, format-aware, "lab notebook not
  blog post." Workflow: dataset shape check, reproducible reports.
- [x] **5.3** Created `agents/_defaults/openclaw/role-research/` (Qwen,
  no subagent). Personality: curious + citation-disciplined.
  Workflow: cite or skip, primary vs secondary source distinction.
- [x] **5.4** Created `agents/_defaults/openclaw/role-code-dev/`:
  - `role.meta` (`type: team`)
  - `agent.yaml` v2, NO `model:` block (uses runtime default Opus),
    `subagents.model.provider: openai`, name `qwen-2.5-72b-instruct`,
    `delegation_mode: prefer`, `max_concurrent: 4`
  - **Implementation note**: spec.md mentioned `model.provider:
    anthropic` for Opus primary, but the overlay enum is `{ollama,
    openai}` only — anthropic primaries are expressed by omitting
    the `model:` block (runtime default applies). README.md
    documents this for operators.
  - SOUL.md heavy on subagent delegation guidance ("Reasoning is your
    job; lookups are not")
  - AGENTS.md: code review / debugging workflows showing exactly
    when to spawn a subagent and what to pass it
- [x] **5.5** Created `agents/_defaults/openclaw/role-writing/` (Opus +
  Qwen subagent, type: team). Personality: voice-aware editor,
  delegates mechanical text work to Qwen.
- [x] **5.6** Mirrored all 5 roles under `agents/_defaults/hermes/`
  via `cp -r`, then applied the runtime-specific tweaks (Hermes-
  flavored deployment paragraph in SOUL.md + Hermes-flavored Tools
  section in AGENTS.md — both matching the existing pattern in
  `agents/_defaults/hermes/{user,team}/`).
- [x] **5.7** Each role has a `README.md` documenting:
  - purpose / when to use it
  - post-provision customization (especially `base_url`)
  - egress requirements (which hosts to allow)
  - channel suggestions where applicable
- [x] **5.8** Added `pkg/common/role_defaults_test.go` —
  `TestRoleDefaults_AgentYAMLParses` walks
  `agents/_defaults/<runtime>/role-*/` and confirms every shipped
  `agent.yaml` passes the v2 loader (parse + Validate). Also asserts
  the Qwen-vs-Opus role split: Qwen roles declare `model:`, Opus
  roles leave it unset (inherit runtime default). 10 subtests pass
  (5 roles × 2 runtimes). `TestRoleDefaults_RoleMetaPresent` checks
  every role-* directory has a valid `role.meta`.

**Phase 5 acceptance**: directory tree complete; each agent.yaml
parses cleanly through the v2 loader; loader emits no warnings or
errors. ✅

---

## Phase 6 — CLI `--role` flag + interface parity

**Goal**: `--role <slug>` provisioned via CLI, JSON, and MCP. Idempotent
on existing agents (per QA persona note).

- [x] **6.1** `internal/cmd/admin.go` — `--role` flag added to both
  `add-user` and `add-team` commands (shared `adminRole` package var).
  - `internal/cmd/admin_provision.go` — `applyRolePackageIfRequested`
    helper resolves the role package, copies its defaults into the
    agent's overlay dir, and verifies the declared type matches the
    command's intent. Called from `adminAddUserRun` (cmdType="user")
    and `adminAddTeamRun` (cmdType="team") BEFORE
    `prov.ProvisionAgent` so the provider's overlay loader picks up
    the freshly-copied agent.yaml.
  - Spec-vs-implementation reconciliation: the CLI has separate
    `add-user` and `add-team` commands (no `--type` flag), so the
    mutex isn't between `--role` and `--type` — it's between
    `--role` and the implicit command-level type. Behavior:
    `add-user --role role-code-dev` errors with "role declares team;
    use `conga admin add-team --role role-code-dev` instead".
- [x] **6.2** Both `add-user` and `add-team` covered by 6.1 (same
  helper, same flag, different cmdType passed in).
- [x] **6.3** `internal/cmd/json_schema.go` — `role` field added to
  both `admin.add-user` and `admin.add-team` input schemas.
  Description references role.meta type requirement.
- [x] **6.4** `internal/mcpserver/tools_lifecycle.go` —
  `conga_provision_agent` gained two parameters: `role` (optional
  string) and `runtime` (optional string, since role lookup is
  per-runtime). Handler mirrors the CLI: calls
  `common.ApplyRolePackage` before `ProvisionAgent`, validates type
  matches the existing `type` parameter, returns clear error
  messages.
- [x] **6.5** Tests:
  - `pkg/common/role_package_test.go` — 14 tests covering happy
    path, slug normalization (operators can pass "ops" or "role-ops"),
    **idempotency** (QA persona requirement — agent.yaml customization
    preserved), role-not-found with available-roles list, role.meta
    missing / malformed (4 sub-cases), runtime isolation, partial
    role packages, `ResolveOperatorBehaviorDir` walk-up and
    no-repo cases
  - `internal/cmd/admin_provision_test.go` (new file) — 5 tests
    covering empty role is no-op, happy path, type-mismatch with
    helpful error message, missing role lookup, and the
    **idempotency** case end-to-end through
    `applyRolePackageIfRequested`
  - Per-agent USER.md.tmpl support added to
    `pkg/common/behavior.go::resolveBehaviorFiles` so role-copied
    `.tmpl` files are rendered with `{{.AgentName}}` + channel vars
    at deploy time, matching how runtime/type-default `.tmpl` files
    work today
- [x] **6.6** Full test suite green. `go vet ./...` clean. `gofmt -l`
  clean (gofmt applied automatic whitespace normalization to two
  new files during the run).

**Phase 6 acceptance**: all three interfaces (CLI, JSON, MCP) accept
`--role` / `"role"` parameter; idempotency preserved; available-role
error message present; type mismatch caught with actionable message. ✅

---

## Phase 7 — Docs

- [x] **7.1** `agents/_example/agent.yaml.example`:
  - bumped `version: 1` → `version: 2`; documented the v1/v2 split
    in the header comment (v1 still works for Feature #27 documents)
  - replaced the "reserved keys do not use" reserved-keys note for
    `subagents` (now claimed by v2) with a full documented
    `subagents:` block showing all fields + per-runtime applicability
  - kept the other reserved keys (memory/tools/limits/images/pdf/video)
- [x] **7.2** `product-knowledge/standards/config-taxonomy.md`:
  - extended the runtime overlay row to mention `subagents` and
    `agents/_defaults/` is now also committed (role packages live
    under `_defaults/<runtime>/role-*/`)
  - added Worked Example #5 ("Opus primary + Qwen subagent") with
    the full agent.yaml shape and a pointer to `conga admin
    add-user --role role-code-dev`
  - bumped doc Last Updated to 2026-05-22
- [x] **7.3** `CLAUDE.md`:
  - new "Delegation Model" section with the five-role catalog
    table, one paragraph on v2 overlay + `subagents:` block,
    upstream vocabulary map ("subagent" matches upstream;
    "delegate" is OpenClaw's org-identity concept), egress
    requirements, and the customization-before-first-use note
  - touched the per-agent overlay paragraph to reference v1 vs v2
- [x] **7.4** Role package READMEs (created in Phase 5) cross-
  checked for consistency — they were authored in the same pass and
  reference the same patterns as CLAUDE.md's new section.

**Phase 7 acceptance**: docs render cleanly; no stale "delegate"
references in the Tier-1 sense post-rename. ✅

---

## Phase 8 — Verification

- [ ] **8.1** `go test ./... -count=1` clean
- [ ] **8.2** `go vet ./...` clean
- [ ] **8.3** `gofmt -l .` returns nothing
- [ ] **8.4** Live smoke (local provider): provision
  `conga admin add-user --role code-dev --runtime openclaw` on a fresh
  agent name; chat through the gateway briefly; confirm subagent
  config in `~/.conga/data/<name>/openclaw.json`
- [ ] **8.5** Live smoke (AWS, optional): provision `--role ops` on
  the existing test environment; `conga refresh-all` succeeds; no
  regression on Feature #27 production agent
- [ ] **8.6** `/glados:verify-feature` workflow
- [ ] **8.7** Update PROJECT_STATUS.md: move feature to "Recent
  Changes" with completion notes; clear the implementation phase
  checkboxes.

**Phase 8 acceptance**: clean suite, live smokes attached to README
trace, feature marked complete in PROJECT_STATUS.
