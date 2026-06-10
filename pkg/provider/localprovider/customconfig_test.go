package localprovider

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cruxdigital-llc/conga-line/pkg/runtime"

	// Register the openclaw runtime so runtime.Get / runtimeForAgent resolve.
	_ "github.com/cruxdigital-llc/conga-line/pkg/runtime/openclaw"
)

func TestEnsureAgentCustomConfig(t *testing.T) {
	p := &LocalProvider{dataDir: t.TempDir()}
	rt, err := runtime.Get(runtime.RuntimeOpenClaw)
	if err != nil {
		t.Fatalf("get runtime: %v", err)
	}
	dataDir := p.dataSubDir("a1")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dataDir, "agent-custom.json")

	// 1. Creates "{}" when absent.
	if err := p.ensureAgentCustomConfig(rt, dataDir); err != nil {
		t.Fatalf("ensure (create): %v", err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("expected include created: %v", err)
	}
	if strings.TrimSpace(string(b)) != "{}" {
		t.Fatalf("want {}, got %q", b)
	}

	// 2. Never clobbers existing admin content (idempotent / self-healing only).
	admin := []byte(`{"mcp":{"servers":{"linear":{"url":"https://mcp.linear.app/sse"}}}}`)
	if err := os.WriteFile(path, admin, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := p.ensureAgentCustomConfig(rt, dataDir); err != nil {
		t.Fatalf("ensure (preserve): %v", err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != string(admin) {
		t.Fatalf("ensure clobbered admin content: %q", got)
	}
}

func TestResetAgentCustomConfig_BacksUpAndEmpties(t *testing.T) {
	p := &LocalProvider{dataDir: t.TempDir()}

	if err := os.MkdirAll(p.agentsDir(), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(p.agentsDir(), "a1.json"),
		[]byte(`{"type":"user","runtime":"openclaw","gateway_port":18789}`), 0o600); err != nil {
		t.Fatal(err)
	}

	dataDir := p.dataSubDir("a1")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dataDir, "agent-custom.json")
	original := `{"mcp":{"servers":{"linear":{}}}}`
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := p.ResetAgentCustomConfig(context.Background(), "a1"); err != nil {
		t.Fatalf("reset: %v", err)
	}

	// File reset to "{}".
	got, _ := os.ReadFile(path)
	if strings.TrimSpace(string(got)) != "{}" {
		t.Fatalf("want {} after reset, got %q", got)
	}

	// A timestamped backup preserves the original content.
	entries, _ := os.ReadDir(dataDir)
	foundBak := false
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "agent-custom.json.bak.") {
			foundBak = true
			bb, _ := os.ReadFile(filepath.Join(dataDir, e.Name()))
			if !strings.Contains(string(bb), "linear") {
				t.Fatalf("backup missing original content: %q", bb)
			}
		}
	}
	if !foundBak {
		t.Fatalf("no .bak backup created in %s", dataDir)
	}
}
