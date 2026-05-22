package runtime

import (
	"strings"
	"testing"
)

func TestAgentOverlay_Validate_Nil(t *testing.T) {
	var o *AgentOverlay
	if err := o.Validate(); err != nil {
		t.Fatalf("nil overlay: want nil error, got %v", err)
	}
}

func TestAgentOverlay_Validate_Version(t *testing.T) {
	tests := []struct {
		name    string
		version int
		wantErr string // substring expected in error, "" = no error
	}{
		{"version absent (0) accepted", 0, ""},
		{"version 1 accepted (legacy)", 1, ""},
		{"version 2 accepted (current)", 2, ""},
		{"version 3 rejected", 3, "version 3 requires a newer conga binary"},
		{"version 99 rejected", 99, "version 99 requires a newer conga binary"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			o := &AgentOverlay{Version: tc.version}
			err := o.Validate()
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("want nil error, got %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("want error containing %q, got nil", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("want error containing %q, got %q", tc.wantErr, err.Error())
			}
		})
	}
}

func TestModelOverlay_Validate_Provider(t *testing.T) {
	tests := []struct {
		name     string
		model    ModelOverlay
		wantErr  string
		wantHint string // additional substring check for friendly hints
	}{
		{
			name:    "empty provider",
			model:   ModelOverlay{Provider: "", Name: "x", BaseURL: "http://host:1/"},
			wantErr: "provider is required",
		},
		{
			name:     "casing mismatch (Ollama)",
			model:    ModelOverlay{Provider: "Ollama", Name: "x", BaseURL: "http://host:1"},
			wantErr:  "must be lowercase",
			wantHint: `"ollama"`,
		},
		{
			name:     "casing mismatch (OPENAI)",
			model:    ModelOverlay{Provider: "OPENAI", Name: "x", BaseURL: "https://api.openai.com/v1"},
			wantErr:  "must be lowercase",
			wantHint: `"openai"`,
		},
		{
			name:     "unknown provider",
			model:    ModelOverlay{Provider: "azure", Name: "x", BaseURL: "https://x/v1"},
			wantErr:  "unknown model provider",
			wantHint: "supported: ollama, openai",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			o := &AgentOverlay{Version: 1, Model: &tc.model}
			err := o.Validate()
			if err == nil {
				t.Fatalf("want error containing %q, got nil", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("want error containing %q, got %q", tc.wantErr, err.Error())
			}
			if tc.wantHint != "" && !strings.Contains(err.Error(), tc.wantHint) {
				t.Fatalf("want hint %q in error, got %q", tc.wantHint, err.Error())
			}
		})
	}
}

func TestModelOverlay_Validate_Name(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"empty name", ""},
		{"whitespace-only name", "   "},
		{"tab-only name", "\t\t"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			o := &AgentOverlay{Version: 1, Model: &ModelOverlay{
				Provider: "ollama",
				Name:     tc.input,
				BaseURL:  "http://h:1",
			}}
			err := o.Validate()
			if err == nil || !strings.Contains(err.Error(), "name is required") {
				t.Fatalf("want 'name is required' error, got %v", err)
			}
		})
	}
}

func TestModelOverlay_Validate_OllamaBaseURL(t *testing.T) {
	tests := []struct {
		name    string
		baseURL string
		wantErr string
	}{
		{"empty base_url for ollama", "", "ollama provider requires base_url"},
		{"ollama with /v1 footgun", "http://host:11434/v1", "without /v1 suffix"},
		{"ollama with /v1/ trailing slash", "http://host:11434/v1/", "without /v1 suffix"},
		{"ollama valid", "http://192.168.1.5:11434", ""},
		{"ollama valid with trailing slash", "http://host:11434/", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			o := &AgentOverlay{Version: 1, Model: &ModelOverlay{
				Provider: "ollama",
				Name:     "qwen3:6b",
				BaseURL:  tc.baseURL,
			}}
			err := o.Validate()
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("want nil error, got %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("want error containing %q, got nil", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("want error containing %q, got %q", tc.wantErr, err.Error())
			}
		})
	}
}

