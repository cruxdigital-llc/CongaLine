package common

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cruxdigital-llc/conga-line/pkg/provider"
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

func TestResolveCustomConfigSources(t *testing.T) {
	dir := t.TempDir()
	// fleet source for openclaw runtime
	fleetDir := filepath.Join(dir, "_defaults", "openclaw")
	if err := os.MkdirAll(fleetDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fleetDir, "fleet-custom.json"), []byte(`{"skills":{"allow":["github"]}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	// per-agent source
	agDir := filepath.Join(dir, "a1")
	if err := os.MkdirAll(agDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agDir, "custom.json"), []byte(`{"mcp":{"servers":{"x":{}}}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	got := ResolveCustomConfigSources(dir, provider.AgentConfig{Name: "a1", Runtime: "openclaw"})
	if string(got.Fleet) != `{"skills":{"allow":["github"]}}` {
		t.Errorf("fleet = %q", got.Fleet)
	}
	if string(got.PerAgent) != `{"mcp":{"servers":{"x":{}}}}` {
		t.Errorf("perAgent = %q", got.PerAgent)
	}

	// agent with no per-agent source, different name → fleet still resolves, perAgent nil
	got2 := ResolveCustomConfigSources(dir, provider.AgentConfig{Name: "nope", Runtime: "openclaw"})
	if got2.Fleet == nil {
		t.Error("fleet should resolve for any agent of the runtime")
	}
	if got2.PerAgent != nil {
		t.Errorf("perAgent should be nil when absent, got %q", got2.PerAgent)
	}
}

func TestValidateManagedConfigSources(t *testing.T) {
	// Clean fleet + per-agent → nil.
	if err := ValidateManagedConfigSources(CustomConfigSources{
		Fleet:    []byte(`{"mcp":{"servers":{}}}`),
		PerAgent: []byte(`{"skills":{"allow":["github"]}}`),
	}); err != nil {
		t.Fatalf("clean sources should pass: %v", err)
	}

	// Reserved key in FLEET → fail closed (blast radius), error names the file.
	err := ValidateManagedConfigSources(CustomConfigSources{Fleet: []byte(`{"channels":{"slack":{}}}`)})
	if err == nil || !strings.Contains(err.Error(), "fleet-custom.json") {
		t.Fatalf("reserved key in fleet must fail closed: %v", err)
	}

	// Reserved key in PER-AGENT → fail closed too.
	err = ValidateManagedConfigSources(CustomConfigSources{PerAgent: []byte(`{"gateway":{"port":1}}`)})
	if err == nil || !strings.Contains(err.Error(), "custom.json") {
		t.Fatalf("reserved key in per-agent must fail closed: %v", err)
	}

	// JSON5 (unparseable) is tolerated — backstopped by on-host load + integrity.
	if err := ValidateManagedConfigSources(CustomConfigSources{Fleet: []byte("{\n  // c\n}")}); err != nil {
		t.Fatalf("JSON5 fleet should be tolerated pre-deploy: %v", err)
	}

	// nil sources → nil.
	if err := ValidateManagedConfigSources(CustomConfigSources{}); err != nil {
		t.Fatalf("nil sources should pass: %v", err)
	}
}

func TestValidateCustomConfigKeys_NamesFile(t *testing.T) {
	// The generic guard names the offending layer (feature #31) instead of the
	// hardcoded "agent-custom.json".
	err := ValidateCustomConfigKeys("fleet-custom.json", []byte(`{"gateway":{"port":1}}`))
	if err == nil {
		t.Fatal("expected reserved-key error")
	}
	if !strings.Contains(err.Error(), "fleet-custom.json") || !strings.Contains(err.Error(), "gateway") {
		t.Fatalf("error should name file + key: %v", err)
	}
}

func TestClassifyIncludeValidation(t *testing.T) {
	// Reserved key → hard violation, no warn.
	warn, err := ClassifyIncludeValidation("fleet-custom.json", []byte(`{"plugins":{"entries":{}}}`))
	if err == nil || warn != "" {
		t.Fatalf("reserved key: want err only, got warn=%q err=%v", warn, err)
	}
	if !strings.Contains(err.Error(), "CONFIG INTEGRITY VIOLATION") || !strings.Contains(err.Error(), "fleet-custom.json") {
		t.Fatalf("violation message shape: %v", err)
	}
	// JSON5 (unparseable) → warn, no hard error.
	warn, err = ClassifyIncludeValidation("agent-managed-custom.json", []byte("{\n  // comment\n}"))
	if err != nil || warn == "" {
		t.Fatalf("json5: want warn only, got warn=%q err=%v", warn, err)
	}
	// Clean → neither.
	warn, err = ClassifyIncludeValidation("fleet-custom.json", []byte(`{"mcp":{"servers":{}}}`))
	if err != nil || warn != "" {
		t.Fatalf("clean: want neither, got warn=%q err=%v", warn, err)
	}
}

func TestResolveRuntimeDefaults(t *testing.T) {
	dir := t.TempDir()
	ocDir := filepath.Join(dir, "_defaults", "openclaw")
	if err := os.MkdirAll(ocDir, 0o755); err != nil {
		t.Fatal(err)
	}
	want := []byte(`{"agents":{"defaults":{"model":{"primary":"x"}}}}`)
	if err := os.WriteFile(filepath.Join(ocDir, RuntimeDefaultsSourceName), want, 0o644); err != nil {
		t.Fatal(err)
	}

	// openclaw agent → reads the on-disk file (runtime-level, not per-agent/type).
	got := ResolveRuntimeDefaults(dir, provider.AgentConfig{Name: "a1", Runtime: "openclaw"})
	if string(got) != string(want) {
		t.Errorf("openclaw defaults = %q, want %q", got, want)
	}

	// hermes agent → nil (only openclaw ships a de-embedded baseline).
	if got := ResolveRuntimeDefaults(dir, provider.AgentConfig{Name: "h1", Runtime: "hermes"}); got != nil {
		t.Errorf("hermes should yield nil, got %q", got)
	}

	// openclaw with no on-disk file → nil (generator uses its embedded fallback).
	empty := t.TempDir()
	if got := ResolveRuntimeDefaults(empty, provider.AgentConfig{Name: "a1", Runtime: "openclaw"}); got != nil {
		t.Errorf("absent file should yield nil, got %q", got)
	}
}
