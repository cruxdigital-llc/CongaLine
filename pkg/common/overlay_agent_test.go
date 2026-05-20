package common

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/cruxdigital-llc/conga-line/pkg/provider"
	"github.com/cruxdigital-llc/conga-line/pkg/runtime"
)

// resetOverlayWarnings clears the warn-once cache so each test runs
// independently regardless of order.
func resetOverlayWarnings() {
	overlayWarningOnce = sync.Map{}
}

// writeOverlay places agent.yaml under behaviorDir/agents/<name>/.
func writeOverlay(t *testing.T, behaviorDir, name, content string) {
	t.Helper()
	dir := filepath.Join(behaviorDir, "agents", name)
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "agent.yaml"), []byte(content), 0600); err != nil {
		t.Fatalf("write overlay: %v", err)
	}
}

// captureStderr runs fn with os.Stderr redirected to a buffer.
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	orig := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stderr = w
	done := make(chan string)
	go func() {
		buf := make([]byte, 4096)
		var sb strings.Builder
		for {
			n, _ := r.Read(buf)
			if n == 0 {
				break
			}
			sb.Write(buf[:n])
		}
		done <- sb.String()
	}()
	fn()
	w.Close()
	os.Stderr = orig
	return <-done
}

func newAgent(name string) provider.AgentConfig {
	return provider.AgentConfig{Name: name, Type: provider.AgentTypeUser}
}

func TestLoadAgentOverlay_FileMissing(t *testing.T) {
	resetOverlayWarnings()
	dir := t.TempDir()
	got, err := LoadAgentOverlay(dir, newAgent("nobody"))
	if err != nil {
		t.Fatalf("want nil error, got %v", err)
	}
	if got != nil {
		t.Fatalf("want nil overlay, got %+v", got)
	}
}

func TestLoadAgentOverlay_EmptyFile(t *testing.T) {
	resetOverlayWarnings()
	dir := t.TempDir()
	writeOverlay(t, dir, "agentx", "")
	var got *runtime.AgentOverlay
	var err error
	stderr := captureStderr(t, func() {
		got, err = LoadAgentOverlay(dir, newAgent("agentx"))
	})
	if err != nil {
		t.Fatalf("want nil error, got %v", err)
	}
	if got == nil {
		t.Fatalf("want non-nil overlay, got nil")
	}
	if got.Version != 0 || got.Model != nil {
		t.Fatalf("want zero-value overlay, got %+v", got)
	}
	if !strings.Contains(stderr, "missing `version:` key") {
		t.Fatalf("want missing-version warning on stderr, got %q", stderr)
	}
}

func TestLoadAgentOverlay_ValidOllama(t *testing.T) {
	resetOverlayWarnings()
	dir := t.TempDir()
	writeOverlay(t, dir, "aaron", `version: 1
model:
  provider: ollama
  name: qwen3:6b
  base_url: http://192.168.181.97:11434
`)
	got, err := LoadAgentOverlay(dir, newAgent("aaron"))
	if err != nil {
		t.Fatalf("want nil error, got %v", err)
	}
	if got == nil || got.Model == nil {
		t.Fatalf("want populated overlay, got %+v", got)
	}
	if got.Version != 1 || got.Model.Provider != "ollama" || got.Model.Name != "qwen3:6b" || got.Model.BaseURL != "http://192.168.181.97:11434" {
		t.Fatalf("unexpected overlay: %+v / %+v", got, got.Model)
	}
}

func TestLoadAgentOverlay_ValidOpenAI(t *testing.T) {
	resetOverlayWarnings()
	dir := t.TempDir()
	writeOverlay(t, dir, "x", `version: 1
model:
  provider: openai
  name: gpt-5.5
  base_url: https://api.openai.com/v1
`)
	got, err := LoadAgentOverlay(dir, newAgent("x"))
	if err != nil {
		t.Fatalf("want nil error, got %v", err)
	}
	if got == nil || got.Model == nil || got.Model.Provider != "openai" || got.Model.Name != "gpt-5.5" {
		t.Fatalf("unexpected overlay: %+v", got)
	}
}

