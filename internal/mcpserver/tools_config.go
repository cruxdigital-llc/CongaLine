package mcpserver

import (
	"context"
	"path"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/cruxdigital-llc/conga-line/pkg/common"
	"github.com/cruxdigital-llc/conga-line/pkg/runtime"
)

// toolAgentShowConfig mirrors the `conga agent show-config` CLI command
// (feature #31): it returns the agent's effective OpenClaw config as its
// precedence-ordered $include layers, read live from the container.
func (s *Server) toolAgentShowConfig() server.ServerTool {
	return server.ServerTool{
		Tool: mcp.Tool{
			Name:        "conga_agent_show_config",
			Description: "Show an agent's layered OpenClaw config ($include precedence): the managed root (Conga) plus the admin-drift, per-agent, and fleet-baseline include layers, read live from the container. Layers are ordered by precedence (1 = highest, wins on conflict); the root wins over every include.",
			InputSchema: mcp.ToolInputSchema{
				Type: "object",
				Properties: map[string]any{
					"agent_name": map[string]any{
						"type":        "string",
						"description": "Agent name",
					},
				},
				Required: []string{"agent_name"},
			},
			Annotations: mcp.ToolAnnotation{
				ReadOnlyHint: boolPtr(true),
			},
		},
		Handler: func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			name, err := req.RequireString("agent_name")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}

			ctx, cancel := toolCtx(ctx)
			defer cancel()

			agent, err := s.prov.GetAgent(ctx, name)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			rt, err := runtime.Get(runtime.ResolveRuntime(agent.Runtime, ""))
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}

			read := func(file string) ([]byte, bool) {
				out, execErr := s.prov.ContainerExec(ctx, name, []string{"cat", path.Join(rt.ContainerDataPath(), file)})
				if execErr != nil {
					return nil, false
				}
				return []byte(out), true
			}

			layers := common.BuildConfigLayers(common.EffectiveConfigSpecs(rt), read)
			return jsonResult(struct {
				Agent   string               `json:"agent"`
				Runtime string               `json:"runtime"`
				Layers  []common.ConfigLayer `json:"layers"`
			}{Agent: name, Runtime: string(rt.Name()), Layers: layers})
		},
	}
}
