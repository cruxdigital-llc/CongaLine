package common

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cruxdigital-llc/conga-line/pkg/provider"
)

// TestRoleDefaults_AgentYAMLParses walks agents/_defaults/<runtime>/role-*/
// and confirms each shipped agent.yaml passes the v2 loader's strict-key
// + validation gauntlet. Guards against drift between the role catalog
// and the overlay schema as either evolves.
func TestRoleDefaults_AgentYAMLParses(t *testing.T) {
	repoRoot := findRepoRoot(t)
	defaultsRoot := filepath.Join(repoRoot, "agents", "_defaults")

	// Walk each runtime directory under _defaults.
	runtimes, err := os.ReadDir(defaultsRoot)
	if err != nil {
		t.Fatalf("read %s: %v", defaultsRoot, err)
	}

	any := false
	for _, runtimeEntry := range runtimes {
		if !runtimeEntry.IsDir() {
			continue
		}
		runtimeDir := filepath.Join(defaultsRoot, runtimeEntry.Name())

		roles, err := os.ReadDir(runtimeDir)
		if err != nil {
			t.Fatalf("read %s: %v", runtimeDir, err)
		}

		for _, roleEntry := range roles {
			if !roleEntry.IsDir() || !strings.HasPrefix(roleEntry.Name(), "role-") {
				continue
			}
			any = true
			roleDir := filepath.Join(runtimeDir, roleEntry.Name())

			t.Run(runtimeEntry.Name()+"/"+roleEntry.Name(), func(t *testing.T) {
				// LoadAgentOverlay expects agents/<agent.Name>/agent.yaml — we
				// fake the per-agent dir by passing the runtime dir as the
				// "behaviorDir" and the role-slug as the "agent name".
				agent := provider.AgentConfig{Name: roleEntry.Name()}
				overlay, err := LoadAgentOverlay(runtimeDir, agent)
				if err != nil {
					t.Fatalf("LoadAgentOverlay(%s): %v", roleDir, err)
				}
				if overlay == nil {
					// No agent.yaml in this role directory; that's OK for the
					// Qwen / Opus split test we're about to do — but warn the
					// developer if a role package shipped without one.
					if _, statErr := os.Stat(filepath.Join(roleDir, "agent.yaml")); statErr == nil {
						t.Fatalf("agent.yaml exists at %s but LoadAgentOverlay returned nil", roleDir)
					}
					return
				}

				// Every role.yaml in this feature is v2.
				if overlay.Version != 2 {
					t.Fatalf("role %s: expected version 2, got %d", roleEntry.Name(), overlay.Version)
				}

				// role-code-dev and role-writing must declare a subagent;
				// role-ops/data/research must not.
				wantsSubagent := roleEntry.Name() == "role-code-dev" || roleEntry.Name() == "role-writing"
				hasSubagent := overlay.Subagents != nil
				if wantsSubagent != hasSubagent {
					t.Fatalf("role %s: wantsSubagent=%v, hasSubagent=%v", roleEntry.Name(), wantsSubagent, hasSubagent)
				}

				// Qwen-only roles must declare a primary model (the whole
				// point of these roles is to point at a cheap model).
				if !wantsSubagent && overlay.Model == nil {
					t.Fatalf("role %s: Qwen role must declare model:", roleEntry.Name())
				}
				// Opus roles must NOT declare a primary model (they inherit
				// the runtime default).
				if wantsSubagent && overlay.Model != nil {
					t.Fatalf("role %s: Opus role should leave model: unset (inherits runtime default), got %+v", roleEntry.Name(), overlay.Model)
				}
			})
		}
	}
	if !any {
		t.Fatal("no role-* directories found under agents/_defaults/<runtime>/")
	}
}

// TestRoleDefaults_RoleMetaPresent confirms every role-* directory has a
// role.meta file declaring its type. The provisioning flow reads role.meta
// to infer --type when --role is passed.
func TestRoleDefaults_RoleMetaPresent(t *testing.T) {
	repoRoot := findRepoRoot(t)
	defaultsRoot := filepath.Join(repoRoot, "agents", "_defaults")

	err := filepath.WalkDir(defaultsRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			return nil
		}
		name := d.Name()
		if !strings.HasPrefix(name, "role-") {
			return nil
		}
		metaPath := filepath.Join(path, "role.meta")
		data, err := os.ReadFile(metaPath)
		if err != nil {
			t.Errorf("role %s missing role.meta: %v", path, err)
			return nil
		}
		content := strings.TrimSpace(string(data))
		if content != "type: user" && content != "type: team" {
			t.Errorf("role %s role.meta has unexpected content %q (want \"type: user\" or \"type: team\")", path, content)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
}

// findRepoRoot walks up from the test's working directory to find the
// congaline go.mod. Matches the repo-finding pattern used elsewhere in
// the codebase (resolveAWSBehaviorDir in pkg/provider/awsprovider).
func findRepoRoot(t *testing.T) string {
	t.Helper()
	const moduleMarker = "module github.com/cruxdigital-llc/conga-line"
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		goMod := filepath.Join(dir, "go.mod")
		if data, err := os.ReadFile(goMod); err == nil && strings.Contains(string(data), moduleMarker) {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("repo root not found from %s", dir)
		}
		dir = parent
	}
}
