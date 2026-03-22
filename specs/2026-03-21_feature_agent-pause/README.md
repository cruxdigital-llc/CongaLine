# Trace Log: Agent Pause / Unpause

**Feature**: Agent Pause / Unpause
**Started**: 2026-03-21
**Active Personas**: Architect, Product Manager, QA
**Active Capabilities**: GitHub (version control)

## Session Log

### 2026-03-21 — Initial Spec (AWS-only)

- **Spec drafted**: AWS-only version covering SSM state model, CLI commands, bootstrap integration
- **Status**: Draft — did not cover local provider or provider interface changes

### 2026-03-21 — Full Spec Session (Provider-agnostic)

- **Feature named**: "Agent Pause / Unpause" (`agent-pause`)
- **Personas selected**: Architect, Product Manager, QA
- **Goal defined**: Temporarily stop agents without destroying them, preserving all state. Works on both AWS and local providers.
- **Key design decisions**:
  - `Paused` field added to both `provider.AgentConfig` and `discovery.AgentConfig` with `omitempty`
  - Two new Provider interface methods: `PauseAgent`, `UnpauseAgent`
  - Local: direct Docker stop/start + JSON file update + routing regeneration
  - AWS: SSM SendCommand scripts + SSM parameter update
  - `GenerateRoutingJSON` filters paused agents (canonical exclusion on local)
  - AWS scripts use `jq` for direct routing.json manipulation
  - Bulk operations (`RefreshAll`, `CycleHost`) skip paused agents
  - `RefreshAgent` on a paused agent returns an error
  - Bootstrap (AWS) skips paused agents via `jq` check
- **Files created**:
  - [requirements.md](requirements.md)
  - [plan.md](plan.md)
  - [spec.md](spec.md) (rewritten from AWS-only draft)
  - [tasks.md](tasks.md)