func TestLoadAgentOverlay_Version2Rejected(t *testing.T) {
	resetOverlayWarnings()
	dir := t.TempDir()
	writeOverlay(t, dir, "x", `version: 2
model:
  provider: ollama
  name: x
  base_url: http://h:1
`)
	_, err := LoadAgentOverlay(dir, newAgent("x"))
	if err == nil || !strings.Contains(err.Error(), "version 2 requires a newer conga binary") {
		t.Fatalf("want version-rejection error, got %v", err)
	}
	if !strings.Contains(err.Error(), "agent.yaml") {
		t.Fatalf("want file path in error, got %v", err)
	}
}

func TestLoadAgentOverlay_MissingVersionWarnsAccepts(t *testing.T) {
	resetOverlayWarnings()
	dir := t.TempDir()
	writeOverlay(t, dir, "x", `model:
  provider: ollama
  name: qwen
  base_url: http://h:1
`)
	var got *runtime.AgentOverlay
	var err error
	stderr := captureStderr(t, func() {
		got, err = LoadAgentOverlay(dir, newAgent("x"))
	})
	if err != nil {
		t.Fatalf("want nil error, got %v", err)
	}
	if got == nil || got.Model == nil {
		t.Fatalf("want populated overlay, got %+v", got)
	}
	if !strings.Contains(stderr, "missing `version:` key") {
		t.Fatalf("want warning on stderr, got %q", stderr)
	}
}

func TestLoadAgentOverlay_WarningEmittedOnce(t *testing.T) {
	resetOverlayWarnings()
	dir := t.TempDir()
	writeOverlay(t, dir, "x", `model:
  provider: ollama
  name: qwen
  base_url: http://h:1
`)
	stderr := captureStderr(t, func() {
		// Two loads from the same path should warn once.
		_, _ = LoadAgentOverlay(dir, newAgent("x"))
		_, _ = LoadAgentOverlay(dir, newAgent("x"))
	})
	if strings.Count(stderr, "missing `version:` key") != 1 {
		t.Fatalf("want exactly one missing-version warning, got %q", stderr)
	}
}

func TestLoadAgentOverlay_ReservedTopLevelKey(t *testing.T) {
	resetOverlayWarnings()
	dir := t.TempDir()
	writeOverlay(t, dir, "x", `version: 1
tools:
  - foo
`)
	_, err := LoadAgentOverlay(dir, newAgent("x"))
	if err == nil {
		t.Fatal("want error on reserved top-level key, got nil")
	}
	if !strings.Contains(err.Error(), `"tools"`) {
		t.Fatalf("want error quoting the reserved key, got %v", err)
	}
	if !strings.Contains(err.Error(), "reserved for a future schema version") {
		t.Fatalf("want error to explain the key is reserved, got %v", err)
	}
}

func TestLoadAgentOverlay_AllReservedKeysRejected(t *testing.T) {
	resetOverlayWarnings()
	for _, key := range []string{"memory", "tools", "limits", "images", "pdf", "video"} {
		t.Run(key, func(t *testing.T) {
			dir := t.TempDir()
			writeOverlay(t, dir, "x", "version: 1\n"+key+": placeholder\n")
			_, err := LoadAgentOverlay(dir, newAgent("x"))
			if err == nil || !strings.Contains(err.Error(), "reserved for a future schema version") {
				t.Fatalf("key %q: want reserved-key error, got %v", key, err)
			}
		})
	}
}

func TestLoadAgentOverlay_NonReservedUnknownKey(t *testing.T) {
	// A typo that isn't on the reserved list should still fail via the
	// strict-key path, but with the generic "field not found" yaml.v3
	// message rather than the reserved-key explanation.
	resetOverlayWarnings()
	dir := t.TempDir()
	writeOverlay(t, dir, "x", `version: 1
mdoel:
  provider: ollama
`)
	_, err := LoadAgentOverlay(dir, newAgent("x"))
	if err == nil {
		t.Fatal("want error on misspelled key, got nil")
	}
	if !strings.Contains(err.Error(), "mdoel") {
		t.Fatalf("want error naming the misspelled key, got %v", err)
	}
	if strings.Contains(err.Error(), "reserved for a future schema version") {
		t.Fatalf("misspelled key should not get reserved-key message, got %v", err)
	}
}

func TestLoadAgentOverlay_UnknownInnerKey(t *testing.T) {
	resetOverlayWarnings()
	dir := t.TempDir()
	writeOverlay(t, dir, "x", `version: 1
model:
  provider: ollama
  name: qwen
  bare_url: http://h:1
`)
	_, err := LoadAgentOverlay(dir, newAgent("x"))
	if err == nil {
		t.Fatal("want error on unknown inner key, got nil")
	}
	if !strings.Contains(err.Error(), "bare_url") {
		t.Fatalf("want error naming the typo, got %v", err)
	}
}

