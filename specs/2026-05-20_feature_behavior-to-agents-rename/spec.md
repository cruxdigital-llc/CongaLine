# Spec: behavior-to-agents-rename

Detailed technical contract for the rename. Mirrors `plan.md`'s phasing but with file:line specifics.

## Data model

No data model changes. This is a pure refactor of:
- Directory names on disk (operator's repo, `~/.conga/`, `/opt/conga/` on remote/AWS hosts).
- S3 key prefixes for behavior file sync.
- Go path constants in `pkg/common/`.
- Terraform `for_each` iterators + resource names.

All file *contents* are unchanged. `agent.yaml` schema is unchanged. `SOUL.md`/`AGENTS.md`/`USER.md.tmpl` semantics are unchanged.

## Path constants (`pkg/common/behavior.go`)

Add at the top of the file, replacing inline string literals:

```go
const (
    // Current layout (post-2026-05-XX rename).
    agentsRootDir        = "agents"
    defaultsSubdir       = "_defaults"

    // Legacy layout, retained for one release as a backward-compat fallback.
    // Drop both of these and the `legacyPathFallbackEnabled` branch in the
    // next minor release. Migration script: scripts/migrate-behavior-to-agents.sh.
    legacyBehaviorDir    = "behavior"
    legacyDefaultsSubdir = "default"

    // Single feature gate for the entire fallback codepath. Set to false (or
    // delete all gated blocks) to fully retire the legacy paths.
    legacyPathFallbackEnabled = true
)
```

## Loader changes

### `pkg/common/behavior.go` — `resolveBehaviorFiles`

Current logic (line 30–74) hardcodes `"agents"` and `"default"` subdirectory literals inside `filepath.Join`. New logic:

```go
func resolveBehaviorFiles(behaviorDir string, agent provider.AgentConfig) BehaviorFiles {
    files := make(BehaviorFiles)
    agentType := string(agent.Type)
    rtName := string(runtime.ResolveRuntime(agent.Runtime, ""))

    agentDir, agentDirIsLegacy := resolveAgentDir(behaviorDir, agent.Name)
    defaultDir, defaultDirIsLegacy := resolveDefaultDir(behaviorDir, rtName, agentType)

    // SOUL.md and AGENTS.md: agent-specific > runtime+type default
    for _, name := range []string{"SOUL.md", "AGENTS.md"} {
        if data, err := os.ReadFile(filepath.Join(agentDir, name)); err == nil {
            files[name] = BehaviorFile{Content: data, Source: "agent"}
            if agentDirIsLegacy { warnLegacyBehaviorPath(filepath.Join(agentDir, name)) }
            continue
        }
        if data, err := os.ReadFile(filepath.Join(defaultDir, name)); err == nil {
            files[name] = BehaviorFile{Content: data, Source: "default"}
            if defaultDirIsLegacy { warnLegacyBehaviorPath(filepath.Join(defaultDir, name)) }
        }
    }

    // USER.md / USER.md.tmpl handling unchanged in shape, with same fallback wrapping.
    // ...
    return files
}

func resolveAgentDir(behaviorDir, agentName string) (path string, isLegacy bool) {
    newPath := filepath.Join(behaviorDir, agentsRootDir, agentName)
    if _, err := os.Stat(newPath); err == nil {
        return newPath, false
    }
    if legacyPathFallbackEnabled {
        legacyPath := filepath.Join(behaviorDir, legacyBehaviorDir, "agents", agentName)
        if _, err := os.Stat(legacyPath); err == nil {
            return legacyPath, true
        }
    }
    return newPath, false // miss — caller handles
}

func resolveDefaultDir(behaviorDir, rtName, agentType string) (path string, isLegacy bool) {
    newPath := filepath.Join(behaviorDir, agentsRootDir, defaultsSubdir, rtName, agentType)
    if _, err := os.Stat(newPath); err == nil {
        return newPath, false
    }
    if legacyPathFallbackEnabled {
        legacyPath := filepath.Join(behaviorDir, legacyBehaviorDir, legacyDefaultsSubdir, rtName, agentType)
        if _, err := os.Stat(legacyPath); err == nil {
            return legacyPath, true
        }
    }
    return newPath, false
}
```

**Naming note**: the `behaviorDir` *parameter* name is preserved across the codebase to limit churn. Renaming the parameter would touch every caller. The directory it points at (`<repo>/`) is the same; only the subdirectories under it move.

### `pkg/common/overlay_agent.go` — `LoadAgentOverlay`

Current line 41: `path := filepath.Join(behaviorDir, "agents", agent.Name, agentOverlayFileName)`

New logic:

```go
func LoadAgentOverlay(behaviorDir string, agent provider.AgentConfig) (*runtime.AgentOverlay, error) {
    path := filepath.Join(behaviorDir, agentsRootDir, agent.Name, agentOverlayFileName)
    isLegacy := false

    data, err := os.ReadFile(path)
    if err != nil {
        if !errors.Is(err, fs.ErrNotExist) {
            return nil, fmt.Errorf("read %s: %w", path, err)
        }
        if legacyPathFallbackEnabled {
            legacyPath := filepath.Join(behaviorDir, legacyBehaviorDir, "agents", agent.Name, agentOverlayFileName)
            data, err = os.ReadFile(legacyPath)
            if err != nil {
                if errors.Is(err, fs.ErrNotExist) {
                    return nil, nil
                }
                return nil, fmt.Errorf("read %s: %w", legacyPath, err)
            }
            path = legacyPath
            isLegacy = true
        } else {
            return nil, nil
        }
    }

    // ... existing decode + validate logic unchanged ...

    if isLegacy {
        warnLegacyBehaviorPath(path)
    }
    return &overlay, nil
}
```

### `warnLegacyBehaviorPath` (new helper, `pkg/common/overlay_agent.go` or sibling)

```go
var behaviorPathWarningOnce sync.Map // map[string]struct{}

func warnLegacyBehaviorPath(path string) {
    if _, loaded := behaviorPathWarningOnce.LoadOrStore("legacy-path:"+path, struct{}{}); loaded {
        return
    }
    fmt.Fprintf(os.Stderr,
        "warning: reading %s from legacy path. Run scripts/migrate-behavior-to-agents.sh to migrate, or rename behavior/ -> agents/. This fallback will be removed in the next release.\n",
        path)
}
```

Single shared warn-once registry so the same path doesn't warn twice even across loader entry points (behavior + overlay).

## Provider wiring

### `pkg/provider/localprovider/provider.go`

- Line 94: `behaviorDir()` returns `filepath.Join(p.dataDir, "behavior")` — **change to** return `filepath.Join(p.dataDir, "agents")`. The legacy fallback in `resolveAgentDir`/`resolveDefaultDir` handles old layouts.
- Lines 105–118: `overlayBehaviorDir()` (just shipped in PR #45) — change cwd-relative `<repo_path>/behavior` to `<repo_path>/agents`. Add fallback to `<repo_path>/behavior` if `<repo_path>/agents` is missing.

### `pkg/provider/remoteprovider/provider.go`

- Line 89: `remoteBehaviorDir()` returns `posixpath.Join(p.remoteDir, "behavior")` — **change to** `posixpath.Join(p.remoteDir, "agents")`.
- Line 283 in `setup.go`: SFTP upload destination follows the new dir name automatically.
- Lines 1065 / 1088: `behaviorDir := filepath.Join(repoPath, "behavior")` → `agentsDir := filepath.Join(repoPath, "agents")` (with the same fallback shape).

### `pkg/provider/awsprovider/channels.go`

- `resolveAWSBehaviorDir()` (just shipped in PR #45):
  - First try `./agents` (was `./behavior`).
  - Walk-up logic — when a `go.mod` matches, try `<repo-root>/agents` first, fall back to `<repo-root>/behavior` if `agents` doesn't exist yet (covers operators with stale repos).
  - Function name retained (`resolveAWSBehaviorDir`) — internal, not worth churning callers.

## Terraform

### `terraform/modules/infrastructure/main.tf`

Locate the `aws_s3_object.behavior` resource (it iterates over `behavior/` via `fileset`). Two changes:

1. **for_each path**: `fileset("${var.repo_root}/behavior", "**")` → `fileset("${var.repo_root}/agents", "**")`.
2. **key prefix**: `key = "conga/behavior/${each.value}"` → `key = "conga/agents/${each.value}"`.
3. **resource rename**: `aws_s3_object.behavior` → `aws_s3_object.agents`. Add a `moved {}` block:

```hcl
moved {
  from = aws_s3_object.behavior
  to   = aws_s3_object.agents
}
```

Note: Terraform's `moved` block with `for_each` can be finicky. The PR includes a smoke `terraform plan` against the production env to confirm the moves are state-only, not destroy-and-recreate. If `moved` doesn't work cleanly, fall back to a one-shot `terraform state mv` script committed in `terraform/state-migrations/2026-05-XX_rename-behavior-to-agents.sh`.

### `terraform_data.behavior_refresh` → `terraform_data.agents_refresh`

Same shape: rename + `moved {}` block + verify the trigger hash logic still flushes correctly.

### `terraform/modules/infrastructure/user-data.sh.tftpl`

References to `/opt/conga/behavior/` → `/opt/conga/agents/`. The bootstrap creates the new dir; the fallback path doesn't apply at bootstrap time because a fresh host has no legacy state.

### `terraform/modules/infrastructure/scripts/deploy-behavior.sh`

- File rename: `deploy-behavior.sh` → `deploy-agents.sh`. (Internal name, low risk.)
- S3 source: `s3://${state_bucket}/conga/agents/` (was `conga/behavior/`).
- Destination: `/opt/conga/agents/` (was `/opt/conga/behavior/`).
- The script does NOT delete `/opt/conga/behavior/` on its own — the migration script handles that, so the fallback remains usable until the operator migrates.

## Migration script — `scripts/migrate-behavior-to-agents.sh`

```bash
#!/usr/bin/env bash
# Migrates /opt/conga/behavior/ → /opt/conga/agents/ on hosts that pre-date
# the 2026-05-XX rename. Idempotent: re-runs on partially-migrated hosts
# complete the move; re-runs on fully-migrated hosts are no-ops.

set -euo pipefail

OLD=/opt/conga/behavior
NEW=/opt/conga/agents

if [[ ! -d "$OLD" ]]; then
    echo "[migrate] nothing to do — $OLD does not exist."
    exit 0
fi

mkdir -p "$NEW"

# Move per-agent dirs: /opt/conga/behavior/agents/<name>/ -> /opt/conga/agents/<name>/
if [[ -d "$OLD/agents" ]]; then
    for entry in "$OLD/agents"/*; do
        [[ -e "$entry" ]] || continue   # empty dir guard
        name=$(basename "$entry")
        if [[ -d "$NEW/$name" ]]; then
            echo "[migrate] skip $name — already at $NEW/$name"
            continue
        fi
        mv "$entry" "$NEW/$name"
        echo "[migrate] moved agents/$name"
    done
    rmdir "$OLD/agents" 2>/dev/null || true
fi

# Move defaults: /opt/conga/behavior/default/<runtime>/<type>/ -> /opt/conga/agents/_defaults/<runtime>/<type>/
if [[ -d "$OLD/default" ]]; then
    if [[ -d "$NEW/_defaults" ]]; then
        echo "[migrate] skip defaults — already at $NEW/_defaults"
    else
        mv "$OLD/default" "$NEW/_defaults"
        echo "[migrate] moved default/ -> _defaults/"
    fi
fi

# Remove the old root if empty.
if [[ -d "$OLD" ]]; then
    rmdir "$OLD" 2>/dev/null && echo "[migrate] removed empty $OLD" || \
        echo "[migrate] warning: $OLD not empty after migration; review manually:" && ls -A "$OLD" || true
fi

echo "[migrate] complete."
```

**Behavior properties**:
- Uses `mv` exclusively — preserves inode, ownership (uid 1000 for container user), perms.
- Skips entries already at the destination.
- Doesn't error on empty source dirs (`[[ -e "$entry" ]] || continue`).
- Final state on success: `/opt/conga/agents/` is the live tree; `/opt/conga/behavior/` no longer exists.
- Operator can inspect intermediate state if anything is unexpected — `ls -A "$OLD"` prints what's left.

## Gitignore

```diff
-# Per-agent behavior files (may contain client/project-sensitive data)
-#
-behavior/agents/*/
-!behavior/agents/_example/
+# Per-agent definitions (may contain client/project-sensitive data).
+# Only the underscore-prefixed structural entries are committed.
+agents/*/
+!agents/_example/
+!agents/_defaults/
```

The old `behavior/agents/*` rules are deleted (not kept alongside). If an operator's local environment still has the old path, those files remain gitignored by virtue of `behavior/` never being added to git in the first place. After the migration script runs, `/opt/conga/behavior/` doesn't exist on the host.

## Test plan

### Unit tests

**`pkg/common/behavior_test.go`** — extend `TestResolveBehaviorFiles` with:
1. `new-only`: only `agents/<name>/SOUL.md` exists → reads new path, no warning emitted.
2. `legacy-only`: only `behavior/agents/<name>/SOUL.md` exists → reads legacy path, warning fires once.
3. `both`: both exist → reads new, no warning, legacy is ignored.
4. `defaults-new`: only `agents/_defaults/<runtime>/<type>/AGENTS.md` exists → reads new.
5. `defaults-legacy`: only `behavior/default/<runtime>/<type>/AGENTS.md` exists → reads legacy with warning.
6. `defaults-both`: prefer new.
7. `neither`: existing semantics — file missing.

**`pkg/common/overlay_agent_test.go`** — extend `TestLoadAgentOverlay_*` with:
1. `legacy-only-overlay`: only `behavior/agents/<name>/agent.yaml` exists → reads legacy with warning.
2. `legacy-warning-emitted-once`: two consecutive `LoadAgentOverlay` calls against the same legacy path → exactly one warning.

**`captureStderr` reuse**: the existing helper from `overlay_agent_test.go` works for the warning assertions; no new test infra needed.

### Integration tests

If the existing CLI integration test harness (per `specs/2026-04-07_feature_cli-integration-tests/`) covers behavior-file resolution end-to-end, add a case:
- Set up a local agent with `behavior/agents/<name>/` layout (old).
- Run `conga refresh --agent <name>` against the new binary.
- Verify the agent's rendered `openclaw.json` matches expected.
- Verify stderr contains the legacy-path warning.

### Migration script test

Shell-based:
1. Create `/tmp/conga-test/behavior/agents/{aaron,zach}/SOUL.md` (placeholder content).
2. Create `/tmp/conga-test/behavior/default/openclaw/user/AGENTS.md`.
3. Run `OPT_CONGA=/tmp/conga-test scripts/migrate-behavior-to-agents.sh` (parameterize the script for testability, or use a wrapper).
4. Assert:
   - `/tmp/conga-test/agents/aaron/SOUL.md` exists.
   - `/tmp/conga-test/agents/zach/SOUL.md` exists.
   - `/tmp/conga-test/agents/_defaults/openclaw/user/AGENTS.md` exists.
   - `/tmp/conga-test/behavior/` is gone.
5. Run the script again — assert it exits cleanly with "nothing to do".

### Terraform smoke test

Before applying to production:
1. `terraform plan` against the production state.
2. Confirm:
   - Resources `aws_s3_object.agents["agents/aaron/agent.yaml"]` and friends appear as `moved` (no destroy + recreate).
   - `terraform_data.agents_refresh` appears as `moved`.
   - No unintended changes to EC2, IAM, KMS, NAT, or any other infra.

## Edge cases

| Scenario | Behavior |
|---|---|
| Fresh clone, never run setup | `agents/_example` and `agents/_defaults` are present (committed). `conga admin setup` creates `~/.conga/agents/` (new). No legacy paths involved. |
| Operator updates the binary but hasn't run terraform/migration script yet | First `conga refresh` reads from the legacy `~/.conga/behavior/` (fallback), warns once per file. After running the migration script, subsequent refreshes read from `~/.conga/agents/` without warnings. |
| Operator runs `conga refresh` mid-migration (during the host-side `mv`) | Worst case: refresh uses fallback to read partially-moved data. Since `mv` is atomic per-file at the directory level, the worst is reading a couple of files from one path and others from the other within the same agent. Refresh either succeeds (consistent data) or fails fast (file-not-found, the operator re-runs after migration completes). |
| Legacy path is a symlink to the new path (operator's manual workaround) | `os.Stat` follows symlinks; the new path resolves successfully and the legacy path is never tried. No warning. Symlink works. |
| Operator deletes `behavior/` from the repo before pulling main | Their gitignored `behavior/agents/<name>/` deletion is a no-op for git. After pulling, they have only `agents/` (committed). Their per-agent overlays are gone — they need to re-add them via the new path. Documented in migration steps. |
| Operator's `conga admin setup` populated `~/.conga/behavior/default/` from an OLD repo, then updates the repo + binary | New code looks for `~/.conga/agents/_defaults/` first — miss. Falls back to `~/.conga/behavior/default/` — hit. Warns. After migration script, `~/.conga/agents/_defaults/` is populated. No more warning. |

## Documentation deltas

All committed docs that mention `behavior/agents/` or `behavior/default/` paths get updated to the new layout. Specifically:

| File | Change |
|---|---|
| `README.md` | "Per-Agent Model Routing" section, repo structure block, any other path references. |
| `CLAUDE.md` | "Behavior files" subsection renamed and updated. |
| `terraform/README.md` | Path references in the per-agent model routing section. |
| `product-knowledge/standards/architecture.md` | Config Format Boundary cross-link references. |
| `product-knowledge/standards/config-taxonomy.md` | Taxonomy table row "Runtime overlay" + worked examples. |
| `product-knowledge/standards/security.md`, `egress-controls.md` | Any path references. |
| `agents/_example/agent.yaml.example` (post-rename) | Comments reference `behavior/agents/<name>/` — update. |

`specs/2026-05-19_feature_local-model-routing/` is **not** mass-edited. It's a historical artifact. Add a `> **Path rename note (2026-05-XX)**: This spec predates the directory rename. Wherever it says `behavior/agents/<name>/`, the current path is `agents/<name>/`. See `specs/2026-05-20_feature_behavior-to-agents-rename/` for the migration.` block at the top of its `README.md`. One-line addition.

## Implementation phases (mirrors plan.md)

1. **Fallback loader** (`pkg/common/behavior.go`, `overlay_agent.go`, new constants) — tests.
2. **`git mv` committed paths** (`behavior/agents/_example` → `agents/_example`, `behavior/default` → `agents/_defaults`).
3. **Provider wiring** (local, remote, AWS path helpers).
4. **Terraform + bootstrap** (`main.tf` `moved {}` blocks, `user-data.sh.tftpl`, `deploy-*.sh`).
5. **Gitignore**.
6. **Migration script** + shell test.
7. **Documentation** updates.
8. **Provider release** (per CLAUDE.md `pkg/` change protocol — this also touches `pkg/`).
9. **One-release deprecation** — file a follow-up tracking issue for `legacyPathFallbackEnabled = false` cleanup.

## Open Questions (resolve during implementation)

1. **Does Terraform's `moved {}` block work cleanly with `for_each` over a `fileset`?** Verify empirically on the staging plan. If not, use `terraform state mv` in a committed migration script.
2. **Exact name of `deploy-behavior.sh` after rename.** Lean `deploy-agents.sh` for consistency. Internal name; low impact.
3. **Should the loader emit a single aggregated warning per refresh, or one per legacy file read?** Current spec says one per file path per process. Could be loud if 10 files migrate together. Revisit if testing shows the noise is bad.
4. **Should `conga admin setup` automatically run the migration script when it detects legacy paths?** Lean **no** for v1 — too much automation hidden behind setup. Operator runs the script explicitly. Document prominently in the rename PR's upgrade notes.
