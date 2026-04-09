# Specification: Remote Integration Test Coverage Expansion

## 1. Overview

Add 4 new test functions (~37 subtests) to `internal/cmd/integration_remote_test.go`
and ~7 new helpers to `integration_helpers_test.go`. These tests close the top
coverage gaps in the remote provider integration suite: error paths, multi-agent
scenarios, channel management, and SSH tunnel connectivity.

All tests reuse the existing SSH container infrastructure (Alpine + openssh +
docker-cli with host Docker socket mounted). Each test function is self-contained
with its own setup/teardown cycle.

## 2. Test Infrastructure Extensions

### 2.1 New helpers in `integration_helpers_test.go`

```go
// readFileOnRemote reads a file from inside the SSH container (the "remote host").
// Used to inspect routing.json, agent JSON, and other files on the remote.
func readFileOnRemote(t *testing.T, remotePath string) string
```

Implementation: `docker exec conga-test-sshd cat <remotePath>`.
Returns file content. Fatals if the file doesn't exist.

```go
// assertRouterRunning asserts the conga-router container is running.
func assertRouterRunning(t *testing.T)
```

Implementation: `docker inspect -f {{.State.Running}} conga-router`.
Retries up to 5 times with 2s sleep (router may take a moment to start).

```go
// assertRouterNotExists asserts no conga-router container exists.
func assertRouterNotExists(t *testing.T)
```

Implementation: `docker inspect conga-router` returns error.

```go
// waitForGateway polls the gateway health endpoint inside the container.
// OpenClaw's gateway needs a few seconds to start accepting connections.
func waitForGateway(t *testing.T, agentName string)
```

Implementation: Retries `docker exec conga-<name> wget -q -O- http://localhost:18789`
up to 15 times with 2s sleep. Fatals after 30s.

```go
// connectInBackground starts `conga connect` in a goroutine and returns
// the URL and a cancel function. Parses the URL from JSON output.
func connectInBackground(t *testing.T, base []string, agentName string) (url string, cancel func())
```

Implementation:
1. Creates a context with cancel.
2. Launches `conga connect --agent <name> --output json` in a goroutine via
   `runCLIWithContext` (new variant of `runCLI` that accepts a context).
3. Reads stdout pipe until JSON output appears (contains `"url"`).
4. Parses the URL from JSON.
5. Returns URL and cancel function that cancels context + waits for goroutine.

Note: This is the most complex helper. The connect command writes JSON to
stdout before blocking. We capture that initial output, then let the tunnel
run until cancelled.

```go
// runCLIWithContext is like runCLI but accepts a context for cancellation.
// Used by connectInBackground to cancel the blocking connect command.
func runCLIWithContext(ctx context.Context, t *testing.T, args ...string) (stdout, stderr string, err error)
```

```go
// cleanupRouter force-removes the conga-router container.
func cleanupRouter()
```

Implementation: `docker rm -f conga-router`. Called in t.Cleanup for channel tests.

### 2.2 Multi-agent cleanup extension

The existing `cleanupTestContainers(agentName)` handles one agent. Multi-agent
test calls it twice (once per agent name). No new helper needed.

## 3. Test Functions

### 3.1 `TestRemoteErrorPaths`

**Purpose**: Verify the CLI returns meaningful errors for invalid operations
through the remote provider's SSH code paths.

**Agent name prefix**: `rtest-<hash>` (same as existing tests — error tests
won't conflict because they don't create long-lived containers).

```go
func TestRemoteErrorPaths(t *testing.T) {
    dataDir, agentName, sshPort, keyPath, remoteDir := setupRemoteTestEnv(t)
    base := remoteBaseArgs(dataDir)
    root := repoRoot(t)

    t.Run("setup", func(t *testing.T) { ... })          // normal setup
    t.Run("add-user", func(t *testing.T) { ... })        // provision one agent

    t.Run("add-duplicate-agent", func(t *testing.T) {
        _, stderr, err := runCLI(t, append(base, "admin", "add-user", agentName)...)
        if err == nil {
            t.Fatal("expected error adding duplicate agent")
        }
        combined := stderr + err.Error()
        if !containsAny(combined, "already exists", "duplicate", "already provisioned") {
            t.Errorf("error should mention duplicate/exists, got: %s", combined)
        }
    })

    t.Run("remove-nonexistent", func(t *testing.T) {
        _, stderr, err := runCLI(t, append(base, "admin", "remove-agent", "nonexistent-agent", "--force", "--delete-secrets")...)
        if err == nil {
            t.Fatal("expected error removing non-existent agent")
        }
        combined := stderr + err.Error()
        if !strings.Contains(combined, "nonexistent-agent") {
            t.Errorf("error should mention agent name, got: %s", combined)
        }
    })

    t.Run("secrets-set-nonexistent", func(t *testing.T) {
        _, _, err := runCLI(t, append(base, "secrets", "set", "key", "--value", "val", "--agent", "nonexistent-agent")...)
        if err == nil {
            t.Fatal("expected error setting secret on non-existent agent")
        }
    })

    t.Run("teardown", func(t *testing.T) { ... })
}
```

