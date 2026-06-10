package common

import (
	"errors"
	"strings"
	"testing"
)

func TestValidateAgentCustomConfig(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		wantErr bool
		wantKey string // substring expected in error (for reserved-key cases)
	}{
		{name: "empty", in: "", wantErr: false},
		{name: "whitespace", in: "  \n ", wantErr: false},
		{name: "empty object", in: "{}", wantErr: false},
		{name: "legit mcp server", in: `{"mcp":{"servers":{"linear":{"url":"https://mcp.linear.app/sse"}}}}`, wantErr: false},
		{name: "legit skills", in: `{"skills":{"allow":["github"]}}`, wantErr: false},
		{name: "injects channels", in: `{"channels":{"slack":{"channels":{"C999":{"enabled":true}}}}}`, wantErr: true, wantKey: "channels"},
		{name: "overrides gateway", in: `{"gateway":{"port":29999}}`, wantErr: true, wantKey: "gateway"},
		{name: "adds plugins", in: `{"plugins":{"entries":{"x":{}}}}`, wantErr: true, wantKey: "plugins"},
		{name: "nested include", in: `{"$include":["evil.json"]}`, wantErr: true, wantKey: "$include"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateAgentCustomConfig([]byte(tc.in))
			if tc.wantErr && err == nil {
				t.Fatalf("expected error for %q", tc.in)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error for %q: %v", tc.in, err)
			}
			if tc.wantKey != "" && (err == nil || !strings.Contains(err.Error(), tc.wantKey)) {
				t.Fatalf("error %v should mention %q", err, tc.wantKey)
			}
		})
	}
}

func TestValidateAgentCustomConfig_JSON5Unparseable(t *testing.T) {
	// JSON5 with a // comment in a URL must NOT be naively stripped/misjudged —
	// we surface ErrCustomConfigUnparseable so callers warn rather than guess.
	in := `{
  // admin: Linear MCP
  "mcp": { "servers": { "linear": { "url": "https://mcp.linear.app/sse" } } },
}`
	err := ValidateAgentCustomConfig([]byte(in))
	if !errors.Is(err, ErrCustomConfigUnparseable) {
		t.Fatalf("want ErrCustomConfigUnparseable, got %v", err)
	}
}
