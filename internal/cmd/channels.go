package cmd

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/cruxdigital-llc/conga-line/pkg/channels"
	"github.com/cruxdigital-llc/conga-line/pkg/provider"
	"github.com/cruxdigital-llc/conga-line/pkg/ui"
	"github.com/spf13/cobra"
)

var (
	channelRemoveForce bool
	channelUnbindForce bool
	channelBindLabel   string
)

func init() {
	channelsCmd := &cobra.Command{
		Use:   "channels",
		Short: "Manage messaging channel integrations",
		Long:  "Add, remove, and manage messaging channel integrations (e.g. Slack) and agent-channel bindings.",
	}

	addCmd := &cobra.Command{
		Use:   "add <platform>",
		Short: "Add a messaging channel integration",
		Long: `Configure a messaging channel platform (e.g. Slack) by providing its credentials.
This stores the shared secrets and starts the router.

Example:
  conga channels add slack`,
		Args: cobra.ExactArgs(1),
		RunE: channelsAddRun,
	}

	removeCmd := &cobra.Command{
		Use:   "remove <platform>",
		Short: "Remove a messaging channel integration",
		Long: `Remove a channel platform. This stops the router, removes all agent bindings
for this platform, and deletes the shared credentials.`,
		Args: cobra.ExactArgs(1),
		RunE: channelsRemoveRun,
	}
	removeCmd.Flags().BoolVar(&channelRemoveForce, "force", false, "Skip confirmation")

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List configured channels and their status",
		Long: `List configured channel platforms (slack, telegram, etc.) and their status.

With the global --agent flag, list the individual bindings for that agent
— one row per (platform, id) binding, including any labels. Useful for
team agents that own multiple bindings on the same platform.

Example:
  conga channels list
  conga channels list --agent contracts`,
		RunE: channelsListRun,
	}
	// Note: --agent here reuses the root persistent flag (flagAgent). A local
	// --agent would shadow and conflict with the persistent one in pflag,
	// causing state leakage between CLI invocations in the same process.

	bindCmd := &cobra.Command{
		Use:   "bind <agent> <platform:id>",
		Short: "Bind an agent to a channel",
		Long: `Add a channel binding to an existing agent.

An agent may own multiple bindings on the same platform (e.g. a team agent
serving several Slack channels). Repeat the command with different channel
IDs to add more bindings — each one must be a channel ID not already bound
to another agent. Rebinding the exact same (platform, id) is an idempotent
no-op; changing the label of an existing binding requires unbinding first.

Example:
  conga channels bind aaron slack:U0123456789
  conga channels bind leadership slack:C0123456789
  conga channels bind contracts slack:C0111 --label "#legal"
  conga channels bind contracts slack:C0222 --label "#sales"`,
		Args: cobra.ExactArgs(2),
		RunE: channelsBindRun,
	}
	bindCmd.Flags().StringVar(&channelBindLabel, "label", "", "Human-readable label for this binding (e.g. a channel name)")

	unbindCmd := &cobra.Command{
		Use:   "unbind <agent> <platform[:id]>",
		Short: "Remove a channel binding from an agent",
		Long: `Remove a channel binding from an agent.

When the agent has a single binding for the platform, the id suffix is optional:
  conga channels unbind aaron slack

When the agent has multiple bindings for the same platform (e.g. a team agent
bound to several Slack channels), you must specify which binding to remove:
  conga channels unbind contracts slack:C0123456789`,
		Args: cobra.ExactArgs(2),
		RunE: channelsUnbindRun,
	}
	unbindCmd.Flags().BoolVar(&channelUnbindForce, "force", false, "Skip confirmation")

	channelsCmd.AddCommand(addCmd, removeCmd, listCmd, bindCmd, unbindCmd)
	rootCmd.AddCommand(channelsCmd)
}

