# Tasks: behavior-to-agents-rename

Single-PR implementation checklist. Mirrors spec.md's phasing. Phase 8 (provider release) is intentionally **out of scope** for this PR — operator step post-merge.

## In scope (this PR)

### Phase 1 — Fallback loader (`pkg/common/`)
- [ ] Add path constants block at top of `pkg/common/behavior.go`: `agentsRootDir`, `defaultsSubdir`, `legacyBehaviorDir`, `legacyDefaultsSubdir`, `legacyPathFallbackEnabled`.
- [ ] Extract `resolveAgentDir(behaviorDir, agentName) (string, bool)` and `resolveDefaultDir(behaviorDir, rtName, agentType) (string, bool)` helpers. Both prefer new paths, fall back to legacy when fallback is enabled, return `isLegacy` flag for warn-once.
- [ ] Refactor `resolveBehaviorFiles` to use the helpers; emit `warnLegacyBehaviorPath` per legacy-resolved file.
- [ ] Add `warnLegacyBehaviorPath` helper using a shared `behaviorPathWarningOnce sync.Map`.
- [ ] Refactor `LoadAgentOverlay` in `pkg/common/overlay_agent.go` to try new path first, fall back to legacy with warn-once.

### Phase 1.5 — Tests (`pkg/common/`)
- [ ] `pkg/common/behavior_test.go`: 6 cases for prompts — new-only, legacy-only (with warning), both-prefer-new, defaults-new, defaults-legacy (with warning), defaults-both-prefer-new.
- [ ] `pkg/common/overlay_agent_test.go`: 2 cases — legacy-overlay-only (with warning), legacy-warning-emitted-once.

### Phase 2 — Move committed paths
- [ ] `git mv behavior/agents/_example agents/_example`
- [ ] `git mv behavior/default agents/_defaults`

### Phase 3 — Provider wiring
- [ ] `pkg/provider/localprovider/provider.go`:
  - `behaviorDir()` returns `<dataDir>/agents` (was `<dataDir>/behavior`).
  - `overlayBehaviorDir()` prefers `<repo_path>/agents`, falls back to `<repo_path>/behavior`, then snapshot.
- [ ] `pkg/provider/remoteprovider/provider.go`:
  - `remoteBehaviorDir()` returns `/opt/conga/agents` (was `/opt/conga/behavior`).
  - `deployBehavior` reads `<repo_path>/agents` (new); falls back to `<repo_path>/behavior`.
- [ ] `pkg/provider/awsprovider/channels.go`:
  - `resolveAWSBehaviorDir()` looks for `./agents` first, then `./behavior` via fallback. Walk-up logic prefers `<repo-root>/agents`, falls back to `<repo-root>/behavior`.

### Phase 4 — Terraform + bootstrap
- [ ] `terraform/modules/infrastructure/main.tf`:
  - `aws_s3_object.behavior` → `aws_s3_object.agents` with `moved {}` block.
  - `fileset(..., "behavior", "**")` → `fileset(..., "agents", "**")`.
  - S3 key prefix `conga/behavior/${each.value}` → `conga/agents/${each.value}`.
  - `terraform_data.behavior_refresh` → `terraform_data.agents_refresh` with `moved {}`.
- [ ] `terraform/modules/infrastructure/user-data.sh.tftpl`: `/opt/conga/behavior/` references → `/opt/conga/agents/`.
- [ ] Bootstrap shell script rename: `deploy-behavior.sh` → `deploy-agents.sh` (locate exact path), update S3 source + destination paths.

### Phase 5 — `.gitignore`
- [ ] Replace `behavior/agents/*/` + `!behavior/agents/_example/` block with `agents/*/` + `!agents/_example/` + `!agents/_defaults/`.

### Phase 6 — Migration script
- [ ] Create `scripts/migrate-behavior-to-agents.sh` per the spec body (idempotent, mv-based, defensive about partial state).
- [ ] Make executable (`chmod +x`).
- [ ] Add shell-level test (or document manual test steps if Go test harness can't drive shell tests).

### Phase 7 — Documentation
- [ ] `README.md`: Per-Agent Model Routing section + repo structure block + any other `behavior/` refs.
- [ ] `CLAUDE.md`: "Behavior files" section.
- [ ] `terraform/README.md`: Per-Agent Model Routing section.
- [ ] `product-knowledge/standards/architecture.md`: Config Format Boundary cross-link.
- [ ] `product-knowledge/standards/config-taxonomy.md`: taxonomy table + decision rule + worked examples.
- [ ] `product-knowledge/standards/security.md` + `egress-controls.md`: any path refs.
- [ ] `agents/_example/agent.yaml.example` (post-rename): inline comments.
- [ ] `specs/2026-05-19_feature_local-model-routing/README.md`: one-line historical note that paths have since been renamed.

### Verification
- [ ] `go test -count=1 ./...` passes.
- [ ] `go vet ./...` clean.
- [ ] `gofmt -l .` clean.
- [ ] `terraform plan` from `terraform/environments/production/`: `moved {}` blocks resolve cleanly (no resource replacement). If `moved {}` doesn't work with `for_each`, fall back to `terraform state mv` documented in a migration script.
- [ ] Smoke: run migration script against a tmpfs fixture; verify destination structure + idempotent re-run.

## Out of scope

| Phase | Reason |
|---|---|
| Phase 8 — Provider release | Per CLAUDE.md, `pkg/` changes require tagging + bumping `terraform-provider-conga` + republishing. Operator step, post-merge. |
| One-release deprecation cleanup | Separate PR after the next release. Single-line flip of `legacyPathFallbackEnabled = false` plus removal of the legacy branches. |
| Mass-edit of historical specs | Existing spec files under `specs/2026-*/` reference the old paths; left as historical artifacts. Only the immediate predecessor (`2026-05-19_*`) gets a one-line note at the top. |

## Branch / merge ordering

This branch is based on `feature/local-model-support` (PR #45). The intended flow:
1. #45 merges to main.
2. Rebase this branch onto main (`git rebase main` from this branch).
3. Resolve any conflicts in the docs that reference the new `agent.yaml` feature.
4. Push + flip PR #47 from draft to ready-for-review.

If #45 has not merged yet, the implementation can still proceed on this branch — the rebase will be straightforward because the file moves involved (`_example/agent.yaml.example`) are exactly the ones #45 introduced.

## Status

| Phase | Status |
|---|---|
| 1 — Fallback loader | ⏳ in progress |
| 1.5 — Tests | pending |
| 2 — git mv | pending |
| 3 — Provider wiring | pending |
| 4 — Terraform/bootstrap | pending |
| 5 — gitignore | pending |
| 6 — Migration script | pending |
| 7 — Docs | pending |
| Verification | pending |
