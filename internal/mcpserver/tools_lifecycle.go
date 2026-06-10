package mcpserver

import (
	"context"
	"fmt"

	"github.com/cruxdigital-llc/conga-line/pkg/channels"
	"github.com/cruxdigital-llc/conga-line/pkg/common"
	"github.com/cruxdigital-llc/conga-line/pkg/provider"
	"github.com/cruxdigital-llc/conga-line/pkg/runtime"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func (s *Server) toolProvisionAgent() server.ServerTool {
	return server.ServerTool{
		Tool: mcp.Tool{
			Name:        "conga_provision_agent",
			Description: "Create a new agent. Type must be 'user' (DM-only) or 'team' (channel-based). Channel binding is optional — agents can run in gateway-only mode. Pass `role` to copy a role-package overlay (e.g. role-ops, role-code-dev) before provisioning; when role is set, the role.meta's declared type must match the `type` parameter.",
			InputSchema: mcp.ToolInputSchema{
				Type: "object",
				Properties: map[string]any{
					"agent_name": map[string]any{
						"type":        "string",
						"description": "Agent name (lowercase alphanumeric + hyphens)",
					},
					"type": map[string]any{
						"type":        "string",
						"enum":        []string{"user", "team"},
						"description": "Agent type: 'user' for DM-only, 'team' for channel-based",
					},
					"channel": map[string]any{
						"type":        "string",
						"description": "Channel binding (format: platform:id, e.g., slack:U0123456789). Omit for gateway-only mode.",
					},
					"gateway_port": map[string]any{
						"type":        "integer",
						"description": "Gateway port (auto-assigned if omitted)",
					},
					"runtime": map[string]any{
						"type":        "string",
						"enum":        []string{"openclaw", "hermes"},
						"description": "Agent runtime (default: openclaw or whatever was selected at setup time). Determines which agents/_defaults/<runtime>/role-<slug>/ tree is consulted when role is set.",
					},
					"role": map[string]any{
						"type":        "string",
						"description": "Optional role-package slug (e.g. role-ops, role-research, role-code-dev). Copies overlay defaults from agents/_defaults/<runtime>/role-<slug>/ before provisioning. Existing per-agent files are preserved (idempotent). role.meta's declared type must match the `type` parameter.",
					},
				},
				Required: []string{"agent_name", "type"},
			},
		},
		Handler: func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			agentName, err := req.RequireString("agent_name")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			agentType, err := req.RequireString("type")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			if agentType != "user" && agentType != "team" {
				return mcp.NewToolResultError(fmt.Sprintf("invalid agent type %q: must be \"user\" or \"team\"", agentType)), nil
			}

			gatewayPort := req.GetInt("gateway_port", 0)

			ctx, cancel := toolCtx(ctx)
			defer cancel()

			// Auto-assign gateway port if not specified, same as CLI.
			if gatewayPort == 0 {
				agents, err := s.prov.ListAgents(ctx)
				if err != nil {
					return mcp.NewToolResultError(fmt.Sprintf("failed to auto-assign port: %v", err)), nil
				}
				gatewayPort = common.NextAvailablePort(agents)
			}

			var bindings []channels.ChannelBinding
			if chStr := req.GetString("channel", ""); chStr != "" {
				binding, err := channels.ParseBinding(chStr)
				if err != nil {
					return mcp.NewToolResultError(err.Error()), nil
				}
				ch, ok := channels.Get(binding.Platform)
				if !ok {
					return mcp.NewToolResultError(fmt.Sprintf("unknown channel platform %q", binding.Platform)), nil
				}
				// MCP doesn't expose a runtime parameter today; defaults to
				// openclaw via runtime.ResolveRuntime. The gate fires against
				// that resolved value so unsupported channel × runtime
				// combinations (e.g. telegram on openclaw) are refused before
				// any provisioning side effects.
				resolvedRuntime := string(runtime.ResolveRuntime("", ""))
				if supported, reason := ch.SupportsRuntime(resolvedRuntime); !supported {
					return mcp.NewToolResultError(fmt.Sprintf("channel %s is not supported for the %s runtime: %s", binding.Platform, resolvedRuntime, reason)), nil
				}
				if err := ch.ValidateBinding(agentType, binding.ID); err != nil {
					return mcp.NewToolResultError(err.Error()), nil
				}
				bindings = append(bindings, binding)
			}

			agentRuntime := req.GetString("runtime", "")

			// Apply role package before provisioning. Mirror of the CLI logic
			// in internal/cmd/admin_provision.go applyRolePackageIfRequested.
			if role := req.GetString("role", ""); role != "" {
				behaviorDir := common.ResolveOperatorBehaviorDir()
				if behaviorDir == "" {
					return mcp.NewToolResultError(fmt.Sprintf("role %s: cannot locate the congaline agents/ directory from the MCP server's working directory. Either invoke `conga` from a CLI inside the repo, or set up the agent's overlay manually.", role)), nil
				}
				resolvedRuntime := string(runtime.ResolveRuntime(agentRuntime, ""))
				declaredType, _, err := common.ApplyRolePackage(behaviorDir, agentName, role, resolvedRuntime)
				if err != nil {
					return mcp.NewToolResultError(fmt.Sprintf("role %s: %v", role, err)), nil
				}
				if declaredType != agentType {
					return mcp.NewToolResultError(fmt.Sprintf("role %s declares type %q but `type` parameter is %q. Pick one or the other.", role, declaredType, agentType)), nil
				}
			}

			cfg := provider.AgentConfig{
				Name:        agentName,
				Type:        provider.AgentType(agentType),
				Runtime:     agentRuntime,
				Channels:    bindings,
				GatewayPort: gatewayPort,
			}

			ctx, sink := withSink(ctx)
			if err := s.prov.ProvisionAgent(ctx, cfg); err != nil {
				return errResultWithWarnings(err, sink), nil
			}
			return okWithWarnings(fmt.Sprintf("Agent %q provisioned successfully.", agentName), sink), nil
		},
	}
}

