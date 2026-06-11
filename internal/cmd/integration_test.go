//go:build integration

package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// skipIfPriorFailed returns true (and skips) when a prior subtest has
// already failed, preventing noisy cascading failures in sequential tests.
func skipIfPriorFailed(t *testing.T, parent *testing.T) {
	t.Helper()
	if parent.Failed() {
		t.Skip("skipped due to prior subtest failure")
	}
}

// TestAgentLifecycle exercises the full user-agent lifecycle: setup, provision,
// secrets, refresh, logs, pause/unpause, removal, teardown. Each subtest
// depends on the previous — if one fails, later ones are skipped.
func TestAgentLifecycle(t *testing.T) {
	dataDir, agentName := setupTestEnv(t)
	base := baseArgs(dataDir)
	parent := t

	t.Run("setup", func(t *testing.T) {
		cfg := fmt.Sprintf(`{"image":%q}`, testImage)
		mustRunCLI(t, append(base, "admin", "setup", "--json", cfg)...)

		if _, err := os.Stat(filepath.Join(dataDir, "local-config.json")); err != nil {
			t.Fatalf("local-config.json not created: %v", err)
		}
	})

	t.Run("add-user", func(t *testing.T) {
		skipIfPriorFailed(t, parent)
		mustRunCLI(t, append(base, "admin", "add-user", agentName)...)
		assertContainerRunning(t, agentName)
	})

	t.Run("list-agents", func(t *testing.T) {
		skipIfPriorFailed(t, parent)
		out := mustRunCLI(t, append(base, "admin", "list-agents", "--output", "json")...)
		if !strings.Contains(out, agentName) {
			t.Errorf("list-agents output does not contain %q:\n%s", agentName, out)
		}
	})

	t.Run("status", func(t *testing.T) {
		skipIfPriorFailed(t, parent)
		out := mustRunCLI(t, append(base, "status", "--agent", agentName, "--output", "json")...)
		if !strings.Contains(out, `"running"`) {
			t.Errorf("status does not show running:\n%s", out)
		}
	})

	t.Run("secrets-set", func(t *testing.T) {
		skipIfPriorFailed(t, parent)
		mustRunCLI(t, append(base, "secrets", "set", "test-key", "--value", "dummy123", "--agent", agentName)...)
	})

	t.Run("secrets-list", func(t *testing.T) {
		skipIfPriorFailed(t, parent)
		out := mustRunCLI(t, append(base, "secrets", "list", "--agent", agentName, "--output", "json")...)
		if !strings.Contains(out, "test-key") {
			t.Errorf("secrets list does not contain test-key:\n%s", out)
		}
	})

	t.Run("secrets-not-in-env-before-refresh", func(t *testing.T) {
		skipIfPriorFailed(t, parent)
		assertNoEnvVar(t, agentName, "TEST_KEY")
	})

	t.Run("refresh", func(t *testing.T) {
		skipIfPriorFailed(t, parent)
		mustRunCLI(t, append(base, "refresh", "--agent", agentName)...)
		assertContainerRunning(t, agentName)
	})

	t.Run("secrets-in-env-after-refresh", func(t *testing.T) {
		skipIfPriorFailed(t, parent)
		assertEnvVar(t, agentName, "TEST_KEY", "dummy123")
	})

	t.Run("secrets-delete", func(t *testing.T) {
		skipIfPriorFailed(t, parent)
		mustRunCLI(t, append(base, "secrets", "delete", "test-key", "--agent", agentName, "--force")...)
		out := mustRunCLI(t, append(base, "secrets", "list", "--agent", agentName, "--output", "json")...)
		if strings.Contains(out, "test-key") {
			t.Errorf("secret test-key still in list after delete:\n%s", out)
		}
	})

	t.Run("refresh-after-delete", func(t *testing.T) {
		skipIfPriorFailed(t, parent)
		mustRunCLI(t, append(base, "refresh", "--agent", agentName)...)
		assertContainerRunning(t, agentName)
	})

	t.Run("secrets-gone-from-env", func(t *testing.T) {
		skipIfPriorFailed(t, parent)
		assertNoEnvVar(t, agentName, "TEST_KEY")
	})

	t.Run("logs", func(t *testing.T) {
		skipIfPriorFailed(t, parent)
		// Use docker logs directly — the CLI pipes through fmt.Print which
		// our stdout capture handles, but the container may need a moment
		// to produce output after restart.
		cName := "conga-" + agentName
		var out string
		for i := 0; i < 5; i++ {
			raw, _ := exec.Command("docker", "logs", "--tail", "10", cName).CombinedOutput()
			out = string(raw)
			if len(strings.TrimSpace(out)) > 0 {
				break
			}
			time.Sleep(2 * time.Second)
		}
		if len(strings.TrimSpace(out)) == 0 {
			t.Error("docker logs output is empty after 10s")
		}
	})

	t.Run("pause", func(t *testing.T) {
		skipIfPriorFailed(t, parent)
		mustRunCLI(t, append(base, "admin", "pause", agentName)...)
		assertContainerStopped(t, agentName)
	})

	t.Run("unpause", func(t *testing.T) {
		skipIfPriorFailed(t, parent)
		mustRunCLI(t, append(base, "admin", "unpause", agentName)...)
		assertContainerRunning(t, agentName)
	})

	// CRIT-4 regression guard for the unpause self-heal path (followups
	// #5): when the runtime resource backing an agent has been removed
	// out-of-band (on AWS that's the systemd unit file; locally the
	// equivalent is the Docker container itself), `conga admin unpause`
	// must recreate it instead of returning the old catch-22 "container
	// not found / no unit to start" error. Without this guard a future
	// refactor that drops the RefreshAgent recreation path inside
	// UnpauseAgent would silently reintroduce the bug.
	t.Run("unpause-recreates-missing-container", func(t *testing.T) {
		skipIfPriorFailed(t, parent)
		cName := "conga-" + agentName

		// Pause to detach the container, then forcibly remove it so the
		// next unpause must recreate it from config. `docker rm -f` may
		// no-op if pause already removed the container — that's fine,
		// the assertion below is the authoritative check.
		mustRunCLI(t, append(base, "admin", "pause", agentName)...)
		assertContainerStopped(t, agentName)
		_ = exec.Command("docker", "rm", "-f", cName).Run()
		assertContainerNotExists(t, agentName)

		mustRunCLI(t, append(base, "admin", "unpause", agentName)...)
		assertContainerRunning(t, agentName)
	})

	t.Run("remove-agent", func(t *testing.T) {
		skipIfPriorFailed(t, parent)
		mustRunCLI(t, append(base, "admin", "remove-agent", agentName, "--force", "--delete-secrets")...)
		assertContainerNotExists(t, agentName)
	})

	t.Run("teardown", func(t *testing.T) {
		mustRunCLI(t, append(base, "admin", "teardown", "--force")...)
	})
}

