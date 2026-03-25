# Fix Plan: SSH Auto-Reconnect

## Approach

Add transparent reconnect-on-failure to `SSHClient`. When a session or SFTP handshake fails, close the dead connection, re-dial using stored parameters, and retry once. One retry is sufficient — if the server is truly unreachable, the second attempt fails fast with a clear error.

This is a **real fix**, not a band-aid. The root cause is that `SSHClient` has no way to recover from a dead connection. Storing the connection parameters and adding a single-retry path at the session layer gives it that ability without changing any caller code.

## Changes

### 1. `ssh.go` — Store connection config on `SSHClient`

> **Deviation from original plan**: Stores `*ssh.ClientConfig` instead of `keyPath`. This avoids re-resolving auth methods on reconnect, makes the reconnect path testable without real SSH keys, and keeps reconnect fast.

Add `config` field to the struct so reconnection can re-dial:

```go
type SSHClient struct {
    client *ssh.Client
    config *ssh.ClientConfig
    host   string
    port   int
    user   string
}
```

Update `SSHConnect` to populate `config` on the returned struct.

### 2. `ssh.go` — Add `reconnect()` method

```go
func (c *SSHClient) reconnect() error {
    c.client.Close()
    addr := fmt.Sprintf("%s:%d", c.host, c.port)
    client, err := ssh.Dial("tcp", addr, c.config)
    if err != nil {
        return fmt.Errorf("ssh reconnect failed: %w", err)
    }
    c.client = client
    go sshKeepalive(client)
    return nil
}
```

Re-dials directly with stored config. Starts a new keepalive goroutine for the fresh connection.

### 3. `ssh.go` — Add `session()` method with retry

```go
func (c *SSHClient) session() (*ssh.Session, error) {
    session, err := c.client.NewSession()
    if err == nil {
        return session, nil
    }
    // Connection likely dead — try reconnecting once
    if reconnErr := c.reconnect(); reconnErr != nil {
        return nil, fmt.Errorf("ssh session failed (reconnect also failed: %v): %w", reconnErr, err)
    }
    return c.client.NewSession()
}
```

### 4. `ssh.go` — Add `sftpClient()` method with retry

Same pattern for SFTP, since `Upload`, `Download`, and `UploadDir` each create SFTP clients from `c.client`:

```go
func (c *SSHClient) sftpClient() (*sftp.Client, error) {
    sc, err := sftp.NewClient(c.client)
    if err == nil {
        return sc, nil
    }
    if reconnErr := c.reconnect(); reconnErr != nil {
        return nil, fmt.Errorf("sftp failed (reconnect also failed: %v): %w", reconnErr, err)
    }
    return sftp.NewClient(c.client)
}
```

### 5. `ssh.go` — Replace all direct `c.client.NewSession()` calls

| Location | Current | After |
|---|---|---|
| `RunWithStderr` (line 147) | `c.client.NewSession()` | `c.session()` |
| `uploadViaShell` (line 220) | `c.client.NewSession()` | `c.session()` |
| `Upload` (line 176) | `sftp.NewClient(c.client)` | `c.sftpClient()` |
| `Download` (line 238) | `sftp.NewClient(c.client)` | `c.sftpClient()` |
| `UploadDir` (line 260) | `sftp.NewClient(c.client)` | `c.sftpClient()` |
| `ForwardPort` (line 344) | `c.client.Dial(...)` | No change (tunnel lifecycle is different — see below) |

### 6. `integrity.go` — Replace direct `p.ssh.client.NewSession()` (line 75)

Change to use the wrapper:

```go
session, err := p.ssh.session()
```

This is the only place outside `ssh.go` that reaches into the raw `*ssh.Client`.

### 7. `ForwardPort` — No change (intentional)

`ForwardPort` uses `c.client.Dial()` inside a long-lived goroutine loop. Reconnecting the SSH client mid-tunnel would invalidate the listener's connection state. Tunnels are short-lived (user connects, uses gateway, disconnects) and already handle `Dial` errors by closing the local conn and continuing. If the SSH connection dies during a tunnel, the user will re-run the connect command, which creates a fresh tunnel on the now-reconnected client.

