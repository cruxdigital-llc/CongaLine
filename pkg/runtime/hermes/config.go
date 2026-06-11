package hermes

import (
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/cruxdigital-llc/conga-line/pkg/runtime"
	"gopkg.in/yaml.v3"
)

// hermesKnownProviderHosts is the set of base_url substrings that Hermes'
// delegation.provider enum natively recognizes. When the operator's overlay
// declares an openai provider with a base_url matching one of these, we leave
// the parent configuration to handle credential/endpoint resolution; we still
// emit only delegation.model since our overlay doesn't carry a Hermes
// provider-adapter name.
//
// Source: cli-config.yaml.example in github.com/NousResearch/hermes-agent
// (delegation: block, "Supported: openrouter, nous, zai, kimi-coding, minimax").
var hermesKnownProviderHosts = []string{
	"openrouter.ai",
	"nousresearch.com",
	"z.ai",
	"kimi.com",
	"minimax.com",
}

// hermesDegradedWarningOnce dedupes degraded-mode warnings so config
// regeneration loops (e.g. conga refresh-all) don't spam stderr.
var hermesDegradedWarningOnce sync.Map // key: "<agent>:<baseURL>"

// stderrWriter is overridable for tests. nil = use os.Stderr.
var stderrWriter = func() *os.File { return os.Stderr }

func (r *Runtime) GenerateConfig(params runtime.ConfigParams) ([]byte, error) {
	apiServerExtra := map[string]any{
		"host": "0.0.0.0",
		"port": ContainerPort,
	}

	// Gateway auth token
	if params.GatewayToken != "" {
		apiServerExtra["key"] = params.GatewayToken
		origins := []string{
			fmt.Sprintf("http://localhost:%d", ContainerPort),
			fmt.Sprintf("http://localhost:%d", params.Agent.GatewayPort),
		}
		apiServerExtra["cors_origins"] = strings.Join(origins, ",")
	}

	platforms := map[string]any{
		"api_server": map[string]any{
			"enabled": true,
			"extra":   apiServerExtra,
		},
	}

	// Enable webhook adapter if any channels are bound.
	// All channel events (Slack, Telegram, etc.) arrive via the webhook adapter.
	if len(params.Agent.Channels) > 0 {
		platforms["webhook"] = map[string]any{
			"enabled": true,
			"extra": map[string]any{
				"host": "0.0.0.0",
				"port": 8644,
			},
		}
	}

	cfg := map[string]any{
		"platforms": platforms,
	}

	// Set model if provided (configured during conga admin setup).
	// If empty, Hermes will prompt the user via `hermes model` on first use.
	if params.Model != "" {
		cfg["model"] = params.Model
	}

	// Per-agent model overlay. Hermes's provider config is structured
	// differently from OpenClaw's models.providers.<id> block, so the overlay
	// is applied in degraded mode: cfg["model"] is set to provider/name so
	// /status reflects operator intent, but custom base_urls (e.g. LiteLLM
	// proxies) are not natively addressable. A one-time stderr warning fires
	// when the base_url isn't a recognized Hermes adapter host. See
	// product-knowledge/standards/upstream-openclaw-issues.md for the full
	// degraded-mode rationale and the spec slot for a real implementation.
	if params.Overlay != nil && params.Overlay.Model != nil {
		applyModelOverlay(cfg, params.Overlay.Model, params.Agent.Name)
	}

	if params.Overlay != nil && params.Overlay.Subagents != nil {
		applySubagentsOverlay(cfg, params.Overlay.Subagents, params.Agent.Name)
	}

	return yaml.Marshal(cfg)
}

// applyModelOverlay emits the operator's primary-model intent into cfg["model"]
// (overriding any setup-time params.Model) and warns when the base_url isn't
// one Hermes natively supports.
func applyModelOverlay(cfg map[string]any, m *runtime.ModelOverlay, agentName string) {
	cfg["model"] = m.Provider + "/" + m.Name

	if m.Provider == runtime.ProviderOpenAI && m.BaseURL != "" && !hermesProviderRecognized(m.BaseURL) {
		emitHermesModelDegradedWarning(agentName, m.BaseURL)
	}
}

