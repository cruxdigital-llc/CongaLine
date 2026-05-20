# Plan: behavior-to-agents-rename

High-level implementation approach. Detailed file:line work lives in `spec.md`. This is a single-PR change with phased internal ordering for easy review and safe rollout.

## Strategy

**Rename + flatten + fallback, all in one PR.** The change touches a wide surface (code, terraform, bootstrap, docs, gitignore) but the *logic* is mechanical. Splitting into multiple PRs adds review overhead without reducing risk — every PR would still need the fallback to keep the tree green.

**Two-stage rollout** (within the single PR):
1. **Land the rename + fallback** so the new code reads both paths and writes only the new one.
2. **Operators run the migration script** post-merge to flip their on-host directories. The fallback covers the gap.

After one release cycle, a separate cleanup PR removes the fallback (gated by a single constant for trivial removal).

## Prerequisites

- **PR #45 (local-model-routing) merged first.** The new `agent.yaml` file participates in the rename; if #45 isn't merged, we'd be moving a file that doesn't yet exist on `main` and the rebase would be awkward. Branch order: `feature/local-model-support` → `main` → `spec/behavior-to-agents-rename` → `main`.

## Phase ordering

### Phase 1 — Backward-compat fallback (lands code that reads both paths)

**Goal**: ship a binary that can read either layout, defaulting to the new one. No `git mv`'s yet.

Files:
- `pkg/common/behavior.go` — `resolveBehaviorFiles` tries new path constants first; falls back to legacy on miss; emits a one-time warning via a `behaviorPathWarningOnce sync.Map`.
- `pkg/common/overlay_agent.go` — `LoadAgentOverlay` does the same: new path first, legacy fallback, warn-once on legacy hit.
- New path constants in `pkg/common/behavior.go`:
  ```go
  const (
      agentsRootDir        = "agents"
      defaultsSubdir       = "_defaults"
      legacyBehaviorDir    = "behavior"
      legacyDefaultsSubdir = "default"
  )
  ```
- A single feature gate `legacyPathFallbackEnabled = true` (constant, not flag) so the next release flips it to `false` (or removes the legacy branches entirely) with one diff hunk.

Tests:
- New cases in `behavior_test.go` and `overlay_agent_test.go` verifying: new-only reads, legacy-only reads (with warning), both-present reads new, neither-present behavior unchanged.

### Phase 2 — Move committed files

Files:
- `git mv behavior/agents/_example agents/_example`
- `git mv behavior/default agents/_defaults`

That's all the committed content. The gitignored `behavior/agents/<name>/` directories stay where they are on operator machines (they're outside git); the fallback covers them until the operator runs the migration script.

### Phase 3 — Provider wiring

Files:
- `pkg/provider/localprovider/provider.go`:
  - `behaviorDir()` returns `<dataDir>/agents` (new). Legacy `<dataDir>/behavior` is fallback-only.
  - `overlayBehaviorDir()` resolves `<repo_path>/agents` first, falls back to `<repo_path>/behavior` and then to the data-dir snapshot.
- `pkg/provider/remoteprovider/provider.go`:
  - `remoteBehaviorDir()` returns `/opt/conga/agents` (new); fallback to `/opt/conga/behavior` if `agents` is missing.
  - `setup.go:283` SFTP upload destination updated to `remoteAgentsDir()`.
- `pkg/provider/awsprovider/channels.go`:
  - `resolveAWSBehaviorDir` looks for `agents/` first, then `behavior/`. Walk-up logic unchanged.

### Phase 4 — Terraform + bootstrap

Files:
- `terraform/modules/infrastructure/main.tf`:
  - `aws_s3_object.behavior` iterator changes to `for_each = fileset("${var.repo_root}/agents", "**")`.
  - S3 key prefix: `conga/agents/<path>` (was `conga/behavior/<path>`).
  - Resource rename: `aws_s3_object.behavior` → `aws_s3_object.agents`. State migration via `moved {}` block to avoid replacement.
- `terraform/modules/infrastructure/user-data.sh.tftpl`:
  - References to `/opt/conga/behavior/` → `/opt/conga/agents/`.
- `terraform/modules/infrastructure/scripts/deploy-behavior.sh` (probably renamed to `deploy-agents.sh`):
  - rsync source: `s3://${state_bucket}/conga/agents/`.
  - rsync destination: `/opt/conga/agents/`.
  - Old `/opt/conga/behavior/` is left in place (not actively deleted) so the fallback works during transition. The migration script handles cleanup.
- `terraform_data.behavior_refresh` → `terraform_data.agents_refresh`. State migration via `moved {}` block.

### Phase 5 — Gitignore

```diff
-# Per-agent behavior files (may contain client/project-sensitive data)
-behavior/agents/*/
-!behavior/agents/_example/
+# Per-agent definitions (may contain client/project-sensitive data)
+agents/*/
+!agents/_example/
+!agents/_defaults/
```

### Phase 6 — Migration script

New file: `scripts/migrate-behavior-to-agents.sh`. Idempotent shell script that:
1. Checks for `/opt/conga/behavior/` existence; exits cleanly if absent.
2. `mkdir -p /opt/conga/agents` if needed.
3. Walks `/opt/conga/behavior/agents/<name>/` → `mv` each `<name>` directory to `/opt/conga/agents/<name>/`.
4. Walks `/opt/conga/behavior/default/<runtime>/<type>/` → `mv` to `/opt/conga/agents/_defaults/<runtime>/<type>/`.
5. Removes the now-empty `/opt/conga/behavior/` if every subdir migrated cleanly.
6. Prints a summary table (paths migrated, paths skipped, final state).