## Risks

| Risk | Mitigation |
|---|---|
| Reconnect during concurrent operations | Acceptable: MCP tool calls are sequential (one tool at a time). No concurrent session creation. |
| Reconnect replaces `client` pointer mid-keepalive | Keepalive goroutine on the old client will error and exit. New client gets its own keepalive via `SSHConnect`. |
| Double-close of old client | `reconnect()` calls `Close()` once on the old client. Subsequent `Close()` from a dead keepalive goroutine is a no-op on an already-closed `*ssh.Client`. |
| Infinite retry loops | Impossible — exactly one retry. If reconnect fails, the original error propagates. |
| Auth parameters change between connect and reconnect | N/A — key file is read fresh on each `SSHConnect` call, so rotated keys are picked up. |

## Test Plan

Tests use an in-process SSH server (`golang.org/x/crypto/ssh` server API) on loopback. No external dependencies, no real network — fast and deterministic. Added to `ssh_test.go` alongside the existing shell-quoting tests.

### Test helper: `testSSHServer`

A reusable helper that starts a minimal SSH server on a random port. Accepts connections, handles "exec" requests by running a callback (or returning canned output). Returns the listener and a stop function. Uses an in-memory host key generated per test.

```go
func testSSHServer(t *testing.T, handler func(cmd string) (string, int)) (port int, stop func())
```

### Test cases

#### `TestSessionReconnectsOnStaleConnection`

1. Start test SSH server on random port, handler returns "pong" for any command
2. `SSHConnect` to it
3. `Run(ctx, "ping")` — assert returns "pong"
4. Call `stop()` to kill the server (simulates connection death)
5. Start a **new** test SSH server on the **same port**, handler returns "pong2"
6. `Run(ctx, "ping")` — first `NewSession()` fails on the dead connection, `reconnect()` dials the new server, retry succeeds
7. Assert returns "pong2"

#### `TestSessionFailsWhenServerTrulyDown`

1. Start test SSH server, connect
2. `Run(ctx, "ping")` — succeeds
3. `stop()` the server, do NOT restart
4. `Run(ctx, "ping")` — assert error contains both "ssh session failed" and "reconnect also failed"
5. Assert only one reconnect attempt (no infinite retries)

#### `TestReconnectPreservesParameters`

1. Start test SSH server
2. `SSHConnect` with explicit host, port, user, keyPath
3. `stop()` and restart on same port
4. `Run` a command — reconnect succeeds
5. Assert `c.host`, `c.port`, `c.user`, `c.keyPath` are unchanged after reconnect

#### `TestSftpReconnectsOnStaleConnection`

1. Start test SSH server with SFTP subsystem support
2. `SSHConnect`, then `Upload` a file — succeeds
3. `stop()` and restart on same port
4. `Upload` again — SFTP handshake fails, reconnect, retry succeeds
5. Verify file content on the "remote" side

If the in-process SFTP subsystem is too heavy for a unit test, this case can alternatively be tested by verifying that `sftpClient()` calls `reconnect()` when `sftp.NewClient` fails — using the same server-kill pattern and asserting the reconnect happened (e.g., the second `sftp.NewClient` call succeeds).

#### `TestRunSucceedsWithoutReconnect` (regression guard)

1. Start test SSH server, connect
2. `Run` 5 commands in sequence — all succeed
3. Assert no reconnect was triggered (connection stayed healthy)

Verifies the retry path doesn't interfere with the happy path.

## Not in scope

- **Connection pooling** — overkill for sequential MCP tool calls
- **Background health checks** — the keepalive already serves this role for NAT prevention; reconnect-on-use handles the failure case
- **Mutex/sync** — MCP tool calls are sequential; no concurrent access to `SSHClient`
- **SFTP client caching** — separate concern (existing TODO in code), not related to this bug
