# Requirements — Delegation Routing

## Problem

Today every Conga agent is configured to talk to a **single primary model**
(plus an OpenClaw `fallbacks` chain — see `pkg/runtime/openclaw/openclaw-defaults.json`).
Feature #27 (Local Model Routing) extended this with the `agent.yaml` overlay
so an operator can point an agent at a self-hosted model like Qwen via LiteLLM.
**One agent ↔ one default model** is the prevailing mental model.

That mental model is now wrong for two reasons:

1. **Qwen alone is not a viable primary** for top-level agents. It lacks the
   reasoning + personality needed for the conversational tier. The user's
   experience: Qwen-as-primary agents feel less coherent and require more
   hand-holding than Opus-as-primary agents.
2. **Opus is too expensive to use for everything.** A lot of an agent's
   workload is mechanical (lookup, file ops, format work, data crunch,
   translation) — work where Qwen is *good enough* and *much cheaper*.

The right shape is a **two-tier delegation model** where Opus orchestrates
and Qwen executes, *both within a single conversation and across persistent
role-bound agents*.

## Goal

Define and implement a delegation architecture in Conga that supports:

- **Tier 1 — Ephemeral delegations**: an Opus-backed agent can delegate
  well-defined mechanical work to a cheaper model (Qwen by default), without
  the operator having to wire anything per task.
- **Tier 2 — Persistent role agents**: first-class Conga agents with a fixed
  role + model + personality + channel binding. Some roles want Opus (Code/Dev,
  Writing), others want Qwen (Ops, Data, Research).

The architecture **must** fit cleanly into the existing config taxonomy
(`product-knowledge/standards/config-taxonomy.md`) — agent runtime config
in `agent.yaml`, prompts in SOUL.md/AGENTS.md, infra in tfvars, policy in
`conga-policy.yaml`, secrets in the secrets store. No new top-level config
file or format.

## Non-Goals (for this feature)

- **Building a Bifrost-style cost-routing proxy.** That's still the deferred
  ROADMAP #22 work. Delegation here is expressed declaratively in the
  overlay, not enforced via a request-interception sidecar.
- **Mid-conversation `/model` switching changes.** The existing additive
  allowlist behavior from Feature #27 stays — operators can still `/model`
  into any model on the agent's allowlist.
- **Channel × runtime compat changes.** Telegram stays Hermes-only; Slack
  stays both. Delegation is orthogonal to channel routing.
- **Spawning Conga agents from inside another agent's conversation.**
  Persistent agents are still created via `conga admin add-user` / `add-team`
  (or tfvars on AWS). Opus does not have CLI superpowers.
- **Cross-provider model routing logic in the CLI.** Routing decisions
  ("when should Opus delegate to Qwen?") belong to the runtime
  (OpenClaw/Hermes), not Conga. Conga's job is to make the right models
  *available* to the runtime via the overlay.

## Users & Use Cases

- **Operator deploying a new agent** wants to say "this agent is a Code/Dev
  agent — give it Opus as primary, Qwen as the cheap delegator, hook it up
  to the right Slack channel" without writing three config files.
- **Operator dogfooding** wants to bind their personal `aaron` agent so that
  Opus drives the conversation but lookups, file digests, and CSV crunching
  use Qwen — observable in the bill (fewer Anthropic tokens).
- **Operator running team agents** wants a persistent `ops` agent on Qwen
  that handles monitoring/health questions in the #ops Slack channel, and
  a persistent `code-review` agent on Opus that lives in #code-review.

## Success Criteria

1. **Tier 2 (persistent role agents) is a first-class Conga concept.**
   - There is a documented set of canonical roles (initial set: Ops, Data,
     Research, Code/Dev, Writing).
   - Each role ships with a default `agent.yaml` + behavior templates
     (SOUL.md, AGENTS.md) under `agents/_defaults/<runtime>/<role>/` or an
     equivalent location chosen during planning.
   - `conga admin add-user --role <name>` (or equivalent) provisions an
     agent with the role's defaults applied. No copy-paste of overlay YAML.
2. **Tier 1 (ephemeral delegation) is expressible in `agent.yaml`.**
   - The overlay schema gains a way to declare *secondary* models the
     primary can delegate to. Naming, key, and semantics are open during
     planning (see Open Questions).
   - The schema bump is **version: 2**, with explicit handling for both v1
     and v2 in the loader. No silent extension of v1.
   - At minimum, OpenClaw's native delegation mechanism (whichever upstream
     concept maps best — `models.providers`, tool-call routing, etc.) is
     populated from the overlay so the runtime *can* delegate. Whether it
     *does* delegate is the runtime's call.
3. **No regression on Feature #27.** A v1 overlay continues to load and
   produce the same `openclaw.json` it does today. The Spark-Qwen production
   agent on AWS is unaffected.
4. **Provider parity.** Local, remote, AWS all support the new overlay
   shape — no provider-specific behavior. (Architecture standard: Interface
   Parity, see `product-knowledge/standards/architecture.md`.)
5. **Channel × Runtime compat respected.** A Hermes-only role (if any) must
   refuse a Telegram binding only when the existing matrix says so.
6. **Egress.** Multi-model agents need both endpoints in their egress
   allowlist. The provisioning flow either auto-derives this from the
   overlay or fails closed with a clear error pointing at tfvars / policy
   (decided during planning).
7. **Tests.** Schema validation tests cover v1→v2 migration, unknown keys,
   missing secondary model secret, invalid secondary `base_url`. The
   live-tested AWS agent is re-verified after the overlay round-trip.
8. **Docs.** `config-taxonomy.md` updated with the role concept and the
   delegation overlay shape. CLAUDE.md updated with the canonical role
   list and a one-liner on the delegation model. The `_example/agent.yaml`
   updated to show a v2 document with a delegated secondary model.

## Out-of-Scope Items Tracked for Follow-Up

- **Token budgeting / cost caps per role.** The `limits:` reserved key in
  the overlay anticipates this — leave reserved for a future spec.
- **Cross-agent invocation as MCP tools.** ("Code/Dev calls Research") —
  promising follow-on if the upstream MCP work matures, but not in scope.
- **Anthropic Task-tool-style nested spawning inside one runtime container.**
  Depends entirely on upstream OpenClaw / Hermes capability; document the
  reality during plan.md.
- **Routing observability** (which delegations happened, what was spent).
  Belongs with #22 Bifrost.

## Open Questions Carried Into plan.md

1. Where exactly does OpenClaw upstream support in-runtime delegation today?
   `models.providers` array? A tool-call config? Something else? (This is
   the hinge question — Tier 1 implementability depends on it.)
2. Is "role" a new top-level concept (`role: code-dev` in agent.yaml) or
   purely a shorthand that the CLI expands into existing fields (model +
   prompt files)?
3. Naming of the two tiers ("sub-agent / task-agent" vs "delegation /
   persona" vs "worker / agent") — settle in plan.md before spec.
4. Should the unified-vs-distinct question (user-deferred) be resolved as
   "two distinct concepts: ephemeral lives in `model.delegates`, persistent
   lives as a Conga agent" — or is there value in a unified abstraction?
