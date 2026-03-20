package cmd

import (
	"fmt"

	awsutil "github.com/cruxdigital-llc/openclaw-template/cli/internal/aws"
	"github.com/cruxdigital-llc/openclaw-template/cli/internal/ui"
	"github.com/spf13/cobra"
)

func adminCycleHostRun(cmd *cobra.Command, args []string) error {
	ctx, cancel := commandContext()
	defer cancel()
	if err := ensureClients(ctx); err != nil {
		return err
	}

	if !adminForce {
		if !ui.Confirm("This will restart the EC2 instance and ALL agent containers. Continue?") {
			fmt.Println("Cancelled.")
			return nil
		}
	}

	instanceID, err := findInstance(ctx)
	if err != nil {
		return err
	}

	// Stop
	fmt.Printf("Stopping instance %s...\n", instanceID)
	if err := awsutil.StopInstance(ctx, clients.EC2, instanceID); err != nil {
		return fmt.Errorf("failed to stop instance: %w", err)
	}

	spin := ui.NewSpinner("Waiting for instance to stop...")
	err = awsutil.WaitForState(ctx, clients.EC2, instanceID, "stopped")
	spin.Stop()
	if err != nil {
		return fmt.Errorf("instance failed to stop: %w", err)
	}
	fmt.Println("Instance stopped.")

	// Start
	fmt.Printf("Starting instance %s...\n", instanceID)
	if err := awsutil.StartInstance(ctx, clients.EC2, instanceID); err != nil {
		return fmt.Errorf("failed to start instance: %w", err)
	}

	spin = ui.NewSpinner("Waiting for instance to start...")
	err = awsutil.WaitForState(ctx, clients.EC2, instanceID, "running")
	spin.Stop()
	if err != nil {
		return fmt.Errorf("instance failed to start: %w", err)
	}

	fmt.Println("Instance running. SSM agent may take 1-2 minutes to reconnect.")
	fmt.Println("Use `cruxclaw status` to verify your container is healthy.")
	return nil
}