func TestModelOverlay_Validate_OpenAIBaseURL(t *testing.T) {
	tests := []struct {
		name    string
		baseURL string
		wantErr string // "" = no validation error (warning is a separate concern)
	}{
		{"openai default (empty) accepted", "", ""},
		{"openai with /v1 accepted", "https://api.openai.com/v1", ""},
		{"openai self-hosted with /v1 accepted", "http://10.0.0.5:8000/v1", ""},
		{"openai non-/v1 accepted (warning at load time)", "http://10.0.0.5:8000", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			o := &AgentOverlay{Version: 1, Model: &ModelOverlay{
				Provider: "openai",
				Name:     "gpt-5.5",
				BaseURL:  tc.baseURL,
			}}
			err := o.Validate()
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("want nil error, got %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("want error containing %q, got %v", tc.wantErr, err)
			}
		})
	}
}

func TestModelOverlay_Validate_CapabilityCaps(t *testing.T) {
	tests := []struct {
		name          string
		contextWindow int
		maxTokens     int
		wantErr       string
	}{
		{"both unset (default)", 0, 0, ""},
		{"context_window only", 131072, 0, ""},
		{"max_tokens only", 0, 8192, ""},
		{"both set, valid", 131072, 8192, ""},
		{"max_tokens equals context_window", 131072, 131072, ""},
		{"negative context_window", -1, 0, "context_window must be positive"},
		{"negative max_tokens", 0, -1, "max_tokens must be positive"},
		{"max_tokens exceeds context_window", 1024, 2048, "max_tokens (2048) cannot exceed context_window (1024)"},
		{"context_window above sane cap", 100_000_000, 0, "exceeds sane cap"},
		{"max_tokens above sane cap", 0, 100_000_000, "exceeds sane cap"},
		{"context_window at sane cap accepted", 10_000_000, 0, ""},
		{"max_tokens at sane cap accepted", 0, 10_000_000, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			o := &AgentOverlay{Version: 1, Model: &ModelOverlay{
				Provider:      "openai",
				Name:          "qwen36",
				BaseURL:       "http://192.168.1.5:4000/v1",
				ContextWindow: tc.contextWindow,
				MaxTokens:     tc.maxTokens,
			}}
			err := o.Validate()
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("want nil error, got %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("want error containing %q, got nil", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("want error containing %q, got %q", tc.wantErr, err.Error())
			}
		})
	}
}

func TestModelOverlay_Validate_URLShape(t *testing.T) {
	tests := []struct {
		name    string
		baseURL string
		wantErr string
	}{
		{"ftp scheme", "ftp://host:1", "must use http or https"},
		{"file scheme", "file:///etc/passwd", "must use http or https"},
		{"no scheme", "host:1/path", "must use http or https"},
		{"malformed URL", "http://[::1", "not a valid URL"},
		{"no host", "http:///path", "no host"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			o := &AgentOverlay{Version: 1, Model: &ModelOverlay{
				Provider: "openai",
				Name:     "x",
				BaseURL:  tc.baseURL,
			}}
			err := o.Validate()
			if err == nil {
				t.Fatalf("want error containing %q, got nil", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("want error containing %q, got %q", tc.wantErr, err.Error())
			}
		})
	}
}

func TestModelOverlay_Validate_HappyPath(t *testing.T) {
	tests := []struct {
		name    string
		overlay AgentOverlay
	}{
		{
			name: "ollama spark example",
			overlay: AgentOverlay{
				Version: 1,
				Model: &ModelOverlay{
					Provider: "ollama",
					Name:     "qwen3:6b",
					BaseURL:  "http://192.168.181.97:11434",
				},
			},
		},
		{
			name: "openai hosted (no base_url)",
			overlay: AgentOverlay{
				Version: 1,
				Model: &ModelOverlay{
					Provider: "openai",
					Name:     "gpt-5.5",
				},
			},
		},
		{
			name: "openai self-hosted with /v1",
			overlay: AgentOverlay{
				Version: 1,
				Model: &ModelOverlay{
					Provider: "openai",
					Name:     "qwen-2.5-72b-instruct",
					BaseURL:  "http://10.0.0.5:8000/v1",
				},
			},
		},
		{
			name:    "no model block (just version)",
			overlay: AgentOverlay{Version: 1},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.overlay.Validate(); err != nil {
				t.Fatalf("want nil error, got %v", err)
			}
		})
	}
}

