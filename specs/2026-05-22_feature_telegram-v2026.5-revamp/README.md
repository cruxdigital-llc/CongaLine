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


## Implementation (Phase Complete)

- **2026-05-22 (implementation)**: `/glados:implement-feature` invoked.
  All 6 phases from `tasks.md` ran in order:
  - **Phase 1** — `Channel` interface gained `SupportsRuntime(string) (bool, string)`. Import-cycle check forced the `string` parameter form (the `runtime.RuntimeName` form would have created `pkg/channels` → `pkg/runtime` → `pkg/channels`).
  - **Phase 2** — Slack returns `(true, "")` for any runtime. Telegram returns `(true, "")` for `"hermes"`, `(false, unsupportedOpenClawMsg)` otherwise. `OpenClawChannelConfig` rewritten as a hard-error defense-in-depth path. Package doc rewritten.
  - **Phase 3** — Gates added at five `ValidateBinding` call sites: CLI `resolveChannelBinding`, MCP `provision_agent` tool, local/remote/aws `BindChannel`. `runtime.ResolveRuntime(a.Runtime, "")` resolves the effective runtime before consulting `SupportsRuntime` so legacy empty-string agents are correctly treated as openclaw.
  - **Phase 4** — Tests added: `slack_test.go::TestSupportsRuntime` (runtime-neutral assertion); `telegram_test.go::TestSupportsRuntime` (table-driven: hermes ok, openclaw/empty/unknown rejected, reason mentions hermes fix + spec path); `telegram_test.go::TestOpenClawChannelConfig_Errors` (defense-in-depth); `registry_test.go::TestChannelInterface_Compile` (interface evolution guard); `localprovider/channels_test.go::TestBindChannel_RuntimeGate_RejectsTelegramOnOpenClaw` (provider-layer test, seeds shared telegram secret + openclaw agent, asserts rejection + no state mutation); `integration_telegram_test.go::TestAddUser_TelegramOnOpenClawRejected` (integration, build tag `integration`).
  - **Phase 5** — `CLAUDE.md` channel × runtime compatibility table added above the Slack Architecture section. `product-knowledge/standards/architecture.md` "Adding a New Channel" expanded with `SupportsRuntime` contract. `agents/_example/agent.yaml.example` left as-is (channel concerns don't fit the runtime-overlay schema).
  - **Phase 6** — V1 manual verification:
    ```
    $ conga --provider local --data-dir /tmp/conga-verify-tg \
        admin add-user testuser --runtime openclaw \
        --channel telegram:123456789
    Error: channel telegram is not supported for the openclaw runtime: \
      use the hermes runtime instead — see \
      specs/2026-05-22_feature_telegram-v2026.5-revamp/ for context
    $ echo $?
    1
    ```
    Agent dir confirmed unchanged. The error string was tightened mid-implementation to avoid restating "telegram is not supported for the openclaw runtime" both in the wrapper prefix and the channel-provided reason — the per-channel message now leads with the actionable fix.

- **Verification gates** all clean: `go build ./...`, `go vet ./...`, `gofmt -l .` empty, `go test ./... -count=1` passes across 19 packages including the integration tag build.

## Verification (`/glados:verify-feature`)

- **2026-05-22 (verify phase, automated)**: full suite + lint re-run on
  branch HEAD `ce3014c`:
  - `go test ./... -count=1` — 19/19 packages pass
  - `go vet ./...` — clean
  - `gofmt -l .` — empty
  - `go build -tags integration ./internal/cmd/` — clean
  - `go mod tidy -diff` — minor diff (`spf13/pflag` could move from
    indirect to direct), **but pre-existing on `origin/main`** — not
    introduced by this feature. Filing as a follow-up rather than
    fixing in scope.

- **2026-05-22 (verify phase, persona review of implementation)**:

  ### Architect — ✅ APPROVE
  - **Diff size honest**: 1505 insertions / 44 deletions across 21
    files. Implementation code is small (~120 lines of new logic);
    bulk is spec docs (~900 lines) and tests (~225 lines).
    Diff:code ratio is healthy for an interface-evolution change.
  - **Interface evolution discipline held**: `Channel.SupportsRuntime`
    landed with both implementations updated in the same commit. The
    interface-guard test (`registry_test.go::TestChannelInterface_Compile`)
    locks the contract for future channels.
  - **Import-cycle resolution**: the `string` parameter form was the
    forced choice (pkg/runtime already imports pkg/channels). Documented
    in the interface comment so future maintainers don't try to
    "improve" it to a typed parameter without re-checking.
  - **Provider parity**: gate landed in all 3 providers' `BindChannel`
    (local, remote, AWS) plus CLI + MCP. The single non-redundant gate
    is at `resolveChannelBinding` in admin_provision.go; the providers'
    gates are defense-in-depth against direct `BindChannel` callers.
    No interface-parity violations.
  - **Channel-abstraction boundary preserved**: no telegram-specific
    code outside `pkg/channels/telegram/`. The reason string is
    constant-encapsulated (`unsupportedOpenClawMsg`) so future Slack
    extensions to the same pattern can mirror without copy-paste.

  ### QA — ✅ APPROVE
  - **Test coverage matches spec inventory exactly**: T1–T6 from
    `spec.md` all landed with the planned promotion of T5 to the
    integration build tag.
  - **Manual verification (V1) confirmed end-to-end**: exit 1, no
    state mutation, error message points at spec dir.
  - **Edge cases asserted in tests**: empty runtime (defaults to
    openclaw), unknown runtime name, hermes-supported, openclaw-rejected
    — all in `TestSupportsRuntime` table.
  - **Defense-in-depth path tested**: `TestOpenClawChannelConfig_Errors`
    catches any code that reaches the dormant emission directly.
  - **Provider-layer regression guard**: the local-provider test
    seeds a real openclaw agent + telegram secret and asserts no
    `Channels[]` mutation on rejection — catches a class of "gate fires
    but state still updates" bugs that pure-unit tests would miss.

- **2026-05-22 (verify phase, post-impl standards gate)**:

  | Standard | Status | Notes |
  |---|---|---|
  | architecture.md — Provider contract | ✅ | All 3 providers symmetric |
  | architecture.md — Shared logic in pkg/channels | ✅ | `SupportsRuntime` is part of the interface, not provider code |
  | architecture.md — Channel abstraction | ✅ | No telegram-specific code outside pkg/channels/telegram/ |
  | architecture.md — Interface parity (CLI/JSON/MCP) | ✅ | Gate fires identically in CLI + MCP |
  | architecture.md — Module Structure (pkg/internal) | ✅ | Interface public; provider gating public; CLI/MCP gates private |
  | architecture.md — Agent Data Safety | ✅ | Gate fires before any data path; verified by provider-layer test |
  | security.md — Universal Baseline / pinned image / secrets via env | ✅ | No security-surface change |
  | config-taxonomy.md — Per-agent decision rule | ✅ | No new per-agent fields |
  | egress-controls.md — iptables baseline | ✅ | No egress-control change |

  **Post-impl gate result**: identical to pre-impl gate — no must
  violations, no should warnings. The implementation faithfully
  reflects what the spec promised.

- **2026-05-22 (verify phase, spec retrospection)**:

  Three divergences from `spec.md` reconciled by updating the spec:

  1. **Parameter type**: spec said `SupportsRuntime(runtime.RuntimeName)`;
     impl uses `SupportsRuntime(string)` because pkg/runtime already
     imports pkg/channels — the typed form would have created a cycle.
     spec.md interface block + file-level change row both updated to
     reflect the `string` form, with the cycle-check reasoning carried
     in the spec doc for future re-evaluation.
  2. **Error-message form**: spec showed the full
     `"telegram is not supported for the openclaw runtime; use the
     hermes runtime — see specs/..."` string. Impl tightened to
     `"use the hermes runtime instead — see specs/..."` after V1
     manual verification showed the wrapper prefix at the gate call
     site was duplicating the unsupported-runtime phrase. spec.md
     defense-in-depth code block updated.
  3. **D3 dropped**: `agents/_example/agent.yaml.example` (runtime
     overlay schema) was untouched in impl. Channel × runtime
     constraints are a provisioning concern that doesn't fit the
     overlay schema; documenting it there would muddy the file's
     purpose. spec.md D3 row marked as dropped with reasoning.

  Standards files (`product-knowledge/standards/*.md`) audited for stale
  code examples referencing the dormant telegram emission pattern —
  none found. `product-knowledge/standards/architecture.md` Channel
  Abstraction section was updated during implementation and correctly
  shows the `string` parameter form (matches impl).

- **2026-05-22 (verify phase, test synchronization)**:

  - **Stale references**: searched new test files for `TODO`/`XXX`/
    `deleted`/`removed`/`FIXME` markers — none.
  - **Fake/double alignment**: only `testChannel` in
    `registry_test.go` exists; updated to include `SupportsRuntime` so
    it stays in sync with the `Channel` interface. `TestChannelInterface_Compile`
    locks this guard.
  - **New method coverage**: `Channel.SupportsRuntime` is the only new
    public method; covered by `TestSupportsRuntime` in BOTH slack and
    telegram test files.
  - **Sibling comparison (slack_test.go vs telegram_test.go)**:
    slack has `TestBehaviorTemplateVars`; telegram had the method but
    not the test. **Closed the gap** — added
    `TestBehaviorTemplateVars` to telegram_test.go asserting
    `TELEGRAM_ID` is set from `binding.ID`. Without this, future
    template-rendering changes could silently break agent
    behavior-file substitutions.
  - Slack's `TestOpenClawChannelConfig_{User,Team,NoID}` have no
    telegram equivalent because telegram's `OpenClawChannelConfig`
    returns a hard error for all inputs (Option C); the
    `TestOpenClawChannelConfig_Errors` test we added covers both
    "user" and "team" agent types against that error path.
  - **Final regression**: `go test ./... -count=1` clean across 19
    packages with the new test added. `go vet`, `gofmt` clean.

## Final State

Implementation is verified, tests are in sync, standards gate passed
both pre- and post-implementation, no divergences left undocumented.

**Branch**: `spec/telegram-v2026.5-revamp` at HEAD `ce3014c` (plus the
verify-phase doc updates being committed now).
**PR**: [#52](https://github.com/cruxdigital-llc/CongaLine/pull/52).
**Status**: ready to merge.

**Carried-forward follow-ups (out of scope; tracked here, not
implemented)**:
- Option B (per-agent direct telegram for OpenClaw) — `plan.md` Phase
  2+ remains the starting point. No timeline; pick up when an operator
  needs telegram + OpenClaw.
- `go mod tidy -diff` discrepancy (pflag indirect→direct). Pre-existing
  on `origin/main`; safe to fix in a follow-up PR.
