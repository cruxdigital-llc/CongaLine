package localprovider

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cruxdigital-llc/conga-line/pkg/provider"
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

// writeOpenClawAgent persists a minimal openclaw agent record so GetAgent /
// runtimeForAgent resolve in integrity tests.
func writeOpenClawAgent(t *testing.T, p *LocalProvider, name string) {
	t.Helper()
	if err := os.MkdirAll(p.agentsDir(), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(p.agentsDir(), name+".json"),
		[]byte(`{"type":"user","runtime":"openclaw","gateway_port":18789}`), 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestCheckIncludeReservedKeys_AllLayers covers feature #31 T5.1/T5.3: the
// reserved-key guard fires for a Conga-owned key declared in ANY $include layer
// (fleet, per-agent managed, or admin-drift), and the error names the offender.
func TestCheckIncludeReservedKeys_AllLayers(t *testing.T) {
	p := &LocalProvider{dataDir: t.TempDir()}
	writeOpenClawAgent(t, p, "a1")
	dataDir := p.dataSubDir("a1")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatal(err)
	}
	layers := []string{"fleet-custom.json", "agent-managed-custom.json", "agent-custom.json"}
	writeAll := func(content map[string]string) {
		for _, f := range layers {
			c := "{}"
			if v, ok := content[f]; ok {
				c = v
			}
			if err := os.WriteFile(filepath.Join(dataDir, f), []byte(c), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}

	// All clean → no warns, no error.
	writeAll(nil)
	if warns, err := p.checkIncludeReservedKeys(context.Background(), "a1"); err != nil || len(warns) != 0 {
		t.Fatalf("clean layers: warns=%v err=%v", warns, err)
	}

	// A reserved key in each layer is independently flagged, naming the file.
	for _, f := range layers {
		writeAll(map[string]string{f: `{"channels":{"slack":{}}}`})
		_, err := p.checkIncludeReservedKeys(context.Background(), "a1")
		if err == nil {
			t.Fatalf("reserved key in %s not flagged", f)
		}
		if !strings.Contains(err.Error(), f) || !strings.Contains(err.Error(), "channels") {
			t.Fatalf("error for %s should name file + key: %v", f, err)
		}
	}
}

// TestCheckManagedIncludeIntegrity_DetectsTamper covers feature #31 T5.2: the
// managed layers are hash-verified against their deployed baseline; the
// admin-owned agent-custom.json is not.
func TestCheckManagedIncludeIntegrity_DetectsTamper(t *testing.T) {
	p := &LocalProvider{dataDir: t.TempDir()}
	writeOpenClawAgent(t, p, "a1")
	dataDir := p.dataSubDir("a1")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(p.configDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "fleet-custom.json"), []byte(`{"a":1}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "agent-managed-custom.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := p.saveManagedIncludeBaselines(context.Background(), "a1"); err != nil {
		t.Fatalf("save baselines: %v", err)
	}

	// Unchanged → OK.
	if err := p.checkManagedIncludeIntegrity(context.Background(), "a1"); err != nil {
		t.Fatalf("clean managed includes flagged: %v", err)
	}

	// Tamper the fleet layer on-host → violation that names the file.
	if err := os.WriteFile(filepath.Join(dataDir, "fleet-custom.json"), []byte(`{"a":2}`), 0o644); err != nil {
		t.Fatal(err)
	}
	err := p.checkManagedIncludeIntegrity(context.Background(), "a1")
	if err == nil || !strings.Contains(err.Error(), "fleet-custom.json") {
		t.Fatalf("on-host tamper not detected: %v", err)
	}
}

// TestDeployManagedCustomConfig covers feature #31 T4.5/T9.1: the deploy writes
// the fleet + per-agent layers from committed sources (or "{}"), re-syncs them on
// each call (propagation), never touches the admin-drift agent-custom.json, and
// fails closed on a reserved-key fleet source (blast radius).
func TestDeployManagedCustomConfig(t *testing.T) {
	p := &LocalProvider{dataDir: t.TempDir()}
	rt, err := runtime.Get(runtime.RuntimeOpenClaw)
	if err != nil {
		t.Fatal(err)
	}
	cfg := provider.AgentConfig{Name: "a1", Runtime: "openclaw", Type: provider.AgentTypeUser}
	dataDir := p.dataSubDir("a1")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatal(err)
	}
	fleetDir := filepath.Join(p.behaviorDir(), "_defaults", "openclaw")
	if err := os.MkdirAll(fleetDir, 0o755); err != nil {
		t.Fatal(err)
	}
	perAgentDir := filepath.Join(p.behaviorDir(), "a1")
	if err := os.MkdirAll(perAgentDir, 0o755); err != nil {
		t.Fatal(err)
	}
	fleetSrc := filepath.Join(fleetDir, "fleet-custom.json")
	perAgentSrc := filepath.Join(perAgentDir, "custom.json")

	// 1. No sources → both managed files deployed as "{}".
	if err := p.deployManagedCustomConfig(rt, cfg, dataDir); err != nil {
		t.Fatalf("deploy (empty): %v", err)
	}
	for _, f := range []string{"fleet-custom.json", "agent-managed-custom.json"} {
		b, _ := os.ReadFile(filepath.Join(dataDir, f))
		if strings.TrimSpace(string(b)) != "{}" {
			t.Fatalf("%s should be {} when no source, got %q", f, b)
		}
	}

	// 2. With sources → deployed from source; re-sync overwrites prior content.
	if err := os.WriteFile(fleetSrc, []byte(`{"skills":{"allow":["github"]}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(perAgentSrc, []byte(`{"mcp":{"servers":{"x":{}}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	// Admin drift present — must survive the managed deploy untouched.
	adminPath := filepath.Join(dataDir, "agent-custom.json")
	admin := []byte(`{"tools":{"allow":["bash"]}}`)
	if err := os.WriteFile(adminPath, admin, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := p.deployManagedCustomConfig(rt, cfg, dataDir); err != nil {
		t.Fatalf("deploy (sources): %v", err)
	}
	if b, _ := os.ReadFile(filepath.Join(dataDir, "fleet-custom.json")); !strings.Contains(string(b), "github") {
		t.Errorf("fleet not synced from source: %s", b)
	}
	if b, _ := os.ReadFile(filepath.Join(dataDir, "agent-managed-custom.json")); !strings.Contains(string(b), "mcp") {
		t.Errorf("per-agent not synced from source: %s", b)
	}
	if b, _ := os.ReadFile(adminPath); string(b) != string(admin) {
		t.Errorf("admin-drift agent-custom.json was modified by managed deploy: %s", b)
	}

	// 3. Reserved-key fleet source → fail closed (deploy aborts before writing).
	if err := os.WriteFile(fleetSrc, []byte(`{"channels":{"slack":{}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := p.deployManagedCustomConfig(rt, cfg, dataDir); err == nil {
		t.Fatal("reserved-key fleet source should fail the deploy closed")
	}
	// The prior good fleet content must remain (no partial write of the bad file).
	if b, _ := os.ReadFile(filepath.Join(dataDir, "fleet-custom.json")); !strings.Contains(string(b), "github") {
		t.Errorf("failed deploy should not have overwritten fleet-custom.json: %s", b)
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
