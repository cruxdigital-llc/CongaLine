package executor

import (
	"context"
	"time"

	awsutil "github.com/cruxdigital-llc/openclaw-template/cli/internal/aws"
)

// SSMExecutor executes scripts on a remote EC2 instance via AWS SSM.
type SSMExecutor struct {
	client     awsutil.SSMClient
	instanceID string
}

func NewSSMExecutor(client awsutil.SSMClient, instanceID string) *SSMExecutor {
	return &SSMExecutor{client: client, instanceID: instanceID}
}

func (e *SSMExecutor) RunScript(ctx context.Context, script string, timeout time.Duration) (*Result, error) {
	r, err := awsutil.RunCommand(ctx, e.client, e.instanceID, script, timeout)
	if err != nil {
		return nil, err
	}
	return &Result{Status: r.Status, Stdout: r.Stdout, Stderr: r.Stderr}, nil
}

func (e *SSMExecutor) InstanceID() string {
	return e.instanceID
}
