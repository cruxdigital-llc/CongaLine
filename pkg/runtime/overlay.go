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
//
// v1: model: block only (Feature #27 Local Model Routing).
// v2: adds subagents: block for in-runtime delegation (this feature).
const CurrentOverlaySchemaVersion = 2

// maxSaneSubagentConcurrency bounds SubagentsOverlay.MaxConcurrent. Real
// runtimes top out well below this (OpenClaw default ~8, Hermes default 3).
// The cap catches typos like 80 → 800.
const maxSaneSubagentConcurrency = 128

// Supported delegation modes (OpenClaw-only knob; Hermes ignores).
const (
	DelegationModeSuggest = "suggest"
	DelegationModePrefer  = "prefer"
)

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
	// is accepted as the current version with a one-time loader warning for
	// graceful onboarding.
	Version int `yaml:"version"`

	// Model overrides the runtime's default model selection. nil = no override.
	// Available in v1 and v2.
	Model *ModelOverlay `yaml:"model,omitempty"`

	// Subagents configures the runtime's native in-runtime delegation system
	// (OpenClaw agents.defaults.subagents / Hermes delegation:). nil = no
	// subagent configured.
	//
	// Available in v2 only. Setting this block in a v1 document is rejected
	// at validation time with a friendly "bump version" message.
	Subagents *SubagentsOverlay `yaml:"subagents,omitempty"`
}

// SubagentsOverlay configures the runtime's native subagent system. When
// present, Model is required — there is no point in declaring a subagent
// block without telling the runtime which model to delegate to.
//
// See specs/2026-05-22_feature_delegation-routing/ for design rationale and
// upstream-capability.md for the OpenClaw / Hermes mechanisms this maps to.
type SubagentsOverlay struct {
	// Model is the subagent's LLM. Required when the block is present.
	// Reuses ModelOverlay for the same validation surface as the primary model.
	Model *ModelOverlay `yaml:"model"`

	// DelegationMode is OpenClaw's prompt-level nudge: "suggest" (default) or
	// "prefer". Empty = use runtime default. Hermes ignores this field.
	DelegationMode string `yaml:"delegation_mode,omitempty"`

	// MaxConcurrent caps concurrent subagent runs. 0 = use runtime default
	// (OpenClaw ~8, Hermes 3). Maps to maxConcurrent (OpenClaw) and
	// max_concurrent_children (Hermes).
	MaxConcurrent int `yaml:"max_concurrent,omitempty"`

	// MaxSpawnDepth caps the nesting depth of subagent spawns
	// (Hermes-only knob; range 1..3). 0 = use runtime default.
	// OpenClaw ignores this field — its nesting is implicit/policy-driven.
	MaxSpawnDepth int `yaml:"max_spawn_depth,omitempty"`
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
// specs/2026-05-19_feature_local-model-routing/spec.md and
// specs/2026-05-22_feature_delegation-routing/spec.md.
//
// Returns nil for a nil receiver (lets callers call Validate unconditionally).
func (o *AgentOverlay) Validate() error {
	if o == nil {
		return nil
	}

	// Version gate runs first; a document declaring a future version may have
	// fields the current binary cannot interpret correctly. v1 stays explicitly
	// accepted so existing Feature #27 documents continue to parse without
	// modification.
	switch o.Version {
	case 0, 1, CurrentOverlaySchemaVersion:
		// 0 = absent in YAML; accepted as current with a one-time warning at load time.
		// 1 = legacy v1 documents (Feature #27). Subagents block is rejected below.
	default:
		return fmt.Errorf("agent.yaml version %d requires a newer conga binary; this binary supports version %d only", o.Version, CurrentOverlaySchemaVersion)
	}

	// Schema-version vs feature-use gate. A v1 document explicitly opting in
	// to v1 cannot use the v2 subagents block. (v0 / absent is treated as
	// current, so subagents is fine there.)
	if o.Subagents != nil && o.Version == 1 {
		return errors.New("subagents: requires schema version 2; bump `version:` to 2 to use subagents")
	}

	if o.Model != nil {
		if err := o.Model.validate(); err != nil {
			return fmt.Errorf("model: %w", err)
		}
	}

	if o.Subagents != nil {
		if err := o.Subagents.validate(); err != nil {
			return fmt.Errorf("subagents: %w", err)
		}
		// Cross-block: primary + subagent cannot share a provider key with
		// different endpoints (the runtime config generator emits one provider
		// entry per provider id).
		if o.Model != nil && o.Subagents.Model != nil {
			if err := checkSameProviderConflict(o.Model, o.Subagents.Model); err != nil {
				return fmt.Errorf("subagents: %w", err)
			}
		}
	}

	return nil
}

// validate enforces the subagents-block rules. Run by AgentOverlay.Validate
// only when the block is present.
func (s *SubagentsOverlay) validate() error {
	if s.Model == nil {
		return errors.New("model is required when subagents block is present")
	}
	// Model goes through the same enum + URL + cap validation as the primary.
	// In v2 this means provider ∈ {ollama, openai}; anthropic is rejected
	// implicitly via the existing enum.
	if err := s.Model.validate(); err != nil {
		return fmt.Errorf("model: %w", err)
	}

	if s.DelegationMode != "" &&
		s.DelegationMode != DelegationModeSuggest &&
		s.DelegationMode != DelegationModePrefer {
		return fmt.Errorf("delegation_mode %q: must be empty, %q, or %q",
			s.DelegationMode, DelegationModeSuggest, DelegationModePrefer)
	}

	if s.MaxConcurrent < 0 {
		return fmt.Errorf("max_concurrent must be non-negative, got %d", s.MaxConcurrent)
	}
	if s.MaxConcurrent > maxSaneSubagentConcurrency {
		return fmt.Errorf("max_concurrent %d exceeds sane cap (%d) — did you add an extra zero?",
			s.MaxConcurrent, maxSaneSubagentConcurrency)
	}

	// MaxSpawnDepth is a Hermes-only knob, range 0..3 (0 = use runtime default).
	if s.MaxSpawnDepth < 0 || s.MaxSpawnDepth > 3 {
		return fmt.Errorf("max_spawn_depth must be in range 0..3, got %d", s.MaxSpawnDepth)
	}

	return nil
}

// checkSameProviderConflict rejects: primary and subagent both reference the
// same provider key with different base_urls. The runtime config generator
// emits one `models.providers.<id>` block per provider id (one endpoint),
// so the two configs would clobber each other.
//
// Same provider + same base_url is fine (no conflict; just both models on the
// same endpoint). Different providers are fine (different `providers` block).
func checkSameProviderConflict(primary, sub *ModelOverlay) error {
	if primary.Provider != sub.Provider {
		return nil
	}
	if trimTrailingSlash(primary.BaseURL) == trimTrailingSlash(sub.BaseURL) {
		return nil
	}
	return fmt.Errorf("model provider %q is used by both primary and subagent with different base_urls (%q vs %q); each provider id must map to one endpoint in v2",
		sub.Provider, primary.BaseURL, sub.BaseURL)
}

func trimTrailingSlash(s string) string {
	return strings.TrimRight(s, "/")
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
