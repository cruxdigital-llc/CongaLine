# Implementation Tasks: Agent Pause / Unpause

## Task 1: Add Paused field to agent config structs
- [ ] `cli/internal/provider/provider.go` — add `Paused bool json:"paused,omitempty"` to `AgentConfig`
- [ ] `cli/internal/discovery/agent.go` — add `Paused bool json:"paused,omitempty"` to `AgentConfig`
- [ ] `cli/internal/provider/awsprovider/provider.go` — propagate `Paused` in `convertAgent` helper

## Task 2: Add PauseAgent/UnpauseAgent to Provider interface
- [ ] `cli/internal/provider/provider.go` — add two methods to the `Provider` interface

## Task 3: Update routing to exclude paused agents
- [ ] `cli/internal/common/routing.go` — add `if a.Paused { continue }` in `GenerateRoutingJSON`

## Task 4: Implement local provider pause/unpause
- [ ] `cli/internal/provider/localprovider/provider.go` — implement `PauseAgent`
- [ ] `cli/internal/provider/localprovider/provider.go` — implement `UnpauseAgent`
- [ ] `cli/internal/provider/localprovider/provider.go` — extract `saveAgentConfig` helper
- [ ] `cli/internal/provider/localprovider/provider.go` — add paused guard to `RefreshAgent`
- [ ] `cli/internal/provider/localprovider/provider.go` — skip paused agents in `RefreshAll`
- [ ] `cli/internal/provider/localprovider/provider.go` — skip paused agents in `CycleHost`

## Task 5: Implement AWS provider pause/unpause
- [ ] Create `cli/scripts/pause-agent.sh.tmpl`
- [ ] Create `cli/scripts/unpause-agent.sh.tmpl`
- [ ] Update `cli/scripts/embed.go` — embed new templates
- [ ] `cli/internal/provider/awsprovider/provider.go` — implement `PauseAgent`
- [ ] `cli/internal/provider/awsprovider/provider.go` — implement `UnpauseAgent`
- [ ] `cli/internal/provider/awsprovider/provider.go` — implement `setAgentPaused` helper
- [ ] `cli/internal/provider/awsprovider/provider.go` — add paused guard to `RefreshAgent`
- [ ] `cli/internal/provider/awsprovider/provider.go` — skip paused agents in `RefreshAll`

## Task 6: CLI commands
- [ ] Create `cli/cmd/admin_pause.go` — `adminPauseRun`, `adminUnpauseRun`
- [ ] Update `cli/cmd/admin.go` — register pause/unpause subcommands
- [ ] Update `cli/cmd/admin.go` — add STATUS column to `adminListAgentsRun`

## Task 7: Bootstrap integration (AWS)
- [ ] Update `terraform/user-data.sh.tftpl` — skip agents with `paused: true` in discovery loop

## Task 8: Build verification
- [ ] `go build` CLI compiles without errors
- [ ] `go vet ./...` clean
- [ ] `go test ./...` all pass
- [ ] `terraform validate` passes