func channelsAddRun(cmd *cobra.Command, args []string) error {
	ctx, cancel := commandContext()
	defer cancel()

	platform := args[0]
	ch, ok := channels.Get(platform)
	if !ok {
		return fmt.Errorf("unknown channel platform %q", platform)
	}

	// Show setup guide before prompting (skip in JSON/non-interactive mode)
	if !ui.JSONInputActive {
		if guide := ch.SetupGuide(); guide != "" {
			fmt.Println()
			fmt.Println(guide)
			fmt.Println()
		}
	}

	// Collect secrets
	secrets := map[string]string{}
	for _, def := range ch.SharedSecrets() {
		var value string
		var err error

		// Check JSON input first
		if ui.JSONInputActive {
			value, _ = ui.GetString(def.Name)
		}

		if value == "" && !ui.JSONInputActive {
			label := def.Prompt
			if !def.Required {
				label += " (optional)"
			}
			value, err = ui.SecretPrompt(fmt.Sprintf("  %s", label))
			if err != nil {
				return err
			}
		}

		if value == "" {
			if def.Required {
				return fmt.Errorf("missing required secret %q", def.Name)
			}
			continue
		}
		secrets[def.Name] = value
	}

	if err := prov.AddChannel(ctx, platform, secrets); err != nil {
		return err
	}

	if ui.OutputJSON {
		ui.EmitJSON(map[string]any{
			"platform":       platform,
			"status":         "configured",
			"router_started": true,
		})
	} else {
		fmt.Printf("Channel %s configured. Router started.\n", platform)
	}
	return nil
}

func channelsRemoveRun(cmd *cobra.Command, args []string) error {
	ctx, cancel := commandContext()
	defer cancel()

	platform := args[0]

	// Confirmation
	if !channelRemoveForce && !ui.JSONInputActive {
		statuses, err := prov.ListChannels(ctx)
		if err != nil {
			return err
		}
		var boundAgents []string
		for _, s := range statuses {
			if s.Platform == platform {
				boundAgents = s.BoundAgents
				break
			}
		}

		msg := fmt.Sprintf("This will remove the %s channel", platform)
		if len(boundAgents) > 0 {
			msg += fmt.Sprintf(", unbind agents (%s)", strings.Join(boundAgents, ", "))
		}
		msg += ", and delete credentials. Continue?"
		if !ui.Confirm(msg) {
			fmt.Println("Cancelled.")
			return nil
		}
	}

	if err := prov.RemoveChannel(ctx, platform); err != nil {
		return err
	}

	if ui.OutputJSON {
		ui.EmitJSON(map[string]any{
			"platform": platform,
			"status":   "removed",
		})
	} else {
		fmt.Printf("Channel %s removed.\n", platform)
	}
	return nil
}

func channelsListRun(cmd *cobra.Command, args []string) error {
	ctx, cancel := commandContext()
	defer cancel()

	// --agent <name> → per-binding detail for one agent. Uses the root
	// persistent --agent flag (flagAgent) rather than a local shadow so
	// pflag state stays consistent across commands.
	if flagAgent != "" {
		return channelsListAgentBindings(ctx, flagAgent)
	}

	statuses, err := prov.ListChannels(ctx)
	if err != nil {
		return err
	}

	if ui.OutputJSON {
		ui.EmitJSON(statuses)
		return nil
	}

	if len(statuses) == 0 {
		fmt.Println("No channel platforms registered.")
		return nil
	}

	headers := []string{"PLATFORM", "STATUS", "ROUTER", "BOUND AGENTS"}
	var rows [][]string
	for _, s := range statuses {
		status := "not configured"
		if s.Configured {
			status = "configured"
		}
		router := "-"
		if s.RouterRunning {
			router = "running"
		} else if s.Configured {
			router = "stopped"
		}
		agents := "-"
		if len(s.BoundAgents) > 0 {
			agents = strings.Join(s.BoundAgents, ", ")
		}
		rows = append(rows, []string{s.Platform, status, router, agents})
	}

	ui.PrintTable(headers, rows)
	return nil
}

// channelsListAgentBindings renders a table of an agent's individual
// channel bindings — one row per (platform, id). Sorted by platform,
// then by the order the bindings were added.
func channelsListAgentBindings(ctx context.Context, agentName string) error {
	a, err := prov.GetAgent(ctx, agentName)
	if err != nil {
		return err
	}

	if ui.OutputJSON {
		payload := make([]map[string]any, 0, len(a.Channels))
		for _, b := range a.Channels {
			payload = append(payload, map[string]any{
				"platform": b.Platform,
				"id":       b.ID,
				"label":    b.Label,
			})
		}
		ui.EmitJSON(payload)
		return nil
	}

	if len(a.Channels) == 0 {
		fmt.Printf("Agent %s has no channel bindings (gateway-only).\n", agentName)
		return nil
	}

	// Stable order: platform alphabetical, then insertion order within a platform.
	byPlatform := map[string][]channels.ChannelBinding{}
	var platforms []string
	for _, b := range a.Channels {
		if _, seen := byPlatform[b.Platform]; !seen {
			platforms = append(platforms, b.Platform)
		}
		byPlatform[b.Platform] = append(byPlatform[b.Platform], b)
	}
	sort.Strings(platforms)

	headers := []string{"PLATFORM", "ID", "LABEL"}
	var rows [][]string
	for _, platform := range platforms {
		for _, b := range byPlatform[platform] {
			label := b.Label
			if label == "" {
				label = "-"
			}
			rows = append(rows, []string{b.Platform, b.ID, label})
		}
	}
	ui.PrintTable(headers, rows)
	return nil
}

