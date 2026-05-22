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

// maxSaneTokenCount bounds context_window and max_tokens in the overlay
// schema. The point is to catch typos (an extra zero on what was meant to
// be 131072 → 1310720, etc.) at load time rather than at runtime. Real
// models top out well below this today; if a future model exceeds 10M
// tokens, lift this and add a test.
const maxSaneTokenCount = 10_000_000

// Supported overlay providers.
const (
	ProviderOllama = "ollama"
	ProviderOpenAI = "openai"
)

// OllamaLocalAPIKey is the sentinel apiKey value OpenClaw recognizes for
// loopback / LAN / .local Ollama hosts. No real bearer token is required.
const OllamaLocalAPIKey = "ollama-local"

// AgentOverlay is the parsed shape of agents/<name>/agent.yaml.
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

	// ContextWindow is the model's maximum prompt+completion token limit.
	// Optional. When set, emitted as models.providers.<id>.models[0].contextWindow
	// in openclaw.json. Use this when the runtime's auto-detected value is
	// wrong or when the endpoint advertises one number but the underlying
	// model enforces a lower cap (e.g. LiteLLM in front of vLLM where
	// max_model_len < the advertised metadata).
	ContextWindow int `yaml:"context_window,omitempty"`

	// MaxTokens is the per-response max output tokens. Optional. When set,
	// emitted as models.providers.<id>.models[0].maxTokens. Defaults from the
	// runtime are not always safe for self-hosted endpoints — set this
	// explicitly when the provider rejects requests with max_completion_tokens
	// greater than its hard limit.
	MaxTokens int `yaml:"max_tokens,omitempty"`
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

	if m.ContextWindow < 0 {
		return fmt.Errorf("context_window must be positive when set, got %d", m.ContextWindow)
	}
	if m.MaxTokens < 0 {
		return fmt.Errorf("max_tokens must be positive when set, got %d", m.MaxTokens)
	}
	if m.ContextWindow > maxSaneTokenCount {
		return fmt.Errorf("context_window %d exceeds sane cap (%d) — did you add an extra zero?", m.ContextWindow, maxSaneTokenCount)
	}
	if m.MaxTokens > maxSaneTokenCount {
		return fmt.Errorf("max_tokens %d exceeds sane cap (%d) — did you add an extra zero?", m.MaxTokens, maxSaneTokenCount)
	}
	if m.ContextWindow > 0 && m.MaxTokens > 0 && m.MaxTokens > m.ContextWindow {
		return fmt.Errorf("max_tokens (%d) cannot exceed context_window (%d)", m.MaxTokens, m.ContextWindow)
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
