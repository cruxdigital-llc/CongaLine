package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestSyncBehaviorToDeployed_PreservesIdentityJSON guards against a real
// regression: after the 2026-05-XX rename, dataDir/agents/ holds BOTH
// per-agent overlay directories (<name>/) and the local provider's
// per-agent identity JSON files (<name>.json). syncBehaviorToDeployed's
// deletion-reconciliation walk used to consider every file under dst,
// including the JSON files, and remove any that weren't in src — silently
// wiping the local agent identity store on the first conga agent add
// after refresh. The fix: only reconcile files inside subdirectories
// (rel contains a path separator).
func TestSyncBehaviorToDeployed_PreservesIdentityJSON(t *testing.T) {
	// Set up a fake repo with one agent's overlay.
	repoRoot := t.TempDir()
	repoAgents := filepath.Join(repoRoot, "agents")
	if err := os.MkdirAll(filepath.Join(repoAgents, "aaron"), 0755); err != nil {
		t.Fatalf("mkdir repo aaron: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repoAgents, "aaron", "SOUL.md"), []byte("aaron soul"), 0644); err != nil {
		t.Fatalf("write repo SOUL.md: %v", err)
	}

	// Set up dataDir with: an existing identity JSON for aaron, an
	// identity JSON for nathan (no overlay), and an old overlay file
	// for a deleted agent.
	dataDir := t.TempDir()
	deployedAgents := filepath.Join(dataDir, "agents")
	if err := os.MkdirAll(deployedAgents, 0755); err != nil {
		t.Fatalf("mkdir deployed: %v", err)
	}
	aaronJSON := filepath.Join(deployedAgents, "aaron.json")
	nathanJSON := filepath.Join(deployedAgents, "nathan.json")
	if err := os.WriteFile(aaronJSON, []byte(`{"name":"aaron","type":"user"}`), 0600); err != nil {
		t.Fatalf("write aaron.json: %v", err)
	}
	if err := os.WriteFile(nathanJSON, []byte(`{"name":"nathan","type":"user"}`), 0600); err != nil {
		t.Fatalf("write nathan.json: %v", err)
	}
	// Old overlay for an agent that's been removed from the repo.
	if err := os.MkdirAll(filepath.Join(deployedAgents, "removed-agent"), 0755); err != nil {
		t.Fatalf("mkdir removed-agent: %v", err)
	}
	if err := os.WriteFile(filepath.Join(deployedAgents, "removed-agent", "SOUL.md"), []byte("stale"), 0644); err != nil {
		t.Fatalf("write removed SOUL.md: %v", err)
	}

	// Wire up the local-config.json so syncBehaviorToDeployed finds repo_path.
	localCfg := map[string]string{"repo_path": repoRoot}
	cfgBytes, _ := json.Marshal(localCfg)
	if err := os.WriteFile(filepath.Join(dataDir, "local-config.json"), cfgBytes, 0600); err != nil {
		t.Fatalf("write local-config.json: %v", err)
	}

	syncBehaviorToDeployed(dataDir)

	// Forward-copy did its job — aaron's SOUL.md is now in deployed.
	if data, err := os.ReadFile(filepath.Join(deployedAgents, "aaron", "SOUL.md")); err != nil {
		t.Fatalf("aaron's SOUL.md should have been synced to deployed: %v", err)
	} else if string(data) != "aaron soul" {
		t.Fatalf("SOUL.md content: want 'aaron soul', got %q", string(data))
	}

	// Identity JSON files MUST survive — they're not overlay content.
	for _, p := range []string{aaronJSON, nathanJSON} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("identity JSON %s was deleted by sync — this is the regression we're guarding against: %v", filepath.Base(p), err)
		}
	}

	// Stale overlay from a removed agent SHOULD have been cleaned up
	// (it's under a subdirectory, so the reconciliation walk handles it).
	if _, err := os.Stat(filepath.Join(deployedAgents, "removed-agent", "SOUL.md")); !os.IsNotExist(err) {
		t.Errorf("removed-agent/SOUL.md should have been deleted from deployed: stat err=%v", err)
	}
}