func channelsBindRun(cmd *cobra.Command, args []string) error {
	ctx, cancel := commandContext()
	defer cancel()

	agentName := args[0]
	if err := validateAgentName(agentName); err != nil {
		return err
	}

	binding, err := channels.ParseBinding(args[1])
	if err != nil {
		return err
	}
	if channelBindLabel != "" {
		binding.Label = channelBindLabel
	}

	if err := prov.BindChannel(ctx, agentName, binding); err != nil {
		return err
	}

	if ui.OutputJSON {
		ui.EmitJSON(map[string]any{
			"agent":    agentName,
			"platform": binding.Platform,
			"id":       binding.ID,
			"label":    binding.Label,
			"status":   "bound",
		})
	} else {
		fmt.Printf("Agent %s bound to %s:%s.\n", agentName, binding.Platform, binding.ID)
	}
	return nil
}

func channelsUnbindRun(cmd *cobra.Command, args []string) error {
	ctx, cancel := commandContext()
	defer cancel()

	agentName := args[0]

	// Accept either "<platform>" (legacy, single-binding) or "<platform>:<id>".
	platform, id := splitPlatformID(args[1])

	// Resolve which bindings to remove. Interactive picker kicks in only when
	// the user didn't specify an id AND we're not in JSON/MCP mode AND the
	// agent actually has multiple bindings for the platform. In every other
	// case we defer to the provider (which applies the legacy single-binding
	// removal or returns ErrAmbiguousUnbind for JSON callers).
	idsToRemove, cancelled, err := resolveUnbindTargets(ctx, agentName, platform, id)
	if err != nil {
		return err
	}
	if cancelled {
		fmt.Println("Cancelled.")
		return nil
	}

	// Confirm before a specific-id removal (not needed after the picker —
	// selecting in the picker is itself the confirmation). Skip in JSON mode.
	if !channelUnbindForce && !ui.JSONInputActive && id != "" {
		if !ui.Confirm(fmt.Sprintf("Remove %s:%s binding from agent %s?", platform, id, agentName)) {
			fmt.Println("Cancelled.")
			return nil
		}
	}

	for _, targetID := range idsToRemove {
		if err := prov.UnbindChannel(ctx, agentName, platform, targetID); err != nil {
			// JSON/MCP callers hitting the empty-id-with-multiple-bindings path
			// reach this branch; enhance the error with the enumerated list.
			if errors.Is(err, provider.ErrAmbiguousUnbind) {
				if a, getErr := prov.GetAgent(ctx, agentName); getErr == nil {
					return provider.FormatAmbiguousUnbindError(agentName, platform, a.ChannelBindings(platform))
				}
			}
			return err
		}
	}

	emitUnbindResult(agentName, platform, idsToRemove, id)
	return nil
}

// splitPlatformID splits "platform" or "platform:id" into its parts.
// An empty id means "remove the sole binding if exactly one exists".
func splitPlatformID(arg string) (platform, id string) {
	if i := strings.Index(arg, ":"); i >= 0 {
		return arg[:i], arg[i+1:]
	}
	return arg, ""
}

