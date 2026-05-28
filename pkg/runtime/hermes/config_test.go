package hermes

import (
	"bytes"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/cruxdigital-llc/conga-line/pkg/provider"
	"github.com/cruxdigital-llc/conga-line/pkg/runtime"
	"gopkg.in/yaml.v3"
)

// resetHermesWarnings clears the warn-once cache so each test runs
// independently regardless of order.
func resetHermesWarnings() {
	hermesDegradedWarningOnce = sync.Map{}
}

// captureHermesStderr redirects stderrWriter to a pipe for the duration of
// fn and returns whatever was written. Matches the pattern used in
// pkg/common/overlay_agent_test.go.
func captureHermesStderr(t *testing.T, fn func()) string {
	t.Helper()
	orig := stderrWriter
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	stderrWriter = func() *os.File { return w }
	t.Cleanup(func() { stderrWriter = orig })

	done := make(chan string)
	go func() {
		var buf bytes.Buffer
		_, _ = buf.ReadFrom(r)
		done <- buf.String()
	}()
	fn()
	w.Close()
	return <-done
}

// baseHermesParams returns a minimal ConfigParams exercising the api_server
// path without any channels, overlay, or model. Used as the "no overlay"
// regression baseline.
func baseHermesParams() runtime.ConfigParams {
	return runtime.ConfigParams{
		Agent: provider.AgentConfig{
			Name:        "test",
			Type:        provider.AgentTypeUser,
			GatewayPort: 18791,
		},
		Secrets:      provider.SharedSecrets{Values: map[string]string{}},
		GatewayToken: "fixed-token-for-tests",
	}
}

func decodeYAML(t *testing.T, b []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := yaml.Unmarshal(b, &m); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return m
}

func TestGenerateConfig_HermesNoOverlay(t *testing.T) {
	// Regression baseline: without any overlay, the Hermes config must NOT
	// have a delegation: block. Matches the existing pre-Phase-3 behavior.
	r := &Runtime{}
	out, err := r.GenerateConfig(baseHermesParams())
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	cfg := decodeYAML(t, out)

	if _, ok := cfg["delegation"]; ok {
		t.Fatalf("delegation block should not be set without overlay: %+v", cfg)
	}
	if _, ok := cfg["platforms"]; !ok {
		t.Fatalf("platforms block missing: %+v", cfg)
	}
}

func TestGenerateConfig_HermesV2NoSubagents_IdenticalToBaseline(t *testing.T) {
	// A v2 overlay without a subagents block must produce byte-identical
	// output to the same Hermes config without any overlay at all.
	r := &Runtime{}

	baseline, err := r.GenerateConfig(baseHermesParams())
	if err != nil {
		t.Fatalf("baseline: %v", err)
	}

	params := baseHermesParams()
	params.Overlay = &runtime.AgentOverlay{Version: 2} // no model, no subagents
	v2Out, err := r.GenerateConfig(params)
	if err != nil {
		t.Fatalf("v2-no-subagents: %v", err)
	}

	if !bytes.Equal(baseline, v2Out) {
		t.Fatalf("v2 without subagents must be byte-identical to baseline\nbaseline: %s\nv2:       %s", baseline, v2Out)
	}
}

func TestGenerateConfig_HermesSubagents_OllamaInherit(t *testing.T) {
	// Ollama subagent: delegation.model emitted as provider/name; no
	// delegation.provider; no warning (Hermes transparently inherits via
	// parent's setup, no degradation).
	resetHermesWarnings()
	params := baseHermesParams()
	params.Overlay = &runtime.AgentOverlay{
		Version: 2,
		Subagents: &runtime.SubagentsOverlay{
			Model: &runtime.ModelOverlay{
				Provider: runtime.ProviderOllama,
				Name:     "qwen3:6b",
				BaseURL:  "http://192.168.1.5:11434",
			},
		},
	}

	r := &Runtime{}
	var out []byte
	var err error
	stderr := captureHermesStderr(t, func() {
		out, err = r.GenerateConfig(params)
	})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	cfg := decodeYAML(t, out)
	delegation, ok := cfg["delegation"].(map[string]any)
	if !ok {
		t.Fatalf("missing delegation block: %+v", cfg)
	}
	if got := delegation["model"]; got != "ollama/qwen3:6b" {
		t.Fatalf("delegation.model: want ollama/qwen3:6b, got %v", got)
	}
	if _, ok := delegation["provider"]; ok {
		t.Fatalf("delegation.provider must not be set for ollama (our overlay doesn't carry a Hermes adapter name): %+v", delegation)
	}
	if stderr != "" {
		t.Fatalf("ollama should not emit a degraded-mode warning, got %q", stderr)
	}
}

