//go:build integration

package cmd

import (
	"context"
	"fmt"
	"hash/crc32"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestRemoteAgentLifecycle exercises the full user-agent lifecycle through
// the remote provider's SSH+SFTP code paths.
func TestRemoteAgentLifecycle(t *testing.T) {
	dataDir, agentName, sshPort, keyPath, remoteDir := setupRemoteTestEnv(t)
	base := remoteBaseArgs(dataDir)
	root := repoRoot(t)

	t.Run("setup", func(t *testing.T) {
		cfg := fmt.Sprintf(
			`{"ssh_host":"127.0.0.1","ssh_port":%d,"ssh_user":"root","ssh_key_path":%q,"image":%q,"repo_path":%q,"remote_dir":%q}`,
			sshPort, keyPath, testImage, root, remoteDir)
		mustRunCLI(t, append(base, "admin", "setup", "--json", cfg)...)

		if _, err := os.Stat(filepath.Join(dataDir, "remote-config.json")); err != nil {
			t.Fatalf("remote-config.json not created: %v", err)
		}
	})

	t.Run("add-user", func(t *testing.T) {
		mustRunCLI(t, append(base, "admin", "add-user", agentName)...)
		assertContainerRunning(t, agentName)
	})

	t.Run("list-agents", func(t *testing.T) {
		out := mustRunCLI(t, append(base, "admin", "list-agents", "--output", "json")...)
		if !strings.Contains(out, agentName) {
			t.Errorf("list-agents output does not contain %q:\n%s", agentName, out)
		}
	})

	t.Run("status", func(t *testing.T) {
		out := mustRunCLI(t, append(base, "status", "--agent", agentName, "--output", "json")...)
		if !strings.Contains(out, `"running"`) {
			t.Errorf("status does not show running:\n%s", out)
		}
	})

	t.Run("secrets-set", func(t *testing.T) {
		mustRunCLI(t, append(base, "secrets", "set", "test-key", "--value", "dummy123", "--agent", agentName)...)
	})

	t.Run("secrets-list", func(t *testing.T) {
		out := mustRunCLI(t, append(base, "secrets", "list", "--agent", agentName, "--output", "json")...)
		if !strings.Contains(out, "test-key") {
			t.Errorf("secrets list does not contain test-key:\n%s", out)
		}
	})

	t.Run("secrets-not-in-env-before-refresh", func(t *testing.T) {
		assertNoEnvVar(t, agentName, "TEST_KEY")
	})

	t.Run("refresh", func(t *testing.T) {
		mustRunCLI(t, append(base, "refresh", "--agent", agentName)...)
		assertContainerRunning(t, agentName)
	})

	t.Run("secrets-in-env-after-refresh", func(t *testing.T) {
		assertEnvVar(t, agentName, "TEST_KEY", "dummy123")
	})

	t.Run("secrets-delete", func(t *testing.T) {
		mustRunCLI(t, append(base, "secrets", "delete", "test-key", "--agent", agentName, "--force")...)
		out := mustRunCLI(t, append(base, "secrets", "list", "--agent", agentName, "--output", "json")...)
		if strings.Contains(out, "test-key") {
			t.Errorf("secret test-key still in list after delete:\n%s", out)
		}
	})

	t.Run("refresh-after-delete", func(t *testing.T) {
		mustRunCLI(t, append(base, "refresh", "--agent", agentName)...)
		assertContainerRunning(t, agentName)
	})

	t.Run("secrets-gone-from-env", func(t *testing.T) {
		assertNoEnvVar(t, agentName, "TEST_KEY")
	})

	t.Run("logs", func(t *testing.T) {
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
		mustRunCLI(t, append(base, "admin", "pause", agentName)...)
		assertContainerStopped(t, agentName)
	})

	t.Run("unpause", func(t *testing.T) {
		mustRunCLI(t, append(base, "admin", "unpause", agentName)...)
		assertContainerRunning(t, agentName)
	})

	t.Run("remove-agent", func(t *testing.T) {
		mustRunCLI(t, append(base, "admin", "remove-agent", agentName, "--force", "--delete-secrets")...)
		assertContainerNotExists(t, agentName)
	})

	t.Run("teardown", func(t *testing.T) {
		mustRunCLI(t, append(base, "admin", "teardown", "--force")...)
	})
}

// TestRemoteTeamAgentWithBehavior tests per-agent behavior file deployment
// through the remote provider's SFTP code paths.
func TestRemoteTeamAgentWithBehavior(t *testing.T) {
	dataDir, agentName, sshPort, keyPath, remoteDir := setupRemoteTestEnv(t)
	base := remoteBaseArgs(dataDir)
	root := repoRoot(t)

	workspacePath := "/home/node/.openclaw/data/workspace"

	t.Run("setup", func(t *testing.T) {
		cfg := fmt.Sprintf(
			`{"ssh_host":"127.0.0.1","ssh_port":%d,"ssh_user":"root","ssh_key_path":%q,"image":%q,"repo_path":%q,"remote_dir":%q}`,
			sshPort, keyPath, testImage, root, remoteDir)
		mustRunCLI(t, append(base, "admin", "setup", "--json", cfg)...)
	})

	// Create agent-specific behavior dir in the repo (remote provider reads from repo_path).
	// Cleanup registered on the parent test so the dir persists across subtests.
	agentBehaviorDir := filepath.Join(root, "behavior", "agents", agentName)
	os.MkdirAll(agentBehaviorDir, 0755)
	t.Cleanup(func() { os.RemoveAll(agentBehaviorDir) })

	t.Run("create-agent-behavior", func(t *testing.T) {
		if err := os.WriteFile(filepath.Join(agentBehaviorDir, "SOUL.md"),
			[]byte("# Remote Test Soul\n\nDeployed via SFTP."), 0644); err != nil {
			t.Fatalf("failed to write test SOUL.md: %v", err)
		}
	})

	t.Run("add-team", func(t *testing.T) {
		mustRunCLI(t, append(base, "admin", "add-team", agentName)...)
		assertContainerRunning(t, agentName)
	})

	t.Run("verify-soul-in-container", func(t *testing.T) {
		assertFileContent(t, agentName, workspacePath+"/SOUL.md", "Remote Test Soul")
	})

	t.Run("verify-agents-default", func(t *testing.T) {
		assertFileContent(t, agentName, workspacePath+"/AGENTS.md", "Your Workspace")
	})

	t.Run("verify-memory-pristine", func(t *testing.T) {
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
		content := []byte("# Custom Remote AGENTS.md\n\nOverridden via SFTP.")
		agentDir := agentBehaviorDir
		if err := os.WriteFile(filepath.Join(agentDir, "AGENTS.md"), content, 0644); err != nil {
			t.Fatalf("failed to write AGENTS.md: %v", err)
		}
	})

	t.Run("refresh-for-behavior", func(t *testing.T) {
		mustRunCLI(t, append(base, "refresh", "--agent", agentName)...)
		assertContainerRunning(t, agentName)
	})

	t.Run("verify-agents-md-overridden", func(t *testing.T) {
		assertFileContent(t, agentName, workspacePath+"/AGENTS.md", "Custom Remote AGENTS.md")
	})

	t.Run("remove-agents-md-override", func(t *testing.T) {
		os.Remove(filepath.Join(agentBehaviorDir, "AGENTS.md"))
	})

	t.Run("refresh-after-rm", func(t *testing.T) {
		mustRunCLI(t, append(base, "refresh", "--agent", agentName)...)
	})

	t.Run("verify-agents-md-reverted", func(t *testing.T) {
		assertFileContent(t, agentName, workspacePath+"/AGENTS.md", "Your Workspace")
	})

	t.Run("verify-memory-still-pristine", func(t *testing.T) {
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

// TestRemoteEgressPolicyEnforcement verifies egress proxy behavior through
// the remote provider.
func TestRemoteEgressPolicyEnforcement(t *testing.T) {
	dataDir, agentName, sshPort, keyPath, remoteDir := setupRemoteTestEnv(t)
	base := remoteBaseArgs(dataDir)
	root := repoRoot(t)

	t.Run("setup", func(t *testing.T) {
		cfg := fmt.Sprintf(
			`{"ssh_host":"127.0.0.1","ssh_port":%d,"ssh_user":"root","ssh_key_path":%q,"image":%q,"repo_path":%q,"remote_dir":%q}`,
			sshPort, keyPath, testImage, root, remoteDir)
		mustRunCLI(t, append(base, "admin", "setup", "--json", cfg)...)
	})

	t.Run("add-user", func(t *testing.T) {
		mustRunCLI(t, append(base, "admin", "add-user", agentName)...)
		assertContainerRunning(t, agentName)
	})

	t.Run("no-policy-blocks", func(t *testing.T) {
		_, err := makeHTTPRequest(t, agentName, "https://api.anthropic.com")
		if err == nil {
			t.Error("expected HTTP request to be blocked with no policy (deny-all)")
		}
	})

	t.Run("write-validate-policy", func(t *testing.T) {
		writePolicyFile(t, dataDir, `apiVersion: conga.dev/v1alpha1
egress:
  mode: validate
  allowed_domains:
    - api.anthropic.com
`)
	})

	t.Run("refresh-validate", func(t *testing.T) {
		mustRunCLI(t, append(base, "refresh", "--agent", agentName)...)
		assertContainerRunning(t, agentName)
	})

	t.Run("validate-allows", func(t *testing.T) {
		code, err := makeHTTPRequest(t, agentName, "https://api.anthropic.com")
		if err != nil {
			t.Errorf("expected request to succeed in validate mode, got error: %v", err)
		} else {
			t.Logf("validate mode: api.anthropic.com returned HTTP %d", code)
		}
	})

	t.Run("write-enforce-policy", func(t *testing.T) {
		writePolicyFile(t, dataDir, `apiVersion: conga.dev/v1alpha1
egress:
  mode: enforce
  allowed_domains:
    - api.anthropic.com
`)
	})

	t.Run("refresh-enforce", func(t *testing.T) {
		mustRunCLI(t, append(base, "refresh", "--agent", agentName)...)
		assertContainerRunning(t, agentName)
	})

	t.Run("enforce-allowed", func(t *testing.T) {
		code, err := makeHTTPRequest(t, agentName, "https://api.anthropic.com")
		if err != nil {
			t.Errorf("expected request to api.anthropic.com to succeed in enforce mode, got error: %v", err)
		} else {
			t.Logf("enforce mode: api.anthropic.com returned HTTP %d", code)
		}
	})

	t.Run("enforce-blocked", func(t *testing.T) {
		_, err := makeHTTPRequest(t, agentName, "https://example.com")
		if err == nil {
			t.Error("expected request to example.com to be blocked in enforce mode")
		}
	})

	t.Run("teardown", func(t *testing.T) {
		mustRunCLI(t, append(base, "admin", "teardown", "--force")...)
	})
}

// TestRemoteErrorPaths verifies the CLI returns meaningful errors for
// invalid operations through the remote provider's SSH code paths.
func TestRemoteErrorPaths(t *testing.T) {
	dataDir, agentName, sshPort, keyPath, remoteDir := setupRemoteTestEnv(t)
	base := remoteBaseArgs(dataDir)
	root := repoRoot(t)

	t.Run("setup", func(t *testing.T) {
		cfg := fmt.Sprintf(
			`{"ssh_host":"127.0.0.1","ssh_port":%d,"ssh_user":"root","ssh_key_path":%q,"image":%q,"repo_path":%q,"remote_dir":%q}`,
			sshPort, keyPath, testImage, root, remoteDir)
		mustRunCLI(t, append(base, "admin", "setup", "--json", cfg)...)
	})

	t.Run("add-user", func(t *testing.T) {
		mustRunCLI(t, append(base, "admin", "add-user", agentName)...)
		assertContainerRunning(t, agentName)
	})

	t.Run("remove-nonexistent", func(t *testing.T) {
		_, stderr, err := runCLI(t, append(base, "admin", "remove-agent", "nonexistent-agent", "--force", "--delete-secrets")...)
		if err == nil {
			t.Fatal("expected error removing non-existent agent")
		}
		combined := stderr + err.Error()
		if !containsAny(combined, "nonexistent-agent", "not found", "does not exist", "no such") {
			t.Errorf("error should mention agent name or not found, got: %s", combined)
		}
	})

	t.Run("refresh-nonexistent", func(t *testing.T) {
		_, _, err := runCLI(t, append(base, "refresh", "--agent", "nonexistent-agent")...)
		if err == nil {
			t.Fatal("expected error refreshing non-existent agent")
		}
	})

	t.Run("pause-nonexistent", func(t *testing.T) {
		_, _, err := runCLI(t, append(base, "admin", "pause", "nonexistent-agent")...)
		if err == nil {
			t.Fatal("expected error pausing non-existent agent")
		}
	})

	t.Run("bind-channel-no-platform", func(t *testing.T) {
		_, _, err := runCLI(t, append(base, "channels", "bind", agentName, "nonexistent:U123")...)
		if err == nil {
			t.Fatal("expected error binding unknown channel platform")
		}
	})

	t.Run("teardown", func(t *testing.T) {
		mustRunCLI(t, append(base, "admin", "teardown", "--force")...)
	})
}

// TestRemoteMultiAgent provisions 2 agents simultaneously and verifies
// port allocation, routing.json, network isolation, RefreshAll, and
// independent lifecycle management.
func TestRemoteMultiAgent(t *testing.T) {
	dataDir, _, sshPort, keyPath, remoteDir := setupRemoteTestEnv(t)
	base := remoteBaseArgs(dataDir)
	root := repoRoot(t)
	hash := fmt.Sprintf("%08x", crc32.ChecksumIEEE([]byte(t.Name())))
	if len(hash) > 8 {
		hash = hash[:8]
	}
	agentA := "rtest-" + hash + "-a"
	agentB := "rtest-" + hash + "-b"

	// Register cleanup for both agents
	t.Cleanup(func() { cleanupTestContainers(agentB) })
	t.Cleanup(func() { cleanupTestContainers(agentA) })

	t.Run("setup", func(t *testing.T) {
		cfg := fmt.Sprintf(
			`{"ssh_host":"127.0.0.1","ssh_port":%d,"ssh_user":"root","ssh_key_path":%q,"image":%q,"repo_path":%q,"remote_dir":%q}`,
			sshPort, keyPath, testImage, root, remoteDir)
		mustRunCLI(t, append(base, "admin", "setup", "--json", cfg)...)
	})

	t.Run("add-user-alpha", func(t *testing.T) {
		mustRunCLI(t, append(base, "admin", "add-user", agentA)...)
		assertContainerRunning(t, agentA)
	})

	t.Run("add-team-beta", func(t *testing.T) {
		mustRunCLI(t, append(base, "admin", "add-team", agentB)...)
		assertContainerRunning(t, agentB)
	})

	t.Run("list-agents", func(t *testing.T) {
		out := mustRunCLI(t, append(base, "admin", "list-agents", "--output", "json")...)
		if !strings.Contains(out, agentA) || !strings.Contains(out, agentB) {
			t.Errorf("list-agents should contain both agents:\n%s", out)
		}
	})

	t.Run("verify-unique-ports", func(t *testing.T) {
		cfgA := readFileOnRemote(t, filepath.Join(remoteDir, "agents", agentA+".json"))
		cfgB := readFileOnRemote(t, filepath.Join(remoteDir, "agents", agentB+".json"))
		portA := extractJSONField(t, cfgA, "gateway_port")
		portB := extractJSONField(t, cfgB, "gateway_port")
		if portA == portB {
			t.Errorf("agents should have unique gateway ports, both got %s", portA)
		}
	})

	t.Run("verify-routing-exists", func(t *testing.T) {
		// Routing.json is generated but empty without channel bindings —
		// verify it exists and is valid JSON.
		routing := readFileOnRemote(t, filepath.Join(remoteDir, "config", "routing.json"))
		if !strings.Contains(routing, "channels") || !strings.Contains(routing, "members") {
			t.Errorf("routing.json should be valid with channels/members keys:\n%s", routing)
		}
	})

	t.Run("verify-network-isolation", func(t *testing.T) {
		netA, _ := exec.Command("docker", "network", "inspect", "conga-"+agentA).Output()
		netB, _ := exec.Command("docker", "network", "inspect", "conga-"+agentB).Output()
		if strings.Contains(string(netA), "conga-"+agentB) {
			t.Error("agent A's network should not contain agent B's container")
		}
		if strings.Contains(string(netB), "conga-"+agentA) {
			t.Error("agent B's network should not contain agent A's container")
		}
	})

	t.Run("refresh-all", func(t *testing.T) {
		mustRunCLI(t, append(base, "admin", "refresh-all", "--force")...)
		assertContainerRunning(t, agentA)
		assertContainerRunning(t, agentB)
	})

	t.Run("remove-alpha", func(t *testing.T) {
		mustRunCLI(t, append(base, "admin", "remove-agent", agentA, "--force", "--delete-secrets")...)
		assertContainerNotExists(t, agentA)
	})

	t.Run("verify-beta-survives", func(t *testing.T) {
		assertContainerRunning(t, agentB)
	})

	t.Run("verify-alpha-network-gone", func(t *testing.T) {
		err := exec.Command("docker", "network", "inspect", "conga-"+agentA).Run()
		if err == nil {
			t.Error("agent A's Docker network should have been removed")
		}
	})

	t.Run("teardown", func(t *testing.T) {
		mustRunCLI(t, append(base, "admin", "teardown", "--force")...)
	})
}

// TestRemoteChannelManagement exercises all 5 channel Provider methods
// (AddChannel, RemoveChannel, ListChannels, BindChannel, UnbindChannel)
// through the remote provider's SSH paths with dummy Slack credentials.
func TestRemoteChannelManagement(t *testing.T) {
	dataDir, agentName, sshPort, keyPath, remoteDir := setupRemoteTestEnv(t)
	base := remoteBaseArgs(dataDir)
	root := repoRoot(t)

	t.Cleanup(func() { cleanupRouter() })

	t.Run("setup", func(t *testing.T) {
		cfg := fmt.Sprintf(
			`{"ssh_host":"127.0.0.1","ssh_port":%d,"ssh_user":"root","ssh_key_path":%q,"image":%q,"repo_path":%q,"remote_dir":%q}`,
			sshPort, keyPath, testImage, root, remoteDir)
		mustRunCLI(t, append(base, "admin", "setup", "--json", cfg)...)
	})

	t.Run("add-user", func(t *testing.T) {
		mustRunCLI(t, append(base, "admin", "add-user", agentName)...)
		assertContainerRunning(t, agentName)
	})

	t.Run("channels-add-slack", func(t *testing.T) {
		cfg := `{"slack-bot-token":"xoxb-fake-000","slack-signing-secret":"fakesigningsecret","slack-app-token":"xapp-fake-000"}`
		mustRunCLI(t, append(base, "channels", "add", "slack", "--json", cfg)...)
	})

	t.Run("channels-list", func(t *testing.T) {
		out := mustRunCLI(t, append(base, "channels", "list", "--output", "json")...)
		if !strings.Contains(out, "slack") {
			t.Errorf("channels list should contain slack:\n%s", out)
		}
	})

	t.Run("verify-router-started", func(t *testing.T) {
		assertRouterRunning(t)
	})

	t.Run("channels-bind", func(t *testing.T) {
		mustRunCLI(t, append(base, "channels", "bind", agentName, "slack:U00FAKEUSER")...)
	})

	t.Run("verify-openclaw-config", func(t *testing.T) {
		assertFileContent(t, agentName, "/home/node/.openclaw/openclaw.json", "signingSecret")
	})

	t.Run("verify-routing-entry", func(t *testing.T) {
		routing := readFileOnRemote(t, filepath.Join(remoteDir, "config", "routing.json"))
		if !strings.Contains(routing, "U00FAKEUSER") {
			t.Errorf("routing.json should contain member ID U00FAKEUSER:\n%s", routing)
		}
		if !strings.Contains(routing, "conga-"+agentName) {
			t.Errorf("routing.json should route to agent container:\n%s", routing)
		}
	})

	t.Run("channels-unbind", func(t *testing.T) {
		mustRunCLI(t, append(base, "channels", "unbind", agentName, "slack", "--force")...)
	})

	t.Run("verify-routing-cleared", func(t *testing.T) {
		routing := readFileOnRemote(t, filepath.Join(remoteDir, "config", "routing.json"))
		if strings.Contains(routing, "U00FAKEUSER") {
			t.Errorf("routing.json should no longer contain U00FAKEUSER:\n%s", routing)
		}
	})

	t.Run("channels-remove", func(t *testing.T) {
		mustRunCLI(t, append(base, "channels", "remove", "slack", "--force")...)
	})

	t.Run("channels-list-empty", func(t *testing.T) {
		out := mustRunCLI(t, append(base, "channels", "list", "--output", "json")...)
		if strings.Contains(out, `"configured":true`) {
			t.Errorf("channels list should show no configured channels:\n%s", out)
		}
	})

	t.Run("verify-router-stopped", func(t *testing.T) {
		assertRouterNotExists(t)
	})

	t.Run("teardown", func(t *testing.T) {
		mustRunCLI(t, append(base, "admin", "teardown", "--force")...)
	})
}

// TestRemoteConnect verifies that Connect() opens an SSH tunnel and the
// gateway responds with HTTP 200 on the forwarded local port.
//
// LIMITATION: In the test setup, the SSH container and Docker host are separate
// entities. The SSH tunnel forwards local → SSH-container:port, but the agent's
// gateway port (18789) is mapped to the HOST's localhost, not the SSH container's
// localhost. In a real deployment the SSH host IS the Docker host, so this works.
// To test end-to-end, we'd need to either:
//   - Run Docker-in-Docker inside the SSH container, or
//   - Forward to the container's IP on the Docker network instead of localhost
//
// For now, we test that Connect() returns valid ConnectInfo (URL, port, token)
// without verifying HTTP through the tunnel.
func TestRemoteConnect(t *testing.T) {
	dataDir, agentName, sshPort, keyPath, remoteDir := setupRemoteTestEnv(t)
	base := remoteBaseArgs(dataDir)
	root := repoRoot(t)

	t.Run("setup", func(t *testing.T) {
		cfg := fmt.Sprintf(
			`{"ssh_host":"127.0.0.1","ssh_port":%d,"ssh_user":"root","ssh_key_path":%q,"image":%q,"repo_path":%q,"remote_dir":%q}`,
			sshPort, keyPath, testImage, root, remoteDir)
		mustRunCLI(t, append(base, "admin", "setup", "--json", cfg)...)
	})

	t.Run("add-user", func(t *testing.T) {
		mustRunCLI(t, append(base, "admin", "add-user", agentName)...)
		assertContainerRunning(t, agentName)
	})

	t.Run("connect-returns-info", func(t *testing.T) {
		// Initialize the provider by running a status command
		mustRunCLI(t, append(base, "status", "--agent", agentName, "--output", "json")...)

		ctx, cancel := context.WithCancel(context.Background())

		freePort := findFreePort(t)
		info, err := prov.Connect(ctx, agentName, freePort)
		cancel() // Close the tunnel immediately — we just test the setup
		if err != nil {
			t.Fatalf("Connect failed: %v", err)
		}

		if info.LocalPort != freePort {
			t.Errorf("expected local port %d, got %d", freePort, info.LocalPort)
		}
		if !strings.HasPrefix(info.URL, fmt.Sprintf("http://localhost:%d", freePort)) {
			t.Errorf("URL should start with http://localhost:%d, got %s", freePort, info.URL)
		}
		t.Logf("Connect returned URL=%s Port=%d Token=%q", info.URL, info.LocalPort, info.Token)
	})

	t.Run("teardown", func(t *testing.T) {
		mustRunCLI(t, append(base, "admin", "teardown", "--force")...)
	})
}