func (s *Server) toolRemoveAgent() server.ServerTool {
	return server.ServerTool{
		Tool: mcp.Tool{
			Name:        "conga_remove_agent",
			Description: "Remove an agent. Stops the container, removes network and config. This is destructive and cannot be undone.",
			InputSchema: mcp.ToolInputSchema{
				Type: "object",
				Properties: map[string]any{
					"agent_name": map[string]any{
						"type":        "string",
						"description": "Agent name to remove",
					},
					"delete_secrets": map[string]any{
						"type":        "boolean",
						"description": "Also delete the agent's secrets (default: false)",
					},
				},
				Required: []string{"agent_name"},
			},
			Annotations: mcp.ToolAnnotation{
				DestructiveHint: boolPtr(true),
			},
		},
		Handler: func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			agentName, err := req.RequireString("agent_name")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			deleteSecrets := req.GetBool("delete_secrets", false)

			ctx, cancel := toolCtx(ctx)
			defer cancel()

			if err := s.prov.RemoveAgent(ctx, agentName, deleteSecrets); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return okResult(fmt.Sprintf("Agent %q removed.", agentName)), nil
		},
	}
}

func (s *Server) toolPauseAgent() server.ServerTool {
	return server.ServerTool{
		Tool: mcp.Tool{
			Name:        "conga_pause_agent",
			Description: "Pause an agent. Stops the container and removes it from routing. Config, secrets, and data are preserved.",
			InputSchema: mcp.ToolInputSchema{
				Type: "object",
				Properties: map[string]any{
					"agent_name": map[string]any{
						"type":        "string",
						"description": "Agent name to pause",
					},
				},
				Required: []string{"agent_name"},
			},
			Annotations: mcp.ToolAnnotation{
				IdempotentHint: boolPtr(true),
			},
		},
		Handler: func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			agentName, err := req.RequireString("agent_name")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}

			ctx, cancel := toolCtx(ctx)
			defer cancel()

			if err := s.prov.PauseAgent(ctx, agentName); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return okResult(fmt.Sprintf("Agent %q paused.", agentName)), nil
		},
	}
}

func (s *Server) toolUnpauseAgent() server.ServerTool {
	return server.ServerTool{
		Tool: mcp.Tool{
			Name:        "conga_unpause_agent",
			Description: "Unpause a previously paused agent. Restarts the container and restores routing.",
			InputSchema: mcp.ToolInputSchema{
				Type: "object",
				Properties: map[string]any{
					"agent_name": map[string]any{
						"type":        "string",
						"description": "Agent name to unpause",
					},
				},
				Required: []string{"agent_name"},
			},
			Annotations: mcp.ToolAnnotation{
				IdempotentHint: boolPtr(true),
			},
		},
		Handler: func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			agentName, err := req.RequireString("agent_name")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}

			ctx, cancel := toolCtx(ctx)
			defer cancel()

			ctx, sink := withSink(ctx)
			if err := s.prov.UnpauseAgent(ctx, agentName); err != nil {
				return errResultWithWarnings(err, sink), nil
			}
			return okWithWarnings(fmt.Sprintf("Agent %q unpaused.", agentName), sink), nil
		},
	}
}

func (s *Server) toolRebaselineAgent() server.ServerTool {
	return server.ServerTool{
		Tool: mcp.Tool{
			Name:        "conga_rebaseline_agent",
			Description: "Reset an agent's admin-owned customization file (agent-custom.json) back to the generated baseline. Backs up the current file to a timestamped .bak, empties it to {}, and refreshes the agent. Discards admin config drift (e.g. added MCP servers). Agent data is preserved.",
			InputSchema: mcp.ToolInputSchema{
				Type: "object",
				Properties: map[string]any{
					"agent_name": map[string]any{
						"type":        "string",
						"description": "Agent name to rebaseline",
					},
				},
				Required: []string{"agent_name"},
			},
			Annotations: mcp.ToolAnnotation{
				DestructiveHint: boolPtr(true),
			},
		},
		Handler: func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			agentName, err := req.RequireString("agent_name")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}

			ctx, cancel := toolCtx(ctx)
			defer cancel()

			if err := s.prov.ResetAgentCustomConfig(ctx, agentName); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			if err := s.prov.RefreshAgent(ctx, agentName); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return okResult(fmt.Sprintf("Agent %q reset to baseline (agent-custom.json backed up and emptied) and refreshed.", agentName)), nil
		},
	}
}

func boolPtr(b bool) *bool { return &b }
