package runtime

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// CurrentOverlaySchemaVersion is the highest agent.yaml schema version this
// binary understands. Documents declaring a higher version are rejected so a
// config authored for a newer binary cannot be silently mis-parsed.
const CurrentOverlaySchemaVersion = 1

// Supported overlay providers.
const (
	ProviderOllama = "ollama"
	ProviderOpenAI = "openai"
)

// OllamaLocalAPIKey is the sentinel apiKey value OpenClaw recognizes for
// loopback / LAN / .local Ollama hosts. No real bearer token is required.
const OllamaLocalAPIKey = "ollama-local"

// AgentOverlay is the parsed shape of behavior/agents/<name>/agent.yaml.
//
// The file is operator-authored, optional, and provider-agnostic. When set,
// runtime-specific config generators (see pkg/runtime/openclaw/config.go)
// translate it into the runtime's native config shape.
type AgentOverlay struct {
	// Version is the schema version. Required in normative documents; absence
	// is accepted as 1 with a one-time loader warning for graceful onboarding.
	Version int `yaml:"version"`

	// Model overrides the runtime's default model selection. nil = no override.
	Model *ModelOverlay `yaml:"model,omitempty"`
}

// ModelOverlay declares which LLM the agent should use.
type ModelOverlay struct {
	// Provider is the OpenClaw provider id (e.g. "ollama", "openai").
	// Case-sensitive, lowercase. Unknown values are rejected.
	Provider string `yaml:"provider"`

	// Name is the model tag as the provider expects it
	// (e.g. "qwen3:6b" for Ollama, "gpt-5.5" for OpenAI).
	Name string `yaml:"name"`

	// BaseURL is the provider endpoint. Required for self-hosted providers.
	//
	// Ollama: must NOT end in "/v1" — the OpenAI-compatible endpoint breaks
	// tool calling. Use the native API URL (e.g. "http://host:11434").
	//
	// OpenAI-compatible: typically ends in "/v1". Empty falls back to the
	// hosted OpenAI API.
	BaseURL string `yaml:"base_url,omitempty"`
}

// Validate enforces the schema rules described in
// specs/2026-05-19_feature_local-model-routing/spec.md.
//
// Returns nil for a nil receiver (lets callers call Validate unconditionally).
func (o *AgentOverlay) Validate() error {
	if o == nil {
		return nil
	}

	// Version gate runs first; a document declaring a future version may have
	// fields the current binary cannot interpret correctly.
	switch o.Version {
	case 0, CurrentOverlaySchemaVersion:
		// 0 = absent in YAML; accepted as current with a one-time warning at load time.
	default:
		return fmt.Errorf("agent.yaml version %d requires a newer conga binary; this binary supports version %d only", o.Version, CurrentOverlaySchemaVersion)
	}

	if o.Model != nil {
		if err := o.Model.validate(); err != nil {
			return fmt.Errorf("model: %w", err)
		}
	}

	return nil
}

// validate enforces the model block rules.
func (m *ModelOverlay) validate() error {
	if m.Provider == "" {
		return errors.New("provider is required when model block is present")
	}

	// Case-sensitive enum. Suggest the canonical form on casing mismatch
	// so operators can fix the typo without reading docs.
	switch m.Provider {
	case ProviderOllama, ProviderOpenAI:
		// ok
	default:
		// Detect a casing-only mismatch and produce a friendlier message.
		lower := strings.ToLower(m.Provider)
		if lower == ProviderOllama || lower == ProviderOpenAI {
			return fmt.Errorf("provider %q must be lowercase %q", m.Provider, lower)
		}
		return fmt.Errorf("unknown model provider %q: supported: %s, %s", m.Provider, ProviderOllama, ProviderOpenAI)
	}

	if strings.TrimSpace(m.Name) == "" {
		return errors.New("name is required when provider is set")
	}

	// Provider-specific base_url rules.
	switch m.Provider {
	case ProviderOllama:
		if m.BaseURL == "" {
			return errors.New("ollama provider requires base_url")
		}
		if strings.HasSuffix(strings.TrimRight(m.BaseURL, "/"), "/v1") {
			return errors.New("ollama provider requires base_url without /v1 suffix; the OpenAI-compatible endpoint breaks tool calling (see openclaw docs/providers/ollama.md)")
		}
	case ProviderOpenAI:
		// base_url is optional for OpenAI; empty = use the hosted API.
		// Non-/v1 suffix is allowed (some compat servers expose /openai/v1 etc.)
		// but emits a soft warning at load time.
	}

	if m.BaseURL != "" {
		u, err := url.Parse(m.BaseURL)
		if err != nil {
			return fmt.Errorf("base_url %q is not a valid URL: %w", m.BaseURL, err)
		}
		if u.Scheme != "http" && u.Scheme != "https" {
			return fmt.Errorf("base_url %q must use http or https scheme", m.BaseURL)
		}
		if u.Host == "" {
			return fmt.Errorf("base_url %q has no host", m.BaseURL)
		}
	}

	return nil
}

// OpenAIBaseURLLooksNonstandard reports whether the given OpenAI base_url is
// missing a recognizable /v1 segment. Callers may emit a soft warning when
// this returns true; validation does NOT reject.
func OpenAIBaseURLLooksNonstandard(baseURL string) bool {
	if baseURL == "" {
		return false
	}
	trimmed := strings.TrimRight(baseURL, "/")
	return !strings.HasSuffix(trimmed, "/v1") && !strings.Contains(trimmed, "/v1/")
}
