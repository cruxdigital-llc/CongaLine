# Plan — Delegation Routing

This is a **high-level plan**, not yet a spec. It frames the design space,
makes the load-bearing decisions, and surfaces the open questions the spec
must close.

## TL;DR

- **The two tiers are two distinct concepts.** They live in different layers
  of the existing config taxonomy. Don't unify them.
- **Tier 1 (ephemeral delegation) is a runtime concern.** Conga's job is to
  make the secondary model *available* in `agent.yaml` (new key under
  `model:` at schema v2); the runtime decides when to delegate.
- **Tier 2 (persistent role agents) is a CLI + defaults concern.** Add a
  canonical role catalog (Ops, Data, Research, Code/Dev, Writing) under
  `agents/_defaults/<runtime>/<role>/`, plus `conga admin add-user --role`.
- **No new YAML files, no new providers, no new runtime.** Everything slots
  into existing extension points.

## Approach

### Phase 1 — Resolve the upstream capability question (precursor)

Before locking the overlay shape, we need ground truth from OpenClaw upstream
(and Hermes) on **how in-runtime delegation actually works today**:

- Does OpenClaw expose a "delegate to model X" tool the orchestrator can
  call? If so, what's the config knob — `models.providers` array, an
  explicit `delegates:` config block, a per-tool model override?
- The existing `agents.defaults.model.fallbacks` chain is a *failover*
  concept, not a *delegation* concept. We need to verify whether they're
  the same plumbing or different.
- Hermes — same questions, separate answers.

Deliverable: a short `upstream-capability.md` in this spec directory
documenting findings. Without this, Tier 1's implementation is speculative.

**Architect's note:** if upstream doesn't yet support delegation, Tier 1
becomes a "schema reservation + roadmap pointer" exercise rather than an
implementation. That's still useful, but the scope changes materially.

### Phase 2 — Overlay schema v2 (Tier 1 wiring)

Extend `pkg/runtime/overlay.go`'s `ModelOverlay` to support secondary models.
The proposed shape (subject to Phase 1 findings):

```yaml
version: 2
model:
  provider: anthropic
  name: claude-opus-4-7
  # ...existing v1 fields...

  delegates:                    # NEW in v2 — list of secondary models
    - id: cheap-helper          # local label used by the runtime
      provider: openai          # any provider supported by overlay v1
      name: qwen-2.5-72b-instruct
      base_url: https://litellm.lan/v1
      # purpose: optional hint to the runtime (lookup, file-ops, data, ...)
      #         Open question: does the runtime want this hint, or is it
      #         pure "available; orchestrator chooses"?
```

Schema bump from v1 → v2 is a **deliberate breaking change for documents
that opt in**; existing v1 documents continue to work unchanged (the loader
keeps both code paths). This is exactly the migration mechanism Feature #27
designed for.

**Architect's note:** `delegates` is a list, not a map, to keep ordering
deterministic. Each item carries its own provider/name/base_url because we
might delegate to multiple different cheap models (Qwen for text, Llama
Vision for image work, etc.).

**Per-overlay strict-key parsing means** adding `delegates:` to a v1
document fails loudly today — which is correct. v2 unlocks it.

### Phase 3 — Role catalog (Tier 2 wiring)

Add a canonical role concept *without* introducing a new top-level config
file. Two routes considered:

**Route A — Role as a CLI shorthand (preferred).**
- `agents/_defaults/<runtime>/<role>/` holds role-specific defaults
  (SOUL.md, AGENTS.md, USER.md.tmpl, agent.yaml).
- `conga admin add-user --role code-dev --runtime openclaw ...` copies the
  defaults into `agents/<name>/` on first provision. No new field on
  `AgentConfig`; the role is implicit in the prompt files + overlay model
  choice.
- Pros: zero schema change. Roles are just curated overlay packages.
  Trivially extendable — add a new directory, you have a new role.
- Cons: no introspection. `conga agent show <name>` can't say "this is a
  code-dev agent" without re-deriving it from file contents.

**Route B — Role as a first-class field on `AgentConfig` + persisted in
agent JSON / SSM.**
- `Role string` added to `AgentConfig` (+ JSON tag, + SSM serialization).
- Provider materializes role into `~/.conga/agents/<name>.json` so
  introspection works.
- Pros: queryable, displayable in status, drives provisioning logic
  cleanly.
- Cons: schema change touches all three providers. Backwards-compat
  requires defaulting `Role` to `""` for existing agents. Adds a concept
  that overlaps with `Type` (user/team).

**Recommendation: Route A first, with the understanding that we may
revisit Route B if introspection becomes important.** Roles ship as overlay
packages; `conga admin add-user --role X` is sugar over "copy these
defaults into the agent's overlay dir then proceed with normal
provisioning."

**PM note:** Route A protects MVP scope. Operators get the canonical roles;
internals stay simple. If Aaron wants Route B in 2 months, it's a small
follow-up.

### Phase 4 — Canonical role definitions

The five roles split cleanly by primary model:

| Role | Primary | Delegates to | Channels | AGENTS.md emphasis |
|---|---|---|---|---|
| Ops | Qwen | (none — Qwen direct) | #ops, DMs | Monitoring, infra checks, status reports |
| Data | Qwen | (none) | #data, scheduled | Reporting, CSV, metrics |
| Research | Qwen | (none) | #research, DMs | Web research, competitive intel |
| Code/Dev | Opus | Qwen (file ops, lookups) | #code-review, #engineering | Architecture, code review, debugging |
| Writing | Opus | Qwen (translation, formatting) | #writing, DMs | Drafts, edits, content strategy |

**The asymmetry is the point.** Qwen-roles are simple mechanical agents —
no delegation needed. Opus-roles get delegation to Qwen because that's
where the cost wins live.

This table lives in `config-taxonomy.md` (extended) and the role default
directories ship pre-configured.

**PM note:** the initial set is five roles. Adding a sixth ("Reviewer",
"Researcher-Deep", whatever) is purely additive — drop in a new defaults
directory.

### Phase 5 — Egress allowlist auto-derivation

A multi-model overlay implies multiple upstream endpoints. Today's egress
allowlist (`tfvars` on AWS, `conga-policy.yaml` everywhere else) is
operator-authored. We have three options:

1. **Operator-authored, status quo** — operator lists all endpoints. We
   document the requirement in `agent.yaml.example` and let provisioning
   succeed; egress proxy denies at runtime if the operator missed an
   endpoint. **Failure mode is observable; provisioning stays simple.**
2. **Auto-derive at provision time** — Conga reads the overlay, computes
   the set of `base_url` hosts (primary + all delegates), and either
   appends them to the agent's egress allowlist OR refuses to provision
   if any are missing.
3. **Hybrid: auto-derive a list and emit a warning** — like (1) but
   surface the gap during `conga admin add-user` rather than at first
   request.

**Recommendation: Option 3.** Auto-derivation across config layers is
brittle (which layer wins?), but visibility at provision time is cheap
and useful. The egress allowlist remains operator-authored — Conga just
helps them see what's needed.

**QA note:** test fixtures need a multi-model overlay where one delegate
endpoint is missing from egress. The expected behavior is a clear warning
at provision and a clear 403 at runtime — not a silent failure or a
mysterious model timeout.

### Phase 6 — Test surface

Per QA, the new test surface is:
- `pkg/runtime/overlay_test.go` — v2 parse, unknown-key rejection,
  `delegates` validation (URL shape, required fields, casing), v1 still
  parses unchanged.
- `pkg/runtime/openclaw/config_test.go` — `applyModelOverlay` on a v2
  overlay produces the expected `openclaw.json` (whatever the upstream
  concept turns out to be after Phase 1).
- `internal/cmd/...` integration test for `add-user --role X` — provisions
  an agent with the role's defaults applied; `agent.yaml` exists in the
  agent's overlay dir.
- Regression: existing Feature #27 tests must pass unchanged.

### Phase 7 — Docs

- `product-knowledge/standards/config-taxonomy.md` — extend the runtime
  overlay row with "delegates and role packages."
- `CLAUDE.md` — add a section: "Delegation Model" with the five-role
  catalog and one paragraph on the v2 overlay.
- `_example/agent.yaml.example` — show a v2 document with delegates;
  carry a comment block noting v1 documents still work.
- `agents/_defaults/<runtime>/<role>/README.md` (light) for each role —
  one paragraph on what the role is for, what model it ships with, what
  channels to typically bind.

## Key Design Decisions

### Decision 1 — Tiers are distinct concepts

User deferred this. **Architect's call: keep them distinct.**

Reasoning:
- They live in different layers of the taxonomy. Tier 1 (delegation) is
  a *runtime* concern — it shapes how an agent's runtime config consumes
  its model list. Tier 2 (persistent role) is a *provisioning* concern —
  it shapes how an operator goes from "I want an Ops agent" to a running
  container.
- Unifying them would force every ephemeral delegation to look like a
  Conga agent (container, port, channel binding) — orders of magnitude
  too much overhead for "run this Qwen call as part of Opus's
  conversation."
- The mental model is cleaner with two concepts: **"models my agent can
  call" (runtime config) vs "agents in my fleet" (provisioning config).**

If Aaron pushes back on this during plan review, the fallback is to call
out the unification as a future experiment but proceed with the split.

### Decision 2 — Terminology

User deferred to GLaDOS. The contenders were "sub-agent / task-agent",
"delegation / persona", "worker / agent."

**Recommendation: "delegate / role agent."**

