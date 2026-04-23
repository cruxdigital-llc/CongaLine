# Trace Log: Multi-Channel Team Agents

**Feature**: Multi-Channel Team Agents
**Spec Directory**: `specs/2026-04-22_feature_multi_channel_team_agents/`
**Started**: 2026-04-22
**Status**: Planning

## Active Personas
- **Architect** — Schema change impact, routing generation, provider parity
- **QA** — Edge cases around bind/unbind semantics, allowlist drift, unbound-channel event handling
- **PM** — Admin UX for binding multiple channels, user-visible ephemeral message for unbound channels

## Active Capabilities
- **MCP Tools**: `conga_*` tools for runtime verification (channels list/bind/unbind, status, logs)
- **GitHub**: PR creation and review

## Relationship to Other Specs
- **Adjacent**: `specs/2026-04-16_feature_dm-agent-routing/` — independent feature. Both touch `pkg/common/routing.go` and `pkg/channels/slack/slack.go`. Whichever lands second will require a small rebase. This spec assumes a **soft preference** for dm-agent-routing landing first: if it has, the router already resolves channel membership in-memory, which is the natural place to hook the unbound-channel ephemeral UX. If not, this spec lands a narrower router change.
- **Builds upon**: `specs/2026-03-26_feature_channel-abstraction/`, `specs/2026-03-27_feature_channel-management-cli/` — prior work established the `Channel` interface and bind/unbind CLI. This spec removes the "one binding per platform" constraint layered on top of that.

## Session Log