func TestGenerateConfig_HermesSubagents_DegradedNoProvider(t *testing.T) {
	// openai + custom base_url (e.g. LiteLLM on LAN) is NOT a known Hermes
	// adapter — we emit delegation.model only and warn the operator that
	// Hermes will inherit the parent's provider connection.
	resetHermesWarnings()
	params := baseHermesParams()
	params.Agent.Name = "agentx"
	params.Overlay = &runtime.AgentOverlay{
		Version: 2,
		Subagents: &runtime.SubagentsOverlay{
			Model: &runtime.ModelOverlay{
				Provider: runtime.ProviderOpenAI,
				Name:     "qwen-2.5-72b-instruct",
				BaseURL:  "https://litellm.lan/v1",
			},
		},
	}

	r := &Runtime{}
	var out []byte
	var err error
	stderr := captureHermesStderr(t, func() {
		out, err = r.GenerateConfig(params)
	})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	cfg := decodeYAML(t, out)
	delegation := cfg["delegation"].(map[string]any)
	if got := delegation["model"]; got != "openai/qwen-2.5-72b-instruct" {
		t.Fatalf("delegation.model: want openai/qwen-2.5-72b-instruct, got %v", got)
	}
	if _, ok := delegation["provider"]; ok {
		t.Fatalf("delegation.provider should be omitted in degraded mode: %+v", delegation)
	}
	if !strings.Contains(stderr, "agent agentx") {
		t.Fatalf("warning should name the agent, got %q", stderr)
	}
	if !strings.Contains(stderr, "https://litellm.lan/v1") {
		t.Fatalf("warning should name the unsupported base_url, got %q", stderr)
	}
	if !strings.Contains(stderr, "inherit the parent's provider") {
		t.Fatalf("warning should explain inheritance, got %q", stderr)
	}
}

func TestGenerateConfig_HermesSubagents_KnownAdapterHostNoWarning(t *testing.T) {
	// openai + base_url containing a known Hermes adapter host (e.g.
	// openrouter.ai) should NOT trigger the degraded-mode warning.
	resetHermesWarnings()
	params := baseHermesParams()
	params.Overlay = &runtime.AgentOverlay{
		Version: 2,
		Subagents: &runtime.SubagentsOverlay{
			Model: &runtime.ModelOverlay{
				Provider: runtime.ProviderOpenAI,
				Name:     "qwen-2.5-72b-instruct",
				BaseURL:  "https://openrouter.ai/api/v1",
			},
		},
	}

	r := &Runtime{}
	var err error
	stderr := captureHermesStderr(t, func() {
		_, err = r.GenerateConfig(params)
	})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if stderr != "" {
		t.Fatalf("openrouter base_url should not warn, got %q", stderr)
	}
}

func TestGenerateConfig_HermesSubagents_MaxConcurrentEmittedAsHermesKey(t *testing.T) {
	// Overlay max_concurrent → Hermes max_concurrent_children
	resetHermesWarnings()
	params := baseHermesParams()
	params.Overlay = &runtime.AgentOverlay{
		Version: 2,
		Subagents: &runtime.SubagentsOverlay{
			Model: &runtime.ModelOverlay{
				Provider: runtime.ProviderOllama,
				Name:     "qwen",
				BaseURL:  "http://h:11434",
			},
			MaxConcurrent: 6,
		},
	}

	r := &Runtime{}
	out, err := r.GenerateConfig(params)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	cfg := decodeYAML(t, out)
	delegation := cfg["delegation"].(map[string]any)
	if got := delegation["max_concurrent_children"]; got != 6 {
		t.Fatalf("max_concurrent_children: want 6, got %v", got)
	}
	if _, ok := delegation["maxConcurrent"]; ok {
		t.Fatalf("must not emit OpenClaw key maxConcurrent in Hermes output: %+v", delegation)
	}
}

func TestGenerateConfig_HermesSubagents_MaxSpawnDepthEmitted(t *testing.T) {
	// max_spawn_depth IS a Hermes knob (unlike OpenClaw) — must be emitted.
	resetHermesWarnings()
	params := baseHermesParams()
	params.Overlay = &runtime.AgentOverlay{
		Version: 2,
		Subagents: &runtime.SubagentsOverlay{
			Model: &runtime.ModelOverlay{
				Provider: runtime.ProviderOllama,
				Name:     "qwen",
				BaseURL:  "http://h:11434",
			},
			MaxSpawnDepth: 2,
		},
	}

	r := &Runtime{}
	out, err := r.GenerateConfig(params)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	cfg := decodeYAML(t, out)
	delegation := cfg["delegation"].(map[string]any)
	if got := delegation["max_spawn_depth"]; got != 2 {
		t.Fatalf("max_spawn_depth: want 2, got %v", got)
	}
}

func TestGenerateConfig_HermesSubagents_DelegationModeFiltered(t *testing.T) {
	// delegation_mode is OpenClaw-only — Hermes generator must not emit it.
	resetHermesWarnings()
	params := baseHermesParams()
	params.Overlay = &runtime.AgentOverlay{
		Version: 2,
		Subagents: &runtime.SubagentsOverlay{
			Model: &runtime.ModelOverlay{
				Provider: runtime.ProviderOllama,
				Name:     "qwen",
				BaseURL:  "http://h:11434",
			},
			DelegationMode: runtime.DelegationModePrefer,
		},
	}

	r := &Runtime{}
	out, err := r.GenerateConfig(params)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	cfg := decodeYAML(t, out)
	delegation := cfg["delegation"].(map[string]any)
	if _, ok := delegation["delegation_mode"]; ok {
		t.Fatalf("delegation_mode is OpenClaw-only, must not appear in Hermes output: %+v", delegation)
	}
	if _, ok := delegation["delegationMode"]; ok {
		t.Fatalf("delegationMode (OpenClaw-style key) must not appear in Hermes output: %+v", delegation)
	}
}