**Separate subtest for pre-setup error** (within the same test function,
using a fresh data dir):

```go
    t.Run("add-before-setup", func(t *testing.T) {
        freshDir := filepath.Join(t.TempDir(), ".conga")
        freshBase := remoteBaseArgs(freshDir)
        _, stderr, err := runCLI(t, append(freshBase, "admin", "add-user", "should-fail")...)
        if err == nil {
            t.Fatal("expected error when adding agent before setup")
        }
        combined := stderr + err.Error()
        if !containsAny(combined, "setup", "config", "not found", "not configured") {
            t.Errorf("error should mention setup/config, got: %s", combined)
        }
    })
```

**Helper used**:
```go
// containsAny returns true if s contains any of the given substrings (case-insensitive).
func containsAny(s string, subs ...string) bool {
    lower := strings.ToLower(s)
    for _, sub := range subs {
        if strings.Contains(lower, strings.ToLower(sub)) {
            return true
        }
    }
    return false
}
```

**Subtest count**: 6

### 3.2 `TestRemoteMultiAgent`

**Purpose**: Provision 2 agents simultaneously and verify port allocation,
routing.json fan-out, network isolation, RefreshAll, and independent lifecycle.

**Agent names**: `rtest-<hash>-a` (user type), `rtest-<hash>-b` (team type).
Using one of each type tests both routing entry types (members vs channels).

```go
func TestRemoteMultiAgent(t *testing.T) {
    dataDir, _, sshPort, keyPath, remoteDir := setupRemoteTestEnv(t)
    base := remoteBaseArgs(dataDir)
    root := repoRoot(t)
    hash := fmt.Sprintf("%08x", crc32.ChecksumIEEE([]byte(t.Name())))
    agentA := "rtest-" + hash + "-a"
    agentB := "rtest-" + hash + "-b"

    // Register cleanup for both agents
    t.Cleanup(func() { cleanupTestContainers(agentA) })
    t.Cleanup(func() { cleanupTestContainers(agentB) })

    t.Run("setup", func(t *testing.T) { ... })

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
        // Read agent configs from the remote host
        cfgA := readFileOnRemote(t, filepath.Join(remoteDir, "agents", agentA+".json"))
        cfgB := readFileOnRemote(t, filepath.Join(remoteDir, "agents", agentB+".json"))
        portA := extractJSONField(t, cfgA, "gateway_port")
        portB := extractJSONField(t, cfgB, "gateway_port")
        if portA == portB {
            t.Errorf("agents should have unique gateway ports, both got %s", portA)
        }
    })

    t.Run("verify-routing-json", func(t *testing.T) {
        routing := readFileOnRemote(t, filepath.Join(remoteDir, "routing.json"))
        // User agent (agentA) should appear in "members" section
        // Team agent (agentB) should appear in routing.json
        // Both agent container URLs should be present
        if !strings.Contains(routing, "conga-"+agentA) {
            t.Errorf("routing.json should contain agent A URL:\n%s", routing)
        }
        if !strings.Contains(routing, "conga-"+agentB) {
            t.Errorf("routing.json should contain agent B URL:\n%s", routing)
        }
    })

    t.Run("verify-network-isolation", func(t *testing.T) {
        // Each agent should be on its own Docker network
        netA, _ := exec.Command("docker", "network", "inspect", "conga-"+agentA).Output()
        netB, _ := exec.Command("docker", "network", "inspect", "conga-"+agentB).Output()
        // agentA container should NOT be on agentB's network and vice versa
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

    t.Run("verify-routing-updated", func(t *testing.T) {
        routing := readFileOnRemote(t, filepath.Join(remoteDir, "routing.json"))
        if strings.Contains(routing, "conga-"+agentA) {
            t.Errorf("routing.json should no longer contain agent A:\n%s", routing)
        }
        if !strings.Contains(routing, "conga-"+agentB) {
            t.Errorf("routing.json should still contain agent B:\n%s", routing)
        }
    })

    t.Run("teardown", func(t *testing.T) {
        mustRunCLI(t, append(base, "admin", "teardown", "--force")...)
    })
}
```

**Additional helper**:
```go
// extractJSONField extracts a top-level string or number field from JSON.
func extractJSONField(t *testing.T, jsonStr, field string) string
```

Implementation: `json.Unmarshal` into `map[string]any`, return `fmt.Sprint(m[field])`.

**Subtest count**: 12

### 3.3 `TestRemoteChannelManagement`

