# Specification: Per-Agent Config Overlay

## 0. Summary

Extend congaline's behavior seeding so each managed agent can carry a set
of arbitrary markdown files layered on top of the type-specific baseline.
This supersedes the existing `behavior/overrides/` directory, which only
supported wholesale replacement of a fixed three-file set.

The design is built around three invariants:
1. **Additive by default** — overlays add files, they don't rewrite the baseline.
2. **Never clobber agent-mutable state** — `MEMORY.md` and peers are off-limits.
3. **Idempotent and refreshable** — running refresh twice produces the same workspace.

> **Updated 2026-04-07** to account for the agent portability PR (#34),
> which introduced the `Runtime` interface (`pkg/runtime/runtime.go`),
> Hermes as a second runtime, `AgentConfig.Runtime` field, Telegram as a
> new channel, and runtime-dependent workspace paths / directory layouts.
> Affected sections: §1.2, §2.1, §3.1, §4.x, §5, §9, §12.

## 1. File Layout

### 1.1 Source tree (repo-side)

```
behavior/
  base/
    SOUL.md
    AGENTS.md
  user/
    SOUL.md
    AGENTS.md
    USER.md.tmpl
  team/
    SOUL.md
    AGENTS.md
    USER.md.tmpl
  overlays/                     # NEW
    .gitkeep
    <agent_name>/
      CLIENT.md                 # arbitrary per-agent markdown
      PROJECT.md
      TEAM.md
      SOUL.md                   # optional — replaces composed SOUL.md
  overrides/                    # DEPRECATED — read-only fallback, removed after one release
```

Rules for `behavior/overlays/<agent_name>/`:
- Directory name must exactly match an agent name as known to the active provider.
- Only `*.md` files are honored. Other extensions are skipped with a warning.
- Paths may be nested (e.g. `overlays/acme-eng/docs/CLIENT.md`), but all
  nested paths resolve into the agent's **workspace root** with their
  parent directories preserved under the workspace. Path traversal
  (`..`) is rejected.
- Files on the protected path list (§5) are rejected with an error.

### 1.2 Workspace layout (container-side)

The agent's workspace root varies by **provider** and **runtime**. The
`runtime.Runtime.WorkspacePath()` method returns the relative path within
the agent's data directory (see `pkg/runtime/runtime.go:55-57`):

| Runtime  | `WorkspacePath()` | Example (local provider) |
|----------|-------------------|--------------------------|
| openclaw | `data/workspace`  | `~/.conga/data/<agent>/data/workspace/` |
| hermes   | `workspace`       | `~/.conga/data/<agent>/workspace/` |

For remote and AWS, substitute `~/.conga/data/` with `/opt/conga/data/`.

The overlay phase resolves the workspace root using the runtime — matching
the existing `deployBehavior` pattern in the local provider
(`provider.go:1672-1676`), which already calls `rt.WorkspacePath()`.

Inside the workspace after seeding (OpenClaw example):

```
workspace/
  SOUL.md                           # composed baseline (or overlay replacement)
  AGENTS.md                         # composed baseline (or overlay replacement)
  USER.md                           # rendered template (or overlay replacement)
  CLIENT.md                         # overlay file
  PROJECT.md                        # overlay file
  TEAM.md                           # overlay file
  MEMORY.md                         # agent-mutable — NEVER touched by seeding
  .conga-overlay-manifest.json      # manifest of files placed by overlay phase
  memory/                           # agent-mutable subtree — NEVER touched
  logs/                             # agent-mutable subtree — NEVER touched
  agents/                           # agent-mutable subtree — NEVER touched (OpenClaw)
```

Hermes layout is similar but with `skills/` instead of `agents/`,
and no `canvas/`, `cron/`, `devices/`, `identity/`, or `media/` subtrees.
See §5 for the runtime-aware protected path list.

### 1.3 Distribution of the overlay source to providers

- **local**: `copyDir` in `LocalProvider.SetupEnvironment` already copies
  `behavior/` → `~/.conga/behavior/`. No change — `overlays/` is carried
  along automatically.
- **remote**: `remoteprovider.Setup` pushes `behavior/` to
  `/opt/conga/behavior/` via SFTP. Extend the push to include
  `behavior/overlays/`. Deletions on the remote are handled by a mirror
  step that removes paths under `/opt/conga/behavior/overlays/` not
  present in the local source (prevents stale overlays from outliving a
  `conga agent overlay rm`).
- **aws**: `terraform/behavior.tf` currently uploads each file under
  `behavior/**/*` to S3 as an individual object. This works unchanged for
  `overlays/`. The IAM policy already grants `s3:GetObject` on
  `conga/behavior/*`, so no IAM change is required. The bootstrap-time
  `aws s3 sync s3://.../conga/behavior/ /opt/conga/behavior/` picks up
  the new tree automatically.

## 2. Data Model

### 2.1 Manifest

The manifest is the source of truth for "what did the overlay phase put
here last time". It lives inside the workspace and is treated as
provider-managed metadata.

**Path**: `<workspace>/.conga-overlay-manifest.json`
**Mode**: 0644, owned by the runtime's container user (OpenClaw: uid 1000,
Hermes: uid 0). The provider resolves this from
`runtime.Runtime.ContainerSpec().User`.

