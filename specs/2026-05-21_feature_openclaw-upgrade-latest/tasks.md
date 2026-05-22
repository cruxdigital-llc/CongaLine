# Implementation Tasks: OpenClaw Upgrade Latest

Derived from `spec.md`. Each task is small and independently reviewable.

## Phase 0 — Upstream changelog audit (BLOCKING)

- [ ] **T0.1** Pull `https://raw.githubusercontent.com/openclaw/openclaw/v2026.5.18/CHANGELOG.md` (full file).
- [ ] **T0.2** Slice the entries between `v2026.3.12` (inclusive) and `v2026.5.18` (inclusive).
- [ ] **T0.3** Classify each entry as **Blocking** / **Adjacent** / **Irrelevant** per the rubric in spec.md §"Phase 0".
- [ ] **T0.4** Write `specs/2026-05-21_feature_openclaw-upgrade-latest/changelog-review.md` listing every Adjacent entry with the verification scenario it maps to (S1–S5).
- [ ] **T0.5** **Gate**: if any Blocking entry exists → STOP, summarise it to the user, open a separate migration spec; do NOT proceed to Phase 1.

## Phase 1 — Repository changes (single commit)

Only after Phase 0 clears.

### A. In-repo defaults (5 files)

- [ ] **T1.A1** `terraform/environments/production/variables.tf:60` — tag bump.
- [ ] **T1.A2** `terraform/modules/congaline/variables.tf:4` — tag bump.
- [ ] **T1.A3** `terraform/modules/infrastructure/variables.tf:49` — tag bump.
- [ ] **T1.A4** `pkg/provider/remoteprovider/setup.go:190` — tag bump.
- [ ] **T1.A5** `pkg/provider/remoteprovider/setup.go:193` — tag bump.

### B. Docs + standards (7 edits across 4 files)

- [ ] **T1.B1** `CLAUDE.md:92` — rewrite pin paragraph: new tag, note #45311 resolved in v2026.3.22 (PR #45953), drop the "we'd rather build against current" framing in favour of "pin specific minors for stability/bisectability".
- [ ] **T1.B2** `README.md:67` — tag in TECH_STACK table.
- [ ] **T1.B3** `README.md:717` — rewrite the "Pinned to v2026.3.11" sentence.
- [ ] **T1.B4** `README.md:720` — tag in code block.
- [ ] **T1.B5** `README.md:723` — **delete** the "Once the bug ... we'll update" note (bug is fixed; updating now).
- [ ] **T1.B6** `product-knowledge/standards/security.md:41` — Universal Baseline row: new tag + rationale rewrite (stability/bisectability, not v2026.3.12-specific).
- [ ] **T1.B7** `product-knowledge/TECH_STACK.md:38` — tag in tech-stack table.

### C. Tests + CI (6 edits across 4 files)

- [ ] **T1.C1** `internal/cmd/json_schema.go:43` — JSON-schema example string.
- [ ] **T1.C2** `internal/cmd/integration_helpers_test.go:26` — `const testImage` bump.
- [ ] **T1.C3** `pkg/manifest/manifest_test.go:14` — YAML fixture image string.
- [ ] **T1.C4** `pkg/manifest/manifest_test.go:55` — expected assertion (must match fixture).
- [ ] **T1.C5** `.github/workflows/ci.yml:58` — cache key.
- [ ] **T1.C6** `.github/workflows/ci.yml:65, :67` — both `docker pull` and `docker save` lines.

### Verification of the edit itself (mechanical, not feature-level)

- [ ] **T1.V1** `grep -rn "2026\.3\.11" --include="*.go" --include="*.md" --include="*.tf" --include="*.tftpl" --include="*.yaml" --include="*.yml" --include="*.json" --include="*.sh" .` returns **only** historical references inside `specs/` and `product-knowledge/PROJECT_STATUS.md:210` (the deferred-Phase-1 line in the Local Model Routing entry, which is history not config).
- [ ] **T1.V2** `go build ./...` succeeds.
- [ ] **T1.V3** `go vet ./...` clean.
- [ ] **T1.V4** `gofmt -l .` shows no files needing formatting.
- [ ] **T1.V5** Affected unit tests pass: `go test ./pkg/manifest/...` and `go test ./internal/cmd/...` (the latter without `-tags integration` so we don't actually pull containers).
- [ ] **T1.V6** Commit the change as a single commit using the message in spec.md §"Phase 1".

## Phases 3–5 — Operator-driven (NOT part of this implementation)

Phase 3 (5-scenario verification) and Phase 4 (per-environment rollout) and Phase 5 (post-rollout hygiene including memory + soak window tracking) all require running real environments and are the operator's responsibility. They are documented in `spec.md` and tracked in `PROJECT_STATUS.md` Feature #28 but are not Claude's to execute.

## What this implementation will produce

1. `changelog-review.md` artifact (or a STOP-gate report if blocking entries exist).
2. One commit containing the 14-file edit catalogued in Phase 1.
3. Trace updates in `README.md`.

## What it will NOT produce

- Pushed branches or PRs (operator-controlled).
- Live environment verification.
- Memory updates (those happen post-rollout in Phase 5).
- A bumped `pkg/runtime/openclaw/container.go:23` `:latest` fallback (explicitly out of scope).
