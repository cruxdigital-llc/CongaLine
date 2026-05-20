# Feature: behavior-to-agents-rename

**Trace log for GLaDOS feature workflow.**

## Session Start — 2026-05-20

**Initiated by**: DX feedback during PR #45 (local-model-routing) post-review.
**Origin observation**: The current per-agent config tree at `behavior/agents/<name>/` buries the primary navigation target one directory deeper than necessary. With the new `agent.yaml` file joining the prompts under that tree, `behavior/` is also becoming a slight misnomer — the directory holds runtime *configuration*, not just personality content.

## Feature Name
`behavior-to-agents-rename`

## Goal
Rename the per-agent config tree from `behavior/agents/<name>/` to `agents/<name>/`, move shipped defaults from `behavior/default/<runtime>/<type>/` to `agents/_defaults/<runtime>/<type>/`, and provide a one-release backward-compatibility fallback so existing deployments migrate cleanly. The rename improves daily DX (operators `cd agents/<name>/` directly), settles the naming question while the project is young, and keeps the `_example` / `_defaults` underscore convention consistent.

## Branched off
`feature/local-model-support` (PR #45). The rename will land *after* #45 merges, so the new files added by #45 (`behavior/agents/_example/agent.yaml.example` + the operator's gitignored `behavior/agents/<name>/agent.yaml`) participate in the move.

## Active Personas
- **architect** — directory structure, code path constants, bootstrap/terraform impact, backward-compat fallback design
- **product-manager** — DX value, scope guardrails (no schema changes; no new agent fields)
- **qa** — migration safety, deprecation-warning semantics, test coverage for the fallback path

## Active Capabilities
- **Bash** — file moves (`git mv`), gitignore edits, multi-file path updates, running tests across packages.
- **gh** — referencing the upstream PR #45 and (eventually) opening this rename's own PR.
- Conga MCP — verifying live agents survive the migration via `mcp__conga__conga_get_agent` and `mcp__conga__conga_get_logs`.

## Decisions captured

| Question | Answer |
|---|---|
| New top-level directory name | `agents/` (was `behavior/`) |
| Per-agent entries | Flatten — `agents/<name>/` (was `behavior/agents/<name>/`) |
| Shipped defaults location | `agents/_defaults/<runtime>/<type>/` (was `behavior/default/<runtime>/<type>/`) — leading underscore flags it as non-agent, matches `_example/` |
| Backward-compat strategy | **None.** Project has zero external adoption — there's no on-disk state to migrate anywhere except the developer's own machine, which they migrate by hand (`mv behavior/agents/* agents/`, `mv behavior/default agents/_defaults`) before pulling. |
| Migration story | Not needed. The author migrated their own local state in this PR's working tree. |
| Scope guard | Strictly a rename + flatten. No schema changes, no new agent fields, no overlay-content changes. |
| Branching | Off `feature/local-model-support` so we don't fight the new `agent.yaml` file `_example` shape. Merges *after* #45. |

## Out of scope
- Any change to `agent.yaml`, `SOUL.md`, `AGENTS.md`, `USER.md.tmpl` contents.
- Any change to per-agent JSON / SSM identity records (those live at `~/.conga/agents/<name>.json` and SSM `/conga/agents/<name>` — already at the right level; no rename needed).
- Adding new layers (e.g. a `runtime/` subdir per agent). The flat shape `agents/<name>/<file>` is the target.
- Renaming the `behavior_refresh` terraform_data resource or its trigger logic — those are internal names that don't affect the operator-facing structure. They can be renamed in a follow-up cleanup.
- The "schema-validation test gap" follow-up (issue #46) — orthogonal.

## Artifacts in this trace

| File | Purpose |
|---|---|
| `README.md` | This trace log. |
| `requirements.md` | Goal, functional requirements, non-goals, success criteria. |
| `plan.md` | High-level approach + phased implementation order. |
| `spec.md` | Detailed file-by-file changes, fallback semantics, migration script behavior, edge cases, test plan. |

## Handoff
Next step: `/glados:spec-feature` (if running the full workflow) or proceed directly to implementation once #45 merges. The spec is small enough that a single PR (rename + fallback + migration script + tests + doc updates) is the right shape.
