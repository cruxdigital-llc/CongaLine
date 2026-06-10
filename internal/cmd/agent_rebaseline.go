package cmd

import (
	"fmt"

	"github.com/cruxdigital-llc/conga-line/pkg/ui"
	"github.com/spf13/cobra"
)

var rebaselineYes bool

func agentRebaselineRun(cmd *cobra.Command, args []string) error {
	ctx, cancel := commandContext()
	defer cancel()

	name := args[0]

	if !rebaselineYes && !ui.OutputJSON {
		if !ui.Confirm(fmt.Sprintf("Reset %s's agent-custom.json to the generated baseline? "+
			"This discards admin customizations (a timestamped backup is kept).", name)) {
			fmt.Println("Aborted.")
			return nil
		}
	}

	if err := prov.ResetAgentCustomConfig(ctx, name); err != nil {
		return err
	}
	if err := prov.RefreshAgent(ctx, name); err != nil {
		return err
	}

	if ui.OutputJSON {
		ui.EmitJSON(struct {
			Agent  string `json:"agent"`
			Status string `json:"status"`
		}{Agent: name, Status: "rebaselined"})
		return nil
	}

	fmt.Printf("Agent %s reset to baseline and refreshed.\n", name)
	return nil
}