// resolveUnbindTargets decides which channel IDs the caller wants to remove.
// In interactive mode, if the user omitted the id and the agent has multiple
// bindings for the platform, an interactive picker runs and returns either
// one selected binding, all bindings, or a cancel signal. In every other
// case the function returns a single-element slice whose value is the
// user-supplied id (possibly empty — the provider handles legacy single-binding
// semantics or returns ErrAmbiguousUnbind for JSON callers).
func resolveUnbindTargets(ctx context.Context, agentName, platform, id string) (ids []string, cancelled bool, err error) {
	if id != "" {
		return []string{id}, false, nil
	}
	// Skip the interactive picker for any machine-readable invocation —
	// either JSON input mode (no TTY to prompt from) or JSON output mode
	// (callers parsing stdout can't handle the picker's stderr prompt
	// cleanly). The provider will return ErrAmbiguousUnbind, which the
	// outer handler formats into a structured enumerated error.
	if ui.JSONInputActive || ui.OutputJSON {
		return []string{""}, false, nil
	}

	a, err := prov.GetAgent(ctx, agentName)
	if err != nil {
		return nil, false, err
	}
	bindings := a.ChannelBindings(platform)
	if len(bindings) <= 1 {
		// 0 bindings → provider returns "no binding" error; 1 binding → legacy
		// single-removal path. Either way, defer to the provider.
		return []string{""}, false, nil
	}

	chosen, cancelled, pickErr := pickBindingFrom(os.Stdin, os.Stderr, agentName, platform, bindings)
	if pickErr != nil {
		return nil, false, pickErr
	}
	return chosen, cancelled, nil
}

// pickBindingFrom runs the interactive multi-binding picker, rendering a
// numbered list of bindings (with labels when present) and reading one of:
//
//	"1"..."N"  → remove that specific binding
//	"a" / "all"→ remove every listed binding
//	""/"n"/"N" → cancel (no removal)
//
// Any other input is treated as a cancel with an error explaining the
// invalid choice. Returns (ids, cancelled, err) where a non-nil err means
// the picker itself failed (stdin closed, etc.) and the caller should
// surface the error rather than act on the other return values.
//
// Accepts reader/writer arguments to make the picker testable.
func pickBindingFrom(r io.Reader, w io.Writer, agentName, platform string, bindings []channels.ChannelBinding) (ids []string, cancelled bool, err error) {
	fmt.Fprintf(w, "Agent %q has %d %s bindings:\n", agentName, len(bindings), platform)
	for i, b := range bindings {
		label := ""
		if b.Label != "" {
			label = " (" + b.Label + ")"
		}
		fmt.Fprintf(w, "  [%d] %s:%s%s\n", i+1, b.Platform, b.ID, label)
	}
	fmt.Fprintln(w, "  [a] all")

	choices := make([]string, 0, len(bindings)+2)
	for i := range bindings {
		choices = append(choices, strconv.Itoa(i+1))
	}
	choices = append(choices, "a", "N")
	fmt.Fprintf(w, "Which to remove? [%s]: ", strings.Join(choices, "/"))

	scanner := bufio.NewScanner(r)
	if !scanner.Scan() {
		if scanErr := scanner.Err(); scanErr != nil {
			return nil, false, fmt.Errorf("failed to read input: %w", scanErr)
		}
		// EOF with no input — treat as cancel (safe default).
		return nil, true, nil
	}
	answer := strings.ToLower(strings.TrimSpace(scanner.Text()))

	switch answer {
	case "", "n", "no":
		return nil, true, nil
	case "a", "all":
		out := make([]string, len(bindings))
		for i, b := range bindings {
			out[i] = b.ID
		}
		return out, false, nil
	}
	if n, convErr := strconv.Atoi(answer); convErr == nil && n >= 1 && n <= len(bindings) {
		return []string{bindings[n-1].ID}, false, nil
	}
	return nil, false, fmt.Errorf("invalid choice %q; cancelled without removing anything", answer)
}

// emitUnbindResult writes the success message to stdout/JSON, adapting to
// whether one specific binding, the sole-legacy binding, or all bindings
// were removed.
func emitUnbindResult(agentName, platform string, idsRemoved []string, userSuppliedID string) {
	if ui.OutputJSON {
		ui.EmitJSON(map[string]any{
			"agent":    agentName,
			"platform": platform,
			"id":       userSuppliedID,
			"count":    len(idsRemoved),
			"status":   "unbound",
		})
		return
	}
	switch {
	case len(idsRemoved) == 1 && idsRemoved[0] == "":
		fmt.Printf("Agent %s unbound from %s.\n", agentName, platform)
	case len(idsRemoved) == 1:
		fmt.Printf("Agent %s unbound from %s:%s.\n", agentName, platform, idsRemoved[0])
	default:
		fmt.Printf("Agent %s unbound from %d %s channels.\n", agentName, len(idsRemoved), platform)
	}
}
