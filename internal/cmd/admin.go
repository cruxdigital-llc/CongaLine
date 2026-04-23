package cmd

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/cruxdigital-llc/conga-line/pkg/provider"
	"github.com/cruxdigital-llc/conga-line/pkg/ui"
	"github.com/spf13/cobra"
)

var (
	adminGatewayPort   int
	adminIAMIdentity   string
	adminChannel       string
	adminForce         bool
	adminDeleteSecrets bool
)

func init() {
	adminCmd := &cobra.Command{
		Use:   "admin",
		Short: "Admin operations (requires elevated permissions)",
	}

	addUserCmd := &cobra.Command{
		Use:   "add-user <name>",
		Short: "Provision a new user agent",
		Args:  cobra.ExactArgs(1),
		RunE:  adminAddUserRun,
	}
	addUserCmd.Flags().IntVar(&adminGatewayPort, "gateway-port", 0, "Gateway port (auto-assigned if 0)")
	addUserCmd.Flags().StringVar(&adminIAMIdentity, "iam-identity", "", "IAM identity (SSO username/email)")
	addUserCmd.Flags().StringVar(&adminChannel, "channel", "", "Channel binding (platform:id, e.g., slack:U0123456789)")

	addTeamCmd := &cobra.Command{
		Use:   "add-team <name>",
		Short: "Provision a new team agent",
		Args:  cobra.ExactArgs(1),
		RunE:  adminAddTeamRun,
	}
	addTeamCmd.Flags().IntVar(&adminGatewayPort, "gateway-port", 0, "Gateway port (auto-assigned if 0)")
	addTeamCmd.Flags().StringVar(&adminChannel, "channel", "", "Channel binding (platform:id, e.g., slack:C0123456789)")

	listAgentsCmd := &cobra.Command{
		Use:   "list-agents",
		Short: "List all provisioned agents",
		RunE:  adminListAgentsRun,
	}

	removeAgentCmd := &cobra.Command{
		Use:   "remove-agent <name>",
		Short: "Remove an agent",
		Args:  cobra.ExactArgs(1),
		RunE:  adminRemoveAgentRun,
	}
	removeAgentCmd.Flags().BoolVar(&adminForce, "force", false, "Skip confirmation")
	removeAgentCmd.Flags().BoolVar(&adminDeleteSecrets, "delete-secrets", false, "Also delete agent secrets")

	cycleHostCmd := &cobra.Command{
		Use:   "cycle-host",
		Short: "Restart the deployment environment (re-bootstraps all containers)",
		RunE:  adminCycleHostRun,
	}
	cycleHostCmd.Flags().BoolVar(&adminForce, "force", false, "Skip confirmation")

	setupCmd := &cobra.Command{
		Use:   "setup",
		Short: "Configure shared secrets and settings",
		RunE:  adminSetupRun,
	}
	setupCmd.Flags().StringVar(&adminSetupConfig, "config", "", "JSON config (inline or file path) for non-interactive setup")

	refreshAllCmd := &cobra.Command{
		Use:   "refresh-all",
		Short: "Restart all agent containers (picks up latest behavior, config, secrets)",
		RunE:  adminRefreshAllRun,
	}
	refreshAllCmd.Flags().BoolVar(&adminForce, "force", false, "Skip confirmation")

	teardownCmd := &cobra.Command{
		Use:   "teardown",
		Short: "Remove the entire deployment (all agents, containers, config)",
		RunE:  adminTeardownRun,
	}
	teardownCmd.Flags().BoolVar(&adminForce, "force", false, "Skip confirmation")

	pauseCmd := &cobra.Command{
		Use:   "pause <name>",
		Short: "Temporarily stop an agent (preserves all data)",
		Args:  cobra.ExactArgs(1),
		RunE:  adminPauseRun,
	}

	unpauseCmd := &cobra.Command{
		Use:   "unpause <name>",
		Short: "Resume a paused agent",
		Args:  cobra.ExactArgs(1),
		RunE:  adminUnpauseRun,
	}

	adminCmd.AddCommand(setupCmd, addUserCmd, addTeamCmd, listAgentsCmd, removeAgentCmd, cycleHostCmd, refreshAllCmd, teardownCmd, pauseCmd, unpauseCmd)
	rootCmd.AddCommand(adminCmd)
}

func adminListAgentsRun(cmd *cobra.Command, args []string) error {
	ctx, cancel := commandContext()
	defer cancel()

	agents, err := prov.ListAgents(ctx)
	if err != nil {
		return err
	}

	if ui.OutputJSON {
		if agents == nil {
			agents = []provider.AgentConfig{}
		}
		ui.EmitJSON(agents)
		return nil
	}

	if len(agents) == 0 {
		fmt.Println("No agents found.")
		return nil
	}

	headers := []string{"NAME", "TYPE", "STATUS", "CHANNEL", "GATEWAY PORT"}
	var rows [][]string
	for _, a := range agents {
		status := "active"
		if a.Paused {
			status = "paused"
		}
		rows = append(rows, []string{a.Name, string(a.Type), status, formatAgentChannels(a), strconv.Itoa(a.GatewayPort)})
	}

	ui.PrintTable(headers, rows)
	return nil
}

// formatAgentChannels renders the CHANNEL column for an agent. For a
// gateway-only agent (no bindings) returns "(gateway-only)". For a single
// binding, returns "<platform>:<id>". For N>1 bindings, groups by platform
// and shows either a short aggregated form ("slack (3)") or a comma list
// when it stays under the soft width limit.
func formatAgentChannels(a provider.AgentConfig) string {
	if len(a.Channels) == 0 {
		return "(gateway-only)"
	}

	// Group by platform, preserving insertion order per platform.
	byPlatform := map[string][]string{}
	platformOrder := []string{}
	for _, b := range a.Channels {
		if _, seen := byPlatform[b.Platform]; !seen {
			platformOrder = append(platformOrder, b.Platform)
		}
		byPlatform[b.Platform] = append(byPlatform[b.Platform], b.ID)
	}

	const softLimit = 48 // keeps the table readable on most terminals
	parts := make([]string, 0, len(platformOrder))
	for _, platform := range platformOrder {
		ids := byPlatform[platform]
		full := platform + ":" + strings.Join(ids, ",")
		if len(full) <= softLimit {
			parts = append(parts, full)
			continue
		}
		parts = append(parts, fmt.Sprintf("%s (%d)", platform, len(ids)))
	}
	return strings.Join(parts, "; ")
}
