# Plan: Remote Integration Test Coverage Expansion

## Approach

Add 4 new test functions to `internal/cmd/integration_remote_test.go`,
extending the existing SSH container infrastructure. Each test is
self-contained with its own setup/teardown cycle. New helpers are added to
`integration_helpers_test.go` where reusable across tests.

The tests are ordered by implementation complexity and dependency:
error paths first (simplest, no new infra), then multi-agent (new helper
for 2-agent setup), then channels (needs dummy secrets + router assertions),
then connect (needs HTTP client through tunnel).

## Test Function Designs

### 1. `TestRemoteErrorPaths`

**Purpose**: Verify that the CLI returns meaningful errors for invalid operations.

**Subtests**:

| # | Subtest | Action | Expected |
|---|---------|--------|----------|
| 1 | setup | Normal remote setup | exit 0 |
| 2 | add-user | Provision one agent | container running |
| 3 | add-duplicate-agent | `admin add-user <same-name>` | non-zero exit, error mentions "already exists" or "duplicate" |
| 4 | remove-nonexistent | `admin remove-agent nonexistent --force` | non-zero exit, error mentions agent name |
| 5 | secrets-set-nonexistent | `secrets set k --value v --agent nonexistent` | non-zero exit |
| 6 | teardown | Clean up | exit 0 |

**Pre-setup error** (separate test or subtest):

| # | Subtest | Action | Expected |
|---|---------|--------|----------|
| 7 | add-before-setup | `admin add-user foo` without prior `admin setup` | non-zero exit, error mentions setup/config |

**Notes**:
- The "add-before-setup" test needs a fresh data dir (no `remote-config.json`).
  Use a second `setupRemoteTestEnv` call or a manually constructed data dir.
- Error message assertions use `strings.Contains` on stderr + error string,
  case-insensitive where appropriate.

**New helpers**: None required — uses existing `runCLI` (not `mustRunCLI`)
to capture errors.

### 2. `TestRemoteMultiAgent`

**Purpose**: Provision 2 agents and verify port allocation, routing, network
isolation, RefreshAll, and independent lifecycle management.

**Subtests**:

| # | Subtest | Action | Expected |
|---|---------|--------|----------|
| 1 | setup | Normal remote setup | exit 0 |
| 2 | add-user-alpha | `admin add-user <agent-a>` | container running |
| 3 | add-team-beta | `admin add-team <agent-b>` | container running |
| 4 | list-agents | `admin list-agents --output json` | both agents in output |
| 5 | verify-unique-ports | Read agent JSON files, compare GatewayPort | ports differ |
| 6 | verify-routing-json | Read routing.json from remote data dir | both agents have entries |
| 7 | verify-network-isolation | `docker network inspect conga-<a>` and `conga-<b>` | each agent on its own network |
| 8 | refresh-all | `admin refresh-all` | both containers running |
| 9 | remove-alpha | `admin remove-agent <agent-a> --force` | alpha gone, beta still running |
| 10 | verify-beta-survives | `status --agent <agent-b>` | still running |
| 11 | verify-routing-updated | Read routing.json | only beta remains |
| 12 | teardown | Clean up | exit 0 |

**New helpers**:
- `setupRemoteMultiAgentEnv(t)` — like `setupRemoteTestEnv` but returns 2
  agent names (`rtest-<hash>-a`, `rtest-<hash>-b`). Cleanup tears down both.
- `readRemoteFile(t, sshPort, keyPath, remotePath)` — reads a file from the
  SSH container via `docker exec` into the SSH container. Used to inspect
  routing.json and agent JSON on the "remote host".
  Alternative: use `docker exec conga-test-sshd cat <path>` directly.

**Design decisions**:
- One user agent + one team agent to test both routing entry types
  (members vs channels in routing.json).
- Port verification reads from the agent JSON files on the remote host
  (`/opt/conga/agents/<name>.json`) via `docker exec` into the SSH container.
- Network isolation verified by inspecting Docker network membership —
  each agent container should be on `conga-<name>` but NOT on the other's network.

### 3. `TestRemoteChannelManagement`

**Purpose**: Exercise all 5 channel Provider methods (AddChannel, RemoveChannel,
ListChannels, BindChannel, UnbindChannel) through SSH paths.

**Subtests**:

| # | Subtest | Action | Expected |
|---|---------|--------|----------|
| 1 | setup | Normal remote setup | exit 0 |
| 2 | add-user | Provision one agent | container running |
| 3 | channels-add-slack | `channels add slack --json '{"bot_token":"xoxb-fake","signing_secret":"fakesecret","app_token":"xapp-fake"}'` | exit 0 |
| 4 | channels-list | `channels list --output json` | slack listed, "configured" |
| 5 | channels-bind | `channels bind <agent> --channel slack:U00FAKE` | exit 0 |
| 6 | verify-openclaw-config | Read openclaw.json inside container | contains `channels.slack` section |
| 7 | verify-routing-entry | Read routing.json on remote | contains member ID mapping |
| 8 | channels-unbind | `channels unbind <agent> --platform slack` | exit 0 |
| 9 | verify-routing-cleared | Read routing.json on remote | member mapping removed |
| 10 | channels-remove | `channels remove slack --force` | exit 0 |
| 11 | channels-list-empty | `channels list --output json` | no channels |
| 12 | teardown | Clean up | exit 0 |

**Notes**:
- Slack secrets are dummy values — the router will start but fail to connect
  to Slack. This is expected; we're testing config generation and lifecycle,
  not Slack connectivity.
- The `app_token` is the `SLACK_APP_TOKEN` for the router. The `bot_token`
  and `signing_secret` go into agent configs.