**Schema** (`pkg/common/overlay.go`):

```go
type OverlayManifest struct {
    Version int             `json:"version"` // 1
    Files   []OverlayEntry  `json:"files"`
}

type OverlayEntry struct {
    Path   string `json:"path"`   // workspace-relative, forward slashes
    SHA256 string `json:"sha256"` // hex-encoded
    Source string `json:"source"` // "overlay" | "composed"
}
```

`Source` discriminates overlay-placed files from baseline-composed files
so the deletion logic (§3.3) only reclaims overlay files when the
composition rules change.

### 2.2 `BehaviorFiles` map extension

`pkg/common/behavior.go` today defines:

```go
type BehaviorFiles map[string][]byte
```

This is extended to carry per-file metadata for the manifest:

```go
type BehaviorFile struct {
    Content []byte
    Source  string // "composed" | "overlay"
}

type BehaviorFiles map[string]BehaviorFile
```

Existing callers (local/remote/aws provider `deployBehavior`
implementations) are updated to iterate the new map shape. Keys remain
workspace-relative paths (e.g. `"SOUL.md"`, `"docs/CLIENT.md"`).

## 3. Composition Pipeline

### 3.1 Entry point

Replace the current `ComposeBehaviorFiles` with a richer entry point.
The function is **runtime-agnostic** — it operates on the source tree
only. The caller (provider) is responsible for resolving the workspace
path via `runtime.Runtime.WorkspacePath()` and passing the previous
manifest read from that workspace.

```go
// pkg/common/behavior.go
func ComposeAgentWorkspaceFiles(
    behaviorDir string,
    agent provider.AgentConfig,
    prevManifest *OverlayManifest,
) (files BehaviorFiles, toDelete []string, next OverlayManifest, err error)
```

Steps, in order:
1. **Baseline compose** — existing logic for `SOUL.md`, `AGENTS.md`,
   `USER.md`. Results are tagged `Source: "composed"`.
2. **Overlay walk** — `filepath.WalkDir(behaviorDir/overlays/<agent>)`,
   filtering to `*.md`. Each file becomes a `BehaviorFile` with
   `Source: "overlay"`.
3. **Protected path check** — any overlay path matching §5 returns an error.
4. **Overlay merge** — overlay entries are written into the map after
   composed entries. On collision (same key), the overlay entry wins and
   its `Source` becomes `"overlay"`, so subsequent deletion logic knows
   this file is overlay-managed.
5. **Deletion reconciliation** — compute `toDelete` by diffing
   `prevManifest` against the new file set (§3.3).
6. **Manifest construction** — `next` contains one entry per file placed,
   with SHA-256 of the final content.

`ComposeBehaviorFiles` (old name) becomes a thin wrapper that calls
`ComposeAgentWorkspaceFiles` with `prevManifest=nil` and discards
`toDelete` — kept so the terraform provider repo doesn't break on a
version bump. Marked deprecated; removed after one release.

### 3.2 Overlay walk details