- **"Delegate"** for Tier 1. Lines up with how the overlay key reads
  (`model.delegates`). Avoids "sub-agent" (collides with the Anthropic
  Task tool / Claude Code sub-agent concept) and "worker" (implies a
  separate process, which it isn't).
- **"Role agent"** for Tier 2. Lines up with the role catalog (Ops, Data,
  Research, Code/Dev, Writing) and the proposed CLI flag (`--role`). Avoids
  "task agent" (the Anthropic conflict again) and "persona" (already used
  in GLaDOS spec workflows — overload risk).

**PM note:** "delegate" reads naturally as a verb too ("Opus delegates the
lookup to Qwen"), which helps when explaining the model in conversation.

### Decision 3 — The Opus delegation mechanism is the runtime's job, not Conga's

Conga's contribution to Tier 1 is **declarative**:
- The overlay says "this agent has a primary Opus and a Qwen delegate."
- The runtime config generator translates that into whatever the upstream
  runtime understands (TBD in Phase 1).
- The runtime decides when to actually delegate, based on its own
  heuristics, tool definitions, or user-explicit invocation.

Conga **does not**:
- Run a routing proxy between the agent and the model API.
- Inspect requests / responses to make routing decisions.
- Implement Bifrost.

That keeps the blast radius tight and the architecture honest. Bifrost
(ROADMAP #22) remains a separate, future feature.

### Decision 4 — Role packages, not role fields

Per Phase 3 Route A. Roles are curated bundles of defaults living in
`agents/_defaults/<runtime>/<role>/`. The CLI flag `--role` is sugar over
"copy these defaults to the agent's overlay dir before provisioning."

This means **Conga's data model doesn't grow.** `AgentConfig.Role` does
not exist. Role is encoded in the prompt files + overlay model choice,
which is exactly where personality + behavior already live. Future
introspection can be a follow-up if it earns its place.

## Plug-In Points (concrete files affected)

| File | Change |
|---|---|
| `pkg/runtime/overlay.go` | `ModelOverlay` gains `Delegates []DelegateOverlay`; new `CurrentOverlaySchemaVersion = 2`; both versions parse |
| `pkg/runtime/openclaw/config.go` | `applyModelOverlay` extended to emit the upstream delegation shape (Phase 1 deliverable defines this) |
| `pkg/runtime/hermes/config.go` | Same, for Hermes (deferred if Hermes lacks the upstream concept) |
| `agents/_defaults/<runtime>/<role>/` | New directories for each role with SOUL.md / AGENTS.md / USER.md.tmpl / agent.yaml |
| `internal/cmd/admin_provision.go` | `--role` flag handling — copies role defaults into agent overlay dir before normal provision |
| `agents/_example/agent.yaml.example` | Show v2 doc with delegates |
| `product-knowledge/standards/config-taxonomy.md` | Extend overlay row + add worked example #5 (role package) |
| `CLAUDE.md` | New "Delegation Model" section |

No new packages. No new providers. No new runtime.

## Open Questions To Close In spec.md

1. **Upstream truth (Phase 1)**: what is OpenClaw's actual mechanism for
   in-runtime model delegation as of `v2026.5.18`? Hermes equivalent?
2. **Hint or no hint**: does `delegates[].purpose` (or an equivalent
   semantic label) carry weight in upstream, or is it pure documentation?
3. **Egress strategy**: confirm Option 3 (auto-derive + warn) over (1)
   pure operator-authored or (2) auto-derive + enforce.
4. **Role catalog finality**: lock the initial five (Ops, Data, Research,
   Code/Dev, Writing) or add/remove? Anyone get omitted?
5. **`AgentConfig.Role` field**: stay in Route A or upgrade to Route B if
   Aaron wants introspection? Decide before implementation.
6. **Hermes parity**: if Hermes can't yet support delegation, what's the
   degraded-mode behavior — refuse to load a v2 overlay against Hermes?
   Load it and ignore `delegates`? Refuse with a clear "v2 + delegates is
   OpenClaw-only" error?
7. **Channel × Runtime + Role × Runtime**: a Telegram binding + Hermes
   role is fine today. Does a v2-overlay-with-delegates + Hermes role
   need a new compat-matrix entry?

## Risks

- **Phase 1 (upstream capability) could invalidate the schema shape.** We
  may need to ship Tier 2 first and defer Tier 1 to a follow-up if
  OpenClaw doesn't yet expose what we need.
- **Operators may try to use a v2 overlay with Hermes** and hit a
  confusing error. Mitigation: clear error message + docs.
- **The role defaults will drift** as OpenClaw evolves (same risk noted
  in `PROJECT_STATUS.md` for the existing user/team defaults). Mitigation:
  the existing `/glados:recombobulate` observation already covers this.
- **Naming sticks.** "Delegate" and "role agent" need to read clearly to
  operators. PM should sanity-check before spec.

## Handoff to spec.md

When `/glados:spec-feature` picks this up, it should:
1. Run Phase 1 (upstream capability check) and write
   `upstream-capability.md` first.
2. Close the seven open questions above.
3. Produce a phased implementation contract — schema, generator, defaults,
   CLI, docs, tests — with exact file paths and acceptance criteria per
   phase.
4. Run a pre-implementation standards gate (architecture parity, schema
   versioning, secret handling).
5. Get persona sign-off (Architect + PM + QA) before any code lands.
