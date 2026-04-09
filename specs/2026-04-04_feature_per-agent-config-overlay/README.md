# Per-Agent Config Overlay

Extend behavior seeding so each managed agent can carry its own set of
markdown files layered on top of the type-specific baseline.

## Motivation
A team agent needs project-specific grounding (client, team members, project
scope). The current `behavior/overrides/<agent>/` mechanism only supports
replacing the fixed set `{SOUL.md, AGENTS.md, USER.md}` wholesale. We need
to add arbitrary additional files per agent without clobbering agent-mutable
state (MEMORY.md, agent-authored notes).

## Session log
- 2026-04-04: Spec drafted via `/glados/spec-feature`. Requirements and plan
  written alongside the spec since the user jumped directly to specification
  with a fully-formed problem statement.
- 2026-04-07: Spec updated after agent portability PR (#34) merged. Added
  runtime-aware workspace paths, per-runtime protected path lists,
  runtime-dependent file ownership (uid 1000 vs 0), Hermes directory
  layout differences, and new §14 (Multi-Runtime Considerations).
  Scoped v1 to OpenClaw only — Hermes support scaffolded but untested,
  with a theoretical example and clear TODO list for future work.
- 2026-04-07: Implementation started via `/glados/implement-feature`.

## Active Capabilities
- context7 MCP (library docs lookup)
- conga MCP (agent management, testing)
- Bash (go build, go test)

## Modified Files
- `pkg/common/overlay.go` — NEW: manifest types, protected path list, hash helpers
- `pkg/common/behavior.go` — `BehaviorFiles` type changed to struct, `ComposeAgentWorkspaceFiles` added with overlay walk + deletion reconciliation, `composeBaseline` factored out with legacy `overrides/` fallback
- `pkg/common/overlay_test.go` — NEW: 6 tests (protected paths, manifest round-trip, corrupt/missing/wrong-version, deletion reconciliation)
- `pkg/common/behavior_test.go` — NEW: 11 tests (baseline, overlay, collision, protected path, non-md skip, size cap, deletion, legacy fallback)
- `pkg/provider/localprovider/provider.go` — `deployBehavior` rewritten: manifest read/write, overlay composition, deletion reconciliation
- `pkg/provider/remoteprovider/provider.go` — `deployBehavior` rewritten: same pattern via SSH/SFTP
- `scripts/deploy-behavior.sh.tmpl` — overlay copy loop added with protected-path regex
- `internal/cmd/agent_overlay.go` — NEW: `conga overlay {list,add,rm,show,diff}` subcommands
- `behavior/overlays/.gitkeep` — NEW: placeholder
- `behavior/overrides/README.md` — NEW: deprecation notice