func TestSubagentsOverlay_RequiresVersion2(t *testing.T) {
	o := &AgentOverlay{
		Version: 1,
		Subagents: &SubagentsOverlay{
			Model: &ModelOverlay{
				Provider: "openai",
				Name:     "qwen-2.5-72b-instruct",
				BaseURL:  "https://litellm.lan/v1",
			},
		},
	}
	err := o.Validate()
	if err == nil || !strings.Contains(err.Error(), "subagents: requires schema version 2") {
		t.Fatalf("want subagents-needs-v2 error, got %v", err)
	}
}

func TestSubagentsOverlay_ModelRequired(t *testing.T) {
	o := &AgentOverlay{
		Version:   2,
		Subagents: &SubagentsOverlay{}, // empty: no Model
	}
	err := o.Validate()
	if err == nil || !strings.Contains(err.Error(), "model is required when subagents block is present") {
		t.Fatalf("want missing-model error, got %v", err)
	}
}

func TestSubagentsOverlay_ModelGoesThroughExistingValidation(t *testing.T) {
	tests := []struct {
		name    string
		model   ModelOverlay
		wantErr string
	}{
		{
			name:    "anthropic rejected via existing enum",
			model:   ModelOverlay{Provider: "anthropic", Name: "claude-opus-4-7", BaseURL: ""},
			wantErr: "unknown model provider",
		},
		{
			name:    "ollama subagent without base_url",
			model:   ModelOverlay{Provider: "ollama", Name: "qwen3:6b"},
			wantErr: "ollama provider requires base_url",
		},
		{
			name:    "ollama subagent with /v1 footgun",
			model:   ModelOverlay{Provider: "ollama", Name: "qwen", BaseURL: "http://h:11434/v1"},
			wantErr: "without /v1 suffix",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			o := &AgentOverlay{
				Version:   2,
				Subagents: &SubagentsOverlay{Model: &tc.model},
			}
			err := o.Validate()
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("want error containing %q, got %v", tc.wantErr, err)
			}
		})
	}
}

func TestSubagentsOverlay_DelegationMode(t *testing.T) {
	tests := []struct {
		name    string
		mode    string
		wantErr string
	}{
		{"empty accepted", "", ""},
		{"suggest accepted", "suggest", ""},
		{"prefer accepted", "prefer", ""},
		{"unknown rejected", "encourage", `delegation_mode "encourage"`},
		{"casing rejected", "Prefer", `delegation_mode "Prefer"`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			o := &AgentOverlay{
				Version: 2,
				Subagents: &SubagentsOverlay{
					Model: &ModelOverlay{
						Provider: "openai",
						Name:     "x",
						BaseURL:  "http://h:8000/v1",
					},
					DelegationMode: tc.mode,
				},
			}
			err := o.Validate()
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("want nil error, got %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("want error containing %q, got %v", tc.wantErr, err)
			}
		})
	}
}

func TestSubagentsOverlay_MaxConcurrent(t *testing.T) {
	tests := []struct {
		name    string
		max     int
		wantErr string
	}{
		{"zero (use default) accepted", 0, ""},
		{"reasonable value accepted", 4, ""},
		{"at sane cap accepted", 128, ""},
		{"negative rejected", -1, "max_concurrent must be non-negative"},
		{"above sane cap rejected", 1000, "exceeds sane cap"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			o := &AgentOverlay{
				Version: 2,
				Subagents: &SubagentsOverlay{
					Model: &ModelOverlay{
						Provider: "openai",
						Name:     "x",
						BaseURL:  "http://h:8000/v1",
					},
					MaxConcurrent: tc.max,
				},
			}
			err := o.Validate()
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("want nil error, got %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("want error containing %q, got %v", tc.wantErr, err)
			}
		})
	}
}

