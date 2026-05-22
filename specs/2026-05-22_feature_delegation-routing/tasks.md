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

- [ ] **3.1** `pkg/runtime/hermes/config.go`:
  - new helper `applySubagentsOverlay` emitting `delegation:` YAML
  - `delegation.model: "<provider>/<name>"`
  - `delegation.max_concurrent_children` only when set
  - `delegation.max_spawn_depth` only when set
  - `delegation_mode` from overlay → not emitted (Hermes ignores)
  - degraded-mode path: openai provider + base_url not in Hermes'
    known adapter set → emit `delegation.model` only (omit
    `delegation.provider`) + one-time stderr warning via
    `overlayWarningOnce` pattern
- [ ] **3.2** Tests `pkg/runtime/hermes/config_test.go`:
  - `TestSubagentsOverlay_Hermes_OllamaInherit` — ollama → only model
    emitted, no provider
  - `TestSubagentsOverlay_Hermes_DegradedNoProvider` — openai + custom
    base_url → only model emitted, warning logged
  - `TestSubagentsOverlay_Hermes_NoBlock` — output identical to v1
  - `TestSubagentsOverlay_Hermes_MaxConcurrent` — value emitted as
    `max_concurrent_children`
- [ ] **3.3** Full test suite green.

**Phase 3 acceptance**: Hermes generator produces the documented shape;
degraded path is logged, not silent.

---

## Phase 4 — Egress check helper

**Goal**: Provisioning detects subagent endpoints missing from the
agent's effective egress allowlist; emits a clear warning; does NOT
block.