- Router container (`conga-router`) lifecycle: started by `channels add`,
  stopped by `channels remove`. Verify with `docker inspect`.
- After `channels add`, verify router container exists. After `channels remove`,
  verify router container is gone.
- `assertFileContent` can verify openclaw.json inside the agent container.
- routing.json lives on the remote host at `<remoteDir>/routing.json` —
  read via `docker exec conga-test-sshd cat <path>`.

**New helpers**:
- `assertRouterRunning(t)` / `assertRouterNotExists(t)` — check the
  `conga-router` container state.
- `readFileOnRemote(t, path)` — `docker exec conga-test-sshd cat <path>`.
  Reusable by multi-agent test too.
- Cleanup must also remove `conga-router` container if channel tests fail
  mid-run.

### 4. `TestRemoteConnect`

**Purpose**: Verify that `Connect()` opens an SSH tunnel and the gateway
responds through it.

**Subtests**:

| # | Subtest | Action | Expected |
|---|---------|--------|----------|
| 1 | setup | Normal remote setup | exit 0 |
| 2 | add-user | Provision one agent | container running |
| 3 | wait-for-gateway | Poll gateway health inside container | HTTP 200 on :18789 |
| 4 | connect | Call Connect via CLI or provider directly | returns local URL |
| 5 | verify-tunnel | HTTP GET to local forwarded port | HTTP 200 |
| 6 | teardown | Clean up | exit 0 |

**Design challenge**: The `conga connect` CLI command blocks (it opens a
tunnel and waits). Options:

**Option A — Run connect in a goroutine**: Start `conga connect` in a
background goroutine, parse its output for the local URL, make HTTP request,
then cancel.

**Option B — Use the provider directly**: Call `prov.Connect(ctx, agentName, 0)`
which returns `ConnectInfo{URL, Tunnel}`. Make HTTP request to URL. Close tunnel.
This requires instantiating the remote provider in the test, which bypasses
the CLI but tests the actual SSH tunnel code.

**Option C — Use `go test` with a timeout subtest**: Run `connect` as a
subprocess with a timeout, capture its output, verify the URL format.

**Recommendation**: Option A (goroutine) is most consistent with existing
test patterns that use `runCLI`. The connect command outputs the URL before
blocking, so we can capture it from stdout.

**New helpers**:
- `waitForGateway(t, agentName)` — polls `docker exec conga-<name> wget -q -O-
  http://localhost:18789/health` until it returns 200 (OpenClaw's gateway
  needs a few seconds to start).
- `connectInBackground(t, base, agentName)` — starts `conga connect` in a
  goroutine, returns the URL and a cancel function.

## Helper Extensions Summary

New helpers in `integration_helpers_test.go`:

| Helper | Used by | Description |
|--------|---------|-------------|
| `readFileOnRemote(t, path)` | multi-agent, channels | `docker exec conga-test-sshd cat <path>` |
| `assertRouterRunning(t)` | channels | verify `conga-router` is running |
| `assertRouterNotExists(t)` | channels | verify `conga-router` doesn't exist |
| `waitForGateway(t, agentName)` | connect | poll gateway health |
| `connectInBackground(t, base, agentName)` | connect | run connect in goroutine |

## Cleanup Strategy

Each test registers cleanup in LIFO order:
1. Stop SSH container (registered first, runs last)
2. Force-remove router container (channels test only)
3. Force-remove agent containers + networks
4. `admin teardown --force`

Multi-agent test cleanup removes both agent containers. Channel test
cleanup also removes `conga-router`.

## Implementation Order

| Phase | Test | Est. subtests | New helpers | Risk |
|-------|------|--------------|-------------|------|
| 1 | `TestRemoteErrorPaths` | 7 | 0 | Low — pure negative cases |
| 2 | `TestRemoteMultiAgent` | 12 | 2 | Medium — port/routing verification |
| 3 | `TestRemoteChannelManagement` | 12 | 3 | Medium — router lifecycle, dummy secrets |
| 4 | `TestRemoteConnect` | 6 | 2 | High — background goroutine, tunnel timing |

Total: ~37 new subtests, ~7 new helpers.

## Risks

| Risk | Mitigation |
|------|------------|
| Multi-agent test is slow (2 containers + egress proxies) | Skip egress proxy for multi-agent test (no policy file = no proxy) |
| Router fails immediately with dummy Slack tokens | Expected — we test config/lifecycle, not Slack connectivity. Assert router _started_, not _healthy_. |
| Connect tunnel is racy (goroutine + HTTP) | Use retries with short timeout. Gateway health check before tunnel. |
| Channel CLI flags may differ from expected | Verify exact CLI syntax against `conga channels --help` before implementing |
| SSH container reuse across tests | Each test gets its own SSH container via `setupRemoteTestEnv`. No cross-test state. |
| CI time increase | ~30s per new test (4 tests = ~2 min extra). Total CI: ~5 min. Acceptable. |

## Provider Method Coverage After Implementation

| Method | Before | After |
|--------|--------|-------|
| RefreshAll | untested | multi-agent |
| AddChannel | untested | channels |
| RemoveChannel | untested | channels |
| ListChannels | untested | channels |
| BindChannel | untested | channels |
| UnbindChannel | untested | channels |
| Connect | untested | connect |
| CycleHost | untested | still untested (deferred — low risk, covered by RefreshAll) |
| WhoAmI | untested | still untested (identity only meaningful with real credentials) |
| ResolveAgentByIdentity | untested | still untested (same as WhoAmI) |

**Coverage improvement**: 16/26 → 23/26 methods tested (88%).