// TestTeamAgentWithBehavior tests per-agent behavior file deployment and
// manifest reconciliation using a team agent with custom behavior files.
func TestTeamAgentWithBehavior(t *testing.T) {
	dataDir, agentName := setupTestEnv(t)
	base := baseArgs(dataDir)
	root := repoRoot(t)
	parent := t

	workspacePath := "/home/node/.openclaw/data/workspace"

	t.Run("setup", func(t *testing.T) {
		cfg := fmt.Sprintf(`{"image":%q,"repo_path":%q}`, testImage, root)
		mustRunCLI(t, append(base, "admin", "setup", "--json", cfg)...)
	})

	t.Run("create-agent-behavior", func(t *testing.T) {
		skipIfPriorFailed(t, parent)
		agentBehaviorDir := filepath.Join(dataDir, "agents", agentName)
		if err := os.MkdirAll(agentBehaviorDir, 0755); err != nil {
			t.Fatalf("failed to create agent behavior dir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(agentBehaviorDir, "SOUL.md"),
			[]byte("# Test Soul\n\nThis is a test-specific SOUL.md."), 0644); err != nil {
			t.Fatalf("failed to write test SOUL.md: %v", err)
		}
	})

	t.Run("add-team", func(t *testing.T) {
		skipIfPriorFailed(t, parent)
		mustRunCLI(t, append(base, "admin", "add-team", agentName)...)
		assertContainerRunning(t, agentName)
	})

	t.Run("verify-soul-in-container", func(t *testing.T) {
		skipIfPriorFailed(t, parent)
		assertFileContent(t, agentName, workspacePath+"/SOUL.md", "Test Soul")
	})

	t.Run("verify-agents-default", func(t *testing.T) {
		skipIfPriorFailed(t, parent)
		// AGENTS.md should come from default (not agent-specific)
		assertFileContent(t, agentName, workspacePath+"/AGENTS.md", "Your Workspace")
	})

	t.Run("verify-memory-pristine", func(t *testing.T) {
		skipIfPriorFailed(t, parent)
		cName := "conga-" + agentName
		out, err := dockerExec(t, cName, "cat", workspacePath+"/MEMORY.md")
		if err != nil {
			t.Fatalf("failed to read MEMORY.md: %v", err)
		}
		if strings.TrimSpace(out) != "# Memory" {
			t.Errorf("MEMORY.md is not pristine: %q", out)
		}
	})

	t.Run("add-agents-md-override", func(t *testing.T) {
		skipIfPriorFailed(t, parent)
		// Write an agent-specific AGENTS.md (overriding the default)
		content := []byte("# Custom AGENTS.md\n\nAdded by integration test.")
		agentDir := filepath.Join(dataDir, "agents", agentName)
		if err := os.WriteFile(filepath.Join(agentDir, "AGENTS.md"), content, 0644); err != nil {
			t.Fatalf("failed to write AGENTS.md: %v", err)
		}
	})

	t.Run("refresh-for-behavior", func(t *testing.T) {
		skipIfPriorFailed(t, parent)
		mustRunCLI(t, append(base, "refresh", "--agent", agentName)...)
		assertContainerRunning(t, agentName)
	})

	t.Run("verify-agents-md-overridden", func(t *testing.T) {
		skipIfPriorFailed(t, parent)
		assertFileContent(t, agentName, workspacePath+"/AGENTS.md", "Custom AGENTS.md")
	})

	t.Run("remove-agents-md-override", func(t *testing.T) {
		skipIfPriorFailed(t, parent)
		os.Remove(filepath.Join(dataDir, "agents", agentName, "AGENTS.md"))
	})

	t.Run("refresh-after-rm", func(t *testing.T) {
		skipIfPriorFailed(t, parent)
		mustRunCLI(t, append(base, "refresh", "--agent", agentName)...)
	})

	t.Run("verify-agents-md-reverted", func(t *testing.T) {
		skipIfPriorFailed(t, parent)
		// Should revert to the default AGENTS.md
		assertFileContent(t, agentName, workspacePath+"/AGENTS.md", "Your Workspace")
	})

	t.Run("verify-memory-still-pristine", func(t *testing.T) {
		skipIfPriorFailed(t, parent)
		cName := "conga-" + agentName
		out, err := dockerExec(t, cName, "cat", workspacePath+"/MEMORY.md")
		if err != nil {
			t.Fatalf("failed to read MEMORY.md: %v", err)
		}
		if strings.TrimSpace(out) != "# Memory" {
			t.Errorf("MEMORY.md was modified: %q", out)
		}
	})

	t.Run("teardown", func(t *testing.T) {
		mustRunCLI(t, append(base, "admin", "teardown", "--force")...)
	})
}

// TestFleetAndPerAgentConfig codifies the feature #31 live verification (T9.2):
// the declarative fleet + per-agent custom-config layers are deployed via the
// $include array, OpenClaw composes them (union + precedence), changes propagate
// on refresh with baselines kept fresh, a reserved-key fleet source fails closed,
// show-config renders the layers, and the managed-include baselines are cleaned
// up on removal.
//
// #31 sources resolve from the LIVE repo (overlayBehaviorDir == repo_path == root),
// not the dataDir snapshot, so the sources are written into <root>/agents/. The
// per-agent custom.json is gitignored; the fleet source is not, so it is guarded
// (skip if one already exists) and removed on cleanup.
func TestFleetAndPerAgentConfig(t *testing.T) {
	dataDir, agentName := setupTestEnv(t)
	base := baseArgs(dataDir)
	root := repoRoot(t)
	parent := t

	const containerDataPath = "/home/node/.openclaw"
	fleetSrc := filepath.Join(root, "agents", "_defaults", "openclaw", "fleet-custom.json")
	perAgentDir := filepath.Join(root, "agents", agentName)
	perAgentSrc := filepath.Join(perAgentDir, "custom.json")

	if _, err := os.Stat(fleetSrc); err == nil {
		t.Skipf("refusing to clobber an existing committed %s", fleetSrc)
	}
	t.Cleanup(func() {
		os.Remove(fleetSrc)
		os.RemoveAll(perAgentDir)
	})

	t.Run("setup", func(t *testing.T) {
		cfg := fmt.Sprintf(`{"image":%q,"repo_path":%q}`, testImage, root)
		mustRunCLI(t, append(base, "admin", "setup", "--json", cfg)...)
	})

	t.Run("write-config-sources", func(t *testing.T) {
		skipIfPriorFailed(t, parent)
		if err := os.MkdirAll(perAgentDir, 0755); err != nil {
			t.Fatal(err)
		}
		// Fleet: two servers (one shared key to be overridden). Per-agent: a
		// distinct server (proves union) + overrides the shared key (proves
		// per-agent > fleet). Keys live under mcp.* which the root does not own,
		// so include precedence is observable.
		if err := os.WriteFile(fleetSrc, []byte(`{"mcp":{"servers":{"fleetmcp":{"url":"https://fleet.example/sse"},"shared":{"url":"https://fleet.example/shared"}}}}`), 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(perAgentSrc, []byte(`{"mcp":{"servers":{"agentmcp":{"url":"https://agent.example/sse"},"shared":{"url":"https://agent.example/shared"}}}}`), 0644); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("add-user", func(t *testing.T) {
		skipIfPriorFailed(t, parent)
		mustRunCLI(t, append(base, "admin", "add-user", agentName)...)
		assertContainerRunning(t, agentName)
	})

	t.Run("include-array-deployed-in-precedence-order", func(t *testing.T) {
		skipIfPriorFailed(t, parent)
		out, err := dockerExec(t, "conga-"+agentName, "cat", containerDataPath+"/openclaw.json")
		if err != nil {
			t.Fatalf("cat openclaw.json: %v\n%s", err, out)
		}
		var cfg struct {
			Include []string `json:"$include"`
		}
		if err := json.Unmarshal([]byte(out), &cfg); err != nil {
			t.Fatalf("parse openclaw.json: %v", err)
		}
		want := []string{"fleet-custom.json", "agent-managed-custom.json", "agent-custom.json"}
		if len(cfg.Include) != len(want) {
			t.Fatalf("$include = %v, want %v", cfg.Include, want)
		}
		for i := range want {
			if cfg.Include[i] != want[i] {
				t.Errorf("$include[%d] = %q, want %q", i, cfg.Include[i], want[i])
			}
		}
	})

	t.Run("layers-deployed-from-sources", func(t *testing.T) {
		skipIfPriorFailed(t, parent)
		assertFileContent(t, agentName, containerDataPath+"/fleet-custom.json", "fleetmcp")
		assertFileContent(t, agentName, containerDataPath+"/agent-managed-custom.json", "agentmcp")
		// agent-custom.json (admin drift) starts empty.
		assertFileContent(t, agentName, containerDataPath+"/agent-custom.json", "{}")
	})

	// OpenClaw resolves the $include merge — verify its actual effective config.
	t.Run("effective-merge-union", func(t *testing.T) {
		skipIfPriorFailed(t, parent)
		out, err := dockerExec(t, "conga-"+agentName, "openclaw", "config", "get", "mcp.servers")
		if err != nil {
			t.Fatalf("openclaw config get mcp.servers: %v\n%s", err, out)
		}
		for _, want := range []string{"fleetmcp", "agentmcp", "shared"} {
			if !strings.Contains(out, want) {
				t.Errorf("effective mcp.servers missing %q (union broken)\ngot: %s", want, out)
			}
		}
	})

	t.Run("effective-merge-per-agent-over-fleet", func(t *testing.T) {
		skipIfPriorFailed(t, parent)
		out, err := dockerExec(t, "conga-"+agentName, "openclaw", "config", "get", "mcp.servers.shared.url")
		if err != nil {
			t.Fatalf("config get shared.url: %v\n%s", err, out)
		}
		if !strings.Contains(out, "agent.example") {
			t.Errorf("per-agent must win over fleet on shared.url, got: %s", out)
		}
	})

	t.Run("admin-drift-over-per-agent", func(t *testing.T) {
		skipIfPriorFailed(t, parent)
		// Admin edits the on-host agent-custom.json (highest-precedence include).
		adminPath := filepath.Join(dataDir, "data", agentName, "agent-custom.json")
		if err := os.WriteFile(adminPath, []byte(`{"mcp":{"servers":{"shared":{"url":"https://admin.example/shared"}}}}`), 0644); err != nil {
			t.Fatal(err)
		}
		mustRunCLI(t, append(base, "refresh", "--agent", agentName)...)
		assertContainerRunning(t, agentName)
		out, err := dockerExec(t, "conga-"+agentName, "openclaw", "config", "get", "mcp.servers.shared.url")
		if err != nil {
			t.Fatalf("config get shared.url: %v\n%s", err, out)
		}
		if !strings.Contains(out, "admin.example") {
			t.Errorf("admin drift must win over per-agent, got: %s", out)
		}
	})

	t.Run("fleet-propagation-on-refresh", func(t *testing.T) {
		skipIfPriorFailed(t, parent)
		// Edit the fleet source → refresh → the new server lands on the agent.
		if err := os.WriteFile(fleetSrc, []byte(`{"mcp":{"servers":{"fleetmcp":{"url":"https://fleet.example/sse"},"shared":{"url":"https://fleet.example/shared"},"fleetv2":{"url":"https://fleet.example/v2"}}}}`), 0644); err != nil {
			t.Fatal(err)
		}
		mustRunCLI(t, append(base, "refresh", "--agent", agentName)...)
		assertContainerRunning(t, agentName)
		assertFileContent(t, agentName, containerDataPath+"/fleet-custom.json", "fleetv2")
		out, err := dockerExec(t, "conga-"+agentName, "openclaw", "config", "get", "mcp.servers")
		if err != nil {
			t.Fatalf("config get mcp.servers: %v\n%s", err, out)
		}
		if !strings.Contains(out, "fleetv2") {
			t.Errorf("fleet change did not propagate to effective config\ngot: %s", out)
		}
	})

	t.Run("show-config-renders-layers", func(t *testing.T) {
		skipIfPriorFailed(t, parent)
		stdout := mustRunCLI(t, append(base, "agent", "show-config", agentName, "--output", "json")...)
		var got struct {
			Agent  string `json:"agent"`
			Layers []struct {
				File       string `json:"file"`
				Role       string `json:"role"`
				Precedence int    `json:"precedence"`
				Present    bool   `json:"present"`
			} `json:"layers"`
		}
		if err := json.Unmarshal([]byte(stdout), &got); err != nil {
			t.Fatalf("parse show-config json: %v\n%s", err, stdout)
		}
		wantFiles := []string{"openclaw.json", "agent-custom.json", "agent-managed-custom.json", "fleet-custom.json"}
		if len(got.Layers) != len(wantFiles) {
			t.Fatalf("show-config returned %d layers, want %d: %+v", len(got.Layers), len(wantFiles), got.Layers)
		}
		for i, want := range wantFiles {
			l := got.Layers[i]
			if l.File != want || l.Precedence != i+1 || !l.Present {
				t.Errorf("layer %d = {file:%q prec:%d present:%v}, want file:%q prec:%d present:true", i, l.File, l.Precedence, l.Present, want, i+1)
			}
		}
	})

	t.Run("reserved-key-fleet-source-fails-closed", func(t *testing.T) {
		skipIfPriorFailed(t, parent)
		// A reserved key in the FLEET source would compromise every agent — the
		// refresh must abort before deploying, and the bad content must not reach
		// the host (blast-radius mitigation).
		if err := os.WriteFile(fleetSrc, []byte(`{"channels":{"slack":{"allowFrom":["U-EVIL"]}}}`), 0644); err != nil {
			t.Fatal(err)
		}
		_, stderr, err := runCLI(t, append(base, "refresh", "--agent", agentName)...)
		if err == nil {
			t.Fatal("refresh with a reserved-key fleet source should fail closed, but succeeded")
		}
		if !containsAny(stderr, "reserved", "conga-owned", "channels", "pre-deploy validation") {
			t.Errorf("expected a reserved-key/fail-closed error, got stderr: %s", stderr)
		}
		// The bad content must not have reached the agent.
		out, derr := dockerExec(t, "conga-"+agentName, "cat", containerDataPath+"/fleet-custom.json")
		if derr == nil && strings.Contains(out, "channels") {
			t.Errorf("reserved-key fleet content leaked to the host: %s", out)
		}
		// Restore a clean fleet source so cleanup/refresh paths stay sane.
		if err := os.WriteFile(fleetSrc, []byte(`{"mcp":{"servers":{"fleetmcp":{"url":"https://fleet.example/sse"}}}}`), 0644); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("remove-cleans-managed-baselines", func(t *testing.T) {
		skipIfPriorFailed(t, parent)
		mustRunCLI(t, append(base, "admin", "remove-agent", agentName, "--force")...)
		assertContainerNotExists(t, agentName)
		// The two managed-include baselines must be removed alongside the root one.
		for _, bn := range []string{
			agentName + ".fleet-custom.json.sha256",
			agentName + ".agent-managed-custom.json.sha256",
		} {
			if _, err := os.Stat(filepath.Join(dataDir, "config", bn)); !os.IsNotExist(err) {
				t.Errorf("orphaned managed-include baseline %s (err=%v)", bn, err)
			}
		}
	})

	t.Run("teardown", func(t *testing.T) {
		mustRunCLI(t, append(base, "admin", "teardown", "--force")...)
	})
}

// TestPolicyValidate tests the policy validation command without Docker containers.
func TestPolicyValidate(t *testing.T) {
	dataDir := setupPolicyTestEnv(t)
	base := baseArgs(dataDir)

	t.Run("setup", func(t *testing.T) {
		cfg := fmt.Sprintf(`{"image":%q}`, testImage)
		mustRunCLI(t, append(base, "admin", "setup", "--json", cfg)...)
	})

	t.Run("write-valid-policy", func(t *testing.T) {
		writePolicyFile(t, dataDir, `apiVersion: conga.dev/v1alpha1
egress:
  mode: enforce
  allowed_domains:
    - api.anthropic.com
`)
	})

	t.Run("validate-passes", func(t *testing.T) {
		_, _, err := runCLI(t, append(base, "policy", "validate")...)
		if err != nil {
			t.Errorf("policy validate failed for valid policy: %v", err)
		}
	})

	t.Run("write-invalid-policy", func(t *testing.T) {
		writePolicyFile(t, dataDir, `egress:
  mode: enforce
`)
	})

	t.Run("validate-fails", func(t *testing.T) {
		_, stderr, err := runCLI(t, append(base, "policy", "validate")...)
		if err == nil {
			t.Fatal("policy validate should fail for missing apiVersion")
		}
		combined := stderr + err.Error()
		if !strings.Contains(strings.ToLower(combined), "apiversion") {
			t.Errorf("error should mention apiVersion, got: %s", combined)
		}
	})

	t.Run("teardown", func(t *testing.T) {
		mustRunCLI(t, append(base, "admin", "teardown", "--force")...)
	})
}

// TestEgressPolicyEnforcement verifies that the egress proxy actually controls
// outbound traffic from inside the container across all three policy modes.
func TestEgressPolicyEnforcement(t *testing.T) {
	dataDir, agentName := setupTestEnv(t)
	base := baseArgs(dataDir)
	parent := t

	t.Run("setup", func(t *testing.T) {
		cfg := fmt.Sprintf(`{"image":%q}`, testImage)
		mustRunCLI(t, append(base, "admin", "setup", "--json", cfg)...)
	})

	t.Run("add-user", func(t *testing.T) {
		skipIfPriorFailed(t, parent)
		mustRunCLI(t, append(base, "admin", "add-user", agentName)...)
		assertContainerRunning(t, agentName)
	})

	t.Run("no-policy-blocks", func(t *testing.T) {
		skipIfPriorFailed(t, parent)
		// Default: no policy file → egress proxy deny-all
		_, err := makeHTTPRequest(t, agentName, "https://api.anthropic.com")
		if err == nil {
			t.Error("expected HTTP request to be blocked with no policy (deny-all)")
		}
	})

	t.Run("write-validate-policy", func(t *testing.T) {
		skipIfPriorFailed(t, parent)
		writePolicyFile(t, dataDir, `apiVersion: conga.dev/v1alpha1
egress:
  mode: validate
  allowed_domains:
    - api.anthropic.com
`)
	})

	t.Run("refresh-validate", func(t *testing.T) {
		skipIfPriorFailed(t, parent)
		mustRunCLI(t, append(base, "refresh", "--agent", agentName)...)
		assertContainerRunning(t, agentName)
	})

	t.Run("validate-allows", func(t *testing.T) {
		skipIfPriorFailed(t, parent)
		// Validate mode: proxy logs but allows traffic
		code, err := makeHTTPRequest(t, agentName, "https://api.anthropic.com")
		if err != nil {
			t.Errorf("expected request to succeed in validate mode, got error: %v", err)
		} else {
			t.Logf("validate mode: api.anthropic.com returned HTTP %d", code)
		}
	})

	t.Run("write-enforce-policy", func(t *testing.T) {
		skipIfPriorFailed(t, parent)
		writePolicyFile(t, dataDir, `apiVersion: conga.dev/v1alpha1
egress:
  mode: enforce
  allowed_domains:
    - api.anthropic.com
`)
	})

	t.Run("refresh-enforce", func(t *testing.T) {
		skipIfPriorFailed(t, parent)
		mustRunCLI(t, append(base, "refresh", "--agent", agentName)...)
		assertContainerRunning(t, agentName)
	})

	t.Run("enforce-allowed", func(t *testing.T) {
		skipIfPriorFailed(t, parent)
		// Enforce mode: allowed domain should get through
		code, err := makeHTTPRequest(t, agentName, "https://api.anthropic.com")
		if err != nil {
			t.Errorf("expected request to api.anthropic.com to succeed in enforce mode, got error: %v", err)
		} else {
			t.Logf("enforce mode: api.anthropic.com returned HTTP %d", code)
		}
	})

	t.Run("enforce-blocked", func(t *testing.T) {
		skipIfPriorFailed(t, parent)
		// Enforce mode: non-allowed domain should be blocked
		_, err := makeHTTPRequest(t, agentName, "https://example.com")
		if err == nil {
			t.Error("expected request to example.com to be blocked in enforce mode")
		}
	})

	t.Run("teardown", func(t *testing.T) {
		mustRunCLI(t, append(base, "admin", "teardown", "--force")...)
	})
}