func TestSubagentsOverlay_MaxSpawnDepth(t *testing.T) {
	tests := []struct {
		name    string
		depth   int
		wantErr string
	}{
		{"zero accepted", 0, ""},
		{"one accepted", 1, ""},
		{"three accepted", 3, ""},
		{"negative rejected", -1, "max_spawn_depth must be in range 0..3"},
		{"four rejected", 4, "max_spawn_depth must be in range 0..3"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			o := &AgentOverlay{
				Version: 2,
				Subagents: &SubagentsOverlay{
					Model: &ModelOverlay{
						Provider: "openai",
						Name:     "x",
						BaseURL:  "http://h:8000/v1",
					},
					MaxSpawnDepth: tc.depth,
				},
			}
			err := o.Validate()
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("want nil error, got %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("want error containing %q, got %v", tc.wantErr, err)
			}
		})
	}
}

func TestSubagentsOverlay_SameProviderConflict(t *testing.T) {
	tests := []struct {
		name     string
		primary  *ModelOverlay
		subagent *ModelOverlay
		wantErr  string // "" = accept
	}{
		{
			name: "same provider, same base_url — no conflict",
			primary: &ModelOverlay{
				Provider: "openai",
				Name:     "primary-model",
				BaseURL:  "http://litellm.lan/v1",
			},
			subagent: &ModelOverlay{
				Provider: "openai",
				Name:     "subagent-model",
				BaseURL:  "http://litellm.lan/v1",
			},
		},
		{
			name: "same provider, trailing-slash difference — no conflict",
			primary: &ModelOverlay{
				Provider: "openai",
				Name:     "primary-model",
				BaseURL:  "http://litellm.lan/v1/",
			},
			subagent: &ModelOverlay{
				Provider: "openai",
				Name:     "subagent-model",
				BaseURL:  "http://litellm.lan/v1",
			},
		},
		{
			name: "different providers — no conflict",
			primary: &ModelOverlay{
				Provider: "ollama",
				Name:     "primary",
				BaseURL:  "http://h:11434",
			},
			subagent: &ModelOverlay{
				Provider: "openai",
				Name:     "subagent",
				BaseURL:  "http://litellm.lan/v1",
			},
		},
		{
			name: "same provider, different base_urls — REJECTED",
			primary: &ModelOverlay{
				Provider: "openai",
				Name:     "primary",
				BaseURL:  "https://api.openai.com/v1",
			},
			subagent: &ModelOverlay{
				Provider: "openai",
				Name:     "subagent",
				BaseURL:  "http://litellm.lan/v1",
			},
			wantErr: `provider "openai" is used by both primary and subagent with different base_urls`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			o := &AgentOverlay{
				Version:   2,
				Model:     tc.primary,
				Subagents: &SubagentsOverlay{Model: tc.subagent},
			}
			err := o.Validate()
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("want nil error, got %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("want error containing %q, got %v", tc.wantErr, err)
			}
		})
	}
}

func TestSubagentsOverlay_HappyPath(t *testing.T) {
	o := &AgentOverlay{
		Version: 2,
		// No primary model = inherit runtime default (anthropic). Subagent is
		// Qwen via LiteLLM. Mirrors the role-code-dev default.
		Subagents: &SubagentsOverlay{
			Model: &ModelOverlay{
				Provider: "openai",
				Name:     "qwen-2.5-72b-instruct",
				BaseURL:  "https://litellm.lan/v1",
			},
			DelegationMode: "prefer",
			MaxConcurrent:  4,
		},
	}
	if err := o.Validate(); err != nil {
		t.Fatalf("want nil error, got %v", err)
	}
}

func TestOpenAIBaseURLLooksNonstandard(t *testing.T) {
	tests := []struct {
		baseURL string
		want    bool
	}{
		{"", false},
		{"https://api.openai.com/v1", false},
		{"https://api.openai.com/v1/", false},
		{"http://host:8000/v1", false},
		{"http://host:8000/openai/v1/embed", false},
		{"http://host:8000", true},
		{"http://host:8000/api", true},
	}
	for _, tc := range tests {
		t.Run(tc.baseURL, func(t *testing.T) {
			if got := OpenAIBaseURLLooksNonstandard(tc.baseURL); got != tc.want {
				t.Fatalf("OpenAIBaseURLLooksNonstandard(%q) = %v, want %v", tc.baseURL, got, tc.want)
			}
		})
	}
}
