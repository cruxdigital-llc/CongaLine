# Tasks — Channel Abstraction

## Phase 1: Channel Interface + Slack Implementation
- [x] 1.1 Create `cli/internal/channels/channels.go` — interface, types (`ChannelBinding`, `SecretDef`, `RoutingEntry`)
- [x] 1.2 Create `cli/internal/channels/registry.go` — registry + `ParseBinding()`
- [x] 1.3 Create `cli/internal/channels/slack/slack.go` — Slack `Channel` implementation
- [x] 1.4 Create `cli/internal/channels/slack/slack_test.go` — 13 test cases
- [x] 1.5 Create `cli/internal/channels/registry_test.go` — registry + ParseBinding tests
- [x] 1.6 Run tests: `go test ./internal/channels/...` — PASS

## Phase 2: AgentConfig Refactor
- [x] 2.1 Modify `cli/internal/provider/provider.go` — replace `SlackMemberID`/`SlackChannel` with `Channels`, add `ChannelBinding()` helper
- [x] 2.2 Modify `cli/internal/provider/setup_config.go` — replace Slack fields with `Secrets map[string]string`, simplify `SecretValue()`

## Phase 3: Rewire common/ Package
- [x] 3.1 Modify `cli/internal/common/config.go` — `SharedSecrets` to use `Values` map, remove `HasSlack()`, refactor `GenerateOpenClawConfig()` and `GenerateEnvFile()`
- [x] 3.2 Modify `cli/internal/common/routing.go` — refactor `GenerateRoutingJSON()` to delegate to channels
- [x] 3.3 Modify `cli/internal/common/behavior.go` — replace `{{SLACK_ID}}` hardcoding with channel template vars
- [x] 3.4 Modify `cli/internal/common/validate.go` — remove `ValidateMemberID()`, `ValidateChannelID()` (moved to Slack channel)
- [x] 3.5 Update `cli/internal/common/routing_test.go` — new `AgentConfig.Channels` format + gateway-only test
- [x] 3.6 Run tests: `go test ./internal/common/...` — PASS

## Phase 4: Rewire CLI Commands
- [x] 4.1 Modify `cli/cmd/admin.go` — update add-user/add-team defs (remove positional arg, add `--channel`), update list-agents display
- [x] 4.2 Modify `cli/cmd/admin_provision.go` — rewrite to use `channels.ParseBinding()` + `ch.ValidateBinding()`
- [x] 4.3 Modify `cli/cmd/root.go` — remove validation wrappers, add Slack channel import
- [x] 4.4 Modify `cli/cmd/json_schema.go` — update schemas for add-user, add-team, list-agents, setup
- [x] 4.5 Update `cli/cmd/root_test.go` — removed Slack validation tests (moved to channels/slack)

## Phase 5: Rewire MCP Tools
- [x] 5.1 Modify `cli/internal/mcpserver/tools_lifecycle.go` — replace `slack_member_id`/`slack_channel` with `channel` param
- [x] 5.2 Modify `cli/internal/mcpserver/tools_env.go` — replace Slack fields with generic secrets map
- [x] 5.3 Update `cli/internal/mcpserver/server_test.go` — add Slack channel import, update provision + setup tests

## Phase 6: Rewire Providers
- [x] 6.1 Modify `cli/internal/provider/localprovider/provider.go` — channel-driven setup, `hasAnyChannel()`, router env via channel interface
- [x] 6.2 Modify `cli/internal/provider/localprovider/secrets.go` — `readSharedSecrets()` generic map
- [x] 6.3 Modify `cli/internal/provider/remoteprovider/secrets.go` — `readSharedSecrets()` generic map
- [x] 6.4 Modify `cli/internal/provider/remoteprovider/setup.go` — channel-driven setup prompts, router env via channel interface
- [x] 6.5 Modify `cli/internal/provider/remoteprovider/provider.go` — `hasAnyChannel()` helper
- [x] 6.6 Modify `cli/internal/provider/awsprovider/provider.go` — channel bindings in SSM params and template data
- [x] 6.7 Update `cli/internal/provider/awsprovider/provider_test.go` — new channels format
- [x] 6.8 Update `cli/internal/discovery/agent.go` — channels field
- [x] 6.9 Update `cli/internal/discovery/identity_test.go` — channels JSON format

## Phase 7: Final Validation
- [x] 7.1 Run full test suite: `go test ./...` — ALL PASS
- [x] 7.2 Build: `go build ./...` — CLEAN
- [x] 7.3 Verify no remaining `SlackMemberID`/`SlackChannel`/`HasSlack` references in Go source — NONE
