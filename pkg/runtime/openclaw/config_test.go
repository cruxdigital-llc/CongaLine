package openclaw

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/cruxdigital-llc/conga-line/pkg/channels"
	"github.com/cruxdigital-llc/conga-line/pkg/provider"
	"github.com/cruxdigital-llc/conga-line/pkg/runtime"

	// Register the slack channel so OpenClawChannelConfig resolves.
	_ "github.com/cruxdigital-llc/conga-line/pkg/channels/slack"
)

func decodeJSON(t *testing.T, b []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return m
}

// baseParams returns a minimal ConfigParams that exercises the gateway path
// without any channels or overlay. Used as the "no overlay" regression baseline.
func baseParams() runtime.ConfigParams {
	return runtime.ConfigParams{
		Agent: provider.AgentConfig{
			Name:        "test",
			Type:        provider.AgentTypeUser,
			GatewayPort: 18789,
		},
		Secrets:      provider.SharedSecrets{Values: map[string]string{}},
		GatewayToken: "fixed-token-for-tests",
	}
}

func TestGenerateConfig_NoOverlay_PreservesDefaults(t *testing.T) {
	r := &Runtime{}
	out, err := r.GenerateConfig(baseParams())
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	cfg := decodeJSON(t, out)
	agents, ok := cfg["agents"].(map[string]any)
	if !ok {
		t.Fatalf("missing agents section")
	}
	defaults, ok := agents["defaults"].(map[string]any)
	if !ok {
		t.Fatalf("missing agents.defaults section")
	}

	// model.primary unchanged from openclaw-defaults.json
	model, ok := defaults["model"].(map[string]any)
	if !ok {
		t.Fatalf("missing agents.defaults.model")
	}
	if got := model["primary"]; got != "anthropic/claude-opus-4-6" {
		t.Fatalf("want anthropic/claude-opus-4-6, got %v", got)
	}

	// models allowlist unchanged
	models, ok := defaults["models"].(map[string]any)
	if !ok {
		t.Fatalf("missing agents.defaults.models")
	}
	if _, ok := models["anthropic/claude-opus-4-6"]; !ok {
		t.Fatalf("anthropic entry missing from allowlist: %+v", models)
	}

	// No models.providers block should be present without an overlay
	if _, ok := cfg["models"]; ok {
		t.Fatalf("models top-level key should not be set without overlay; got %+v", cfg["models"])
	}
}

func TestGenerateConfig_OllamaOverlay(t *testing.T) {
	params := baseParams()
	params.Overlay = &runtime.AgentOverlay{
		Version: 1,
		Model: &runtime.ModelOverlay{
			Provider: runtime.ProviderOllama,
			Name:     "qwen3:6b",
			BaseURL:  "http://192.168.181.97:11434",
		},
	}

	r := &Runtime{}
	out, err := r.GenerateConfig(params)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	cfg := decodeJSON(t, out)

	defaults := cfg["agents"].(map[string]any)["defaults"].(map[string]any)
	model := defaults["model"].(map[string]any)

	if got := model["primary"]; got != "ollama/qwen3:6b" {
		t.Fatalf("primary: want ollama/qwen3:6b, got %v", got)
	}
	fallbacks, ok := model["fallbacks"].([]any)
	if !ok || len(fallbacks) != 0 {
		t.Fatalf("fallbacks: want [], got %+v", model["fallbacks"])
	}

	allow := defaults["models"].(map[string]any)
	if _, ok := allow["ollama/qwen3:6b"]; !ok {
		t.Fatalf("allowlist missing ollama/qwen3:6b: %+v", allow)
	}
	if _, ok := allow["anthropic/claude-opus-4-6"]; ok {
		t.Fatalf("allowlist should not retain anthropic default: %+v", allow)
	}

	providers := cfg["models"].(map[string]any)["providers"].(map[string]any)
	ollama, ok := providers["ollama"].(map[string]any)
	if !ok {
		t.Fatalf("missing models.providers.ollama: %+v", providers)
	}
	want := map[string]any{
		"baseUrl": "http://192.168.181.97:11434",
		"apiKey":  "ollama-local",
		"api":     "ollama",
	}
	if !reflect.DeepEqual(ollama, want) {
		t.Fatalf("ollama provider: want %+v, got %+v", want, ollama)
	}

	// Workspace, heartbeat, etc. should be preserved from openclaw-defaults.json.
	if _, ok := defaults["workspace"]; !ok {
		t.Fatalf("defaults.workspace should be preserved from openclaw-defaults.json")
	}
	if _, ok := defaults["heartbeat"]; !ok {
		t.Fatalf("defaults.heartbeat should be preserved")
	}
}

