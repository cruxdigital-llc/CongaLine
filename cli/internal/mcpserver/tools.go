package mcpserver

func (s *Server) registerTools() {
	// Intentionally omitted Provider methods:
	// - Connect(): returns a long-lived tunnel (Waiter channel), not suited for MCP request/response.
	// - ResolveAgentByIdentity(): CLI convenience for auto-detecting the caller's agent.
	//   MCP callers specify agent names explicitly; WhoAmI() already returns the mapped agent name.

	s.mcp.AddTools(
		// Identity & Discovery
		s.toolWhoAmI(),
		s.toolListAgents(),
		s.toolGetAgent(),

		// Agent Lifecycle
		s.toolProvisionAgent(),
		s.toolRemoveAgent(),
		s.toolPauseAgent(),
		s.toolUnpauseAgent(),

		// Container Operations
		s.toolGetStatus(),
		s.toolGetLogs(),
		s.toolRefreshAgent(),
		s.toolRefreshAll(),
		s.toolContainerExec(),

		// Secrets
		s.toolSetSecret(),
		s.toolListSecrets(),
		s.toolDeleteSecret(),

		// Environment Management
		s.toolSetup(),
		s.toolCycleHost(),
		s.toolTeardown(),
	)
}
