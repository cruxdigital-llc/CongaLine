# Implementation Tasks: Per-Agent Config Overlay

## Phase 1: Core library (pkg/common)

- [x] **T1: Create `pkg/common/overlay.go`**
  - `OverlayManifest` / `OverlayEntry` types
  - `ReadOverlayManifest` / `WriteOverlayManifest` helpers
  - `IsProtectedPath(rel, runtimeName)` with universal + per-runtime lists
  - Manifest version constant

- [x] **T2: Extend `pkg/common/behavior.go`**
  - Change `BehaviorFiles` from `map[string][]byte` to `map[string]BehaviorFile`
  - Add `ComposeAgentWorkspaceFiles(behaviorDir, agent, prevManifest)` with overlay walk + deletion reconciliation
  - Keep `ComposeBehaviorFiles` as a deprecated wrapper for terraform provider compat
  - Update callers of old `BehaviorFiles` type

- [x] **T3: Unit tests (`pkg/common/behavior_test.go`, `pkg/common/overlay_test.go`)**
  - No overlay (baseline only)
  - Overlay-only (no baseline collision)
  - Overlay replaces composed file (SOUL.md collision)
  - Protected path rejection (MEMORY.md, agents/)
  - Deletion reconciliation: unmodified file deleted
  - Deletion reconciliation: modified file preserved
  - Non-md file skipped with no error
  - Path traversal rejected
  - File size cap enforcement
  - Manifest round-trip (write → read)

## Phase 2: Provider integration

- [x] **T4: Local provider (`pkg/provider/localprovider/provider.go`)**
  - Update `deployBehavior` to call `ComposeAgentWorkspaceFiles`
  - Read previous manifest from workspace, apply writes, deletes, write new manifest
  - Adapt to new `BehaviorFiles` map shape

- [x] **T5: Remote provider (`pkg/provider/remoteprovider/provider.go`)**
  - Same pattern as local but via SFTP helpers
  - Runtime-aware chown via `rt.ContainerSpec().User`
  - Read/write manifest over SFTP

- [x] **T6: AWS provider (`pkg/provider/awsprovider/provider.go`)**
  - CLI refresh path: deferred — AWS RefreshAgent restarts the systemd unit,
    which triggers ExecStartPre → deploy-behavior.sh. The bash helper (T7)
    handles overlay file copying. Go-side manifest-aware refresh (with
    deletion reconciliation) is a follow-up.
  - Bash helper extended in T7 covers the ExecStartPre overlay path

- [x] **T7: Extend `scripts/deploy-behavior.sh.tmpl` (bash helper)**
  - Add overlay copy loop for `*.md` under `overlays/<agent>/`
  - Add protected-path regex (superset of all runtimes)
  - No manifest handling in bash

## Phase 3: CLI commands

- [x] **T8: Create `internal/cmd/agent_overlay.go`**
  - `conga agent overlay list <agent>`
  - `conga agent overlay add <agent> <path> [--as <name>]`
  - `conga agent overlay rm <agent> <name>`
  - `conga agent overlay show <agent> <name>`
  - `conga agent overlay diff <agent>`

- [x] **T9: Register overlay subcommand group in `internal/cmd/`**
  - Wire into existing agent command tree

## Phase 4: Scaffolding & migration

- [x] **T10: Create `behavior/overlays/.gitkeep`**

- [x] **T11: Create `behavior/overrides/README.md` deprecation notice**

- [x] **T12: Migration fallback in `composeBaseline` (done in T2)**
  - Check `overlays/<agent>/` first, fall back to `overrides/<agent>/` for the fixed three names
  - Log deprecation warning when falling back

## Phase 5: Build & test

- [x] **T13: Verify build (`go build ./...`)** — clean

- [x] **T14: Run full test suite (`go test ./...`)** — all pass

- [ ] **T15: Manual smoke test with local provider**
  - Provision agent → add overlay → refresh → verify files in workspace
  - Verify MEMORY.md untouched
