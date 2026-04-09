# Remote Integration Test Coverage Expansion

Extend the remote provider integration tests (Feature #25) with four new
test functions covering the top coverage gaps: multi-agent scenarios,
channel management, error paths, and SSH tunnel connectivity.

## Session log
- 2026-04-08: Feature planning started via `/glados/plan-feature`.
  Prior art: remote provider integration tests (specs/2026-04-07_feature_remote-provider-integration-tests/).
  Coverage gap analysis identified 10 untested Provider interface methods and
  several missing scenario categories.
- 2026-04-08: Requirements and plan written. 4 test functions planned
  (~37 subtests, ~7 new helpers). Personas: QA + Architect.
  Key decisions:
  - Error paths first (simplest), then multi-agent, channels, connect
  - Channel tests use dummy Slack secrets (test config gen, not connectivity)
  - Connect test uses background goroutine approach for tunnel verification
  - Coverage goes from 16/26 to 23/26 Provider methods (88%)
- 2026-04-08: Spec written. Persona review:
  - **QA**: Approve. Recommends adding secret file permission check (0400) in
    channel test — non-blocking.
  - **Architect**: Approve. Connect approach (direct `prov.Connect()` call)
    avoids stdout capture race with CLI mutex. No new files needed.
  Standards gate: 0 violations, 0 warnings across 7 checks (security,
  architecture, egress). Proceed to implementation.
- 2026-04-08: Implementation complete. 4 new test functions, 8 new helpers.
  All 7 remote test functions pass (86s total). go vet + gofmt clean.
  Files modified:
  - `internal/cmd/integration_remote_test.go` — 4 new test functions
  - `internal/cmd/integration_helpers_test.go` — 8 new helpers
  Adjustments from spec:
  - Error paths: removed duplicate-agent and secrets-set-nonexistent tests
    (provider is idempotent by design). Added refresh-nonexistent,
    pause-nonexistent, bind-channel-no-platform instead.
  - Multi-agent: routing.json is empty without channel bindings (by design).
    Changed to verify routing.json structure exists. Added verify-alpha-network-gone.
  - Connect: SSH tunnel can't reach gateway in test setup (SSH container !=
    Docker host). Test verifies ConnectInfo returned correctly instead of
    HTTP through tunnel. Documented limitation.
- 2026-04-08: Verification complete.
  - Automated: all 22 test packages pass, go vet clean, gofmt clean.
    All 7 remote integration tests pass (86s).
  - Persona review: QA + Architect approve implementation.
  - Standards gate (post-impl): 7/7 checks pass, 0 violations.
  - Spec retrospection: 3 divergences documented (error paths, routing, connect).
  - Test sync: no stale references, no fakes, remote tests ahead of local sibling.

## Active Capabilities
- Bash (go build, go test, docker)

## Active Personas
- QA (test coverage, edge cases, error paths)
- Architect (multi-agent isolation, port allocation, cleanup ordering)