- [ ] **4.1** `pkg/common/egress_check.go` (new):
  - `CheckOverlayEgress(overlay *runtime.AgentOverlay,
    effectiveAllowlist []string) []string`
  - extracts hosts from `Model.BaseURL` and `Subagents.Model.BaseURL`
  - skips empty/null hosts (hosted Anthropic doesn't need an entry)
  - compares each derived host against allowlist (case-insensitive,
    treats `*.example.com` wildcard the existing way the policy module
    does)
  - returns missing-hosts slice (nil = all good)
- [ ] **4.2** Tests `pkg/common/egress_check_test.go` (new):
  - overlay with only `model.base_url` empty → returns nil
  - overlay with `subagents.model.base_url: litellm.lan` + allowlist
    without it → returns `["litellm.lan"]`
  - overlay with multiple endpoints all present → returns nil
  - overlay with wildcard match (`*.lan`) → returns nil
- [ ] **4.3** `internal/cmd/admin_provision.go`:
  - call `CheckOverlayEgress` after overlay load + effective allowlist
    resolution from the provider
  - emit one-line warnings per missing host (format from spec § "Egress
    integration / Provisioning-time check")
  - provider-specific resolution: AWS reads tfvars SSM, local/remote
    read `conga-policy.yaml` per-agent. Add a thin provider method or
    keep resolution in `internal/cmd/` if the provider interface
    already exposes the data
- [ ] **4.4** Tests `internal/cmd/admin_provision_test.go` (or new
  integration test): provisioning flow with a missing egress entry
  shows the warning; with all entries present, no warning.
- [ ] **4.5** Full test suite green.

**Phase 4 acceptance**: warnings visible at provision; provisioning
itself unaffected.

---

## Phase 5 — Role packages (catalog)

**Goal**: Ten directories under `agents/_defaults/<runtime>/role-*/`,
each with SOUL.md, AGENTS.md, USER.md.tmpl, agent.yaml, role.meta.

- [ ] **5.1** Create `agents/_defaults/openclaw/role-ops/`:
  - `role.meta` (`type: user`)
  - `SOUL.md` — Qwen-personality ops focus
  - `AGENTS.md` — monitoring/infra-checks emphasis
  - `USER.md.tmpl` — standard template
  - `agent.yaml` — v2, `model.provider: openai`, model name placeholder,
    base_url placeholder, NO subagents block
- [ ] **5.2** Create `agents/_defaults/openclaw/role-data/` (Qwen, no
  subagent)
- [ ] **5.3** Create `agents/_defaults/openclaw/role-research/` (Qwen,
  no subagent)
- [ ] **5.4** Create `agents/_defaults/openclaw/role-code-dev/`:
  - `agent.yaml` v2, `model.provider: anthropic`, model claude-opus-4-7,
    `subagents.model.provider: openai`, qwen-2.5-72b-instruct,
    `subagents.delegation_mode: prefer`, `subagents.max_concurrent: 4`
- [ ] **5.5** Create `agents/_defaults/openclaw/role-writing/` (Opus +
  Qwen subagent)
- [ ] **5.6** Mirror 5.1–5.5 under `agents/_defaults/hermes/`
- [ ] **5.7** Each role gets a tiny `README.md` (one paragraph: purpose,
  default model, suggested channels)
- [ ] **5.8** Verify each `agent.yaml` parses through the v2 loader
  (`go run ./cmd/conga agent show` against a synthetic test, or a unit
  test that walks the defaults tree and parses each one)

**Phase 5 acceptance**: directory tree complete; each agent.yaml
parses; loader doesn't warn or error.

---

## Phase 6 — CLI `--role` flag + interface parity

**Goal**: `--role <slug>` provisioned via CLI, JSON, and MCP. Idempotent
on existing agents (per QA persona note).

- [ ] **6.1** `internal/cmd/admin_provision.go`:
  - add `--role` flag (string, default `""`)
  - mutex with `--type` (error if both set)
  - resolve `agents/_defaults/<runtime>/role-<slug>/` (exists check;
    error with available roles list if not)
  - read `role.meta` → infer `type`
  - copy missing files from role default → `agents/<name>/`
    (PRESERVE existing files)
  - continue normal provisioning
- [ ] **6.2** Same flag on `add-team` (if a distinct command) — verify
  layout in `internal/cmd/`
- [ ] **6.3** `internal/cmd/json_schema.go`:
  - add `role` field to `AddUser` / `AddTeam` JSON schemas
  - mutex with `type` documented
- [ ] **6.4** `internal/mcpserver/tools_lifecycle.go`:
  - `conga_provision_agent` gains `role` string parameter
  - mutex enforcement in handler
- [ ] **6.5** Tests:
  - `internal/cmd/admin_provision_test.go` — `--role code-dev`
    happy-path; resolves role, copies files, infers type
  - **Idempotency test** (QA note): `--role code-dev` against a
    pre-existing agent dir with a customized agent.yaml — agent.yaml
    is preserved, only missing files copied
  - `--role X` + `--type Y` → CLI rejects
  - `--role unknown-slug --runtime openclaw` → error lists available
    roles for openclaw
  - JSON-input variant via `internal/cmd/json_schema_test.go`
  - MCP-tool variant via `internal/mcpserver/tools_lifecycle_test.go`
- [ ] **6.6** Full test suite green.

**Phase 6 acceptance**: all three interfaces accept `--role` / `role:`;
idempotency preserved; available-role error message present.

---

## Phase 7 — Docs

- [ ] **7.1** `agents/_example/agent.yaml.example`:
  - bump opening `version:` from 1 → 2
  - add `subagents:` block with all fields commented + a worked
    example
  - keep a "v1 still works" reference comment for back-compat
- [ ] **7.2** `product-knowledge/standards/config-taxonomy.md`:
  - extend the runtime overlay row to mention `subagents`
  - add Worked Example #5: "I want this agent to delegate mechanical
    work to a cheap model"
- [ ] **7.3** `CLAUDE.md`:
  - new section "Delegation Model" with:
    - the five-role catalog table
    - one paragraph on v2 overlay + `subagents:` block
    - upstream vocabulary map (subagent → `sessions_spawn` /
      `delegate_task`; "delegate" is upstream's org-identity concept)
- [ ] **7.4** Update `agents/_defaults/openclaw/<role>/README.md`
  files (already covered in Phase 5.7) — cross-check consistency.

**Phase 7 acceptance**: docs render cleanly; manual scan for stale
references to "delegate" in the Tier-1 sense (we should never use that
term post-rename).

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