**Purpose**: Exercise all 5 channel Provider methods through SSH paths with
dummy Slack credentials.

The Slack channel requires 2 secrets (bot_token, signing_secret) with an
optional 3rd (app_token, router-only). We provide all 3 as dummy values.

```go
func TestRemoteChannelManagement(t *testing.T) {
    dataDir, agentName, sshPort, keyPath, remoteDir := setupRemoteTestEnv(t)
    base := remoteBaseArgs(dataDir)
    root := repoRoot(t)

    // Cleanup router container on test exit
    t.Cleanup(func() { cleanupRouter() })

    t.Run("setup", func(t *testing.T) { ... })

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
        if !strings.Contains(out, "configured") {
            t.Errorf("slack should be configured:\n%s", out)
        }
    })

    t.Run("verify-router-started", func(t *testing.T) {
        assertRouterRunning(t)
    })

    t.Run("channels-bind", func(t *testing.T) {
        mustRunCLI(t, append(base, "channels", "bind", agentName, "slack:U00FAKEUSER")...)
    })

    t.Run("verify-openclaw-config", func(t *testing.T) {
        // Check that openclaw.json inside the container has the channels.slack section
        assertFileContent(t, agentName, "/home/node/.openclaw/openclaw.json", "signingSecret")
    })

    t.Run("verify-routing-entry", func(t *testing.T) {
        routing := readFileOnRemote(t, filepath.Join(remoteDir, "routing.json"))
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
        routing := readFileOnRemote(t, filepath.Join(remoteDir, "routing.json"))
        if strings.Contains(routing, "U00FAKEUSER") {
            t.Errorf("routing.json should no longer contain U00FAKEUSER:\n%s", routing)
        }
    })

    t.Run("channels-remove", func(t *testing.T) {
        mustRunCLI(t, append(base, "channels", "remove", "slack", "--force")...)
    })

    t.Run("channels-list-empty", func(t *testing.T) {
        out := mustRunCLI(t, append(base, "channels", "list", "--output", "json")...)
        if strings.Contains(out, "slack") && strings.Contains(out, "configured") {
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
```

**Key behaviors verified**:
- `channels add slack` with `--json` input stores dummy secrets on remote,
  starts router container
- `channels list` reports slack as configured with router running
- `channels bind` adds member ID to routing.json, updates openclaw.json
  inside agent container (requires agent refresh — the bind command does
  this automatically for non-paused agents)
- `channels unbind` removes the routing entry
- `channels remove` stops the router, cleans up secrets

**Subtest count**: 13

### 3.4 `TestRemoteConnect`

**Purpose**: Verify SSH tunnel connectivity to the gateway web UI.

```go
func TestRemoteConnect(t *testing.T) {
    dataDir, agentName, sshPort, keyPath, remoteDir := setupRemoteTestEnv(t)
    base := remoteBaseArgs(dataDir)
    root := repoRoot(t)

    t.Run("setup", func(t *testing.T) { ... })

    t.Run("add-user", func(t *testing.T) {
        mustRunCLI(t, append(base, "admin", "add-user", agentName)...)
        assertContainerRunning(t, agentName)
    })

    t.Run("wait-for-gateway", func(t *testing.T) {
        waitForGateway(t, agentName)
    })

    t.Run("connect-and-verify", func(t *testing.T) {
        url, cancel := connectInBackground(t, base, agentName)
        defer cancel()

        // Give the tunnel a moment to establish
        time.Sleep(1 * time.Second)

        // Make HTTP request to the tunneled URL
        // Strip the hash fragment (token) — we just need the host:port
        resp, err := http.Get(strings.SplitN(url, "#", 2)[0])
        if err != nil {
            t.Fatalf("HTTP GET to tunnel URL %s failed: %v", url, err)
        }
        resp.Body.Close()

        // Gateway returns 200 for the web UI page
        if resp.StatusCode != http.StatusOK {
            t.Errorf("expected HTTP 200, got %d", resp.StatusCode)
        }
    })

    t.Run("teardown", func(t *testing.T) {
        mustRunCLI(t, append(base, "admin", "teardown", "--force")...)
    })
}
```

**Design notes**:

The `connectInBackground` helper is the critical piece. Implementation approach:

1. Start a goroutine that calls `prov.Connect(ctx, agentName, 0)` directly
   (bypassing the CLI, which blocks and captures stdout). This is cleaner
   than trying to capture partial stdout from a blocking CLI command.
2. The goroutine writes `ConnectInfo` to a channel.
3. The caller reads the URL, makes the HTTP request, then cancels the context.
4. On cancel, the SSH tunnel closes and the goroutine exits.

Actually, using the provider directly is simpler and still tests the SSH
tunnel code path (which is what we care about):