func TestLoadAgentOverlay_MalformedYAML(t *testing.T) {
	resetOverlayWarnings()
	dir := t.TempDir()
	writeOverlay(t, dir, "x", `version: 1
model:
  provider: ollama
  name: [unterminated
`)
	_, err := LoadAgentOverlay(dir, newAgent("x"))
	if err == nil {
		t.Fatal("want parse error, got nil")
	}
	if !strings.Contains(err.Error(), "agent.yaml") {
		t.Fatalf("want file path in error, got %v", err)
	}
}

func TestLoadAgentOverlay_UnknownProvider(t *testing.T) {
	resetOverlayWarnings()
	dir := t.TempDir()
	writeOverlay(t, dir, "x", `version: 1
model:
  provider: azure
  name: gpt
  base_url: https://x/v1
`)
	_, err := LoadAgentOverlay(dir, newAgent("x"))
	if err == nil || !strings.Contains(err.Error(), "unknown model provider") {
		t.Fatalf("want unknown-provider error, got %v", err)
	}
	if !strings.Contains(err.Error(), "supported: ollama, openai") {
		t.Fatalf("want supported-set hint, got %v", err)
	}
}

func TestLoadAgentOverlay_OllamaV1Footgun(t *testing.T) {
	resetOverlayWarnings()
	dir := t.TempDir()
	writeOverlay(t, dir, "x", `version: 1
model:
  provider: ollama
  name: qwen
  base_url: http://h:11434/v1
`)
	_, err := LoadAgentOverlay(dir, newAgent("x"))
	if err == nil || !strings.Contains(err.Error(), "without /v1 suffix") {
		t.Fatalf("want /v1 rejection, got %v", err)
	}
}

func TestLoadAgentOverlay_OllamaEmptyBaseURL(t *testing.T) {
	resetOverlayWarnings()
	dir := t.TempDir()
	writeOverlay(t, dir, "x", `version: 1
model:
  provider: ollama
  name: qwen
`)
	_, err := LoadAgentOverlay(dir, newAgent("x"))
	if err == nil || !strings.Contains(err.Error(), "ollama provider requires base_url") {
		t.Fatalf("want missing-base_url error, got %v", err)
	}
}

func TestLoadAgentOverlay_BaseURLNoScheme(t *testing.T) {
	resetOverlayWarnings()
	dir := t.TempDir()
	writeOverlay(t, dir, "x", `version: 1
model:
  provider: openai
  name: gpt
  base_url: just-a-string
`)
	_, err := LoadAgentOverlay(dir, newAgent("x"))
	if err == nil || !strings.Contains(err.Error(), "must use http or https") {
		t.Fatalf("want scheme error, got %v", err)
	}
}

func TestLoadAgentOverlay_OpenAINonV1WarnsAccepts(t *testing.T) {
	resetOverlayWarnings()
	dir := t.TempDir()
	writeOverlay(t, dir, "x", `version: 1
model:
  provider: openai
  name: gpt
  base_url: http://10.0.0.5:8000
`)
	var got *runtime.AgentOverlay
	var err error
	stderr := captureStderr(t, func() {
		got, err = LoadAgentOverlay(dir, newAgent("x"))
	})
	if err != nil {
		t.Fatalf("want nil error, got %v", err)
	}
	if got == nil || got.Model == nil {
		t.Fatalf("want populated overlay, got %+v", got)
	}
	if !strings.Contains(stderr, "does not look like an OpenAI-compatible /v1 path") {
		t.Fatalf("want nonstandard-base_url warning, got %q", stderr)
	}
}

func TestLoadAgentOverlay_ProviderCasing(t *testing.T) {
	resetOverlayWarnings()
	dir := t.TempDir()
	writeOverlay(t, dir, "x", `version: 1
model:
  provider: Ollama
  name: qwen
  base_url: http://h:11434
`)
	_, err := LoadAgentOverlay(dir, newAgent("x"))
	if err == nil || !strings.Contains(err.Error(), "must be lowercase") {
		t.Fatalf("want casing error, got %v", err)
	}
	if !strings.Contains(err.Error(), `"ollama"`) {
		t.Fatalf("want canonical-form hint, got %v", err)
	}
}