```go
root := filepath.Join(behaviorDir, "overlays", agent.Name)
if _, err := os.Stat(root); os.IsNotExist(err) {
    return files, nil, manifest, nil // no overlay — valid
}
err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
    if err != nil { return err }
    if d.IsDir() { return nil }
    rel, _ := filepath.Rel(root, path)
    rel = filepath.ToSlash(rel)
    if strings.HasPrefix(rel, "../") || rel == ".." {
        return fmt.Errorf("overlay path escapes root: %s", rel)
    }
    if !strings.HasSuffix(strings.ToLower(rel), ".md") {
        log.Warn("skipping non-markdown overlay file", "path", rel)
        return nil
    }
    if isProtectedPath(rel) {
        return fmt.Errorf("overlay file %s is on the protected path list", rel)
    }
    data, err := os.ReadFile(path)
    if err != nil { return err }
    files[rel] = BehaviorFile{Content: data, Source: "overlay"}
    return nil
})
```

### 3.3 Deletion reconciliation

For each entry in `prevManifest.Files` where `Source == "overlay"`:
- If the path is still in the new `files` map → kept, no-op.
- If the path is not in the new map:
  - Provider re-hashes the file at the workspace path.
  - Hash matches `prevManifest` entry → append to `toDelete`.
  - Hash differs → log `WARNING: overlay file <path> was modified in
    workspace; not deleting — remove it manually if desired`. The path
    is **not** added to the new manifest, so the next refresh with the
    overlay still missing will re-check and eventually reclaim if the
    agent stops modifying it.

Providers are responsible for executing `toDelete` via their native
mechanism (`os.Remove` for local, SFTP delete for remote, `rm` over SSM
for AWS).

### 3.4 Manifest persistence

Providers write `next` as `.conga-overlay-manifest.json` in the workspace
**after** all file writes and deletions succeed. If any step fails, the
old manifest remains — guaranteeing the next refresh retries the same
reconciliation rather than losing track of managed files.

## 4. Provider Integration

### 4.1 Local provider

`pkg/provider/localprovider/provider.go` — replace `deployBehavior`.
The existing function already resolves the workspace path via the runtime
interface (`p.runtimeForAgent(cfg)` → `rt.WorkspacePath()`, see
`provider.go:1672-1676`). The new version preserves that pattern and adds
manifest + deletion handling:

```go
func (p *LocalProvider) deployBehavior(cfg provider.AgentConfig) error {
    behaviorDir := p.behaviorDir()
    if _, err := os.Stat(behaviorDir); os.IsNotExist(err) {
        return nil
    }

    // Resolve workspace path from the runtime (OpenClaw: "data/workspace",
    // Hermes: "workspace") — same pattern as the existing deployBehavior.
    workspaceSub := "data/workspace" // default (OpenClaw)
    if rt, rtErr := p.runtimeForAgent(cfg); rtErr == nil {
        workspaceSub = rt.WorkspacePath()
    }
    workspaceDir := filepath.Join(p.dataSubDir(cfg.Name), workspaceSub)
    if err := os.MkdirAll(workspaceDir, 0755); err != nil {
        return err
    }

    prev, _ := common.ReadOverlayManifest(workspaceDir) // nil-safe
    files, toDelete, next, err := common.ComposeAgentWorkspaceFiles(behaviorDir, cfg, prev)
    if err != nil {
        return err
    }

    // Writes
    for relPath, f := range files {
        target := filepath.Join(workspaceDir, relPath)
        if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
            return err
        }
        if err := os.WriteFile(target, f.Content, 0644); err != nil {
            return err
        }
    }
    // Deletes (deletion reconciliation, §3.3)
    for _, relPath := range toDelete {
        _ = os.Remove(filepath.Join(workspaceDir, relPath))
    }
    // Manifest
    return common.WriteOverlayManifest(workspaceDir, next)
}
```

No changes needed to the `MEMORY.md` seeding — OpenClaw's
`CreateDirectories()` in `pkg/runtime/openclaw/dirs.go` pre-creates the
file with `# Memory\n` content. Hermes's `CreateDirectories()` does not
create a `MEMORY.md` (different runtime convention). In both cases the
overlay phase never touches `MEMORY.md` because it is on the protected
list.

### 4.2 Remote provider

`pkg/provider/remoteprovider/provider.go` — mirror the local pattern but
route writes/deletes through the existing SSH/SFTP helpers:

The remote workspace path is resolved via the runtime, same as local.
File ownership must use the runtime's container UID (OpenClaw: 1000,
Hermes: 0 — parsed from `rt.ContainerSpec().User`):

```go
rt, _ := p.runtimeForAgent(cfg)
workspaceSub := rt.WorkspacePath()
uid, gid := parseContainerUser(rt.ContainerSpec(cfg).User) // "1000:1000" → 1000, 1000

files, toDelete, next, err := common.ComposeAgentWorkspaceFiles(localBehaviorDir, cfg, prev)
// ...
for relPath, f := range files {
    remotePath := path.Join(remoteWorkspaceDir, relPath)
    if err := p.sftpMkdirAll(path.Dir(remotePath)); err != nil { return err }
    if err := p.sftpWrite(remotePath, f.Content, 0644); err != nil { return err }
    if err := p.sftpChown(remotePath, uid, gid); err != nil { return err }
}
for _, relPath := range toDelete {
    _ = p.sftpRemove(path.Join(remoteWorkspaceDir, relPath))
}
return p.sftpWriteJSON(path.Join(remoteWorkspaceDir, ".conga-overlay-manifest.json"), next)
```

`prev` is fetched via SFTP read of the remote manifest; absent/malformed
treated as nil (first-run behavior).

### 4.3 AWS provider

The AWS flow is the only one that composes on the host via a bash helper
(`deploy-behavior.sh`). Two options were considered:

| Option | Pros | Cons |
|--------|------|------|
| **A. Port composition to Go, ship via SSM uploadFile** | Single implementation, manifest semantics preserved, no bash string munging | New SSM round-trips per refresh; one provision slower |
| **B. Extend `deploy-behavior.sh` to handle overlays via a JSON manifest from S3** | Keeps host-side composition on the hot path (systemd ExecStartPre runs on every container start) | Two implementations drift; bash hashing / manifest reconciliation is painful |

**Decision: Option A for the CLI path, Option B (simplified) for the
ExecStartPre path.**

Rationale: the CLI-initiated refresh is where overlay changes land — this
must use the Go composition for correctness, manifest writing, and error
reporting. The systemd ExecStartPre only needs to re-apply the latest S3
state on a plain restart; it does **not** need to handle deletions
(deletions happen when an operator runs refresh, not when the container
restarts on its own).

Concretely for AWS:
- **CLI refresh path** (`awsprovider.RefreshAgent`): calls
  `common.ComposeAgentWorkspaceFiles` in Go, then uses SSM
  `uploadFile` + SSM `runOnInstance` to write the files and the manifest
  and execute deletions. Minimum `timeoutSeconds=30` per existing
  convention (see CLAUDE.md).
- **ExecStartPre path** (bash, on container restart): `deploy-behavior.sh`
  is extended to (a) continue composing SOUL/AGENTS/USER as today, and
  (b) copy any `*.md` files under `/opt/conga/behavior/overlays/<agent>/`
  into the workspace, skipping anything on a hard-coded protected list
  matching §5. It does **not** read or write the manifest, and it does
  **not** delete anything. This is a strict subset of the Go behavior
  — safe because deletion and manifest reconciliation are only needed
  when the overlay source actually changes, which only happens via a
  CLI-initiated refresh.

The bash protected list uses the conservative superset from §5:

```bash
PROTECTED_REGEX='^(MEMORY\.md|memory/|logs/|agents/|skills/|canvas/|cron/|devices/|identity/|media/|\.conga-overlay-manifest\.json)'
```

## 5. Protected Path List

Different runtimes have different mutable-state directories (OpenClaw:
`agents/`, `canvas/`, `cron/`, `devices/`, `identity/`, `media/`;
Hermes: `skills/`). The protected path list is split into a **universal**
set (shared across all runtimes) and **per-runtime** extensions.

Defined in `pkg/common/overlay.go`:

```go
// Universal protected paths — apply to every runtime.
var protectedWorkspacePaths = []string{
    "MEMORY.md",
    "memory/",
    "logs/",
    ".conga-overlay-manifest.json",
}

// Per-runtime protected paths, keyed by runtime.RuntimeName.
var runtimeProtectedPaths = map[runtime.RuntimeName][]string{
    runtime.RuntimeOpenClaw: {"agents/", "canvas/", "cron/", "devices/", "identity/", "media/"},
    runtime.RuntimeHermes:   {"skills/"},
}

func IsProtectedPath(rel string, rt runtime.RuntimeName) bool {
    rel = filepath.ToSlash(rel)
    for _, p := range protectedWorkspacePaths {
        if rel == p { return true }
        if strings.HasSuffix(p, "/") && strings.HasPrefix(rel, p) { return true }
    }
    for _, p := range runtimeProtectedPaths[rt] {
        if rel == p { return true }
        if strings.HasSuffix(p, "/") && strings.HasPrefix(rel, p) { return true }
    }
    return false
}
```

`ComposeAgentWorkspaceFiles` resolves the runtime name from
`agent.Runtime` (via `runtime.ResolveRuntime`) and passes it to
`IsProtectedPath`. This means `pkg/common` imports `pkg/runtime` for the
`RuntimeName` type only — no circular dependency since `runtime` does not
import `common`.

The bash helper (`deploy-behavior.sh`) mirrors a conservative superset
regex covering all runtimes:

```bash
PROTECTED_REGEX='^(MEMORY\.md|memory/|logs/|agents/|skills/|canvas/|cron/|devices/|identity/|media/|\.conga-overlay-manifest\.json)'
```

Any overlay file matching this list **fails the refresh with an error**;
it is not silently skipped. This is deliberate — an operator who drops a
`MEMORY.md` into the overlay tree almost certainly has a misunderstanding
we want to surface loudly.

## 6. CLI Surface

New command family `conga agent overlay`:

### 6.1 `conga agent overlay list <agent>`

List overlay files in the source tree for an agent.

```
$ conga agent overlay list acme-eng
behavior/overlays/acme-eng/
├── CLIENT.md     (2.1 KB, modified 2026-04-03)
├── PROJECT.md    (8.4 KB, modified 2026-04-04)
└── TEAM.md       (1.2 KB, modified 2026-04-01)
```

### 6.2 `conga agent overlay add <agent> <path> [--as <name>]`

Copy a local file into `behavior/overlays/<agent>/`. `--as` lets the
operator rename on copy.

```
$ conga agent overlay add acme-eng ~/notes/client-brief.md --as CLIENT.md
Copied client-brief.md → behavior/overlays/acme-eng/CLIENT.md
Run 'conga refresh --agent acme-eng' to deploy.
```

Validation (fails the command, no partial writes):
- `<agent>` exists in the active provider's agent list.
- Source file exists and is `.md`.
- Target name is not on the protected list.
- Target name contains no path traversal.

### 6.3 `conga agent overlay rm <agent> <name>`

Remove a file from the overlay source. Does not touch the workspace —
the deletion propagates on the next refresh via the manifest logic (§3.3).

```
$ conga agent overlay rm acme-eng OLD_CLIENT.md
Removed behavior/overlays/acme-eng/OLD_CLIENT.md
Run 'conga refresh --agent acme-eng' to apply (workspace file will be
deleted if unmodified since last refresh).
```

### 6.4 `conga agent overlay show <agent> <name>`

Cat an overlay source file to stdout.

### 6.5 `conga agent overlay diff <agent>`

Compare overlay source to workspace. Useful for spotting agent-modified
files that won't be reclaimed on refresh:

```
$ conga agent overlay diff acme-eng
CLIENT.md       in-sync
PROJECT.md      MODIFIED IN WORKSPACE (would NOT be reclaimed if removed from source)
TEAM.md         in-sync
OLD_NOTES.md    in workspace only (orphaned, not in source or manifest)
```

Works identically across providers by reading the workspace via the
provider's `container_exec` / direct-fs / SSH-cat path.

### 6.6 Help integration

Registered in `internal/cmd/agent.go` (new file `internal/cmd/agent_overlay.go`).
The top-level `agent` command already exists for agent-scoped operations;
overlay fits naturally under it.

## 7. Refresh and Provision Flow

### 7.1 Provision (`conga admin add-user`, `conga admin add-team`)

No change to the CLI command. The provider's `ProvisionAgent` /
`RefreshAgent` calls `deployBehavior`, which now includes the overlay
phase. If the operator hasn't yet created an overlay directory for the
agent, the overlay walk is a no-op and provisioning proceeds with baseline
composition only — 100% backwards compatible.