### 2026-04-22 — Planning Session
- **Context**: User wants a single team agent to serve multiple Slack channels. Current code enforces one binding per platform per agent (`pkg/provider/localprovider/channels.go:187-191` and parity code in remote/aws). Router fan-out already handles N-to-1 channel→container forwarding; the blocker is the bind-time guard and one-entry-per-agent assumptions in routing/policy generation.
- **Decision**: Feature is the multi-channel binding primitive. Specific use cases (e.g. contract-review agent, Google Drive MCP) are out of scope — those are per-agent behavior files, not framework features.
- **Decision**: Cross-channel memory isolation is advisory, not enforced. The single container shares memory across all bound channels; SOUL.md guidance can suggest channel-keyed organization but nothing prevents cross-channel reference. This is acceptable for the intended use cases (team agents whose channels are intentionally related).
- **Decision**: When the bot is invited to an unbound channel, the router posts an ephemeral "not configured for this channel — ask an admin to run `conga channels bind`" message to the inviting user (or the first message author if invite isn't observed). Rate-limited to avoid flooding busy channels.
- **Decision**: `conga channels bind` becomes additive (not replacing). `unbind` targets a specific `(platform, id)` pair. `list` renders multiple bindings per agent.

### 2026-04-22 — Plan Refinement (post-initial-draft)
- **Decision**: Ephemeral unbound-channel message uses a generic `<agent>` placeholder — does NOT name the target agent. Rationale: preserve agent-name privacy. Admins know which agent owns which channel; users posting in the channel do not need that mapping.
- **Decision**: Allowlist expansion uses Go interface embedding — new `MultiBindingChannel` interface extends `Channel` with a second method. Slack implements both; Telegram and other 1:1 channels implement only the base `Channel`. Keeps the existing `Channel` contract stable and lets the call site discover capability via type assertion.
- **Decision**: Single PR for all phases in congaline. Router changes and Go changes ship together so the feature is coherent and the ephemeral notice isn't live without the binding support it references.
- **Decision**: Drop the standards doc phase (originally Phase 7). Memory organization is a per-agent behavior concern, not a framework-level standard. Operators author their own SOUL.md guidance via `behavior/agents/<name>/`.

### 2026-04-22 — Production-Readiness Audit
- **Finding**: `Provider.UnbindChannel(ctx, agentName, platform)` is insufficient — with multi-binding allowed, the caller must name which binding to remove. Signature changes to `UnbindChannel(ctx, agentName, platform, id)`. Breaking API change to the `Provider` interface; all three provider implementations and all four call sites (CLI, MCP, test mock, terraform provider) update in lockstep.
- **Finding**: `ErrBindingExists` sentinel is used externally by `terraform-provider-conga/internal/terraform/binding_resource.go:110` for idempotent terraform apply reconciliation. Under the new idempotent-on-exact-match semantics, the sentinel's only use case disappears. Remove the sentinel; delete the reconciliation block in the Terraform provider as part of the coordinated release. Add `ErrAmbiguousUnbind` as the one new sentinel (for when `id` is omitted but multiple bindings exist).
- **Finding**: Terraform resource identity (`agent/platform`) collides when multiple bindings exist for the same agent+platform. Schema version bump to 1 with a state upgrader rewriting IDs to `agent/platform/id`. Existing `.tfstate` files migrate automatically on next plan/apply.
- **Finding**: Terraform provider `ImportState` uses 2-part `agent/platform`. Change to 3-part `agent/platform/id`; accept the old format with a deprecation warning for a minor version.
- **Finding**: Terraform provider `Read` uses singular `ChannelBinding(platform)` — returns wrong binding when N > 1. Iterate `ChannelBindings(platform)` and match by ID from state.
- **Decision**: Ship as two coordinated PRs — one in congaline, one in terraform-provider-conga, sequenced by tag. Full rollout and rollback plans documented in spec.md §7. Rollback is a one-way door for operators who actually use multi-binding; release notes call this out.
- **Scope confirmation**: Only `terraform-provider-conga` imports congaline's `pkg/provider/` exports. No other external Go consumers to coordinate.

### 2026-04-22 — Persona Review on Specification

**Architect (hybrid persona)** — reviewing against architecture, standards, performance.

- ✅ **Fits existing architecture.** `Provider` interface change is coordinated with the one external consumer. `MultiBindingChannel` extends `Channel` via Go interface embedding — clean capability-discovery pattern.
- ⚠️ **Novel patterns introduced** (non-blocking but worth noting):
  - Interface embedding for capability extension is not used elsewhere in this codebase — existing interfaces (`Provider`, `Channel`, `Runtime`, the AWS SDK interfaces) are all flat. First use. Idiomatic Go, but reviewers should understand the intent.
  - `var _ channels.MultiBindingChannel = (*Slack)(nil)` compile-time assertion is not used anywhere else in the repo. Low cost, catches interface drift at build time. Recommend standardizing this pattern as we add more interface-satisfying types.
- ⚠️ **Preexisting concern surfaced, not introduced**: `routing.json` is written with `os.WriteFile` (`pkg/provider/localprovider/provider.go:1663`) — not atomic. The router's `fs.watch` reload handles parse errors gracefully (keeps prior config), so the race is self-healing. **Not a blocker for this spec** since the race window is unchanged by multi-binding. Flagged as a separate hardening candidate.
- ❗ **Gap: same channel ID bound to two different agents.** Current routing generation does `cfg.Channels[id] = url` without checking for collisions. If operator A binds `slack:C123` to agent `legal` and operator B binds the same `slack:C123` to agent `sales`, the second write silently overwrites the first in `routing.json`. **Recommend adding a bind-time check across all agents**: reject if the `(platform, id)` is already bound to a different agent. Update spec §2.1 (bind guard) and §2.6 MCP/CLI error messaging.
- ✅ **No new dependencies.** Router change uses native `fetch`. Go changes are additive within existing packages.
- ✅ **Data model change is schema-compatible** at the wire level. `AgentConfig.Channels` JSON unchanged.
- ⚠️ **`ErrAmbiguousUnbind` sentinel placement**: belongs in `pkg/provider/errors.go` alongside `ErrNotFound`. Confirm spec §2.5 — it does. Good.
- ✅ **Scalability**: N bindings per agent bounded by practical limits (Slack workspaces rarely have >100 channels for one agent). `openclaw.json` regeneration O(N). No concern.

**Architect verdict**: One ❗ blocker (same-ID-to-different-agent collision). Address in spec before proceeding.

---

**QA (hybrid persona)** — reviewing against edge cases, regression, test coverage.

- ✅ **Snapshot test for single-binding byte-identical output** is in the plan (§4.1 `TestGenerateRoutingJSON_TeamAgentSingleChannel_ByteIdentical`). Good regression protection.
- ✅ **Rate-limit test matrix** for unbound ephemeral covers: first call, second call within TTL, different user, TTL expiration, bot sender, missing user. Covers the obvious edges.
- ❗ **Missing test: label-mismatch on idempotent rebind.** Spec §3.1 row 2 says "different label wins first-bind's label". But there's no test for this. QA wants either: (a) a test that asserts "first label wins" (documenting the chosen behavior), or (b) reverting to an explicit error so the ambiguity surfaces to the operator. **Recommend: explicit error** — idempotent success for exact `(platform, id)` match, but *different* labels on the same `(platform, id)` should error ("binding exists with different label — unbind first if you want to relabel"). Protects against accidental label drift.
- ❗ **Missing test: `terraform-provider-conga` state upgrader with missing `binding_id`.** My spec's state upgrader assumes `binding_id` is present in v0 state. But what about state files from extremely old provider versions where the attribute shape differed, or state manually edited? Add test: `TestStateUpgrader_MissingBindingID_ErrorsHelpfully` — fails with a clear message pointing the operator at `terraform state rm` + re-import.
- ⚠️ **Gap: bind during router hot-reload.** Spec §3.1 row 5 claims "router either sees old config or new — never partial." QA: this relies on `fs.watch` emitting ONE event per rename, which is platform-dependent (Linux inotify vs. macOS FSEvents). Not unique to this spec — preexisting behavior — but the claim in the spec is stronger than the actual guarantee. **Soften §3.1 row 5**: "atomic rename produces at-most-one reload; the reload handler in `index.js:29-30` falls back to prior config on parse errors."
- ⚠️ **Missing test: `chat.postEphemeral` common non-fatal errors.** Spec §2.7 `postEphemeral` logs but does not react to `not_in_channel`, `user_not_in_channel`, `channel_not_found`. Add test asserting the warn log is emitted and the rate-limit entry is STILL set (so we don't retry infinitely every message).
- ✅ **Integration test covers** provision → bind 3 → read configs → unbind 1 → unbind all. Comprehensive.
- ❗ **Missing coverage: CLI `unbind` without ID when exactly 1 binding.** Spec §2.6 says "legacy form preserved; removes the sole binding." Add explicit test: `TestUnbind_NoID_SingleBinding_Succeeds` and `TestUnbind_NoID_MultipleBindings_Errors`.
- ⚠️ **Test for concurrent binds on AWS (SSM) path.** SSM parameter writes are not transactional. If two operators run `conga channels bind` simultaneously against the same agent, last-write-wins could drop one binding silently. **Flag for implementation-time**: add an optimistic-concurrency check using SSM parameter version or ETag. Not blocking the spec — preexisting behavior inherited from the AWS provider — but worth surfacing.

**QA verdict**: Two ❗ blockers (label-mismatch test + state upgrader missing-attr test). Two ⚠️ hardening notes (hot-reload guarantee softening, postEphemeral error test). Address blockers in spec; keep hardening notes as follow-ups or implementation-time tasks.

---

**PM (review persona)** — reviewing against user value, scope, testability.

- ✅ **"Why" and "Who" clearly defined.** Operators running team agents across multiple channels. Pain: N agents, N× memory, fragmented context. Clear.
- ✅ **Scope held tight.** Drive MCP / contract review use case correctly excluded. Terraform provider coordination correctly included (production readiness).
- ✅ **Success criteria are testable.** All 9 criteria map to concrete assertions.
- ❗ **Success metric for adoption not defined.** PM wants one operational signal: post-release, how do we know operators are using multi-binding? **Recommend adding §5 (Observability)**: log a structured event when an agent's `routing.json` is regenerated with >1 Slack binding. Not instrumentation-heavy; just ensures we can see adoption in logs.
- ⚠️ **Ephemeral message UX regression**: spec §2.7 uses literal `<agent>` placeholder. PM concern: operators inviting the bot see a message telling them to run a command with `<agent>` they're expected to fill in — but how do they know WHICH agent to ask for? If the organization has multiple Conga admins managing different agents, the inviting user might not know the mapping. **Mitigation options**:
  1. Message suggests a single known "admin" channel (e.g. `#conga-admin` configurable env var) for the user to post in.
  2. Message includes the *list of agents the router knows about* — less privacy-preserving but discoverable.
  3. Keep current message; accept that admin discovery is out of scope.
  - PM recommendation: **option 3 for v1** but document in the spec that this UX is intentionally minimal and feedback-driven. Add a follow-up in §8 Open Questions.
- ✅ **Acceptance criteria reflect both CLI and Terraform paths** with upgrade scenarios (Scenario H in requirements.md). Good.
- ⚠️ **Rollback warning should surface in release notes prominently.** Already in §7.3, but PM wants the first line of the release notes to say "This release contains breaking API changes and a one-way schema upgrade for Terraform-managed bindings. Review rollback constraints before upgrading."
- ✅ **Idempotent bind matches operator mental model** (desired state, not imperative). PM happy.

**PM verdict**: One ❗ blocker (adoption metric). One ⚠️ documentation note (release-notes headline) — carry into implementation.

---

**Synthesis**

Blockers to address in the spec before proceeding:

1. [Architect] Add bind-time check against same `(platform, id)` being bound to multiple *different* agents.
2. [QA] Define behavior + test for idempotent rebind with different label — recommend explicit error.
3. [QA] Add state-upgrader test for missing `binding_id` in v0 state.
4. [PM] Add an adoption metric / structured log entry for multi-binding usage.

Non-blocking hardening to carry as follow-ups or implementation-time notes:

- Soften §3.1 row 5 language around hot-reload atomicity.
- Add `postEphemeral` non-fatal-error test case.
- Add CLI `unbind` tests for no-ID / single / multi cases.
- Investigate SSM concurrent-write protection for AWS provider (preexisting, not this spec's doing but worth a follow-up issue).
- Document "admin discovery" ephemeral UX as an Open Question for v2.

### 2026-04-22 — Standards Gate (pre-implementation)

**Scanned**: `product-knowledge/standards/{architecture, security, egress-controls}.md`. No `philosophies/` directory. No `standards/index.yml` — all three markdown files treated as applicable.

**Standards Gate Report:**

| Standard | Section | Severity | Verdict |
|---|---|---|---|
| architecture.md | Provider contract is the API boundary | must | ✅ PASSES |
| architecture.md | Shared logic lives in common or its own package | must | ✅ PASSES |
| architecture.md | Portable artifacts, provider-specific state | must | ✅ PASSES |
| architecture.md | Channel abstraction over platform coupling | must | ✅ PASSES |
| architecture.md | **Agent Data Safety — spec must include a Data Safety section** | must | ❌ VIOLATION → fixed |
| architecture.md | CLI Conventions (Cobra patterns, agent resolution, error wrapping) | should | ✅ PASSES |
| architecture.md | **Interface Parity — every flag/command covered across CLI, JSON, MCP** | must | ⚠️ WARNING (preexisting gap, not introduced by this spec) |
| architecture.md | Config Format Boundary | should | ✅ PASSES (no new config files) |
| architecture.md | Module Structure (pkg/ vs internal/) | must | ✅ PASSES |
| architecture.md | Package Boundaries | must | ✅ PASSES |
| architecture.md | New code must not deepen Slack coupling | should | ✅ PASSES (`MultiBindingChannel` is platform-agnostic) |
| architecture.md | Testing Conventions | should | ✅ PASSES |
| security.md | Zero trust the AI agent | must | ✅ PASSES (no new agent privilege) |
| security.md | Immutable configuration (allowlist controlled by config, not agent) | must | ✅ PASSES (allowlist expansion uses same read-only mount) |
| security.md | Secrets protected at rest | must | ✅ PASSES (no new secrets; router uses existing `SLACK_BOT_TOKEN`) |
| security.md | **Channel allowlist is security-critical** | must | ✅ PASSES (cross-agent uniqueness check prevents silent overwrites; hash integrity monitoring still covers allowlist changes) |
| security.md | Enforcement Escalation by Provider | must | ✅ PASSES (no change) |
| egress-controls.md | Agent egress blocked by default, allowed by policy | must | ✅ PASSES (agent-container egress policy unchanged; the new `chat.postEphemeral` call is made by the router, which has its own egress profile already reaching `slack.com`) |

**Violations resolved:**

1. **Agent Data Safety section missing** (`architecture.md` § Agent Data Safety, rule 5) — fixed by adding a "Data Safety" section to `spec.md` §5.1 confirming no agent data directory is touched by bind/unbind/routing regeneration. Per the same standard's rule 3: "Refresh operations rebuild config, not data. RefreshAgent...may regenerate openclaw.json, env files, systemd units...They must never touch the data directory contents." This is exactly the code path this spec extends.

**Warnings (not blocking):**

1. **Interface Parity gap** (`architecture.md` § Interface Parity, rule 2: "Every new command must have an MCP tool...All three must be updated together"). Verified `internal/cmd/json_schema.go` contains NO entries for `channels.bind`, `channels.unbind`, or `channels.list` — these entries were never written when the CLI commands landed. **This gap predates the current spec and is preexisting drift.** Scope-increase discipline: this spec does not take on fixing the preexisting gap; it only updates the MCP tool descriptions (§2.6) for the behavior it changes. A follow-up issue tracks adding the missing JSON schema entries. Flagging rather than blocking.

**Gate decision**: proceed after the Data Safety section is added. Warnings recorded for follow-up.

### 2026-04-22 — Planning Session Complete

**Artifacts produced (all in this directory):**
- [README.md](README.md) — trace log with all decisions and review results
- [requirements.md](requirements.md) — 8 scenarios (incl. F/F'/F'' label + cross-agent cases), 9 success criteria, non-goals, constraints
- [plan.md](plan.md) — 9 phases, two-repo coordinated rollout
- [spec.md](spec.md) — detailed spec across 10 sections: data models, API changes (incl. breaking `Provider.UnbindChannel` signature), edge cases, test plan, data safety, observability, security, rollout + rollback, open questions, and coordinated-change summary

**Peripheral artifacts:**
- `product-knowledge/PROJECT_STATUS.md` — entry #27 added with implementation phase checklist

**Follow-ups tracked in spec §9 (not blocking):**
- Admin-discovery UX for unbound-channel ephemeral (v2 consideration)
- SSM concurrent-write protection for AWS provider (preexisting hardening)
- Atomic write for `routing.json` (preexisting hardening)
- JSON schema entries for `channels bind/unbind/list` commands (preexisting Interface Parity drift)
- `app_mention` event handling in unbound channels (v2 consideration)

**Handoff**: Ready for `/glados/implement-feature` invocation when implementation is scheduled. Implementation target: one PR in congaline (Phases 1-8) + one coordinated PR in terraform-provider-conga (Phase 9), sequenced per spec §8 Rollout.