Defensive — never deletes a path that has unmigrated content. Uses `mv` exclusively (preserves uid/gid for the container user).

### Phase 7 — Documentation

Files updated to reference `agents/` paths and `_defaults`:
- `README.md` (Per-Agent Model Routing section + repo structure block + any "Behavior files" subsections)
- `CLAUDE.md` ("Behavior files" → "Agent files"; cross-link to `config-taxonomy.md` preserved)
- `terraform/README.md`
- `product-knowledge/standards/architecture.md`
- `product-knowledge/standards/config-taxonomy.md`
- `product-knowledge/standards/security.md`
- `product-knowledge/standards/egress-controls.md`
- `agents/_example/agent.yaml.example` (inline comments)
- `specs/2026-05-19_feature_local-model-routing/README.md` — append a note that the directory has since been renamed (don't rewrite history; future readers should see both names referenced)

Existing specs under `specs/` that reference old paths are NOT mass-edited. They're historical artifacts. The README's intro paragraph for each spec dir can carry a brief "path references in this spec predate the 2026-05-XX rename" note if needed.

## Touchpoint summary

| Change kind | Files |
|---|---|
| Code constants | `pkg/common/behavior.go` (new constants block) |
| Fallback logic | `pkg/common/behavior.go`, `pkg/common/overlay_agent.go` |
| Provider path helpers | `pkg/provider/{localprovider,remoteprovider,awsprovider}/...` |
| Terraform | `terraform/modules/infrastructure/main.tf`, `user-data.sh.tftpl`, `deploy-*.sh` |
| Gitignore | `.gitignore` |
| Tests | `pkg/common/behavior_test.go`, `pkg/common/overlay_agent_test.go`, possibly a shell-script test for `scripts/migrate-behavior-to-agents.sh` |
| Committed file moves | `git mv behavior/agents/_example agents/_example`, `git mv behavior/default agents/_defaults` |
| Migration script | `scripts/migrate-behavior-to-agents.sh` (new) |
| Docs | README, CLAUDE.md, terraform/README, all standards docs |

## Verification

### Pre-implementation
- Spec persona review (architect + PM + QA) — same workflow as #45.
- Confirm #45 is merged before starting (or rebase as needed).

### During implementation
- After Phase 1 (fallback): `go test ./...` passes. Manually verify that an old-layout test fixture still loads with the warning.
- After Phase 3 (provider wiring): a fresh `conga admin setup --provider local` against a clean `~/.conga/` lays out `~/.conga/agents/`. An existing setup with `~/.conga/behavior/` still works via fallback.
- After Phase 4 (terraform): `terraform plan` against a deployed environment shows the `moved {}` migrations cleanly. No `aws_s3_object` replacements; only path key changes.
- After Phase 6 (migration script): run against a tmpfs-mounted fake `/opt/conga/behavior/` tree and verify the result.

### Post-merge
- On the production AWS environment:
  1. Pull main, build the CLI.
  2. `terraform apply` — picks up the renamed S3 paths and the `moved {}` blocks.
  3. Run the migration script via `aws ssm start-session` or `mcp__conga__conga_container_exec` style invocation.
  4. `./bin/conga refresh-all` — every agent re-renders against the new paths.
  5. DM the agents to confirm health.

## Risks

| Risk | Mitigation |
|---|---|
| `aws_s3_object` resource rename triggers full replacement, deleting then recreating every behavior file | Use `moved {}` blocks; verify via `terraform plan` before apply. If `moved {}` doesn't suffice (Terraform's `moved` is sometimes fussy with for_each), use `terraform state mv` in a one-shot script committed alongside the PR. |
| Migration script run mid-refresh leaves a half-migrated directory | Script is idempotent — re-runs detect the partial state and complete. Worst-case agent refresh during the gap uses fallback to legacy path, logs the warning, and succeeds. |
| Fallback-warning noise during normal operation if some agents weren't migrated | Per-process warn-once cap means at most one warning per `conga` invocation per file. Acceptable noise level until operators migrate. |
| Live deployment with non-default `conga admin setup` working dir | Confirm `local-config.json` `repo_path` resolves correctly post-rename. The `repo_path` itself doesn't change; only the subdirs under it. |
| Existing specs and observations under `specs/` reference `behavior/agents/` | These are historical artifacts — not mass-edited. New specs use new paths. Operators reading old specs see the old name and can cross-reference this rename's docs. |

## Open Questions (deferred to `spec.md` or implementation)

1. Exact wording of the deprecation warning — keep terse, name the script.
2. Whether to also rename the internal Go constants like `BehaviorFiles` map type. Lean **no** — the type holds files for any agent, "behavior" or "config"; renaming touches too many call sites for marginal clarity. Keep `BehaviorFiles` as a type name for now; revisit later.
3. Whether to ship a `conga admin migrate-paths` CLI wrapper around the shell script. Lean **no** for v1 — shell is fine for a one-time operation. Revisit if SSM-exec ergonomics become a problem.