### 7.2 Refresh (`conga refresh --agent <name>`, `admin refresh-all`)

`RefreshAgent` already calls `deployBehavior` before restarting the
container. No flow change — the overlay phase runs inside the existing
call. Because manifest reconciliation produces a `toDelete` list that the
provider applies **before** writing the new manifest, refresh is
idempotent: running it twice produces the same workspace and the same
manifest content.

### 7.3 Container restart (systemd)

On AWS only: the systemd `ExecStartPre` runs `deploy-behavior.sh` on
every container start. With the bash subset (§4.3), this re-applies the
latest overlay files from the S3-synced tree but does **not** reclaim
deleted files — deletion only happens via CLI refresh. This is safe
because:
- A CLI refresh always happens when an operator removes an overlay file.
- A plain container restart (OOM, daily reboot, manual `systemctl
  restart`) never has a reason to delete files the operator didn't touch.

## 8. Schema & Validation

### 8.1 Source-tree validation (`conga admin validate` or implicit on refresh)

- Every directory under `behavior/overlays/` must match an existing
  agent name. Stray directories → warning, not error.
- Every file under `behavior/overlays/<agent>/` must be `.md`. Non-md →
  warning, skipped.
- No file may match the protected path list → hard error.
- No file may contain `..` in its relative path → hard error.
- File size cap: 1 MiB per file (sanity bound against accidentally
  committing a blob). Above the cap → hard error with the offending path.

### 8.2 Manifest schema validation

On read, the manifest must parse as JSON and have `Version == 1`. Any
other value → treat as nil (first-run). Unknown fields are tolerated for
forward compatibility.

## 9. Edge Cases

