# Plan: Per-Agent Config Overlay

## Approach

Extend `pkg/common/behavior.go` with a second phase — **overlay seeding** —
that runs after the existing `ComposeBehaviorFiles` logic. The overlay
phase walks a new per-agent directory tree, applies a protected-path deny
list, and returns an augmented `BehaviorFiles` map plus a manifest of
managed paths. Providers continue to call a single entry point and do not
need to know about the layering internally.

## Source layout

```
behavior/
  base/              # unchanged
  user/              # unchanged
  team/              # unchanged
  overlays/          # NEW — per-agent overlay trees, one dir per agent
    <agent_name>/
      CLIENT.md
      PROJECT.md
      TEAM.md
      SOUL.md        # optional — replaces composed SOUL.md (like today's overrides/)
  overrides/         # DEPRECATED alias — symlink or fallback read path for one release
```

`behavior/overrides/` is kept working for one release cycle as a read-only
fallback so in-flight branches don't break; new code writes only to
`behavior/overlays/`. A migration note is added to the release notes.

## Layering pipeline

1. **Baseline compose** (existing): produces `{SOUL.md, AGENTS.md, USER.md}`
   from base + type + template rendering.
2. **Overlay walk** (new): walks `behavior/overlays/<agent_name>/**/*.md`,
   filters against the protected-path deny list, and overlays each file
   into the result map (overwriting on collision — this is how a per-agent
   `SOUL.md` replaces the composed one).
3. **Manifest emit** (new): records `{path, sha256}` for every file the
   overlay phase placed.

## Deletion handling (C3)

Seeding reads the **previous** manifest from the workspace
(`.conga-overlay-manifest.json`). Any path present in the previous
manifest but missing from the new overlay is a candidate for deletion.
For each candidate, the provider re-hashes the workspace file:

- Hash matches previous manifest → safe to delete (agent hasn't touched it).
- Hash differs → leave alone, log `WARNING: overlay file <path> was
  modified outside overlay source; not deleting`.

After processing, the new manifest is written to the workspace.

## Provider integration

All three providers already call a `deployBehavior(cfg)` equivalent. The
change is isolated to the common helper:

```go
// pkg/common/behavior.go
func ComposeAgentWorkspaceFiles(
    behaviorDir string,
    agent provider.AgentConfig,
    prevManifest Manifest,
) (BehaviorFiles, []string /* to delete */, Manifest, error)
```

Providers:
- **local** (`pkg/provider/localprovider/provider.go`): already reads
  behavior from `~/.conga/behavior/`. Extend `deployBehavior` to call the
  new helper, apply deletions, and write the manifest.
- **remote** (`pkg/provider/remoteprovider/provider.go`): pushes files via
  SFTP. Extend the push set and add an SFTP delete loop for C3 deletions.
- **aws**: the bootstrap-time `deploy-behavior.sh` helper is replaced /
  augmented by a Go-side composition that writes files via SSM
  `uploadFile`, OR by extending the bash helper to handle overlays by
  reading a manifest file synced from S3. **Decision pushed to the
  spec** — see `spec.md` §4.3.

## Protected paths (C1)

Hard-coded in `pkg/common/behavior.go`:

```go
var protectedWorkspacePaths = []string{
    "MEMORY.md",
    "memory/",
    "logs/",
    "agents/",
    ".conga-overlay-manifest.json", // meta-protect the manifest itself from overlay
}
```

Overlay files matching any protected path prefix cause
`ComposeAgentWorkspaceFiles` to return an error — surfaced to the CLI at
provision/refresh time as `overlay file <path> is on the protected path list`.

## CLI surface

New subcommand family under `conga agent overlay`:
- `conga agent overlay list <agent>` — list overlay files for an agent
- `conga agent overlay add <agent> <file> [--as <name>]` — copy a file into
  the overlay source tree (`behavior/overlays/<agent>/`)
- `conga agent overlay rm <agent> <name>` — remove a file from the overlay
  source
- `conga agent overlay show <agent> <name>` — cat an overlay file
- `conga agent overlay diff <agent>` — diff overlay source vs workspace

`add`/`rm` mutate only the **source** tree; they do not push to the agent.
The operator must `conga refresh --agent <name>` (or `admin refresh-all`)
afterwards. This matches the existing model where `behavior/` edits
require a refresh to take effect.

## Refresh interaction

No changes to the refresh trigger flow. `RefreshAgent` already calls
`deployBehavior` before restarting the container; the new overlay phase
runs transparently inside that call.

## Schema / validation

- **Manifest schema** (versioned): `{"version": 1, "files": [{"path":
  "CLIENT.md", "sha256": "..."}]}`
- **Validation at refresh**:
  - Overlay source paths must be relative, `.md`, and not escape the
    per-agent root (no `..`).
  - Overlay source must not contain protected paths.
  - Per-agent overlay directory name must match an existing agent name;
    stray directories produce a warning, not an error.

## Out of scope for this pass

- Non-markdown overlay files (images, JSON, YAML). Allowlist is `.md`
  only; other extensions are skipped with a warning.
- Per-agent overlays for `openclaw.json`, policy, or env files. Those have
  their own paths and are not affected.
- Encrypted overlay files. If an operator wants to keep an overlay private,
  they can gitignore the per-agent directory.

## Test plan

- Unit: `ComposeAgentWorkspaceFiles` with (a) no overlay, (b) overlay only,
  (c) overlay that replaces composed files, (d) overlay that tries to
  write MEMORY.md (must error), (e) deletion with unmodified workspace file
  (must delete), (f) deletion with modified workspace file (must preserve
  + warn).
- Integration (local provider): provision agent → add overlay → refresh →
  assert files in workspace → modify MEMORY.md → refresh → assert
  MEMORY.md untouched.
- Remote/AWS: smoke test the same flow end-to-end in a scratch environment.
