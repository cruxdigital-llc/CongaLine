package aws

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	ssmtypes "github.com/aws/aws-sdk-go-v2/service/ssm/types"
)

// mockSSMClient is a minimal mock for testing RunCommand.
type mockSSMClient struct {
	sendCommandFn         func(ctx context.Context, params *ssm.SendCommandInput, optFns ...func(*ssm.Options)) (*ssm.SendCommandOutput, error)
	getCommandInvFn       func(ctx context.Context, params *ssm.GetCommandInvocationInput, optFns ...func(*ssm.Options)) (*ssm.GetCommandInvocationOutput, error)
	getParameterFn        func(ctx context.Context, params *ssm.GetParameterInput, optFns ...func(*ssm.Options)) (*ssm.GetParameterOutput, error)
	putParameterFn        func(ctx context.Context, params *ssm.PutParameterInput, optFns ...func(*ssm.Options)) (*ssm.PutParameterOutput, error)
	deleteParameterFn     func(ctx context.Context, params *ssm.DeleteParameterInput, optFns ...func(*ssm.Options)) (*ssm.DeleteParameterOutput, error)
	getParametersByPathFn func(ctx context.Context, params *ssm.GetParametersByPathInput, optFns ...func(*ssm.Options)) (*ssm.GetParametersByPathOutput, error)
	startSessionFn        func(ctx context.Context, params *ssm.StartSessionInput, optFns ...func(*ssm.Options)) (*ssm.StartSessionOutput, error)
}

func (m *mockSSMClient) SendCommand(ctx context.Context, params *ssm.SendCommandInput, optFns ...func(*ssm.Options)) (*ssm.SendCommandOutput, error) {
	return m.sendCommandFn(ctx, params, optFns...)
}
func (m *mockSSMClient) GetCommandInvocation(ctx context.Context, params *ssm.GetCommandInvocationInput, optFns ...func(*ssm.Options)) (*ssm.GetCommandInvocationOutput, error) {
	return m.getCommandInvFn(ctx, params, optFns...)
}
func (m *mockSSMClient) GetParameter(ctx context.Context, params *ssm.GetParameterInput, optFns ...func(*ssm.Options)) (*ssm.GetParameterOutput, error) {
	if m.getParameterFn != nil {
		return m.getParameterFn(ctx, params, optFns...)
	}
	return nil, fmt.Errorf("not implemented")
}
func (m *mockSSMClient) PutParameter(ctx context.Context, params *ssm.PutParameterInput, optFns ...func(*ssm.Options)) (*ssm.PutParameterOutput, error) {
	if m.putParameterFn != nil {
		return m.putParameterFn(ctx, params, optFns...)
	}
	return nil, fmt.Errorf("not implemented")
}
func (m *mockSSMClient) DeleteParameter(ctx context.Context, params *ssm.DeleteParameterInput, optFns ...func(*ssm.Options)) (*ssm.DeleteParameterOutput, error) {
	if m.deleteParameterFn != nil {
		return m.deleteParameterFn(ctx, params, optFns...)
	}
	return nil, fmt.Errorf("not implemented")
}
func (m *mockSSMClient) GetParametersByPath(ctx context.Context, params *ssm.GetParametersByPathInput, optFns ...func(*ssm.Options)) (*ssm.GetParametersByPathOutput, error) {
	if m.getParametersByPathFn != nil {
		return m.getParametersByPathFn(ctx, params, optFns...)
	}
	return nil, fmt.Errorf("not implemented")
}
func (m *mockSSMClient) StartSession(ctx context.Context, params *ssm.StartSessionInput, optFns ...func(*ssm.Options)) (*ssm.StartSessionOutput, error) {
	if m.startSessionFn != nil {
		return m.startSessionFn(ctx, params, optFns...)
	}
	return nil, fmt.Errorf("not implemented")
}

func TestRunCommand_Success(t *testing.T) {
	callCount := 0
	mock := &mockSSMClient{
		sendCommandFn: func(ctx context.Context, params *ssm.SendCommandInput, optFns ...func(*ssm.Options)) (*ssm.SendCommandOutput, error) {
			return &ssm.SendCommandOutput{
				Command: &ssmtypes.Command{CommandId: aws.String("cmd-123")},
			}, nil
		},
		getCommandInvFn: func(ctx context.Context, params *ssm.GetCommandInvocationInput, optFns ...func(*ssm.Options)) (*ssm.GetCommandInvocationOutput, error) {
			callCount++
			if callCount < 2 {
				return &ssm.GetCommandInvocationOutput{
					Status: ssmtypes.CommandInvocationStatusInProgress,
				}, nil
			}
			return &ssm.GetCommandInvocationOutput{
				Status:                ssmtypes.CommandInvocationStatusSuccess,
				StandardOutputContent: aws.String("hello world"),
				StandardErrorContent:  aws.String(""),
			}, nil
		},
	}

	result, err := RunCommand(context.Background(), mock, "i-12345", "echo hello", 30*time.Second)
	if err != nil {
		t.Fatalf("RunCommand returned error: %v", err)
	}
	if result.Status != "Success" {
		t.Errorf("expected Success, got %s", result.Status)
	}
	if result.Stdout != "hello world" {
		t.Errorf("expected 'hello world', got %q", result.Stdout)
	}
}

