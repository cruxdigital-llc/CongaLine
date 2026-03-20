package executor

import (
	"context"
	"time"
)

// Result represents the output of a command executed on the host.
type Result struct {
	Status string // "Success" or "Failed"
	Stdout string
	Stderr string
}

// HostExecutor abstracts how the CLI executes scripts on the target host.
// The AWS implementation uses SSM SendCommand + polling.
// A local implementation would use exec.Command directly.
type HostExecutor interface {
	// RunScript executes a shell script on the host and returns its output.
	RunScript(ctx context.Context, script string, timeout time.Duration) (*Result, error)

	// InstanceID returns the identifier for the target host.
	// For AWS this is the EC2 instance ID; for local mode this could be "localhost".
	InstanceID() string
}