func TestGenerateConfig_HermesSubagents_WarningEmittedOnce(t *testing.T) {
	// Refresh loops should not spam stderr with the same warning.
	resetHermesWarnings()
	params := baseHermesParams()
	params.Agent.Name = "agentx"
	params.Overlay = &runtime.AgentOverlay{
		Version: 2,
		Subagents: &runtime.SubagentsOverlay{
			Model: &runtime.ModelOverlay{
				Provider: runtime.ProviderOpenAI,
				Name:     "qwen",
				BaseURL:  "https://litellm.lan/v1",
			},
		},
	}

	r := &Runtime{}
	var err error
	stderr := captureHermesStderr(t, func() {
		_, err = r.GenerateConfig(params)
		_, err = r.GenerateConfig(params)
	})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if got := strings.Count(stderr, "does not natively support"); got != 1 {
		t.Fatalf("expected exactly one degraded-mode warning across two calls, got %d: %q", got, stderr)
	}
}

// TestGenerateConfig_HermesModelOverlay_DegradedMode covers CRIT-A from
// review-aggregate-pass2.md follow-up: the Hermes runtime now reads
// params.Overlay.Model in degraded mode. cfg["model"] is set to provider/name
// so /status reflects the operator's intent, and a one-time stderr warning
// fires when base_url isn't a recognized Hermes adapter host.
func TestGenerateConfig_HermesModelOverlay_DegradedMode(t *testing.T) {
	resetHermesWarnings()
	params := baseHermesParams()
	params.Agent.Name = "primary-degraded"
	params.Model = "anthropic/setup-default" // setup-time default we'd otherwise inherit
	params.Overlay = &runtime.AgentOverlay{
		Version: 2,
		Model: &runtime.ModelOverlay{
			Provider: runtime.ProviderOpenAI,
			Name:     "qwen-2.5-72b-instruct",
			BaseURL:  "https://litellm.internal/v1",
		},
	}

	r := &Runtime{}
	var (
		out []byte
		err error
	)
	stderr := captureHermesStderr(t, func() {
		out, err = r.GenerateConfig(params)
	})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	cfg := decodeYAML(t, out)
	if got := cfg["model"]; got != "openai/qwen-2.5-72b-instruct" {
		t.Fatalf("expected cfg.model to reflect overlay (openai/qwen-2.5-72b-instruct), got %v", got)
	}
	if !strings.Contains(stderr, "primary-degraded") {
		t.Fatalf("expected warning to name the agent, got %q", stderr)
	}
	if !strings.Contains(stderr, "litellm.internal") {
		t.Fatalf("expected warning to name the unrecognized base_url, got %q", stderr)
	}
	if !strings.Contains(stderr, "primary model") {
		t.Fatalf("expected warning to clarify it's about the primary model (not the subagent), got %q", stderr)
	}
}

func TestGenerateConfig_HermesModelOverlay_KnownAdapterHostNoWarning(t *testing.T) {
	resetHermesWarnings()
	params := baseHermesParams()
	params.Overlay = &runtime.AgentOverlay{
		Version: 2,
		Model: &runtime.ModelOverlay{
			Provider: runtime.ProviderOpenAI,
			Name:     "qwen3-coder",
			BaseURL:  "https://openrouter.ai/api/v1",
		},
	}

	r := &Runtime{}
	var (
		out []byte
		err error
	)
	stderr := captureHermesStderr(t, func() {
		out, err = r.GenerateConfig(params)
	})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	cfg := decodeYAML(t, out)
	if got := cfg["model"]; got != "openai/qwen3-coder" {
		t.Fatalf("expected cfg.model = openai/qwen3-coder, got %v", got)
	}
	if strings.Contains(stderr, "does not natively address") {
		t.Fatalf("known adapter host should not trigger a primary-model degraded warning; got %q", stderr)
	}
}

// TestGenerateConfig_HermesModelOverlay_OverridesParamsModel asserts the
// overlay's model wins over the setup-time params.Model. This is what gives
// operator intent a visible representation in /status even though the
// underlying provider routing happens elsewhere.
func TestGenerateConfig_HermesModelOverlay_OverridesParamsModel(t *testing.T) {
	resetHermesWarnings()
	params := baseHermesParams()
	params.Model = "anthropic/claude-opus-4-7"
	params.Overlay = &runtime.AgentOverlay{
		Version: 2,
		Model: &runtime.ModelOverlay{
			Provider: runtime.ProviderOllama,
			Name:     "qwen3:6b",
			BaseURL:  "http://192.168.1.10:11434",
		},
	}

	r := &Runtime{}
	out, err := r.GenerateConfig(params)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	cfg := decodeYAML(t, out)
	if got := cfg["model"]; got != "ollama/qwen3:6b" {
		t.Fatalf("overlay must win over params.Model; expected ollama/qwen3:6b, got %v", got)
	}
}
