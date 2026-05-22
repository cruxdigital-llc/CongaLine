# Implementation Tasks: Telegram v2026.5 Revamp (Option C)

Derived from `spec.md` §"File-level changes". Each task is small and
independently reviewable. Order matters: interface change first
(compile errors guide the rest), then implementations, then call-site
gates, then tests, then docs.

## Phase 1 — Channel interface evolution

- [ ] **T1.A1** `pkg/channels/channels.go` — add
  `SupportsRuntime(runtimeName runtime.RuntimeName) (supported bool, reason string)`
  to the `Channel` interface. Import `pkg/runtime` (check for import
  cycle — if `pkg/runtime` already imports `pkg/channels`, switch to
  `runtime.RuntimeName` as a `string` typedef or move the constant
  into `pkg/channels`).
- [ ] **T1.A2** Cycle check before implementing. Run
  `grep -rn "pkg/channels" pkg/runtime/ --include="*.go" | grep -v _test.go`
  to confirm there's no existing edge. If there is, the spec's
  decision falls back to `string` runtime name.

## Phase 2 — Per-channel implementations

- [ ] **T2.A1** `pkg/channels/slack/slack.go` — implement
  `SupportsRuntime`. Always returns `(true, "")`.
- [ ] **T2.A2** `pkg/channels/telegram/telegram.go` — implement
  `SupportsRuntime`. Returns `(true, "")` for `RuntimeHermes`;
  `(false, "telegram is not supported for the openclaw runtime; use the hermes runtime — see specs/2026-05-22_feature_telegram-v2026.5-revamp/")`
  for anything else.
- [ ] **T2.A3** `pkg/channels/telegram/telegram.go` — replace
  `OpenClawChannelConfig` body with an `errors.New(...)` return
  (defense in depth). Drop the old warning comment block (the
  spec/runtime gate is now binding behavior; the comment is stale).
- [ ] **T2.A4** `pkg/channels/telegram/telegram.go` — update the
  package doc comment at line 1-3 from "Uses the same proxy pattern
  as Slack" to "Hermes-only. OpenClaw + Telegram is unsupported (see
  spec)."

## Phase 3 — Gate at every entry point

- [ ] **T3.A1** `internal/cmd/admin_provision.go` — at the line where
  `ch.ValidateBinding(...)` is called (~line 194), insert a
  `SupportsRuntime` check first. Resolve the effective runtime via
  `runtime.ResolveRuntime(agent.Runtime, globalDefault)` so legacy
  empty-string runtime defaults to openclaw correctly.
- [ ] **T3.A2** `internal/mcpserver/tools_lifecycle.go` — same
  insertion inside `provision_agent` tool handler at the existing
  `ValidateBinding` call site (~line 80).
- [ ] **T3.A3** `pkg/provider/localprovider/channels.go` — same
  insertion in `BindChannel` at ~line 212. Resolve effective runtime
  from the agent config.
- [ ] **T3.A4** `pkg/provider/remoteprovider/channels.go` — same in
  `BindChannel` at ~line 232.
- [ ] **T3.A5** `pkg/provider/awsprovider/channels.go` — same in
  `BindChannel` at ~line 201.
- [ ] **T3.V1** After all gates: `grep -rn "ValidateBinding" --include="*.go" pkg/ internal/`
  to confirm every call site is preceded by `SupportsRuntime`. The
  six call sites enumerated above must be it; no others added since
  spec drafted.

## Phase 4 — Tests

- [ ] **T4.A1** `pkg/channels/slack/slack_test.go` — add
  `TestSupportsRuntime` covering openclaw + hermes + unknown
  (sentinel value). All return `(true, "")`.
- [ ] **T4.A2** Create `pkg/channels/telegram/telegram_test.go` if
  absent, OR extend existing. Add `TestSupportsRuntime` covering
  openclaw + hermes + empty + unknown. Hermes returns
  `(true, "")`; everything else returns `(false, msg)` where msg
  contains "openclaw" and references the spec path.
- [ ] **T4.A3** Same file — add `TestOpenClawChannelConfig_Errors`
  asserting the function returns a non-nil error with a message that
  points to the spec.
- [ ] **T4.A4** `pkg/channels/channels_test.go` — add a compile-time
  interface guard that exercises `SupportsRuntime` on every
  registered channel. Pattern: `var _ Channel = (*slack.Slack)(nil)`.
- [ ] **T4.A5** Pick one of `pkg/provider/{local,remote,aws}provider/channels_test.go`
  and add a `TestBindChannel_RuntimeGate` that wires a telegram
  binding against an openclaw agent and asserts the rejection error
  message. Local provider is easiest (no SSM/SSH mock needed).
- [ ] **T4.A6** `internal/cmd/integration_helpers_test.go` (extend)
  — add `TestAddUser_TelegramOnOpenClawRejected` per QA persona
  review request. Gated behind the `integration` build tag.
- [ ] **T4.V1** `go test ./... -count=1` passes.
- [ ] **T4.V2** `go vet ./...` clean.
- [ ] **T4.V3** `gofmt -l .` empty.

## Phase 5 — Documentation

- [ ] **T5.D1** `CLAUDE.md` — add a "Supported channel × runtime
  matrix" note in the Slack Architecture / Channels section: slack
  works for both; telegram is hermes-only.
- [ ] **T5.D2** `product-knowledge/standards/architecture.md` Channel
  Abstraction section — update "Adding a New Channel" to mention
  `SupportsRuntime` as part of the implementation contract.
- [ ] **T5.D3** `agents/_example/agent.yaml.example` — header comment
  block mentions the runtime-compat constraint (no field change).
- [ ] **T5.D4** Verify the package doc comment in
  `pkg/channels/telegram/telegram.go` is updated (T2.A4) and reads
  cleanly as the file's leading commentary.

## Phase 6 — Verify + commit

- [ ] **T6.V1** Run V1 from spec §Verification manually: attempt
  `conga admin add-user testuser --runtime openclaw --channel telegram:123456789 --provider local --data-dir /tmp/conga-verify-tg`
  and confirm rejection. (Hermes side V3 is operator-driven and not
  in this implementation pass.)
- [ ] **T6.V2** Single commit with the message form from spec.md
  §"Commit message" — or a short subject describing the gate +
  defense-in-depth + tests.

## What this implementation will NOT do

- Phase 3 of `plan.md` onward (Option B work — actually wiring
  OpenClaw + Telegram). Explicitly scoped out by the spec decision.
- Touching `router/telegram/src/index.js`. Hermes path stays as-is.
- AWS bash heredocs. No telegram emissions land in those paths under
  Option C (gate fires before any heredoc renders telegram channel
  config).
- Migrating the dormant `channels: { <id>: { allow: true } }` shape.
  The defense-in-depth error return on `OpenClawChannelConfig`
  replaces the shape entirely.
