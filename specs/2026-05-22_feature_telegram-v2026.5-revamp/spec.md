# Spec: Telegram v2026.5 Revamp (Option C — Hermes-only)

**Decision (locked at spec phase)**: Topology **Option C**. Telegram is
supported only for the **Hermes runtime**. OpenClaw + Telegram is
explicitly unsupported and rejected at provision/bind time with a clear
error message.

Rationale: per `protocol-notes.md`, Option A (router-fanout) is not
feasible — OpenClaw v2026.5.18's telegram plugin has no
"receive-forwarded-events" mode and always conflicts with an external
router's connection to `api.telegram.org`. Option B (per-agent direct)
works but is heavy operator UX (N bots in BotFather, N per-agent
secrets, N public HTTPS endpoints) for a use case no operator has
asked for. Option C codifies the current de-facto state: the Hermes
runtime + the existing Hermes-shaped router serve Telegram; OpenClaw
does not.

Out of scope: making OpenClaw + Telegram work. That's a separate spec
if it ever becomes a requirement — Option B in this spec dir's
`plan.md` is the starting point.

## Goal

After this spec lands:

1. `conga admin add-user <name> --runtime openclaw --channel telegram:<id>`
   refuses provisioning with a clear error pointing to the unsupported
   combination.
2. `conga channels bind <agent> telegram:<id>` on an existing OpenClaw
   agent refuses with the same error.
