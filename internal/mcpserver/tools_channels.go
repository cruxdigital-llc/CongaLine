package mcpserver

import (
	"context"
	"errors"
	"fmt"

	"github.com/cruxdigital-llc/conga-line/pkg/channels"
	"github.com/cruxdigital-llc/conga-line/pkg/provider"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func (s *Server) toolChannelsAdd() server.ServerTool {
	return server.ServerTool{
		Tool: mcp.Tool{
			Name:        "conga_channels_add",
			Description: "Add a messaging channel integration. Currently supports Slack only. Stores shared credentials and starts the router.",
			InputSchema: mcp.ToolInputSchema{
				Type: "object",
				Properties: map[string]any{
					"platform": map[string]any{
						"type":        "string",
						"description": "Channel platform (currently only 'slack')",
					},
					"slack_bot_token": map[string]any{
						"type":        "string",
						"description": "Slack bot token (xoxb-..., required for Slack)",
					},
					"slack_signing_secret": map[string]any{
						"type":        "string",
						"description": "Slack signing secret (required for Slack)",
					},
					"slack_app_token": map[string]any{
						"type":        "string",
						"description": "Slack app-level token (xapp-..., optional)",
					},
				},
				Required: []string{"platform"},
			},
			Annotations: mcp.ToolAnnotation{
				IdempotentHint: boolPtr(true),
			},
		},
		Handler: func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			platform, err := req.RequireString("platform")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}

			ctx, cancel := toolCtx(ctx)
			defer cancel()

			// Map MCP parameter names to secret names
			secrets := map[string]string{}
			paramToSecret := map[string]string{
				"slack_bot_token":      "slack-bot-token",
				"slack_signing_secret": "slack-signing-secret",
				"slack_app_token":      "slack-app-token",
			}
			for param, secret := range paramToSecret {
				if v := req.GetString(param, ""); v != "" {
					secrets[secret] = v
				}
			}

			if err := s.prov.AddChannel(ctx, platform, secrets); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return okResult(fmt.Sprintf("Channel %q configured and router started.", platform)), nil
		},
	}
}

func (s *Server) toolChannelsRemove() server.ServerTool {
	return server.ServerTool{
		Tool: mcp.Tool{
			Name:        "conga_channels_remove",
			Description: "Remove a messaging channel integration. Stops the router, removes all agent bindings, and deletes credentials.",
			InputSchema: mcp.ToolInputSchema{
				Type: "object",
				Properties: map[string]any{
					"platform": map[string]any{
						"type":        "string",
						"description": "Channel platform to remove (e.g., 'slack')",
					},
				},
				Required: []string{"platform"},
			},
			Annotations: mcp.ToolAnnotation{
				DestructiveHint: boolPtr(true),
			},
		},
		Handler: func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			platform, err := req.RequireString("platform")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}

			ctx, cancel := toolCtx(ctx)
			defer cancel()

			if err := s.prov.RemoveChannel(ctx, platform); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return okResult(fmt.Sprintf("Channel %q removed.", platform)), nil
		},
	}
}

func (s *Server) toolChannelsList() server.ServerTool {
	return server.ServerTool{
		Tool: mcp.Tool{
			Name: "conga_channels_list",
			Description: "List configured channel platforms and their status (credentials present, router running, bound agents). " +
				"Pass `agent_name` to instead get a per-binding view of that agent — one entry per (platform, id) binding, including labels. " +
				"Use the per-agent mode to audit multi-binding team agents.",
			InputSchema: mcp.ToolInputSchema{
				Type: "object",
				Properties: map[string]any{
					"agent_name": map[string]any{
						"type":        "string",
						"description": "Optional — when set, returns that agent's individual bindings instead of platform statuses.",
					},
				},
			},
			Annotations: mcp.ToolAnnotation{
				ReadOnlyHint: boolPtr(true),
			},
		},
		Handler: func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			ctx, cancel := toolCtx(ctx)
			defer cancel()

			if agentName := req.GetString("agent_name", ""); agentName != "" {
				a, err := s.prov.GetAgent(ctx, agentName)
				if err != nil {
					return mcp.NewToolResultError(err.Error()), nil
				}
				return jsonResult(a.Channels)
			}

			statuses, err := s.prov.ListChannels(ctx)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return jsonResult(statuses)
		},
	}
}