func emitHermesModelDegradedWarning(agentName, baseURL string) {
	key := "model:" + agentName + ":" + baseURL
	if _, loaded := hermesDegradedWarningOnce.LoadOrStore(key, struct{}{}); loaded {
		return
	}
	fmt.Fprintf(stderrWriter(), "warning: agent %s: hermes runtime does not natively address the primary model's openai base_url %q; cfg.model is set to the overlay's provider/name but Hermes will use whatever provider config is wired up at runtime (typically the setup-time default). To genuinely route the primary model through a custom endpoint, configure it directly in Hermes's cli-config.yaml until upstream support lands.\n",
		agentName, baseURL)
}

// applySubagentsOverlay emits Hermes' top-level delegation: block based on
// the operator's overlay. Hermes uses delegate_task at runtime and reads
// delegation.{model, max_concurrent_children, max_spawn_depth, ...} as
// its defaults — see website/docs/user-guide/features/delegation.md and
// cli-config.yaml.example in github.com/NousResearch/hermes-agent.
//
// Provider enum mismatch (degraded mode): Hermes' delegation.provider is a
// fixed enum (openrouter, nous, zai, kimi-coding, minimax). Our overlay uses
// {ollama, openai} with an explicit base_url. We don't try to map between
// the two systems automatically — instead we emit delegation.model only and
// rely on Hermes inheriting the parent's provider config. For openai with a
// base_url that isn't a known Hermes adapter host, we emit a one-time
// stderr warning so operators understand the degradation.
//
// delegation_mode is OpenClaw-only and intentionally NOT emitted in Hermes
// output (Hermes ignores it; emitting it would be confusing).
func applySubagentsOverlay(cfg map[string]any, s *runtime.SubagentsOverlay, agentName string) {
	m := s.Model
	delegation := map[string]any{
		"model": m.Provider + "/" + m.Name,
	}
	if s.MaxConcurrent > 0 {
		delegation["max_concurrent_children"] = s.MaxConcurrent
	}
	if s.MaxSpawnDepth > 0 {
		delegation["max_spawn_depth"] = s.MaxSpawnDepth
	}

	cfg["delegation"] = delegation

	// Degraded-mode warning only applies to openai providers with custom
	// base_urls. Ollama transparently inherits via the parent's setup;
	// hosted openai (empty base_url) needs no special handling.
	if m.Provider == runtime.ProviderOpenAI && m.BaseURL != "" && !hermesProviderRecognized(m.BaseURL) {
		emitHermesDegradedWarning(agentName, m.BaseURL)
	}
}

// hermesProviderRecognized reports whether the given base_url matches one of
// Hermes' built-in provider-adapter hosts.
func hermesProviderRecognized(baseURL string) bool {
	lower := strings.ToLower(baseURL)
	for _, host := range hermesKnownProviderHosts {
		if strings.Contains(lower, host) {
			return true
		}
	}
	return false
}

func emitHermesDegradedWarning(agentName, baseURL string) {
	key := agentName + ":" + baseURL
	if _, loaded := hermesDegradedWarningOnce.LoadOrStore(key, struct{}{}); loaded {
		return
	}
	fmt.Fprintf(stderrWriter(), "warning: agent %s: hermes runtime does not natively support the subagent's openai base_url %q; subagent will inherit the parent's provider config.\n",
		agentName, baseURL)
}

func (r *Runtime) ConfigFileName() string { return "config.yaml" }

// CustomConfigFileName returns "" — the Hermes runtime has no admin-owned
// include file (the $include layering is OpenClaw-specific).
func (r *Runtime) CustomConfigFileName() string { return "" }

// ManagedCustomConfigFiles returns nil — Hermes has no $include layering, so the
// fleet / per-agent declarative layers (feature #31) are a no-op.
func (r *Runtime) ManagedCustomConfigFiles() []string { return nil }
