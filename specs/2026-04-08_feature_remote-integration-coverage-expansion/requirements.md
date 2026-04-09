# Requirements: Remote Integration Test Coverage Expansion

## Problem

The remote provider integration tests (Feature #25) cover the core lifecycle,
behavior files, and egress policy — but leave 10 of 26 Provider interface
methods untested and several high-risk scenario categories unexercised.
A coverage gap analysis identified four priority areas:

1. **Multi-agent** — no test provisions 2+ agents. Port allocation conflicts,
   routing.json fan-out, RefreshAll, network isolation, and CycleHost are
   completely untested. This is the highest real-world risk.

2. **Channels** — 5 Provider methods (AddChannel, RemoveChannel, ListChannels,
   BindChannel, UnbindChannel) have zero coverage. Channel config generation,
   routing.json updates, and router lifecycle are untested through SSH paths.

3. **Error paths** — no negative test cases. Duplicate agent names, missing
   setup, removing non-existent agents, and invalid SSH credentials are
   unverified. These are where real-world breakage tends to hide.

4. **Connect** — the SSH tunnel (`Connect()`) is untested. This is the
   primary way users access the web UI through the remote provider.

## Goals

1. **Multi-agent test**: Provision 2 agents simultaneously. Verify unique
   gateway ports, correct routing.json entries for both agents, RefreshAll
   restarts both, network isolation (each agent on its own Docker network),
   and clean removal of one agent without affecting the other.

2. **Channel management test**: Exercise AddChannel (with mock Slack secrets),
   ListChannels, BindChannel, UnbindChannel, RemoveChannel through SSH paths.
   Verify openclaw.json channel config, routing.json entries, and router
   container lifecycle — without requiring a live Slack connection.

3. **Error path test**: Verify expected failures for: provisioning before
   setup, adding a duplicate agent name, removing a non-existent agent,
   and setting a secret on a non-existent agent. Each should return a
   non-zero exit code with a meaningful error message.

4. **Connect test**: Verify that `Connect()` opens an SSH tunnel and the
   gateway responds with HTTP 200 on the forwarded local port.

## Non-Goals

- Testing live Slack/Telegram message delivery (requires real tokens).
- Testing CycleHost separately (covered implicitly by multi-agent RefreshAll).
- Testing WhoAmI/ResolveAgentByIdentity (identity resolution is only
  meaningful with real channel credentials).
- Achieving 100% Provider method coverage — focus on the highest-value gaps.

## Constraints

- All tests reuse the existing SSH container infrastructure from Feature #25.
- Multi-agent test needs 2 agent name prefixes but shares one SSH container.
- Channel tests use dummy Slack secrets (bot token, signing secret) — the
  router container will start but won't connect to Slack.
- Connect test needs to verify HTTP response through the SSH tunnel, which
  requires the gateway to be healthy inside the container.
- Each test function does its own setup/teardown (no shared state between tests).

## Success Criteria

- All 4 new test functions pass alongside existing tests in a single
  `go test -tags integration ./internal/cmd/` run.
- Multi-agent test verifies: unique ports, routing.json correctness,
  RefreshAll, independent removal.
- Channel test exercises all 5 channel Provider methods through SSH.
- Error path test verifies 4+ negative cases with expected error messages.
- Connect test gets HTTP 200 through the SSH tunnel.
- No leaked containers, networks, or SSH tunnels after test run.
- CI job passes without modifications to the workflow file.
