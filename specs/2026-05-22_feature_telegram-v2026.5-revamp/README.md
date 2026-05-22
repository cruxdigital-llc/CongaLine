# Feature Trace: Telegram v2026.5 Revamp

**Feature**: `telegram-v2026.5-revamp`
**Started**: 2026-05-22
**Status**: **Planning only — no implementation in this trace**
**Lead**: TBD (pending operator pickup)

## Purpose

Make Telegram a fully-supported messaging channel for **OpenClaw** agents on
v2026.5.18+. Today the Telegram channel implementation in this codebase is
in a half-finished state:

- `pkg/channels/telegram/telegram.go` emits an OpenClaw channel config that is
  pre-v2026.5.18 and would be rejected at gateway startup by the new strict-
  additional-properties schema if anyone tried to use it.
- `router/telegram/src/index.js` was built for **Hermes Agent**, not
  OpenClaw — it forwards Telegram events to `/v1/chat/completions` (port
  8642, Hermes' OpenAI-compatible endpoint) rather than to an OpenClaw
  webhook receiver. So even if the channel config were correct, inbound
  Telegram events wouldn't actually reach an OpenClaw agent.
- The OpenClaw Telegram plugin ships **bundled-but-disabled** in v2026.5.18
  (`openclaw plugins list` shows it as `disabled` by default), unlike the
  Slack plugin which was externalized into `@openclaw/slack`. Our generator
  emits `plugins.entries.telegram.enabled: true`, but that's untested
  against the new image.

The production fleet does not currently use Telegram. This is dormant code.
This spec scopes what would need to change to make it functional under
OpenClaw v2026.5.18+, so the next operator who wants Telegram support has a
runnable starting point.

## Discovery context

Surfaced during the `chore/upgrade-openclaw` PR review (2026-05-21/22). The
[pr-review-toolkit:code-reviewer](https://github.com/cruxdigital-llc/CongaLine/pull/51#issuecomment-4516560155)
agent flagged `pkg/channels/telegram/telegram.go:73` as the same `allow: true`
→ `enabled: true` migration we did for Slack in round 6 of that PR. Deeper
investigation revealed the broader architectural mismatch documented here.

## Active Personas

- **Architect** — topology choice (router vs per-agent bot), v2026.5.18
  schema compliance, OpenClaw plugin enablement path
- **QA** — live Telegram bot smoke test plan, multi-account scenarios,
  long-polling vs webhook trade-offs

## Session Log

- **2026-05-22**: Spec dir scaffolded. Requirements + plan drafted from the
  PR #51 investigation. No code changes — implementation deferred to a
  future spec session.
- **2026-05-22 (spec phase)**: `/glados:spec-feature` invoked. Branch
  `spec/telegram-v2026.5-revamp` checked out. Probing v2026.5.18 telegram
  plugin internals to inform the Phase 0 topology decision before
  drafting `spec.md`.
- **2026-05-22 (protocol findings)**: `protocol-notes.md` written.
  Material finding: **Option A (router-fanout) is not feasible** —
  OpenClaw v2026.5.18's telegram plugin has no "receive-forwarded-events"
  mode. `plan.md`'s recommendation was based on extrapolating from
  Slack's `mode: "http"` which doesn't exist for telegram. Plan
  superseded by protocol-notes; spec.md targets Option C
  (Hermes-only + OpenClaw gate).
- **2026-05-22 (topology decision)**: Operator picked **Option C** with
  confirmation that nobody currently uses Telegram + OpenClaw. Spec.md
  drafted: adds `SupportsRuntime(runtime.RuntimeName) (bool, string)`
  to the `Channel` interface, slack returns true for both, telegram
  returns true for hermes / false for openclaw. Gate fires at
  provisioning (CLI, MCP) + binding (all 3 providers) + as
  defense-in-depth in `OpenClawChannelConfig`.

## Persona Review (Spec Phase)

### Architect

**Verdict**: ✅ APPROVE.

- **No new dependencies**, no data-model changes. Channel config JSON
  unchanged.
- **Interface-evolution discipline**: adds one method
  (`SupportsRuntime`) to `Channel`. Both implementations (slack +
  telegram) updated in the same commit. No external implementers exist
  today (channels.Register is a process-local registry; no plugin
  loader for channels). Risk of breaking external consumers is
  effectively zero.
- **Provider contract** held: gate fires in all three providers'
  `BindChannel` paths AND at the CLI / MCP provisioning entry points.
  Symmetric across providers per the architecture.md Interface Parity
  Must standard.
- **Channel Abstraction standard** preserved: no telegram-specific
  logic outside `pkg/channels/telegram/`. The runtime-compat check is
  expressed inside the channel package, not bolted into the runtime.
- **Performance**: a single function call per provisioning. Negligible.
- The defense-in-depth `OpenClawChannelConfig` error return is a good
  belt-and-suspenders pattern for any code path that bypasses the gate
  (e.g., a manifest-apply flow that constructs a binding directly).

**Note (not blocking)**: the `(bool, string)` return type is slightly
unusual — Go convention would be `error` (nil = supported). Spec
explicitly decided `(bool, reason)` is more readable at call sites.
Marginal style preference; accept the spec's choice.

### QA

**Verdict**: ✅ APPROVE with one request.

- **Unhappy path coverage** present: V1 (provisioning refused), V2
  (bind refused), V5 (MCP refused). Each has binary pass/fail
  observable.
- **Edge cases** in spec §"Edge cases" cover the realistic risks:
  case-normalization, empty runtime field, legacy agent JSON, hermes
  team binding (flagged out of scope).
- **Backward compat**: spec §"Edge cases #4" correctly identifies that
  an existing OpenClaw+Telegram agent (hypothetically; operator
  confirmed none exist) would be loudly broken with a clear path
  forward, not silently broken. That's the right call.
- **Test inventory**: T1–T6 cover the new behavior. T1 is a
  compile-time interface guard; T2–T4 are pure unit tests; T5–T6
  exercise the gates at the CLI and provider layers.

**Request (folded into spec)**:
1. V1 (CLI provisioning refused) is currently spec'd as manual
   verification. Promote to an automated integration test using the
   existing `integration_helpers_test.go` pattern, so future
   refactors that accidentally bypass the gate fail in CI rather than
   on a manual smoke. (Hermes side V3 stays manual since it needs
   Hermes infrastructure.)

## Spec adjustments from persona review

Folded into `spec.md`:
- Test T5 promoted from manual `internal/cmd/admin_provision_test.go`
  extension to an integration-style test using
  `integration_helpers_test.go` pattern (requires Docker, gated
  behind the `integration` build tag like the existing helpers).

## Standards Gate Report (Pre-implementation)

| Standard | Scope | Severity | Verdict |
|---|---|---|---|
| `architecture.md` — Provider contract is the API boundary | all providers | must | ✅ PASSES — gate is added to all three providers' `BindChannel` paths symmetrically. No provider-specific behavior beyond the per-provider plumbing. |
| `architecture.md` — Shared logic lives in common or its own package | channel-runtime compat logic | must | ✅ PASSES — `SupportsRuntime` is on the `Channel` interface (in `pkg/channels/`), not in provider packages. Provider call sites are thin one-liners that consult the interface. |
| `architecture.md` — Channel Abstraction (no deeper coupling) | new code | should | ✅ PASSES — no telegram-specific logic anywhere outside `pkg/channels/telegram/`. The compat constraint lives inside the channel package where it belongs. Slack and other future channels can express their own runtime constraints without coupling to the runtime packages. |
| `architecture.md` — Interface Parity | cli/json/mcp | must | ✅ PASSES — gate fires in CLI (`admin_provision.go`), MCP (`tools_lifecycle.go`), and provider `BindChannel` (all three). JSON I/O path goes through the same CLI handlers, so it's covered transitively. |
| `architecture.md` — Module Structure (pkg/internal split) | imports | must | ✅ PASSES — `Channel` interface stays in `pkg/channels/` (public). Provider gating is in `pkg/provider/` (public). CLI/MCP gates are in `internal/` (private). Public API change is one method addition to `Channel`. |
| `architecture.md` — Agent Data Safety | lifecycle | must | ✅ PASSES — gate prevents provisioning before any data directory is created. Existing agents are untouched. No data path runs while the gate is firing. |
| `architecture.md` — Config Format Boundary | config files | should | ✅ PASSES — no new config files or formats. |
| `config-taxonomy.md` — Decision rule for per-agent concerns | new fields | must | ✅ PASSES — no new per-agent fields. The runtime-compat constraint is a code-level invariant, not a config-level one. |
| `security.md` — Universal Baseline | per-agent containers | must | ✅ PASSES — no changes to container hardening, secrets handling, or network controls. |
| `security.md` — Pinned image | runtime baseline | must | ✅ PASSES — no runtime version change. |
| `security.md` — Secrets via env vars, never config | secrets handling | must | ✅ PASSES — Telegram bot token continues to flow via env (router-only for Hermes path). No new secret storage. |
| `egress-controls.md` — iptables active in ALL modes | egress | must | ✅ PASSES — no change to egress proxy or iptables behavior. |

**Result**: ✅ Gate PASSES. No `must` violations, no `should` warnings.
The change is contained, symmetric, and aligned with existing
abstractions. Proceed to implementation.

## Decisions

- **Topology**: Option C — Hermes-only + OpenClaw gate. (Locked.)
- **Interface evolution**: add `SupportsRuntime(runtime.RuntimeName) (bool, string)` to `Channel`. (Marginal `(bool, string)` vs `error` aesthetic call accepted as readability-better-at-call-sites.)
- **Gate placement**: CLI provisioning + MCP provisioning + all three providers' `BindChannel` + defense-in-depth `OpenClawChannelConfig` returning error.
- **Backward compat policy**: if a hypothetical existing OpenClaw + Telegram agent is discovered, the gate triggers immediately and refuses refresh. Loud failure with operator-actionable message preferred over silent breakage.
- **Out of scope, deferred**: Option B (per-agent direct telegram). `plan.md` remains the starting point if/when needed.
