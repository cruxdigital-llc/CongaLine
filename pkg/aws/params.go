package aws

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	ssmtypes "github.com/aws/aws-sdk-go-v2/service/ssm/types"
)

// GetParameter reads a single SSM parameter. Distinguishes "the parameter
// doesn't exist" from other AWS failures (expired SSO token, network error,
// IAM denied, throttling) so callers can see the actual cause instead of
// hiding everything behind a "not found" message.
func GetParameter(ctx context.Context, client SSMClient, name string) (string, error) {
	out, err := client.GetParameter(ctx, &ssm.GetParameterInput{
		Name: aws.String(name),
	})
	if err != nil {
		var notFound *ssmtypes.ParameterNotFound
		if errors.As(err, &notFound) {
			return "", fmt.Errorf("parameter %s not found: %w", name, err)
		}
		return "", fmt.Errorf("failed to read parameter %s: %w", name, err)
	}
	return aws.ToString(out.Parameter.Value), nil
}

func PutParameter(ctx context.Context, client SSMClient, name, value string) error {
	_, err := client.PutParameter(ctx, &ssm.PutParameterInput{
		Name:      aws.String(name),
		Value:     aws.String(value),
		Type:      ssmtypes.ParameterTypeString,
		Overwrite: aws.Bool(true),
	})
	if err != nil {
		return fmt.Errorf("failed to put parameter %s: %w", name, err)
	}
	return nil
}

func DeleteParameter(ctx context.Context, client SSMClient, name string) error {
	_, err := client.DeleteParameter(ctx, &ssm.DeleteParameterInput{
		Name: aws.String(name),
	})
	if err != nil {
		return fmt.Errorf("failed to delete parameter %s: %w", name, err)
	}
	return nil
}

type ParameterEntry struct {
	Name  string
	Value string
}

func GetParametersByPath(ctx context.Context, client SSMClient, path string) ([]ParameterEntry, error) {
	var entries []ParameterEntry
	var nextToken *string

	for {
		out, err := client.GetParametersByPath(ctx, &ssm.GetParametersByPathInput{
			Path:      aws.String(path),
			NextToken: nextToken,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to list parameters under %s: %w", path, err)
		}

		for _, p := range out.Parameters {
			name := aws.ToString(p.Name)
			// Skip by-iam/ sub-path entries
			if strings.Contains(name, "/by-iam/") {
				continue
			}
			entries = append(entries, ParameterEntry{
				Name:  name,
				Value: aws.ToString(p.Value),
			})
		}

		if out.NextToken == nil {
			break
		}
		nextToken = out.NextToken
	}

	return entries, nil
}
