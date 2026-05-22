package cmd

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/cruxdigital-llc/conga-line/pkg/channels"
	"github.com/cruxdigital-llc/conga-line/pkg/common"
	"github.com/cruxdigital-llc/conga-line/pkg/provider"
	"github.com/cruxdigital-llc/conga-line/pkg/runtime"
	"github.com/cruxdigital-llc/conga-line/pkg/ui"
	"github.com/spf13/cobra"
)

// cmdErrWriter returns the stream the --role command writes its operator-
// facing notes to. Indirected so tests can capture output without touching
// os.Stderr directly. Default is os.Stderr.
var cmdErrWriter = func() io.Writer { return os.Stderr }

func adminAddUserRun(cmd *cobra.Command, args []string) error {
	ctx, cancel := commandContext()
	defer cancel()

	agentName := args[0]
	if err := validateAgentName(agentName); err != nil {
		return err
	}

	// Resolve runtime first so resolveChannelBinding can run the
	// runtime-compat gate before validating the binding ID format.
	agentRuntime := flagRuntime
	if agentRuntime == "" {
		if s, ok := ui.GetString("runtime"); ok {
			agentRuntime = s
		}
	}

	// Channel binding: flag > JSON > none (gateway-only)
	bindings, err := resolveChannelBinding("user", agentRuntime)
	if err != nil {
		return err
	}

	// Gateway port: flag > JSON > auto-assign
	if adminGatewayPort == 0 {
		if p, ok := ui.GetInt("gateway_port"); ok {
			adminGatewayPort = p
		}
	}
	gatewayPort, err := resolveGatewayPort(ctx)
	if err != nil {
		return err
	}

	// Get IAM identity: flag > JSON > prompt (AWS only)
	iamIdentity := adminIAMIdentity
	if iamIdentity == "" {
		if s, ok := ui.GetString("iam_identity"); ok {
			iamIdentity = s
		}
	}
	if iamIdentity == "" && prov.Name() == "aws" && !ui.JSONInputActive {
		defaultIdentity := ""
		identity, err := prov.WhoAmI(ctx)
		if err == nil && identity.Name != "" {
			defaultIdentity = identity.Name
		}
		iamIdentity, err = ui.TextPromptWithDefault("SSO username/email of the user to add", defaultIdentity)
		if err != nil {
			return err
		}
	}

	// Role: flag > JSON > none. When set, copy role-package defaults into
	// the agent's overlay dir BEFORE provisioning so the provider's
	// overlay loader picks them up. Mutex with the command's implicit
	// type: add-user requires `type: user` in role.meta.
	role := adminRole
	if role == "" {
		if s, ok := ui.GetString("role"); ok {
			role = s
		}
	}
	if err := applyRolePackageIfRequested(role, agentName, agentRuntime, "user"); err != nil {
		return err
	}

	cfg := provider.AgentConfig{
		Name:        agentName,
		Type:        provider.AgentTypeUser,
		Runtime:     agentRuntime,
		Channels:    bindings,
		GatewayPort: gatewayPort,
		IAMIdentity: iamIdentity,
	}

	if err := prov.ProvisionAgent(ctx, cfg); err != nil {
		return err
	}

	if ui.OutputJSON {
		ui.EmitJSON(struct {
			Agent       string `json:"agent"`
			Type        string `json:"type"`
			GatewayPort int    `json:"gateway_port"`
			Status      string `json:"status"`
		}{
			Agent:       agentName,
			Type:        string(provider.AgentTypeUser),
			GatewayPort: gatewayPort,
			Status:      "provisioned",
		})
		return nil
	}

	fmt.Printf("\nAgent %s provisioned successfully!\n\n", agentName)
	fmt.Println("Next steps:")
	fmt.Printf("  1. conga secrets set anthropic-api-key --agent %s\n", agentName)
	fmt.Printf("  2. conga refresh --agent %s\n", agentName)
	fmt.Printf("  3. conga connect --agent %s\n", agentName)
	return nil
}

func adminAddTeamRun(cmd *cobra.Command, args []string) error {
	ctx, cancel := commandContext()
	defer cancel()

	agentName := args[0]
	if err := validateAgentName(agentName); err != nil {
		return err
	}

	// Resolve runtime first so resolveChannelBinding can run the
	// runtime-compat gate before validating the binding ID format.
	teamRuntime := flagRuntime
	if teamRuntime == "" {
		if s, ok := ui.GetString("runtime"); ok {
			teamRuntime = s
		}
	}

	// Channel binding: flag > JSON > none (gateway-only)
	bindings, err := resolveChannelBinding("team", teamRuntime)
	if err != nil {
		return err
	}

	// Gateway port: flag > JSON > auto-assign
	if adminGatewayPort == 0 {
		if p, ok := ui.GetInt("gateway_port"); ok {
			adminGatewayPort = p
		}
	}
	gatewayPort, err := resolveGatewayPort(ctx)
	if err != nil {
		return err
	}

	// Role: flag > JSON > none. See adminAddUserRun for full semantics.
	role := adminRole
	if role == "" {
		if s, ok := ui.GetString("role"); ok {
			role = s
		}
	}
	if err := applyRolePackageIfRequested(role, agentName, teamRuntime, "team"); err != nil {
		return err
	}

	cfg := provider.AgentConfig{
		Name:        agentName,
		Type:        provider.AgentTypeTeam,
		Runtime:     teamRuntime,
		Channels:    bindings,
		GatewayPort: gatewayPort,
	}

	if err := prov.ProvisionAgent(ctx, cfg); err != nil {
		return err
	}

	if ui.OutputJSON {
		ui.EmitJSON(struct {
			Agent       string `json:"agent"`
			Type        string `json:"type"`
			GatewayPort int    `json:"gateway_port"`
			Status      string `json:"status"`
		}{
			Agent:       agentName,
			Type:        string(provider.AgentTypeTeam),
			GatewayPort: gatewayPort,
			Status:      "provisioned",
		})
		return nil
	}

	fmt.Printf("\nTeam agent %s provisioned successfully!\n", agentName)
	if len(bindings) > 0 {
		fmt.Printf("Channel: %s:%s\n", bindings[0].Platform, bindings[0].ID)
	} else {
		fmt.Println("Mode: gateway-only (no channel)")
	}
	fmt.Printf("Gateway port: %d\n", gatewayPort)
	return nil
}

