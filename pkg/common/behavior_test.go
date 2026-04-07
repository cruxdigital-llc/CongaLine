package common

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cruxdigital-llc/conga-line/pkg/provider"
	_ "github.com/cruxdigital-llc/conga-line/pkg/runtime/openclaw"
)

// setupBehaviorDir creates a temp behavior directory with the new structure:
//
//	default/SOUL.md, default/AGENTS.md, default/team/USER.md.tmpl, default/user/USER.md.tmpl
func setupBehaviorDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	os.MkdirAll(filepath.Join(dir, "default", "team"), 0755)
	os.MkdirAll(filepath.Join(dir, "default", "user"), 0755)
	os.WriteFile(filepath.Join(dir, "default", "SOUL.md"), []byte("default soul"), 0644)
	os.WriteFile(filepath.Join(dir, "default", "AGENTS.md"), []byte("default agents"), 0644)
	os.WriteFile(filepath.Join(dir, "default", "team", "USER.md.tmpl"), []byte("team user: {{AGENT_NAME}}"), 0644)
	os.WriteFile(filepath.Join(dir, "default", "user", "USER.md.tmpl"), []byte("dm user: {{AGENT_NAME}}"), 0644)

	return dir
}

func TestComposeAgentWorkspaceFiles_DefaultsOnly(t *testing.T) {
	dir := setupBehaviorDir(t)
	agent := provider.AgentConfig{Name: "acme", Type: provider.AgentTypeTeam}

	files, toDelete, manifest, err := ComposeAgentWorkspaceFiles(dir, agent, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	soul := string(files["SOUL.md"].Content)
	if soul != "default soul" {
		t.Errorf("SOUL.md = %q, want 'default soul'", soul)
	}
	if files["SOUL.md"].Source != "default" {
		t.Errorf("SOUL.md source = %q, want default", files["SOUL.md"].Source)
	}

	user := string(files["USER.md"].Content)
	if user != "team user: acme" {
		t.Errorf("USER.md = %q, want 'team user: acme'", user)
	}

	if len(toDelete) != 0 {
		t.Errorf("toDelete = %v, want empty", toDelete)
	}
	if manifest.Version != ManifestVersion {
		t.Errorf("manifest version = %d", manifest.Version)
	}
}

func TestComposeAgentWorkspaceFiles_UserAgentDefaults(t *testing.T) {
	dir := setupBehaviorDir(t)
	agent := provider.AgentConfig{Name: "bob", Type: provider.AgentTypeUser}

	files, _, _, err := ComposeAgentWorkspaceFiles(dir, agent, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	user := string(files["USER.md"].Content)
	if user != "dm user: bob" {
		t.Errorf("USER.md = %q, want 'dm user: bob'", user)
	}
}

func TestComposeAgentWorkspaceFiles_AgentOverridesDefault(t *testing.T) {
	dir := setupBehaviorDir(t)
	agent := provider.AgentConfig{Name: "acme", Type: provider.AgentTypeTeam}

	agentDir := filepath.Join(dir, "agents", "acme")
	os.MkdirAll(agentDir, 0755)
	os.WriteFile(filepath.Join(agentDir, "SOUL.md"), []byte("custom soul"), 0644)

	files, _, _, err := ComposeAgentWorkspaceFiles(dir, agent, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	soul := string(files["SOUL.md"].Content)
	if soul != "custom soul" {
		t.Errorf("SOUL.md = %q, want 'custom soul'", soul)
	}
	if files["SOUL.md"].Source != "agent" {
		t.Errorf("SOUL.md source = %q, want agent", files["SOUL.md"].Source)
	}

	// AGENTS.md should still come from default
	agents := string(files["AGENTS.md"].Content)
	if agents != "default agents" {
		t.Errorf("AGENTS.md = %q, want 'default agents' (should fall back)", agents)
	}
}

func TestComposeAgentWorkspaceFiles_AgentOverridesUSERmd(t *testing.T) {
	dir := setupBehaviorDir(t)
	agent := provider.AgentConfig{Name: "acme", Type: provider.AgentTypeTeam}

	agentDir := filepath.Join(dir, "agents", "acme")
	os.MkdirAll(agentDir, 0755)
	os.WriteFile(filepath.Join(agentDir, "USER.md"), []byte("custom user file"), 0644)

	files, _, _, err := ComposeAgentWorkspaceFiles(dir, agent, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	user := string(files["USER.md"].Content)
	if user != "custom user file" {
		t.Errorf("USER.md = %q, want 'custom user file'", user)
	}
	if files["USER.md"].Source != "agent" {
		t.Errorf("USER.md source = %q, want agent", files["USER.md"].Source)
	}
}

func TestComposeAgentWorkspaceFiles_OnlyKnownFilesRead(t *testing.T) {
	dir := setupBehaviorDir(t)
	agent := provider.AgentConfig{Name: "acme", Type: provider.AgentTypeTeam}

	// Even if extra files exist in agents/acme/, only SOUL.md/AGENTS.md/USER.md are read.
	// MEMORY.md in the agent dir is simply ignored (not loaded, not deployed).
	agentDir := filepath.Join(dir, "agents", "acme")
	os.MkdirAll(agentDir, 0755)
	os.WriteFile(filepath.Join(agentDir, "MEMORY.md"), []byte("should be ignored"), 0644)
	os.WriteFile(filepath.Join(agentDir, "SOUL.md"), []byte("custom soul"), 0644)

	files, _, _, err := ComposeAgentWorkspaceFiles(dir, agent, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// SOUL.md should be the agent version
	if string(files["SOUL.md"].Content) != "custom soul" {
		t.Errorf("SOUL.md = %q, want 'custom soul'", string(files["SOUL.md"].Content))
	}

	// MEMORY.md should NOT be in the output (it's not one of the known files)
	if _, ok := files["MEMORY.md"]; ok {
		t.Error("MEMORY.md should not be in files — only SOUL.md, AGENTS.md, USER.md are read")
	}
}

func TestComposeAgentWorkspaceFiles_NoAgentDir(t *testing.T) {
	dir := setupBehaviorDir(t)
	agent := provider.AgentConfig{Name: "acme", Type: provider.AgentTypeTeam}

	// No agents/ directory — should produce default files
	files, _, _, err := ComposeAgentWorkspaceFiles(dir, agent, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := files["SOUL.md"]; !ok {
		t.Error("SOUL.md should be present from defaults")
	}
}

func TestComposeAgentWorkspaceFiles_DeletionReconciliation(t *testing.T) {
	dir := setupBehaviorDir(t)
	agent := provider.AgentConfig{Name: "acme", Type: provider.AgentTypeTeam}

	// Agent had SOUL.md override previously, now removed (no agents/acme/ dir)
	oldContent := []byte("old agent soul")
	prev := &OverlayManifest{
		Version: ManifestVersion,
		Files: []OverlayEntry{
			{Path: "SOUL.md", SHA256: HashFileContent(oldContent), Source: "agent"},
		},
	}

	// Workspace still has the old agent SOUL.md
	hashWorkspaceFile := func(rel string) (string, error) {
		if rel == "SOUL.md" {
			return HashFileContent(oldContent), nil
		}
		return "", os.ErrNotExist
	}

	_, toDelete, _, err := ComposeAgentWorkspaceFiles(dir, agent, prev, hashWorkspaceFile)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// SOUL.md is still in the new file set (from defaults), so it should NOT be deleted
	// — it just switches from agent to default source
	for _, d := range toDelete {
		if d == "SOUL.md" {
			t.Error("SOUL.md should not be in toDelete — it's still present from defaults")
		}
	}
}

func TestComposeAgentWorkspaceFiles_DeletionPreservesModified(t *testing.T) {
	dir := setupBehaviorDir(t)
	agent := provider.AgentConfig{Name: "acme", Type: provider.AgentTypeTeam}

	prev := &OverlayManifest{
		Version: ManifestVersion,
		Files: []OverlayEntry{
			{Path: "CUSTOM.md", SHA256: HashFileContent([]byte("original")), Source: "agent"},
		},
	}

	hashWorkspaceFile := func(rel string) (string, error) {
		return HashFileContent([]byte("agent edited this")), nil
	}

	_, toDelete, _, err := ComposeAgentWorkspaceFiles(dir, agent, prev, hashWorkspaceFile)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(toDelete) != 0 {
		t.Errorf("toDelete = %v, want empty (modified file should be preserved)", toDelete)
	}
}

func TestComposeAgentWorkspaceFiles_BackwardsCompatOldManifest(t *testing.T) {
	dir := setupBehaviorDir(t)
	agent := provider.AgentConfig{Name: "acme", Type: provider.AgentTypeTeam}

	// Old manifest has Source: "overlay" — should still be recognized for deletion
	oldContent := []byte("old overlay content")
	prev := &OverlayManifest{
		Version: ManifestVersion,
		Files: []OverlayEntry{
			{Path: "REMOVED.md", SHA256: HashFileContent(oldContent), Source: "overlay"},
		},
	}

	hashWorkspaceFile := func(rel string) (string, error) {
		if rel == "REMOVED.md" {
			return HashFileContent(oldContent), nil
		}
		return "", os.ErrNotExist
	}

	_, toDelete, _, err := ComposeAgentWorkspaceFiles(dir, agent, prev, hashWorkspaceFile)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(toDelete) != 1 || toDelete[0] != "REMOVED.md" {
		t.Errorf("toDelete = %v, want [REMOVED.md] (backwards compat with 'overlay' source)", toDelete)
	}
}