| Scenario | Behavior |
|----------|----------|
| Overlay directory exists but is empty | No overlay files placed; composed baseline only. Previous manifest (if any) processed for deletions. |
| Overlay file renamed (e.g. `CLIENT.md` → `CLIENT_INFO.md`) | Old path deleted (manifest reclaim), new path written. Effectively a move. |
| Agent edits an overlay file at runtime | On next refresh, the diff logic detects drift and leaves the file alone with a warning. Operator must `conga agent overlay diff` to see it. |
| Overlay has a `SOUL.md` (same name as composed) | Composed version is overwritten in the map; final workspace has the overlay content. `Source = "overlay"` in the manifest. |
| Two agents share an overlay file | Not supported in v1 — overlays are strictly per-agent. Operators duplicate the file across directories or use git to maintain a shared source. |
| Workspace is missing (first provision) | `deployBehavior` creates it; manifest is nil; overlay walk populates from scratch. |
| Manifest file corrupted | Treated as nil. Next refresh writes a fresh manifest covering the current overlay set. Deletion reconciliation is skipped this pass (can't trust prev state). A warning is logged. |
| Operator runs `rm -rf behavior/overlays/<agent>/` | Next refresh: `prevManifest` lists N files, new overlay phase returns zero, all N go into `toDelete`, each is hash-checked and removed if unmodified. |
| File in overlay source larger than 1 MiB | Hard error at refresh; refresh aborts with the offending path. Overlay source and workspace both unchanged. |
| Non-`.md` file in overlay source | Warning, skipped. Does not block refresh. |
| Agent doesn't exist for a stray `overlays/<bogus>/` dir | Warning at refresh of any agent; no error. Operator cleans up manually. |
| Refresh fails mid-write on remote (SFTP error) | Old manifest remains. Next refresh retries the same reconciliation. Workspace may have partial new files; the next successful refresh converges. |
| `.conga-overlay-manifest.json` shows up in overlay source | Hard error (protected path list). |
| OpenClaw agent with `agents/` overlay | Hard error — `agents/` is on OpenClaw's protected list. |
| Hermes agent with `skills/` overlay | Hard error — `skills/` is on Hermes's protected list. *(Theoretical — Hermes not tested in v1.)* |
| Hermes agent with `agents/` overlay | Allowed — `agents/` is not a Hermes-mutable path. *(Theoretical — Hermes not tested in v1.)* |
| Agent switches runtime (e.g. openclaw → hermes) | Workspace path changes. Previous manifest lives in the old workspace and is effectively orphaned. New workspace starts fresh (manifest nil). Operator should teardown + re-provision when changing runtimes. |

## 10. Security Considerations

- **No new trust boundaries.** Overlay files are markdown with the same
  trust level as the existing `behavior/` tree — committed to a repo the
  operator controls.
- **No secret material.** Overlay files are world-readable inside the
  container. Operators must not put credentials in them. This is
  enforced by convention, same as today's behavior files. A lint could
  grep for common secret patterns, but is out of scope for v1.
- **Path traversal.** The walk explicitly rejects `..` in computed
  relative paths and constructs all target paths via `filepath.Join`
  from the workspace root.
- **Protected path enforcement runs before any write.** An overlay that
  targets `MEMORY.md` fails the whole refresh; no partial state.
- **IAM unchanged.** On AWS, `behavior/overlays/` is under the existing
  `conga/behavior/*` S3 prefix; no IAM policy changes needed.
- **No new egress.** The overlay pipeline touches only the local/remote
  filesystem and (on AWS) S3 + SSM — all existing permitted paths.

## 11. Observability

- Every refresh logs a concise summary: `overlay: +3 -1 ~0` (three new,
  one reclaimed, zero unchanged).
- Each protected-path rejection logs the agent and the path.
- Each skipped non-md file logs the path at `warn`.
- The manifest includes a writer timestamp (`"written_at"` optional
  field, not part of the schema version check) for field debugging.

## 12. File Manifest (this feature)

| File | Action | Description |
|------|--------|-------------|
| `pkg/common/behavior.go` | Modify | Rename `ComposeBehaviorFiles` → `ComposeAgentWorkspaceFiles`, change return type, add overlay walk + protected-path check |
| `pkg/common/overlay.go` | Create | Manifest type, read/write helpers, `isProtectedPath` |
| `pkg/common/behavior_test.go` | Create/extend | Unit tests for overlay composition, protection, reconciliation |
| `pkg/provider/localprovider/provider.go` | Modify | `deployBehavior`: integrate manifest read, write, delete reconciliation |
| `pkg/provider/remoteprovider/provider.go` | Modify | Same integration via SFTP |
| `pkg/provider/awsprovider/provider.go` | Modify | `RefreshAgent`: Go-side composition, SSM upload/delete, manifest write |
| `cli/scripts/deploy-behavior.sh.tmpl` | Modify | Add overlay copy loop with protected-path regex; no manifest handling |
| `internal/cmd/agent_overlay.go` | Create | `list`, `add`, `rm`, `show`, `diff` subcommands |
| `internal/cmd/agent.go` | Modify | Register `overlay` subcommand group |
| `behavior/overlays/.gitkeep` | Create | Placeholder so the directory ships empty |
| `behavior/overrides/README.md` | Create | Deprecation notice pointing at `overlays/` |
| `terraform/behavior.tf` | No change | Existing glob `**/*` already covers the new subtree |
| `terraform/iam.tf` | No change | Existing `conga/behavior/*` prefix covers overlays |
| `specs/2026-04-04_feature_per-agent-config-overlay/` | Create | This spec |

## 13. Migration

- `behavior/overrides/<agent>/SOUL.md` etc. continue to work for one
  release. On read, the compose helper checks `overlays/<agent>/` first,
  then falls back to `overrides/<agent>/` for the fixed three names and
  logs a deprecation warning.
- The deprecation warning points the operator at `conga agent overlay
  add <agent> overrides/<agent>/SOUL.md` (or just `git mv`) to move.
- Removal happens in the release after the migration window.

## 14. Multi-Runtime Considerations

The agent portability PR (#34) introduced the `Runtime` interface
(`pkg/runtime/runtime.go`) with OpenClaw and Hermes as implementations.
The overlay feature is designed to be runtime-aware, but **v1
implementation targets OpenClaw only**. Hermes support is scaffolded
(protected paths, workspace path resolution) but not tested end-to-end.

### 14.1 Implementation scope

| Aspect | OpenClaw (v1) | Hermes (future) |
|--------|---------------|-----------------|
| Overlay seeding on local provider | Implemented + tested | Scaffolded (workspace path resolves via `rt.WorkspacePath()`) |
| Overlay seeding on remote provider | Implemented + tested | Scaffolded (same code path, untested) |
| Overlay seeding on AWS provider | Implemented + tested | Scaffolded (same code path, untested) |
| Protected path list | `agents/`, `canvas/`, `cron/`, `devices/`, `identity/`, `media/` | `skills/` — defined in `runtimeProtectedPaths` but not integration-tested |
| CLI `overlay` commands | Full support | Should work (runtime-agnostic), untested |
| Unit tests | Full coverage | Theoretical examples in test table, marked with `// TODO: hermes integration test` |
| File ownership (chown) | uid 1000 | uid 0 — code path exists via `rt.ContainerSpec().User`, untested |

### 14.2 Design points (apply to both runtimes)

1. **Workspace path resolution.** `ComposeAgentWorkspaceFiles` produces
   a `BehaviorFiles` map keyed by workspace-relative paths. The
   **provider** (not the common helper) resolves the absolute workspace
   path using `rt.WorkspacePath()`. This keeps the composition logic
   runtime-agnostic.

2. **Protected paths are runtime-specific.** OpenClaw agents protect
   `agents/`, `canvas/`, `cron/`, etc. Hermes agents protect `skills/`.
   The common `IsProtectedPath()` function takes a `RuntimeName`
   parameter. The bash helper uses a conservative superset regex
   (union of all runtimes' protected paths) since it doesn't know the
   runtime at bash execution time.

3. **File ownership.** OpenClaw containers run as uid 1000; Hermes runs
   as root (uid 0). Providers must use
   `rt.ContainerSpec(cfg).User` when chowning overlay files —
   matching the existing pattern for config file ownership.

4. **Hermes does not create `MEMORY.md`.** OpenClaw's
   `CreateDirectories()` seeds `MEMORY.md`; Hermes's does not. The
   overlay phase handles both cases correctly: `MEMORY.md` is on the
   universal protected list, so it is never written regardless of
   whether it exists.

5. **New runtimes.** When a third runtime is added, the implementer
   must add its mutable-state directories to
   `runtimeProtectedPaths` in `pkg/common/overlay.go` and update the
   bash superset regex. Both changes are compile-time-obvious — a
   runtime without a protected paths entry will log a warning on first
   overlay refresh.

### 14.3 Hermes example (theoretical)

A Hermes agent named `research-bot` with an overlay would resolve as:

```
Source:     behavior/overlays/research-bot/PROJECT.md
Workspace:  ~/.conga/data/research-bot/workspace/PROJECT.md   (local)
            /opt/conga/data/research-bot/workspace/PROJECT.md  (remote/AWS)
Protected:  skills/ (Hermes-specific), MEMORY.md, memory/, logs/ (universal)
Ownership:  uid 0, gid 0  (Hermes runs as root)
```

This is expected to work with the scaffolded code paths but has not been
validated. When Hermes overlay support is prioritized, the work is:
- Add integration tests for local + remote providers with a Hermes agent
- Verify chown behavior on remote (root-owned files via SFTP)
- Test the `overlay diff` command against a Hermes workspace layout

## 15. Open Questions

- **Q1: Symlinks in the overlay source.** Allow or reject? Proposal:
  reject in v1 (read the target via `os.ReadFile` will follow them
  transparently, so the risk is unbounded content outside the source
  tree). Implement an explicit `Lstat` check.
- **Q2: Case sensitivity.** macOS filesystems are case-insensitive;
  Linux is not. Keep paths case-sensitive and let operators handle
  collisions at commit time. Document in the overlay README.
- **Q3: Shared overlays across agents.** Out of scope for v1 (see
  edge cases). Revisit if demand surfaces.
- **Q4: Runtime-specific overlay content.** Should overlays be
  runtime-aware (e.g. different `SOUL.md` for the same agent depending
  on whether it runs OpenClaw vs Hermes)? Proposal: out of scope for
  v1. Overlay content is markdown consumed by the agent regardless of
  runtime. If runtime-specific tuning is needed, use separate agents.

## 16. Handoff

After review + standards gate, next step is
`/glados/implement-feature specs/2026-04-04_feature_per-agent-config-overlay`.