func (s *Server) toolChannelsBind() server.ServerTool {
	return server.ServerTool{
		Tool: mcp.Tool{
			Name: "conga_channels_bind",
			Description: "Bind an agent to a channel. The channel platform must be configured first via conga_channels_add. " +
				"An agent may hold multiple bindings on the same platform (e.g. a team agent serving several Slack channels) — call this tool once per channel. " +
				"Repeat calls with the exact same (agent, platform, id) are idempotent no-ops. " +
				"Binding a channel id already owned by a different agent returns an error.",
			InputSchema: mcp.ToolInputSchema{
				Type: "object",
				Properties: map[string]any{
					"agent_name": map[string]any{
						"type":        "string",
						"description": "Agent name",
					},
					"channel": map[string]any{
						"type":        "string",
						"description": "Channel binding (format: platform:id, e.g., slack:U0123456789)",
					},
					"label": map[string]any{
						"type":        "string",
						"description": "Optional human-readable label (e.g. a channel name like '#legal'). Used in list output and the ambiguous-unbind picker.",
					},
				},
				Required: []string{"agent_name", "channel"},
			},
		},
		Handler: func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			agentName, err := req.RequireString("agent_name")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			chStr, err := req.RequireString("channel")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}

			binding, err := channels.ParseBinding(chStr)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			if label := req.GetString("label", ""); label != "" {
				binding.Label = label
			}

			ctx, cancel := toolCtx(ctx)
			defer cancel()

			if err := s.prov.BindChannel(ctx, agentName, binding); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return okResult(fmt.Sprintf("Agent %q bound to %s:%s.", agentName, binding.Platform, binding.ID)), nil
		},
	}
}

func (s *Server) toolChannelsUnbind() server.ServerTool {
	return server.ServerTool{
		Tool: mcp.Tool{
			Name: "conga_channels_unbind",
			Description: "Remove a channel binding from an agent. " +
				"Omit `id` to remove the sole binding when the agent has only one for this platform. " +
				"When an agent has multiple bindings for the same platform (e.g. a team agent on several Slack channels), `id` is required.",
			InputSchema: mcp.ToolInputSchema{
				Type: "object",
				Properties: map[string]any{
					"agent_name": map[string]any{
						"type":        "string",
						"description": "Agent name",
					},
					"platform": map[string]any{
						"type":        "string",
						"description": "Channel platform to unbind (e.g., 'slack')",
					},
					"id": map[string]any{
						"type":        "string",
						"description": "Specific binding id to remove. Required when the agent has multiple bindings for the platform; optional otherwise.",
					},
				},
				Required: []string{"agent_name", "platform"},
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
			platform, err := req.RequireString("platform")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			// id is optional — empty means "remove the sole binding for this platform".
			id := req.GetString("id", "")

			ctx, cancel := toolCtx(ctx)
			defer cancel()

			if err := s.prov.UnbindChannel(ctx, agentName, platform, id); err != nil {
				// Parity with the CLI: when empty-id unbind hits a multi-bound
				// agent, surface an enumerated error that lists the current
				// bindings and a concrete example command. Spec §2.6 calls
				// this out as a script-safe equivalent of the interactive
				// picker.
				if errors.Is(err, provider.ErrAmbiguousUnbind) {
					if a, getErr := s.prov.GetAgent(ctx, agentName); getErr == nil {
						err = provider.FormatAmbiguousUnbindError(agentName, platform, a.ChannelBindings(platform))
					}
				}
				return mcp.NewToolResultError(err.Error()), nil
			}
			label := platform
			if id != "" {
				label = platform + ":" + id
			}
			return okResult(fmt.Sprintf("Agent %q unbound from %s.", agentName, label)), nil
		},
	}
}
