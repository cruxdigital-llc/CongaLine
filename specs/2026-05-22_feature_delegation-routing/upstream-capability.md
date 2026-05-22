# Upstream Capability Check — Phase 1

**Question (from plan.md)**: How do OpenClaw `v2026.5.18` and Hermes support
in-runtime delegation of work to a secondary model, autonomously, without
user-driven `/model` switching?

**Verdict**: ✅ **Both runtimes have native, mature support.** Our overlay
becomes a thin translation layer over existing upstream config.

**Critical follow-on**: ⚠ Upstream uses the word **"delegate"** for a
different, organizational concept. We must rename our Tier 1 from
"delegate" → **"subagent"** to align with the upstream terminology used
by both runtimes. See "Naming collision" below.

## OpenClaw v2026.5.18

Source docs (in `github.com/openclaw/openclaw` at tag `v2026.5.18`):

- `docs/concepts/parallel-specialist-lanes.md`
- `docs/concepts/delegate-architecture.md` (different concept, see below)
- `docs/tools/subagents.md`

### Mechanism — `sessions_spawn` + `agents.defaults.subagents`

OpenClaw exposes a `sessions_spawn` **tool** the orchestrator agent can
call to launch a background sub-agent run with its own session, isolated
context (default), and optional model override. Completion is
push-based — the sub-agent's announce message lands back in the
requester chat channel.

The runtime-level config knobs live under `agents.defaults.subagents`
(and per-agent `agents.list[].subagents`):

```json5
{
  agents: {
    defaults: {
      subagents: {
        model: "openai/qwen-2.5-72b-instruct",  // default model for sub-agent runs
        delegationMode: "prefer",                // or "suggest" (default)
        maxConcurrent: 4,                        // concurrency cap
        // thinking, runTimeoutSeconds, allowAgents, ...
      },
    },
    list: [
      { id: "code-dev", subagents: { delegationMode: "prefer" } },
    ],
  },
}
```

Spawn-time overrides via `sessions_spawn` parameters: `model`, `thinking`,
`runTimeoutSeconds`, `context: "isolated" | "fork"`, `mode: "run" |
"session"`, `cleanup`, `sandbox`. The orchestrator picks per-call.

Tool policy gate: `sessions_spawn` is exposed by the `coding` and `full`
tool profiles. The `messaging` profile does **not** expose it — would need
`tools.alsoAllow: ["sessions_spawn", "sessions_yield", "subagents"]`.

### What "delegate" means upstream

`docs/concepts/delegate-architecture.md` defines a **"delegate"** as an
OpenClaw agent **with its own identity** acting on behalf of humans in an
organization. Tiers (Read-Only Draft → Send on Behalf → Proactive),
identity-provider hookup (Microsoft 365, Google Workspace), `AGENTS.md`
standing orders, etc.

**This is org-identity at the agent level, not in-runtime model
delegation.** It's closer to our existing team-agent concept than to our
Tier 1.

### Implication for our Tier 1

The natural translation is:

