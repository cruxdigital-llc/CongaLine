package discovery

import (
	"strings"
	"testing"
)

func TestARNSessionNameExtraction(t *testing.T) {
	tests := []struct {
		name       string
		arn        string
		expectName string
	}{
		{
			"assumed role with email",
			"arn:aws:sts::123456789012:assumed-role/OpenClawUser/user@example.com",
			"user@example.com",
		},
		{
			"assumed role with username",
			"arn:aws:sts::123456789012:assumed-role/RoleName/admin",
			"admin",
		},
		{
			"iam user (only 2 slash-separated parts)",
			"arn:aws:iam::123456789012:user/admin",
			"", // Only 2 parts after split by "/", needs >= 3
		},
		{
			"only two parts (no session name)",
			"arn:aws:sts::123456789012:assumed-role/RoleName",
			"",
		},
		{
			"root account (no slash)",
			"arn:aws:iam::123456789012:root",
			"",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Replicate the ARN parsing logic from ResolveIdentity
			sessionName := ""
			parts := strings.Split(tt.arn, "/")
			if len(parts) >= 3 {
				sessionName = parts[len(parts)-1]
			}

			if sessionName != tt.expectName {
				t.Errorf("ARN %q: got session name %q, want %q", tt.arn, sessionName, tt.expectName)
			}
		})
	}
}