func TestRunCommand_Failed(t *testing.T) {
	mock := &mockSSMClient{
		sendCommandFn: func(ctx context.Context, params *ssm.SendCommandInput, optFns ...func(*ssm.Options)) (*ssm.SendCommandOutput, error) {
			return &ssm.SendCommandOutput{
				Command: &ssmtypes.Command{CommandId: aws.String("cmd-123")},
			}, nil
		},
		getCommandInvFn: func(ctx context.Context, params *ssm.GetCommandInvocationInput, optFns ...func(*ssm.Options)) (*ssm.GetCommandInvocationOutput, error) {
			return &ssm.GetCommandInvocationOutput{
				Status:                ssmtypes.CommandInvocationStatusFailed,
				StandardOutputContent: aws.String(""),
				StandardErrorContent:  aws.String("command not found"),
			}, nil
		},
	}

	result, err := RunCommand(context.Background(), mock, "i-12345", "bad-cmd", 30*time.Second)
	if err != nil {
		t.Fatalf("RunCommand returned error: %v", err)
	}
	if result.Status != "Failed" {
		t.Errorf("expected Failed, got %s", result.Status)
	}
	if result.Stderr != "command not found" {
		t.Errorf("expected 'command not found', got %q", result.Stderr)
	}
}

func TestRunCommand_Timeout(t *testing.T) {
	mock := &mockSSMClient{
		sendCommandFn: func(ctx context.Context, params *ssm.SendCommandInput, optFns ...func(*ssm.Options)) (*ssm.SendCommandOutput, error) {
			return &ssm.SendCommandOutput{
				Command: &ssmtypes.Command{CommandId: aws.String("cmd-123")},
			}, nil
		},
		getCommandInvFn: func(ctx context.Context, params *ssm.GetCommandInvocationInput, optFns ...func(*ssm.Options)) (*ssm.GetCommandInvocationOutput, error) {
			// Always return InProgress
			return &ssm.GetCommandInvocationOutput{
				Status: ssmtypes.CommandInvocationStatusInProgress,
			}, nil
		},
	}

	// Use a very short timeout
	_, err := RunCommand(context.Background(), mock, "i-12345", "sleep 999", 4*time.Second)
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
	if err.Error() != "command timed out after 4s" {
		t.Errorf("expected timeout error message, got: %v", err)
	}
}

func TestRunCommand_ConsecutiveErrors_Recovery(t *testing.T) {
	errorCount := 0
	mock := &mockSSMClient{
		sendCommandFn: func(ctx context.Context, params *ssm.SendCommandInput, optFns ...func(*ssm.Options)) (*ssm.SendCommandOutput, error) {
			return &ssm.SendCommandOutput{
				Command: &ssmtypes.Command{CommandId: aws.String("cmd-123")},
			}, nil
		},
		getCommandInvFn: func(ctx context.Context, params *ssm.GetCommandInvocationInput, optFns ...func(*ssm.Options)) (*ssm.GetCommandInvocationOutput, error) {
			errorCount++
			if errorCount <= 5 {
				return nil, fmt.Errorf("transient error")
			}
			return &ssm.GetCommandInvocationOutput{
				Status:                ssmtypes.CommandInvocationStatusSuccess,
				StandardOutputContent: aws.String("recovered"),
				StandardErrorContent:  aws.String(""),
			}, nil
		},
	}

	result, err := RunCommand(context.Background(), mock, "i-12345", "echo test", 60*time.Second)
	if err != nil {
		t.Fatalf("RunCommand returned error: %v", err)
	}
	if result.Stdout != "recovered" {
		t.Errorf("expected 'recovered', got %q", result.Stdout)
	}
}

func TestRunCommand_ConsecutiveErrors_Exceeded(t *testing.T) {
	mock := &mockSSMClient{
		sendCommandFn: func(ctx context.Context, params *ssm.SendCommandInput, optFns ...func(*ssm.Options)) (*ssm.SendCommandOutput, error) {
			return &ssm.SendCommandOutput{
				Command: &ssmtypes.Command{CommandId: aws.String("cmd-123")},
			}, nil
		},
		getCommandInvFn: func(ctx context.Context, params *ssm.GetCommandInvocationInput, optFns ...func(*ssm.Options)) (*ssm.GetCommandInvocationOutput, error) {
			return nil, fmt.Errorf("persistent error")
		},
	}

	_, err := RunCommand(context.Background(), mock, "i-12345", "echo test", 60*time.Second)
	if err == nil {
		t.Fatal("expected error after consecutive failures, got nil")
	}
}

func TestRunCommand_SendCommandFailure(t *testing.T) {
	mock := &mockSSMClient{
		sendCommandFn: func(ctx context.Context, params *ssm.SendCommandInput, optFns ...func(*ssm.Options)) (*ssm.SendCommandOutput, error) {
			return nil, fmt.Errorf("access denied")
		},
	}

	_, err := RunCommand(context.Background(), mock, "i-12345", "echo test", 30*time.Second)
	if err == nil {
		t.Fatal("expected error from SendCommand, got nil")
	}
}
