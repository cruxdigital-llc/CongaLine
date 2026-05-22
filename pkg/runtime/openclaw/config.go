package openclaw

import (
	_ "embed"
	"encoding/json"
	"fmt"

	"github.com/cruxdigital-llc/conga-line/pkg/channels"
	"github.com/cruxdigital-llc/conga-line/pkg/runtime"
)

//go:embed openclaw-defaults.json
var openclawDefaults []byte

func (r *Runtime) GenerateConfig(params runtime.ConfigParams) ([]byte, error) {
	var config map[string]any
	if err := json.Unmarshal(openclawDefaults, &config); err != nil {
		return nil, fmt.Errorf("failed to parse openclaw-defaults.json: %w", err)
	}

	config["gateway"] = buildGatewayConfig(ContainerPort, params.Agent.GatewayPort, params.GatewayToken)

	channelsCfg := map[string]any{}
	pluginsCfg := map[string]any{}

	for _, binding := range params.Agent.Channels {
		ch, ok := channels.Get(binding.Platform)
		if !ok {
			continue
		}
		hasCreds := ch.HasCredentials(params.Secrets.Values)
		pluginsCfg[binding.Platform] = ch.OpenClawPluginConfig(hasCreds)
		if hasCreds {
			section, err := ch.OpenClawChannelConfig(string(params.Agent.Type), binding, params.Secrets.Values)
			if err != nil {
				return nil, fmt.Errorf("channel %s config: %w", binding.Platform, err)
			}
			channelsCfg[binding.Platform] = section
		}
	}

	if len(channelsCfg) > 0 {
		config["channels"] = channelsCfg
	}
	if len(pluginsCfg) > 0 {
		config["plugins"] = map[string]any{"entries": pluginsCfg}
	}

	if params.Overlay != nil && params.Overlay.Model != nil {
		if err := applyModelOverlay(config, params.Overlay.Model); err != nil {
			return nil, fmt.Errorf("apply model overlay: %w", err)
		}
	}

	return json.MarshalIndent(config, "", "  ")
}

// applyModelOverlay mutates config in place to reflect the operator's model
// choice. See spec § "Runtime config generator" for the JSON shape contract.
func applyModelOverlay(config map[string]any, m *runtime.ModelOverlay) error {
	modelRef := m.Provider + "/" + m.Name

	// agents.defaults.model.primary + fallbacks + models allowlist
	agents, ok := config["agents"].(map[string]any)
	if !ok {
		return fmt.Errorf("openclaw-defaults.json missing agents section")
	}
	defaults, ok := agents["defaults"].(map[string]any)
	if !ok {
		return fmt.Errorf("openclaw-defaults.json missing agents.defaults section")
	}
	// agents.defaults.model: set primary to the overlay's choice. Fallbacks stay
	// empty so OpenClaw won't auto-switch on errors — operators control model
	// selection explicitly via /model in chat.
	defaults["model"] = map[string]any{
		"primary":   modelRef,
		"fallbacks": []any{},
	}
	// agents.defaults.models: MERGE the overlay's model into the existing
	// allowlist rather than replacing it. This preserves the runtime defaults
	// (e.g. anthropic/claude-opus-4-6 from openclaw-defaults.json) so operators
	// can /model into them mid-conversation. Lockdown — if you want it — should
	// be enforced at the egress policy layer, not by trimming the allowlist.
	allowlist, ok := defaults["models"].(map[string]any)
	if !ok {
		allowlist = map[string]any{}
		defaults["models"] = allowlist
	}
	allowlist[modelRef] = map[string]any{}

	// models.providers.<id> — endpoint, auth marker, and model entries.
	// OpenClaw's schema validator requires `models` to be a non-empty array
	// when models.providers.<id> is set explicitly; auto-discovery is bypassed
	// (see docs/providers/ollama.md "explicit config" section).
	modelEntry := map[string]any{
		"id":   m.Name,
		"name": m.Name,
	}
	// Capability hints — only emitted when the operator sets them in
	// agent.yaml. Without these, OpenClaw's default for max_completion_tokens
	// can exceed what a self-hosted endpoint enforces (e.g. LiteLLM/vLLM
	// where max_model_len < the advertised contextWindow), producing 400s.
	if m.ContextWindow > 0 {
		modelEntry["contextWindow"] = m.ContextWindow
	}
	if m.MaxTokens > 0 {
		modelEntry["maxTokens"] = m.MaxTokens
	}
	providerModels := []any{modelEntry}
	providerCfg := map[string]any{"models": providerModels}
	switch m.Provider {
	case runtime.ProviderOllama:
		providerCfg["baseUrl"] = m.BaseURL
		providerCfg["apiKey"] = runtime.OllamaLocalAPIKey
		providerCfg["api"] = runtime.ProviderOllama
	case runtime.ProviderOpenAI:
		if m.BaseURL != "" {
			providerCfg["baseUrl"] = m.BaseURL
		}
		// apiKey for openai flows via the OPENAI_API_KEY env var (set from the
		// openai-api-key secret); never write the literal value into config —
		// see CLAUDE.md note on OpenClaw issue #9627.
	default:
		return fmt.Errorf("unsupported overlay provider %q (validation should have caught this)", m.Provider)
	}

	models, ok := config["models"].(map[string]any)
	if !ok {
		models = map[string]any{}
		config["models"] = models
	}
	providers, ok := models["providers"].(map[string]any)
	if !ok {
		providers = map[string]any{}
		models["providers"] = providers
	}
	providers[m.Provider] = providerCfg

	return nil
}

func (r *Runtime) ConfigFileName() string { return "openclaw.json" }

// buildGatewayConfig produces the gateway section of openclaw.json.
func buildGatewayConfig(containerPort, hostPort int, token string) map[string]any {
	origins := []string{
		fmt.Sprintf("http://localhost:%d", containerPort),
		fmt.Sprintf("http://127.0.0.1:%d", containerPort),
	}
	if hostPort != containerPort {
		origins = append(origins,
			fmt.Sprintf("http://localhost:%d", hostPort),
			fmt.Sprintf("http://127.0.0.1:%d", hostPort),
		)
	}

	gw := map[string]any{
		"port": containerPort,
		// Gateway and agent runtime live in the same container, so mode is "local".
		// 0.0.0.0 binding (required for Docker -p port forwarding) comes from
		// bind="lan", not from mode. OpenClaw v2026.3.22+ refuses to start with
		// mode="remote" unless --allow-unconfigured is passed.
		"mode": "local",
		"bind": "lan",
		"controlUi": map[string]any{
			"allowedOrigins": origins,
		},
	}

	if token != "" {
		gw["auth"] = map[string]any{
			"mode":  "token",
			"token": token,
		}
	}

	return gw
}