3. The Hermes + Telegram path (which works today via
   `router/telegram/src/index.js` → Hermes' OpenAI-compatible endpoint)
   continues to work unchanged.
4. The dormant pre-v2026.5.18 `OpenClawChannelConfig` emission in
   `pkg/channels/telegram/telegram.go` is replaced with an explicit
   error return, defending against any code path that reaches it
   directly (bypassing the runtime-compat gate).
5. CLAUDE.md, `agents/_example/agent.yaml.example`, and the channel
   abstraction section of `product-knowledge/standards/architecture.md`
   are updated to reflect the supported matrix.

## Non-goals

- Wiring OpenClaw + Telegram via per-agent direct bots (Option B). If
  this ever becomes a need, `plan.md` Phase 2 onward is the starting
  point. The work is real but uncommitted to.
- Changing the existing Hermes + Telegram delivery shape (the router
  POSTs to `/v1/chat/completions` on port 8642). Preserved as-is.
- Migrating the dormant `channels: { <id>: { allow: true } }` shape to
  the v2026.5.18 canonical (`groups: { <id>: { requireMention } }`).
  That migration is part of Option B; under Option C the emission path
  is gated off before it can run, so the legacy shape never reaches
  the v2026.5.18 image.

## Data model changes

### Channel interface — new `SupportsRuntime` method

`pkg/channels/channels.go` — add one method to the `Channel` interface:

```go
// SupportsRuntime reports whether this channel can be used with the
// named agent runtime. Provisioning and binding paths consult this
// before generating config or accepting a binding, so the unsupported
// combination fails early with an operator-actionable message rather
// than surfacing as a runtime config-generation error.
//
// Returns true for any runtime by default; channels with a runtime
// constraint return false for the unsupported runtimes.
SupportsRuntime(runtimeName runtime.RuntimeName) (bool, string)
```

The second return value is the explanation string when the bool is
`false` ("telegram is not supported for the openclaw runtime; use the
hermes runtime — see specs/2026-05-22_feature_telegram-v2026.5-revamp/
for context"). Empty when bool is `true`.

**Why a method, not a field**: future channels may have version-gated
support (e.g., a hypothetical Discord channel that only works with
specific OpenClaw versions). A method lets each channel encode its
own constraint logic.

**Why a `runtime.RuntimeName` argument, not `string`**: enforces type
safety at the call site so a typo in `"openclawd"` becomes a compile
error rather than a silent always-supported.

### Per-channel implementations

- `pkg/channels/slack/slack.go::SupportsRuntime` — returns
  `(true, "")` for any runtime. (Slack works for both openclaw and
  hermes today.)
- `pkg/channels/telegram/telegram.go::SupportsRuntime` — returns
  `(true, "")` for `RuntimeHermes`,
  `(false, "telegram is not supported for the openclaw runtime; …")`
  for any other (including `RuntimeOpenClaw`).

### `pkg/channels/telegram/telegram.go::OpenClawChannelConfig` — replace body with error

The dormant pre-v2026.5.18 emission is removed entirely. Replaced
with a hard error:

```go
func (t *Telegram) OpenClawChannelConfig(...) (map[string]any, error) {
    return nil, errors.New("telegram is not supported for the openclaw runtime; use the hermes runtime — see specs/2026-05-22_feature_telegram-v2026.5-revamp/")
}
```

Defense in depth: even if a caller bypasses `SupportsRuntime`, the
config generator fails closed.

## API surface changes

### CLI — provisioning + binding gates

Three CLI commands consult `SupportsRuntime`:

| Command | Where the check fires | Behavior on unsupported |
|---|---|---|
| `conga admin add-user --channel <p>:<id>` | `internal/cmd/admin_provision.go` (around line 194 where `ValidateBinding` is called) | Refuse with: `"channel platform <p> is not supported for the <rt> runtime: <reason>"`. Exit non-zero. |
| `conga admin add-team --channel <p>:<id>` | Same | Same |
| `conga channels bind <agent> <p>:<id>` | All three providers' `BindChannel` (e.g. `pkg/provider/localprovider/channels.go:212`) | Same |

`SupportsRuntime` is called BEFORE `ValidateBinding` so the operator
gets the runtime-compat error first, not a confusing "valid telegram
ID but unsupported" two-step.

### MCP — provisioning gate

`internal/mcpserver/tools_lifecycle.go:80` (where `ValidateBinding` is
called for the MCP `conga_provision_agent` tool) gets the same check.
Returns a tool error with the friendly message.

### Existing surfaces — unchanged

- `RouterEnvVars`, `AgentEnvVars`, `RoutingEntries`, `BehaviorTemplateVars`
  on the Telegram channel: unchanged. The router still receives bot
  token + signing secret env vars for the Hermes-shaped delivery path.
- Hermes runtime's channel-event-handling (env-vars + webhook adapter):
  unchanged.

## File-level changes

### Source

| # | File | Change |
|---|---|---|
| 1 | `pkg/channels/channels.go` | Add `SupportsRuntime(runtime.RuntimeName) (bool, string)` to the `Channel` interface. |
| 2 | `pkg/channels/slack/slack.go` | Implement `SupportsRuntime` — always `(true, "")`. |
| 3 | `pkg/channels/telegram/telegram.go` | Implement `SupportsRuntime` — `(true, "")` for `RuntimeHermes`, `(false, …)` for others. Replace `OpenClawChannelConfig` body with an `errors.New(...)` return. Drop the warning block; the spec is now binding behavior. |
| 4 | `internal/cmd/admin_provision.go` | Call `ch.SupportsRuntime(agent.Runtime)` after `channels.Get` and before `ValidateBinding`. Refuse with the explanation string. |
| 5 | `internal/mcpserver/tools_lifecycle.go` | Same insertion point inside the `provision_agent` tool handler. |
| 6 | `pkg/provider/localprovider/channels.go:~212` | Same insertion inside `BindChannel`. |
| 7 | `pkg/provider/remoteprovider/channels.go:~232` | Same insertion inside `BindChannel`. |
| 8 | `pkg/provider/awsprovider/channels.go:~201` | Same insertion inside `BindChannel`. |

### Tests

| # | File | Coverage |
|---|---|---|
| T1 | `pkg/channels/channels_test.go` (extend or create) | Assert every registered channel implements `SupportsRuntime` (compile-time guard via interface assertion). |
| T2 | `pkg/channels/slack/slack_test.go` | `SupportsRuntime` returns `(true, "")` for both openclaw and hermes. |
| T3 | `pkg/channels/telegram/telegram_test.go` | `(true, "")` for hermes; `(false, non-empty)` for openclaw and unknown runtimes. |
| T4 | `pkg/channels/telegram/telegram_test.go` | `OpenClawChannelConfig` returns a non-nil error with a message pointing to the spec. |
| T5 | `internal/cmd/integration_helpers_test.go` (extend) | **Integration test** (build tag `integration`): provisioning an openclaw agent with telegram binding fails before `ValidateBinding` is called; the error message contains the explanation. Per QA persona review, promoted from a plain unit test so future refactors that accidentally bypass the gate fail in CI rather than on a manual smoke. |
| T6 | One of the provider channels_test.go files | `BindChannel` refuses telegram on openclaw with the same message. |

### Docs

| # | File | Change |
|---|---|---|
| D1 | `CLAUDE.md` Slack Architecture / Channels section | Add a "Supported channel × runtime matrix" note: slack works for both; telegram is hermes-only. |
| D2 | `product-knowledge/standards/architecture.md` Channel Abstraction section | Update "Adding a New Channel" to mention `SupportsRuntime` as part of the implementation contract. |
| D3 | `agents/_example/agent.yaml.example` | Document the runtime-compat constraint in the overlay schema header comment (no field change). |
| D4 | `pkg/channels/telegram/telegram.go` package doc comment | Replace the existing "Uses the same proxy pattern as Slack" lead-in with an accurate statement: "Hermes-only. OpenClaw + Telegram is unsupported (see spec)." |

## Verification

Post-implementation manual verification before merge:

### V1 — OpenClaw + Telegram refused at provision

```bash
conga admin add-user testuser --runtime openclaw \
  --channel telegram:123456789 --provider local --data-dir ~/.conga-verify
```

Expect: command exits non-zero with the explanation string. Confirm
the agent JSON is NOT created in `~/.conga-verify/agents/`.

### V2 — OpenClaw + Telegram refused at bind

```bash
# Provision a slack-bound openclaw agent first
conga admin add-user testuser --runtime openclaw \
  --channel slack:U01234567890 ...
# Then try to add a telegram binding
conga channels bind testuser telegram:123456789
```

Expect: bind command exits non-zero with the explanation. Existing
slack binding is unaffected.

### V3 — Hermes + Telegram still works

```bash
conga admin add-user testhermes --runtime hermes \
  --channel telegram:123456789 ...
```

Expect: success. Container provisioned with the existing Hermes config
shape. Router forwards telegram events to the Hermes API endpoint.

### V4 — Existing Slack paths unchanged

Re-run the existing PR #51 verification scenarios (S1, S2, S3) on
aaron, confirm nothing regressed.

### V5 — MCP path

Use the conga MCP tool's `conga_provision_agent` with telegram binding
and openclaw runtime. Expect the same friendly error returned to the
tool caller.

## Edge cases

1. **Operator uses lowercase platform name with mixed case from
   hand-edited JSON**: round-9 of PR #51 normalized platform-name
   matching in `PluginsToInstall`. This spec's `SupportsRuntime` check
   reads `binding.Platform` directly — should it normalize too? Yes:
   apply `strings.ToLower(strings.TrimSpace(...))` at the lookup
   site, consistent with the precedent.

2. **Agent default runtime is empty (legacy agent JSON)**: an older
   agent.json may have `"runtime": ""` rather than `"runtime":
   "openclaw"`. `runtime.ResolveRuntime("", "")` returns
   `RuntimeOpenClaw` per pkg/runtime/runtime.go. The gate must resolve
   the effective runtime BEFORE checking `SupportsRuntime`, not check
   the raw string.

3. **Hermes runtime with a telegram-bound team agent**: today's router
   only handles user binding flows (forwards by `members[<userId>]`).
   Team binding (`channels[<chat-id>]`) on the Hermes router is
   untested. Out of scope here — flag in the spec but don't expand.

4. **Existing OpenClaw + Telegram agents in production**: per operator
   answer at spec phase, zero exist. If any are discovered: this
   spec's gates trigger immediately and refuse a refresh, surfacing
   the issue. The agent would not be silently broken — they'd be
   loudly broken with a clear path forward (switch to hermes runtime
   OR file a follow-on spec to implement Option B).

5. **`channels.Get(platform)` returns false** (unknown platform
   string in the binding): existing code already handles this with
   `fmt.Errorf("unknown channel platform")`. No new edge case
   introduced.

## Risks

| Risk | Likelihood | Mitigation |
|---|---|---|
| Adding a method to the `Channel` interface breaks an external implementer | Low | Slack and Telegram are the only implementations. No external consumers (registry, plugins). One compile-error fix per channel. |
| The new gate accidentally fires for Hermes + Telegram | Low | Test T3 + V3 catches this directly. |
| The provisioning gate doesn't catch a code path that bypasses CLI/MCP/BindChannel (e.g., direct manifest apply) | Medium | The defense-in-depth `OpenClawChannelConfig` returning an error catches this. T4 confirms. |
| Future operator does want OpenClaw + Telegram, finds the gate, doesn't know about the spec | Low | The error string explicitly references the spec dir path. Anyone hitting it gets a breadcrumb. |
| The `SupportsRuntime` method signature `(bool, string)` reads awkwardly compared to a `(string error or nil)` pattern | Medium | Aesthetic. `(bool, reason)` is more readable at call sites than nil-checking an error. Decided in spec phase; not litigating in implementation. |

## Rollback

`git revert <commit-sha>` of the implementation commit. No data
migration to undo. No agent reconfig required (the gate is
provision-time only; existing agents are unaffected). Telegram
provisioning attempts on OpenClaw after revert will silently emit the
old broken config (the pre-spec state) — operator gets the same
runtime errors as before this spec, just without a friendly message.

## Open questions carried into implementation

None. All Phase 1 protocol questions from `requirements.md` are
either resolved by `protocol-notes.md` (we now know the v2026.5.18
plugin design) or scoped out of this spec (Option B work).
