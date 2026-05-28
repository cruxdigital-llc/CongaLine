package cmd

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// setupRoleFixture creates a fake congaline repo layout under tmp:
//   - tmp/go.mod                                       (with the conga-line marker)
//   - tmp/agents/_defaults/<runtime>/role-<slug>/role.meta + agent.yaml
//
// Returns the tmp path. The test should chdir into it so
// ResolveOperatorBehaviorDir finds the agents/ dir.
func setupRoleFixture(t *testing.T, runtimeName, roleSlug, declaredType string) string {
	t.Helper()
	tmp := t.TempDir()

	goMod := "module github.com/cruxdigital-llc/conga-line\n\ngo 1.21\n"
	if err := os.WriteFile(filepath.Join(tmp, "go.mod"), []byte(goMod), 0644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	roleDir := filepath.Join(tmp, "agents", "_defaults", runtimeName, roleSlug)
	if err := os.MkdirAll(roleDir, 0755); err != nil {
		t.Fatalf("mkdir role: %v", err)
	}
	files := map[string]string{
		"role.meta":  "type: " + declaredType + "\n",
		"agent.yaml": "version: 2\nmodel:\n  provider: openai\n  name: x\n  base_url: https://h/v1\n",
		"SOUL.md":    "# soul",
		"AGENTS.md":  "# agents",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(roleDir, name), []byte(content), 0644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	return tmp
}

// chdirTo changes the test's working directory and restores it on cleanup.
func chdirTo(t *testing.T, dir string) {
	t.Helper()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(orig) })
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir %s: %v", dir, err)
	}
}

// captureCmdErrWriter swaps cmdErrWriter to a buffer for the duration of fn.
func captureCmdErrWriter(t *testing.T, fn func()) string {
	t.Helper()
	var buf bytes.Buffer
	orig := cmdErrWriter
	cmdErrWriter = func() io.Writer { return &buf }
	t.Cleanup(func() { cmdErrWriter = orig })
	fn()
	return buf.String()
}

func TestApplyRolePackageIfRequested_Empty_NoOp(t *testing.T) {
	// Empty role → no behavior dir lookup, no error.
	err := applyRolePackageIfRequested("", "any", "openclaw", "user")
	if err != nil {
		t.Fatalf("empty role should be no-op, got %v", err)
	}
}

func TestApplyRolePackageIfRequested_HappyPath_User(t *testing.T) {
	tmp := setupRoleFixture(t, "openclaw", "role-ops", "user")
	chdirTo(t, tmp)

	var stderr string
	stderr = captureCmdErrWriter(t, func() {
		if err := applyRolePackageIfRequested("role-ops", "myagent", "openclaw", "user"); err != nil {
			t.Fatalf("applyRolePackageIfRequested: %v", err)
		}
	})

	if !strings.Contains(stderr, "role-ops") || !strings.Contains(stderr, "myagent") {
		t.Fatalf("expected stderr to mention role and agent name, got %q", stderr)
	}

	// Files should have been copied.
	for _, name := range []string{"role.meta", "agent.yaml", "SOUL.md", "AGENTS.md"} {
		path := filepath.Join(tmp, "agents", "myagent", name)
		exists := false
		if _, err := os.Stat(path); err == nil {
			exists = true
		}
		// role.meta must NOT be copied; others should be.
		wantExists := name != "role.meta"
		if exists != wantExists {
			t.Errorf("agents/myagent/%s: wantExists=%v, exists=%v", name, wantExists, exists)
		}
	}
}

func TestApplyRolePackageIfRequested_TypeMismatch(t *testing.T) {
	// Role declares "team", but we invoke from add-user (cmdType = "user").
	tmp := setupRoleFixture(t, "openclaw", "role-team-only", "team")
	chdirTo(t, tmp)

	err := applyRolePackageIfRequested("role-team-only", "myagent", "openclaw", "user")
	if err == nil {
		t.Fatal("expected type-mismatch error, got nil")
	}
	if !strings.Contains(err.Error(), "declares type \"team\"") || !strings.Contains(err.Error(), "add-user") {
		t.Fatalf("error should explain the mismatch + suggest add-team: %v", err)
	}
	if !strings.Contains(err.Error(), "add-team") {
		t.Fatalf("error should suggest the correct command: %v", err)
	}
}

func TestApplyRolePackageIfRequested_NoRepo(t *testing.T) {
	// Chdir into a tmpdir with no go.mod and no agents/ — ResolveOperator-
	// BehaviorDir returns "" and we should error helpfully. To make this
	// hermetic (TempDir's path may live inside a real conga-line checkout),
	// we don't rely on it failing — instead we set up a fixture WITHOUT the
	// role we'll ask for. ApplyRolePackage will then error with "not found".
	tmp := setupRoleFixture(t, "openclaw", "role-exists", "user")
	chdirTo(t, tmp)

	err := applyRolePackageIfRequested("role-does-not-exist", "myagent", "openclaw", "user")
	if err == nil {
		t.Fatal("expected error for nonexistent role, got nil")
	}
	if !strings.Contains(err.Error(), "role-does-not-exist") {
		t.Fatalf("error should name the requested role: %v", err)
	}
}

func TestApplyRolePackageIfRequested_Idempotency(t *testing.T) {
	// QA persona requirement (spec.md Phase 6 acceptance): running --role on
	// an existing customized agent must preserve the customization.
	tmp := setupRoleFixture(t, "openclaw", "role-ops", "user")
	chdirTo(t, tmp)

	// Pre-create the agent dir with a customized agent.yaml.
	agentDir := filepath.Join(tmp, "agents", "myagent")
	if err := os.MkdirAll(agentDir, 0700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	customYaml := "version: 2\nmodel:\n  provider: openai\n  name: my-custom-pin\n  base_url: https://my.lan/v1\n"
	if err := os.WriteFile(filepath.Join(agentDir, "agent.yaml"), []byte(customYaml), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Run the flow.
	stderr := captureCmdErrWriter(t, func() {
		if err := applyRolePackageIfRequested("role-ops", "myagent", "openclaw", "user"); err != nil {
			t.Fatalf("applyRolePackageIfRequested: %v", err)
		}
	})
	_ = stderr

	// agent.yaml must be the operator's customized version.
	got, err := os.ReadFile(filepath.Join(agentDir, "agent.yaml"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != customYaml {
		t.Fatalf("agent.yaml was overwritten\nwant: %q\ngot:  %q", customYaml, got)
	}

	// SOUL.md and AGENTS.md (which didn't exist before) ARE present.
	for _, name := range []string{"SOUL.md", "AGENTS.md"} {
		if _, err := os.Stat(filepath.Join(agentDir, name)); err != nil {
			t.Errorf("expected %s to be copied, got error: %v", name, err)
		}
	}
}
