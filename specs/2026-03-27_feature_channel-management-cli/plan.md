# Plan: Channel Management CLI

## Approach

The Channel interface is already clean — Slack is a proper plugin. The work is:
1. Remove Slack secret prompting from `admin setup` (make it gateway-only by default)
2. Add provider-level methods for channel add/remove and agent bind/unbind
3. Add CLI commands and MCP tools that call those methods
4. Make the router lifecycle respond to channel state changes

## Phase 1: Provider Interface Extension

**New methods on `Provider`:**

```go
// Channel management
AddChannel(ctx context.Context, platform string, secrets map[string]string) error
RemoveChannel(ctx context.Context, platform string) error
ListChannels(ctx context.Context) ([]ChannelStatus, error)

// Agent-channel binding
BindChannel(ctx context.Context, agentName string, binding channels.ChannelBinding) error
UnbindChannel(ctx context.Context, agentName string, platform string) error
```

**New types:**
```go
type ChannelStatus struct {
    Platform    string   // "slack"
    Configured  bool     // shared secrets present
    RouterRunning bool   // router container is running
    BoundAgents []string // agent names with this channel binding
}
```

**Implementation in each provider:**

- `AddChannel`: Write shared secrets → build/regenerate router.env → start router
- `RemoveChannel`: Remove shared secrets → stop router → strip bindings from all agents → regenerate all agent configs + routing
- `ListChannels`: Read shared secrets → check router status → scan agent bindings
- `BindChannel`: Add binding to agent config → regenerate agent's openclaw.json + .env → regenerate routing.json → ensure router connected to agent network
- `UnbindChannel`: Remove binding from agent config → regenerate agent's openclaw.json + .env → regenerate routing.json

**Files**: `provider.go` (interface), `localprovider/channels.go`, `remoteprovider/channels.go`, `awsprovider/channels.go`

## Phase 2: Setup Flow Simplification

**Modify `admin setup` in both local and remote providers:**

- Remove the `channels.All()` secret collection loop from setup
- Remove router startup from setup
- Setup now creates directory structure, collects repo path + image, stores google credentials (if provided), builds images, creates empty routing.json — done
- `SetupConfig.Secrets` still accepts channel secrets for backwards compatibility (passed through to `AddChannel` internally if present)

**Files**: `localprovider/provider.go` (Setup method), `remoteprovider/setup.go`

## Phase 3: CLI Commands

**New command group**: `conga channels` with subcommands

| Command | Args | Flags | Behavior |
|---------|------|-------|----------|
| `conga channels add <platform>` | `slack` | `--json` | Prompt for secrets (or read from JSON), call `AddChannel` |
| `conga channels remove <platform>` | `slack` | `--json`, `--force` | Confirm, call `RemoveChannel` |
| `conga channels list` | — | `--json`, `--output json` | Call `ListChannels`, display table or JSON |
| `conga channels bind <agent> <platform:id>` | agent name, binding | `--json` | Parse binding, call `BindChannel` |
| `conga channels unbind <agent> <platform>` | agent name, platform | `--json`, `--force` | Confirm, call `UnbindChannel` |

**`--channel` flag on `admin add-user`/`admin add-team` remains** — internally calls `BindChannel` after `ProvisionAgent` (or continues doing both in `ProvisionAgent` as today).

**Files**: `cli/cmd/channels.go` (or `channels_add.go`, `channels_remove.go`, etc.)

## Phase 4: MCP Tool Wrappers

Five new MCP tools in `mcpserver/tools_channels.go`:

| Tool | Parameters | Maps to |
|------|-----------|---------|
| `conga_channels_add` | `platform`, secrets fields | `AddChannel` |
| `conga_channels_remove` | `platform` | `RemoveChannel` |
| `conga_channels_list` | — | `ListChannels` |
| `conga_channels_bind` | `agent_name`, `channel` (platform:id) | `BindChannel` |
| `conga_channels_unbind` | `agent_name`, `platform` | `UnbindChannel` |

Register in `tools.go` alongside existing tool groups.

**Files**: `mcpserver/tools_channels.go`, `mcpserver/tools.go`

## Phase 5: Tests & Demo Update

- Unit tests for new provider methods (local provider focus)
- Unit tests for MCP tool handlers
- Update `DEMO.md` with new 3-step flow:
  1. `conga_setup` (gateway-only, no Slack prompts)
  2. `conga_provision_agent` × 2 (no `--channel` flag)
  3. `conga_channels_add slack` (collects credentials, starts router)
  4. `conga_channels_bind aaron slack:U...` + `conga_channels_bind leadership slack:C...`
  5. Continue with secrets, policy, connect, interactions

## Dependency Order

```
Phase 1 (interface + provider impl)
  → Phase 2 (setup simplification)
  → Phase 3 (CLI commands)
  → Phase 4 (MCP tools)
  → Phase 5 (tests + demo)
```

Phases 3 and 4 can be parallelized after Phase 1+2.

## Risk Assessment

| Risk | Mitigation |
|------|-----------|
| Breaking `SetupConfig` JSON for automation | Keep `Secrets` field; if channel secrets present, auto-invoke `AddChannel` |
| Router restart during bind/unbind | Router is stateless (reads routing.json from mount); just regenerate + restart if needed |
| Partial failure during `RemoveChannel` | Sequential: stop router first, then strip bindings, then delete secrets. Idempotent. |
| AWS provider stubs | `AddChannel`/`RemoveChannel` return "not yet implemented" for AWS (consistent with deferred AWS bootstrap) |

## Architect Review Notes

- No new dependencies introduced
- Extends existing Provider interface (additive, not breaking)
- Channel interface unchanged — only the *management surface* is new
- Consistent with existing patterns: provider methods → CLI commands → MCP tools

## QA Review Notes

- Edge case: `channels remove slack` when agents still have bindings → must strip bindings first, not error
- Edge case: `channels bind` when channel not yet added → clear error: "run `channels add slack` first"
- Edge case: `channels add slack` when already configured → idempotent (update secrets) or error?
- Edge case: `channels unbind` on gateway-only agent → no-op or clear message
- Must test: router starts on `channels add`, stops on `channels remove`, reconnects networks on `bind`
