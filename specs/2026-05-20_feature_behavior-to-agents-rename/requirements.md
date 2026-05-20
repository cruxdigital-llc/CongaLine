# Requirements: behavior-to-agents-rename

## Goal

Rename and flatten the per-agent config tree so each agent's files live at `agents/<name>/<file>` instead of `behavior/agents/<name>/<file>`. Move shipped defaults to `agents/_defaults/<runtime>/<type>/`. Add a one-release backward-compatibility fallback so existing deployments don't break the moment this change ships.

The rename also drops "behavior" from the public surface, which has become a slight misnomer now that `agent.yaml` (runtime config, not personality) joins the tree.

## Why now

- **Daily-DX cost compounds.** Every reference to `behavior/agents/<name>/` in docs, scripts, and operator commands is one directory deeper than it needs to be. The friction is small but linear.
- **`behavior/` is becoming a misnomer.** The new `agent.yaml` carries model selection, and the reserved keyspace (`memory`, `tools`, `limits`) covers concerns broader than "behavior." `agents/` is honestly what's there.
- **Cheaper now than later.** A handful of agents in production today; the rename's blast radius scales with the number of deployments. Doing this while the project is small is a one-time hit instead of a recurring migration burden for every future operator.
- **PR #45 is the right moment.** It just introduced `agent.yaml`, the file most affected by the directory naming. Bundling the rename's spec immediately after means we settle the naming question with the new file in scope, instead of carrying the old name forward.

## Functional Requirements

### FR-1: New directory layout

```
agents/
├── _example/                       # committed (was behavior/agents/_example/)
│   ├── SOUL.md
│   ├── AGENTS.md
│   ├── USER.md.tmpl
│   └── agent.yaml.example
├── _defaults/                      # committed (was behavior/default/)
│   ├── openclaw/
│   │   ├── user/SOUL.md AGENTS.md USER.md.tmpl
│   │   └── team/SOUL.md AGENTS.md USER.md.tmpl
│   └── hermes/...
└── <real-agent>/                   # gitignored (was behavior/agents/<name>/)
    ├── SOUL.md
    ├── AGENTS.md
    ├── USER.md            # optional, overrides the templated default
    └── agent.yaml         # optional, per-agent runtime overlay
```

### FR-2: Underscore convention for non-agent entries

The leading-underscore convention is the contract for "this directory is structural, not an agent definition." Today's `_example/` precedent extends to `_defaults/`. No real agent name may start with `_` (the existing `validateAgentName` rule already forbids leading underscores — see `internal/cmd/root.go` agent-name validation; spec-level reaffirmation only).

### FR-3: `.gitignore` rules

```gitignore
# Per-agent definitions (may contain client/project-sensitive data)
# Only the structural underscore-prefixed entries are committed.
agents/*/
!agents/_example/
!agents/_defaults/
```

### FR-4: Backward-compatibility fallback (one release)

`pkg/common/behavior.go` `resolveBehaviorFiles` and `pkg/common/overlay_agent.go` `LoadAgentOverlay` MUST:

1. Try the new path first (`agents/<name>/<file>` for per-agent, `agents/_defaults/<runtime>/<type>/<file>` for defaults).
2. If the new path is missing, try the old path (`behavior/agents/<name>/<file>`, `behavior/default/<runtime>/<type>/<file>`).
3. On successful old-path read, emit a one-time stderr warning (deduped via the existing `overlayWarningOnce` pattern or a new `behaviorWarningOnce` sibling):
   ```
   warning: reading <file> from legacy path "behavior/agents/<name>/<file>".
   Run scripts/migrate-behavior-to-agents.sh, or rename behavior/ to agents/.
   This fallback will be removed in the next release.
   ```
4. If neither path resolves, behave exactly as today (file-not-found semantics — overlay loader returns `(nil, nil)`; behavior loader returns an empty `BehaviorFiles`).

### FR-5: Migration script

`scripts/migrate-behavior-to-agents.sh` MUST:

- Be **idempotent** — re-runs after partial completion finish cleanly.
- Detect whether the host has the old layout (`/opt/conga/behavior/` exists) and migrate to the new layout (`/opt/conga/agents/`).
- Use `mv` (preserves inode, ownership, perms) rather than `cp + rm` so file ownership for the container user (uid 1000) is preserved.
- Be safe to run with `set -euo pipefail` semantics — every step succeeds or the script exits cleanly with a diagnostic.
- Print a final "migration complete" / "nothing to do" message so the operator knows what happened.

The script ships with the repo and is invoked manually (or via `terraform apply`'s `behavior_refresh` trigger if the rename includes that hook update).

### FR-6: Code path constants

Single source of truth for the new paths in `pkg/common/behavior.go`:

```go
const (
    agentsRootDir       = "agents"            // new
    legacyBehaviorDir   = "behavior"          // old; fallback only
    defaultsSubdir      = "_defaults"         // new (was "default")
    legacyDefaultsSubdir = "default"          // old; fallback only
)
```

No path string is duplicated elsewhere in the codebase.

### FR-7: Bootstrap and terraform updates

- `pkg/provider/localprovider/provider.go` `behaviorDir()` + `overlayBehaviorDir()` → return `agents/` paths.
- `pkg/provider/remoteprovider/provider.go` `remoteBehaviorDir()` → return `/opt/conga/agents/` (with fallback to `/opt/conga/behavior/` for legacy hosts).
- `pkg/provider/awsprovider/channels.go` `resolveAWSBehaviorDir` → look for `agents/` first, fall back to `behavior/`.
- `terraform/modules/infrastructure/main.tf` `aws_s3_object.behavior` resource → iterate over `agents/` directory; S3 key prefix becomes `conga/agents/` (was `conga/behavior/`).
- `terraform/modules/infrastructure/user-data.sh.tftpl` and `deploy-behavior.sh` → sync `conga/agents/` from S3 to `/opt/conga/agents/`.
- `terraform_data.behavior_refresh` is renamed to `terraform_data.agents_refresh` (no operator surface impact; cleaner state diff for the rename PR).

### FR-8: Documentation updates

Every reference to `behavior/agents/`, `behavior/default/`, or "behavior files" in the following files updated to the new paths and names:

- `README.md` (the new "Per-Agent Model Routing" section + repo structure block + any other refs)
- `CLAUDE.md` ("Behavior files" section becomes "Agent files" or equivalent; cross-link to taxonomy doc preserved)
- `terraform/README.md`
- `product-knowledge/standards/architecture.md` (Config Format Boundary table reference)
- `product-knowledge/standards/config-taxonomy.md` (taxonomy table and decision rule references)
- `product-knowledge/standards/security.md` (any path references)
- `product-knowledge/standards/egress-controls.md` (any path references)
- `behavior/agents/_example/agent.yaml.example` → relocated to `agents/_example/agent.yaml.example` with its inline comments updated
- All committed spec docs under `specs/*/` that reference the old paths (most won't, but a search-and-update pass is required)

### FR-9: No breaking changes for live agents during the transition

A live AWS agent on `2026.3.11` whose host has `/opt/conga/behavior/agents/aaron/agent.yaml` must continue to work after the operator's CLI is updated to the new binary, until the migration script runs. The fallback loader path makes this possible.

After migration, the old `/opt/conga/behavior/` directory should no longer exist (the migration script `mv`'s it to `/opt/conga/agents/`). The next refresh reads from the new path; the fallback is not exercised.

## Non-Goals

- Schema changes to `agent.yaml`, `SOUL.md`, `AGENTS.md`, `USER.md.tmpl`.
- Per-agent JSON file location changes (`~/.conga/agents/<name>.json` / SSM `/conga/agents/<name>` already use `agents/` and are fine).
- Multi-runtime overlay support (Hermes adopts `Overlay` later, separate spec).
- New CLI commands. `conga agent {list,add,rm,show,diff}` stay as-is.
- Terraform module rename (`module.congaline`, `module.infrastructure`) — internal organization, not operator-facing.
- Any cleanup of the `behavior_refresh` terraform_data resource trigger logic beyond the rename to `agents_refresh`. Internal name only.

## Success Criteria

### SC-1: Fresh clone works end-to-end
A new operator clones the repo, runs `conga admin setup --provider local`, copies `agents/_example/` to `agents/myagent/`, edits, and runs `conga refresh --agent myagent`. The agent comes up using the new path. No mention of `behavior/` is required.

### SC-2: Existing deployments migrate cleanly
On a live AWS host with the old layout:
1. Operator updates the CLI to the new binary.
2. Operator runs `terraform apply` — picks up the new `aws_s3_object` paths.
3. Operator runs `scripts/migrate-behavior-to-agents.sh` on the host (or via SSM exec) — renames `/opt/conga/behavior/` → `/opt/conga/agents/`.
4. `conga refresh-all` re-renders all agents using the new path.
5. No agent goes offline. No data is lost. The container restarts during refresh are the only operator-visible churn, and they're expected.

### SC-3: Fallback warning fires (and only once per path per process)
On a host that hasn't yet been migrated, an agent refresh logs the deprecation warning exactly once per file path per `conga` process. Subsequent refreshes of the same agent in the same process don't repeat the warning. (Tested with the existing `overlayWarningOnce` sync.Map pattern.)

### SC-4: Fallback can be removed cleanly in the next release
The fallback code path is gated by a clearly named constant or flag (e.g. `legacyBehaviorFallbackEnabled bool = true`) so the next release's cleanup PR is a one-line toggle (or full deletion of the named blocks) rather than untangling logic.

### SC-5: Tests cover the rename + fallback
- `pkg/common/behavior_test.go` and `pkg/common/overlay_agent_test.go` gain cases for:
  - New path present, old path absent → reads new (no warning).
  - New path absent, old path present → reads old (warns once).
  - Both paths present → reads new (no warning; old is ignored).
  - Neither path present → existing not-found semantics.
- A migration-script integration test if practical (shell-based: set up a fake `/tmp/opt/conga/behavior/`, run the script, verify the destination structure).

### SC-6: All test suites pass; lint clean
`go test ./...`, `go vet`, `gofmt -l .`, and the existing CLI integration test suite all clean after the rename.

### SC-7: Docs reference the new paths
A `grep -rn "behavior/agents\|behavior/default" --include="*.md"` over the committed docs returns zero hits (excluding the spec itself, which discusses the rename historically).

### SC-8: Mid-refresh safety
If an operator runs `conga refresh --agent X` *during* the migration (window between the host's `/opt/conga/behavior/` `mv` and the rsync completion), the worst case is a refresh that uses the old path via the fallback, logs the warning, and succeeds. No agent enters a broken state.

## Constraints & Assumptions

- **No live agents are running with overlays that depend on the old name.** The overlay file paths inside `agent.yaml` (like `base_url`) are absolute URLs — they don't reference the directory layout. The rename only affects where the *agent's own files* live, not what they point at.
- **The committed spec for PR #45 will be left as-is.** Historical specs reference the old paths; that's fine. The rename PR's docs and forward-looking specs use the new paths.
- **Hermes runtime is in scope** — both `_defaults/openclaw/` and `_defaults/hermes/` get renamed in parallel. Same fallback applies.
- **The migration script runs once.** Operators who somehow get into a half-migrated state (e.g. partially-mv'd directory) can re-run the script safely — it should detect the situation and complete the migration.

## Open Questions (resolved during spec or implementation)

1. **Should `behavior_refresh` terraform_data be renamed to `agents_refresh`?** Spec says yes — internal name only, no operator impact, cleaner alignment. Confirm during implementation that the rename doesn't trigger an unintended replacement of the parent resources.
2. **Should the migration script also be available via a `conga admin migrate-paths` CLI subcommand?** Spec says no for v1 — shell script is fine, keeps the CLI surface minimal. Revisit if operator feedback shows the shell-script-via-SSM ergonomics are bad.
3. **How long is "one release" for the fallback?** Recommend: keep the fallback through the next minor version bump (e.g. if this lands as `v0.3.0`, drop the fallback in `v0.4.0`). Two months is a reasonable window for ~5 deployments.