// resolveChannelBinding parses the --channel flag or JSON input into a binding slice.
//
// agentRuntime is the resolved runtime name ("openclaw", "hermes"). The
// runtime-compat check fires BEFORE validation so the operator sees the
// most actionable error first: "telegram is not supported on openclaw"
// beats "telegram ID is valid, but…". Pass the resolved runtime, not the
// raw flag value — empty-string runtime defaults to openclaw via the
// runtime registry, which the unsupported-combo error needs to know.
func resolveChannelBinding(agentType, agentRuntime string) ([]channels.ChannelBinding, error) {
	chStr := adminChannel
	if chStr == "" {
		if s, ok := ui.GetString("channel"); ok {
			chStr = s
		}
	}
	if chStr == "" {
		return nil, nil // gateway-only
	}

	binding, err := channels.ParseBinding(chStr)
	if err != nil {
		return nil, err
	}
	ch, ok := channels.Get(binding.Platform)
	if !ok {
		return nil, fmt.Errorf("unknown channel platform %q", binding.Platform)
	}
	resolvedRuntime := string(runtime.ResolveRuntime(agentRuntime, ""))
	if supported, reason := ch.SupportsRuntime(resolvedRuntime); !supported {
		return nil, fmt.Errorf("channel %s is not supported for the %s runtime: %s", binding.Platform, resolvedRuntime, reason)
	}
	if err := ch.ValidateBinding(agentType, binding.ID); err != nil {
		return nil, err
	}
	return []channels.ChannelBinding{binding}, nil
}

// applyRolePackageIfRequested is the CLI side of the --role flow:
// resolves the operator's local agents/ dir, copies the role package's
// default files into the agent's overlay dir, verifies the role's
// declared type matches the command's implicit type (add-user → "user",
// add-team → "team"), and reports what got copied to stderr for
// operator visibility. No-op when role is empty.
//
// Returns an error (which aborts provisioning) when:
//   - the operator's agents/ dir cannot be located
//   - the role doesn't exist for the requested runtime
//   - role.meta is missing or malformed
//   - the role's declared type doesn't match the command intent
//   - file copy fails
//
// Existing files in the destination are preserved (operator
// customizations win — see pkg/common/role_package.go).
func applyRolePackageIfRequested(role, agentName, agentRuntime, cmdType string) error {
	if role == "" {
		return nil
	}

	behaviorDir := common.ResolveOperatorBehaviorDir()
	if behaviorDir == "" {
		return fmt.Errorf("--role %s: cannot locate the congaline agents/ directory from the current working directory. Run `conga` from the conga-line repo root (or a subdirectory of it), or omit --role and author the overlay manually", role)
	}

	resolvedRuntime := string(runtime.ResolveRuntime(agentRuntime, ""))

	declaredType, copied, err := common.ApplyRolePackage(behaviorDir, agentName, role, resolvedRuntime)
	if err != nil {
		return fmt.Errorf("--role %s: %w", role, err)
	}

	if declaredType != cmdType {
		return fmt.Errorf("--role %s declares type %q in role.meta, but you ran `conga admin add-%s`. Use `conga admin add-%s --role %s` instead",
			role, declaredType, cmdType, declaredType, role)
	}

	if len(copied) > 0 {
		fmt.Fprintf(cmdErrWriter(), "role %s: copied %v into agents/%s/. Customize before first use — at minimum, point base_url at your endpoint.\n",
			role, copied, agentName)
	} else {
		fmt.Fprintf(cmdErrWriter(), "role %s: agent dir already populated, no files copied (existing customizations preserved).\n", role)
	}
	return nil
}

func resolveGatewayPort(ctx context.Context) (int, error) {
	if adminGatewayPort != 0 {
		return adminGatewayPort, nil
	}

	agents, err := prov.ListAgents(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to query agents for port assignment: %w", err)
	}

	port := common.NextAvailablePort(agents)
	fmt.Printf("Auto-assigned gateway port: %d\n", port)
	return port, nil
}