```go
func connectInBackground(t *testing.T, base []string, agentName string) (url string, cancel func()) {
    t.Helper()

    // We need the provider to be initialized. Run a no-op command to
    // trigger provider initialization, then call Connect directly.
    // The provider is stored in the package-level `prov` variable.
    mustRunCLI(t, append(base, "status", "--agent", agentName, "--output", "json")...)

    ctx, cancelFunc := context.WithCancel(context.Background())
    info, err := prov.Connect(ctx, agentName, 0)
    if err != nil {
        cancelFunc()
        t.Fatalf("Connect failed: %v", err)
    }

    return info.URL, func() {
        cancelFunc()
        // Wait briefly for tunnel goroutine to clean up
        time.Sleep(500 * time.Millisecond)
    }
}
```

Wait — `prov.Connect` for the remote provider opens an SSH tunnel that blocks
via `info.Waiter`. The `Connect` call itself returns immediately with the
`ConnectInfo`. So we don't need a goroutine — the tunnel runs in the background
via the SSH connection and we just need to cancel the context to close it.

**Revised approach**: Call `prov.Connect()` directly (it returns immediately).
Make HTTP request to `info.URL`. Cancel context to close tunnel.

**Subtest count**: 4

## 4. Edge Cases

### 4.1 Error Paths

| Scenario | Handling |
|----------|----------|
| Duplicate agent with different type | Same error — name collision regardless of type |
| Empty agent name | Caught by CLI arg validation before reaching provider |
| Remove agent while container is running | Provider stops container first, then removes |
| Secret set with empty value | CLI rejects empty `--value` before reaching provider |

### 4.2 Multi-Agent

| Scenario | Handling |
|----------|----------|
| Both agents get port 18789 | Bug — test catches this via port uniqueness assertion |
| Routing.json malformed after agent removal | Test reads and parses routing.json, catches missing entries |
| RefreshAll fails partway | Both agents checked individually after RefreshAll |
| Docker network name collision | Agent names include hash, collision astronomically unlikely |

### 4.3 Channels

| Scenario | Handling |
|----------|----------|
| Router fails to start (bad image) | Router uses the same node image; should start even without valid tokens |
| Bind to non-existent agent | Would be an error path — but not tested here (covered by error paths test) |
| Unbind when not bound | Provider handles gracefully (no-op or clear error) |
| Remove channel with bound agents | `channels remove` automatically unbinds all agents first |

### 4.4 Connect

| Scenario | Handling |
|----------|----------|
| Gateway not ready | `waitForGateway` polls for 30s before connect attempt |
| Tunnel port conflict | `Connect` with `localPort=0` uses OS-assigned port (no conflict) |
| SSH connection drops | Test is short-lived; SSH container is local and stable |
| Token in URL fragment | URL is `http://localhost:<port>#<token>` — strip fragment for HTTP GET |

## 5. Cleanup Strategy

Each test registers cleanup in LIFO order:

**Error paths**: Same as existing tests (teardown → containers → SSH).

**Multi-agent**: Additional cleanup for second agent:
1. Register `stopSSHContainer` (runs last)
2. Register `cleanupTestContainers(agentB)` (runs third)
3. Register `cleanupTestContainers(agentA)` (runs second)
4. Register `admin teardown` (runs first)

**Channels**: Additional cleanup for router:
1. Register `stopSSHContainer` (runs last)
2. Register `cleanupRouter()` (runs third)
3. Register `cleanupTestContainers(agentName)` (runs second)
4. Register `admin teardown` (runs first)

**Connect**: Same as existing tests. Tunnel is closed by context cancellation
in the test itself, not in cleanup.

## 6. File Manifest

| File | Action | Description |
|------|--------|-------------|
| `internal/cmd/integration_remote_test.go` | Modify | Add 4 new test functions |
| `internal/cmd/integration_helpers_test.go` | Modify | Add ~7 new helpers |

No new files needed — all additions go into existing test files.

## 7. CI Impact

- No workflow changes needed
- Estimated time increase: ~30s per new test function = ~2 min additional
- Total CI time: ~5 min (up from ~3 min)
- SSH image build is cached after first run (no additional cost)

## 8. Provider Method Coverage Summary

| Status | Methods |
|--------|---------|
| Already tested (16) | Name, ListAgents, GetAgent, ProvisionAgent, RemoveAgent, PauseAgent, UnpauseAgent, GetStatus, GetLogs, RefreshAgent, ContainerExec, SetSecret, ListSecrets, DeleteSecret, Setup, Teardown |
| New coverage (7) | RefreshAll, AddChannel, RemoveChannel, ListChannels, BindChannel, UnbindChannel, Connect |
| Remaining untested (3) | WhoAmI, ResolveAgentByIdentity, CycleHost |

**Final coverage: 23/26 methods (88%)**
