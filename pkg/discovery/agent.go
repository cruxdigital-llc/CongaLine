package discovery

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	ssmtypes "github.com/aws/aws-sdk-go-v2/service/ssm/types"
	awsutil "github.com/cruxdigital-llc/conga-line/pkg/aws"
	"github.com/cruxdigital-llc/conga-line/pkg/provider"
)

// parseAgentConfig parses an agent config from its SSM parameter name and JSON value.
// The agent name is derived from the last segment of the parameter path.
func parseAgentConfig(paramName, jsonValue string) (*provider.AgentConfig, error) {
	var cfg provider.AgentConfig
	if err := json.Unmarshal([]byte(jsonValue), &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse agent config for %q: %w", paramName, err)
	}
	parts := strings.Split(paramName, "/")
	cfg.Name = parts[len(parts)-1]
	return &cfg, nil
}

// ResolveAgent loads an agent's config by name. Returns provider.ErrNotFound
// (wrapped) only when SSM confirmed the parameter doesn't exist; every other
// AWS failure (expired SSO token, network error, IAM denied, throttling) is
// wrapped with context but the underlying cause is preserved via errors.Is
// / errors.As, so the caller — and the human reading the error — can tell
// "this agent isn't provisioned" from "I can't talk to AWS".
func ResolveAgent(ctx context.Context, ssmClient awsutil.SSMClient, name string) (*provider.AgentConfig, error) {
	paramName := fmt.Sprintf("/conga/agents/%s", name)
	value, err := awsutil.GetParameter(ctx, ssmClient, paramName)
	if err != nil {
		var notFound *ssmtypes.ParameterNotFound
		if errors.As(err, &notFound) {
			return nil, fmt.Errorf("agent %q not found: %w", name, provider.ErrNotFound)
		}
		return nil, fmt.Errorf("failed to look up agent %q: %w", name, err)
	}

	cfg, err := parseAgentConfig(paramName, value)
	if err != nil {
		return nil, err
	}
	return cfg, nil
}

func ResolveAgentByIAM(ctx context.Context, ssmClient awsutil.SSMClient, iamIdentity string) (*provider.AgentConfig, error) {
	entries, err := awsutil.GetParametersByPath(ctx, ssmClient, "/conga/agents/")
	if err != nil {
		return nil, fmt.Errorf("failed to query agents: %w", err)
	}

	for _, e := range entries {
		cfg, err := parseAgentConfig(e.Name, e.Value)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: skipping agent parameter %s: %v\n", e.Name, err)
			continue
		}
		if cfg.IAMIdentity != "" && cfg.IAMIdentity == iamIdentity {
			return cfg, nil
		}
	}

	return nil, fmt.Errorf("no agent found with iam_identity %q", iamIdentity)
}

func ListAgents(ctx context.Context, ssmClient awsutil.SSMClient) ([]provider.AgentConfig, error) {
	entries, err := awsutil.GetParametersByPath(ctx, ssmClient, "/conga/agents/")
	if err != nil {
		return nil, fmt.Errorf("failed to query agents: %w", err)
	}

	var agents []provider.AgentConfig
	for _, e := range entries {
		cfg, err := parseAgentConfig(e.Name, e.Value)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: skipping agent parameter %s: %v\n", e.Name, err)
			continue
		}
		agents = append(agents, *cfg)
	}
	return agents, nil
}