func TestGenerateConfig_OpenAIOverlay(t *testing.T) {
	params := baseParams()
	params.Overlay = &runtime.AgentOverlay{
		Version: 1,
		Model: &runtime.ModelOverlay{
			Provider: runtime.ProviderOpenAI,
			Name:     "qwen-2.5-72b-instruct",
			BaseURL:  "http://10.0.0.5:8000/v1",
		},
	}

	r := &Runtime{}
	out, err := r.GenerateConfig(params)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	cfg := decodeJSON(t, out)

	defaults := cfg["agents"].(map[string]any)["defaults"].(map[string]any)
	if got := defaults["model"].(map[string]any)["primary"]; got != "openai/qwen-2.5-72b-instruct" {
		t.Fatalf("primary: want openai/qwen-2.5-72b-instruct, got %v", got)
	}

	openai := cfg["models"].(map[string]any)["providers"].(map[string]any)["openai"].(map[string]any)
	if got := openai["baseUrl"]; got != "http://10.0.0.5:8000/v1" {
		t.Fatalf("baseUrl: want http://10.0.0.5:8000/v1, got %v", got)
	}
	// apiKey must NOT be in config (flows via OPENAI_API_KEY env var — see CLAUDE.md note on OpenClaw issue #9627).
	if v, ok := openai["apiKey"]; ok {
		t.Fatalf("apiKey should NOT be written to config for openai provider, got %v", v)
	}
	if got, ok := openai["api"]; ok {
		t.Fatalf("api key is ollama-only; openai should omit it, got %v", got)
	}
}

func TestGenerateConfig_OpenAIOverlay_HostedDefault(t *testing.T) {
	// openai provider with no base_url = hosted OpenAI; no baseUrl emitted.
	params := baseParams()
	params.Overlay = &runtime.AgentOverlay{
		Version: 1,
		Model: &runtime.ModelOverlay{
			Provider: runtime.ProviderOpenAI,
			Name:     "gpt-5.5",
		},
	}

	r := &Runtime{}
	out, err := r.GenerateConfig(params)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	cfg := decodeJSON(t, out)

	openai := cfg["models"].(map[string]any)["providers"].(map[string]any)["openai"].(map[string]any)
	if _, ok := openai["baseUrl"]; ok {
		t.Fatalf("baseUrl should be omitted for hosted OpenAI (no base_url in overlay), got %+v", openai)
	}
}

func TestGenerateConfig_OverlayAndChannelsCoexist(t *testing.T) {
	// Overlay should not interfere with channel/plugin generation.
	params := baseParams()
	params.Agent.Channels = []channels.ChannelBinding{
		{Platform: "slack", ID: "UA13HEGTS"},
	}
	params.Secrets.Values = map[string]string{
		"slack-bot-token":      "xoxb-fake",
		"slack-signing-secret": "fake-secret",
	}
	params.Overlay = &runtime.AgentOverlay{
		Version: 1,
		Model: &runtime.ModelOverlay{
			Provider: runtime.ProviderOllama,
			Name:     "qwen3:6b",
			BaseURL:  "http://192.168.181.97:11434",
		},
	}

	r := &Runtime{}
	out, err := r.GenerateConfig(params)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	cfg := decodeJSON(t, out)

	// Channels block present
	if _, ok := cfg["channels"]; !ok {
		t.Fatalf("channels section missing despite slack binding: %+v", cfg)
	}
	// Plugins block present
	if _, ok := cfg["plugins"]; !ok {
		t.Fatalf("plugins section missing despite slack binding: %+v", cfg)
	}
	// Overlay still applied
	defaults := cfg["agents"].(map[string]any)["defaults"].(map[string]any)
	if got := defaults["model"].(map[string]any)["primary"]; got != "ollama/qwen3:6b" {
		t.Fatalf("overlay primary not applied: %v", got)
	}
	if _, ok := cfg["models"].(map[string]any)["providers"].(map[string]any)["ollama"]; !ok {
		t.Fatalf("ollama provider block missing")
	}
}