- Operator declares a sub-agent model in `agent.yaml` under a new
  top-level `subagents:` block (matches OpenClaw's config nesting).
- Generator emits `agents.defaults.subagents.{model, delegationMode,
  maxConcurrent}` in `openclaw.json`.
- Operator does NOT need to author the `sessions_spawn` tool calls —
  that's the runtime's job. The orchestrator agent (running Opus) sees
  the tool, sees the prompt nudge from `delegationMode: prefer`, and
  decides per turn whether to spawn.
- The Anthropic Task-tool analogy from Aaron's brief maps cleanly:
  `sessions_spawn` IS OpenClaw's Task tool.

## Hermes Agent

Source (in `github.com/NousResearch/hermes-agent` at `main`):

- `website/docs/user-guide/features/delegation.md`
- `cli-config.yaml.example` (lines ~867–888)
- `tools/delegate_tool.py`

### Mechanism — `delegate_task` tool + `delegation:` config

Hermes exposes a `delegate_task` tool. Single task or batch (up to 3
concurrent by default). Subagents start with **completely fresh
conversation** — the parent must pass everything the child needs as
`goal` + `context`.

Config (top-level `delegation:` block in `cli-config.yaml`):

```yaml
delegation:
  max_iterations: 50
  max_concurrent_children: 3        # default 3, no hard ceiling
  max_spawn_depth: 1                # 1 = flat; up to 3 for nested orchestrators
  orchestrator_enabled: true
  subagent_auto_approve: false
  inherit_mcp_toolsets: true
  model: "google/gemini-3-flash-preview"   # subagent model override; empty = inherit parent
  provider: "openrouter"                     # subagent provider override
```

Supported providers in Hermes' `delegation.provider`: `openrouter`, `nous`,
`zai`, `kimi-coding`, `minimax`. Notably **does not match our overlay's
`openai`/`ollama` list directly** — Hermes uses an enum of named provider
adapters, where ours uses provider type + base_url.

### Implication for Hermes parity

Our overlay's `subagents.model.{provider,name,base_url}` shape maps
cleanly to OpenClaw. For Hermes, we'd translate `provider: openai` +
`base_url: https://litellm.lan/v1` into Hermes' `delegation.provider:
openrouter` (or one of its named adapters) — likely with a small lookup
table.

**Pragmatic call**: Hermes' `delegation` block accepts a generic `model`
field. If we generate `delegation.model: <provider>/<name>` and skip
`delegation.provider` (leaving Hermes to resolve), it Just Works on
overlays whose `subagents.model.provider == ollama` (Hermes inherits the
parent's LiteLLM/Ollama setup). For `openai`-style providers pointing at
a non-OpenRouter endpoint, Hermes support is degraded — document this
as a known gap, not a blocker.

QA note: spec should add a test that v2 overlay against Hermes
**doesn't crash** even if the resulting `delegation:` config is
incomplete. Loud failure > silent wrong.

## Naming collision

The plan.md proposal was to call Tier 1 a **"delegate"** in our docs and
overlay (e.g. `model.delegates[]`).

**This conflicts with OpenClaw upstream**, where:

- **"delegate"** = an agent with its own identity acting on behalf of
  humans (`docs/concepts/delegate-architecture.md`). Tier 1/2/3 of *that*
  feature is about read-only vs send-on-behalf vs proactive.
- **"subagent"** = ephemeral in-runtime spawned agent run, isolated
  context, optional model override (`docs/tools/subagents.md`).

The collision is exact and unrecoverable — if we adopt "delegate" for
our Tier 1, every Conga doc that says "set up a delegate" reads
ambiguously next to OpenClaw's own docs.

Hermes also uses the word "delegate" — but as a verb on the tool
(`delegate_task`); their docs **headline** as "Subagent Delegation" (see
`website/docs/user-guide/features/delegation.md`).

**Recommendation: rename our Tier 1 from "delegate" → "subagent".**

| Concept | Old name (plan.md) | New name (post-Phase-1) | Why |
|---|---|---|---|
| Tier 1 (ephemeral, in-runtime) | "delegate" / `model.delegates[]` | **"subagent"** / `subagents:` | Matches both upstream runtimes' terminology; avoids OpenClaw's org-identity "delegate" |
| Tier 2 (persistent, model-bound) | "role agent" | **"role agent"** (unchanged) | Still distinct from OpenClaw's "delegate" (which is a specific Tier-3-proactive org configuration). Our role agents are simpler and broader. |

**Aaron's previous answer locked "delegate"** — but that was before this
upstream check. He needs to re-confirm the rename. Flagged as an open
question for the user.

## Shape of the v2 overlay (proposed, pending Aaron's rename confirm)

```yaml
version: 2

model:
  provider: anthropic
  name: claude-opus-4-7
  # ... v1 fields stay as-is ...

# NEW in v2 — top-level peer of `model:`, matching OpenClaw's config nesting.
# Empty / absent = no sub-agent model configured; runtime falls back to its
# own defaults (which inherit the parent model for OpenClaw).
subagents:
  # Required when the block is present. Same provider/name/base_url shape as
  # `model.*` for consistency.
  model:
    provider: openai
    name: qwen-2.5-72b-instruct
    base_url: https://litellm.lan/v1
    # context_window, max_tokens optional, same semantics as v1 model.*

  # Optional behavior knobs. Defaults come from the runtime when omitted.
  delegation_mode: prefer       # OpenClaw: "prefer" | "suggest" (default). Hermes: ignored.
  max_concurrent: 4             # OpenClaw: maxConcurrent. Hermes: max_concurrent_children.
  max_spawn_depth: 1            # Hermes: max_spawn_depth. OpenClaw: not a knob; nesting is implicit.
```

**Why a single sub-agent model, not a list:** OpenClaw's
`agents.defaults.subagents.model` is a single string. Hermes'
`delegation.model` is a single string. Neither runtime has a "set of
secondary models" concept at the defaults level — the orchestrator
overrides per-spawn via the tool call. So our overlay matches: one
declared sub-agent model, with runtime-level per-spawn overrides
available to the orchestrator at runtime via the tool.

If a future use case demands multiple named sub-agent models
(e.g. cheap-text vs cheap-vision), bump to schema v3 and add a list.
Don't pre-build it on v2.

## Open questions resolved by Phase 1

- ✅ Q1 (plan.md): "Does OpenClaw expose an in-runtime delegation
  mechanism?" — **Yes**, `sessions_spawn` tool + `agents.defaults.subagents`
  config. Hermes equivalent: `delegate_task` tool + `delegation:` config.
- ✅ Schema shape: **single sub-agent model**, not a list. Top-level
  `subagents:` key, peer of `model:`.
- ✅ Hermes parity: degraded but non-crashing. Document the provider-enum
  mismatch.

## Open questions still standing

- ⚠ Q-NEW: confirm rename "delegate" → "subagent" with Aaron (raised in
  this doc, not yet asked).
- Q2 (plan.md): "Hint or no hint" — OpenClaw's `delegationMode` is a
  *prompt nudge*, not a hint to the orchestrator about which delegate to
  use. We can expose it in the overlay; no further hint mechanism needed.
- Q5 (plan.md): `AgentConfig.Role` field — still pending; Route A
  (overlay packages only) remains the recommendation.
- Q6 (plan.md): Hermes degraded-mode behavior — answered here: don't
  block, document the gap.
- Q7 (plan.md): Channel × Runtime + Role × Runtime matrix update — to
  decide in spec.md.
