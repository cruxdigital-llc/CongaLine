<!--
GLaDOS-MANAGED DOCUMENT
Last Updated: 2026-05-19
To modify: Edit directly. This is the single source of truth for where per-agent
configuration lives. Update when introducing a new per-agent concern; do NOT
create a new config file/format without consulting the decision rule below.
-->

# Per-Agent Configuration Taxonomy

> **Conga Line is an open-source IaC tool with three runtime environments** (local Docker, remote SSH, AWS). Configuration touching agents spans several layers by design. This document is the canonical map: *for any new per-agent configuration concern, where does it go?*
>
> The goal: **a contributor can pick the right home in under 60 seconds** without reading source. Avoid introducing new files or formats. Extend in place.

## The taxonomy

| Layer | Concern | Location | Format | Provider scope | Authored by |
|---|---|---|---|---|---|
| **Infrastructure** | Agent existence, gateway port, egress allowlist (incl. private IPs), channel bindings, secret values, host resources | `terraform/environments/<env>/terraform.tfvars` `agents = {}` map | HCL (Terraform) | AWS (declarative). Local/remote use CLI flags (`conga admin add-user`). | Operator. Gitignored — only `.example` is committed. |
| **Cluster policy** | Egress allow/deny lists, routing rules, posture (enforce/validate), drift detection | `~/.conga/conga-policy.yaml` with per-agent overrides under `agents.<name>.*` | YAML | All providers | Operator. Lifecycle via `conga policy {validate,deploy,drift}`. Egress overrides are **additive** with the global lists (see `standards/egress-controls.md` — *Global + agent policies are additive*); routing and posture overrides still replace. |
| **Runtime overlay** | Model (provider, name, base_url), prompts (SOUL/AGENTS/USER), future: memory, tools, limits, multi-modal model refs, fallback chains | `agents/<name>/agent.yaml` + `agents/<name>/*.md` | YAML + Markdown | All providers (provider-agnostic; same files produce same runtime config on local/remote/AWS) | Operator. Gitignored — only `agents/_example/` is committed. |
| **Runtime persistence** | Identity (name, type, runtime choice, allocated port, channel binding state) | `~/.conga/agents/<name>.json` (local) / `/opt/conga/agents/<name>.json` (remote) / SSM `/conga/agents/<name>` (AWS) | JSON | Per-provider | **Materialized by the provider** at provision time, not hand-edited. |
| **Secrets** | API keys, OAuth tokens, channel bot tokens | Files mode 0400 (`~/.conga/secrets/agents/<name>/<key>` on local/remote) or AWS Secrets Manager (`conga/agents/<name>/<key>` on AWS) | Native | Per-provider | Operator. Authored via tfvars (AWS) or `conga secrets set` (local/remote). Never in git. |

## Decision rule — answer these in order

When adding a new per-agent concern, walk this list top-to-bottom and stop at the first **yes**:

1. **Does it affect AWS infrastructure** — security groups, NACLs, IAM, EBS, SSM, EC2 sizing, Slack app routing topology, the agent's existence on the host? → **Infrastructure (tfvars).**
2. **Is it a security/policy decision** that has a global default with per-agent override semantics, and benefits from `validate/deploy/drift` lifecycle? → **Cluster policy (`conga-policy.yaml`).**
3. **Does the agent's runtime (OpenClaw, Hermes) consume it directly** to generate `openclaw.json` / `config.yaml`? Is it operator-authored and provider-agnostic? → **Runtime overlay (`agent.yaml`).**
4. **Is it computed by the provider at provision/refresh time** rather than authored by an operator? → **Runtime persistence (per-agent JSON / SSM).** You almost never extend this on purpose; it grows with new fields on `provider.AgentConfig`.
5. **Is it a credential value?** → **Secrets store.**

If two layers seem to apply, default to the lower number in the list — infrastructure beats policy beats overlay. The exception: if a concern is *both* a policy decision *and* operator-authored runtime config (e.g. "this agent uses model X, and only this model"), it goes in the overlay if the runtime is the primary consumer, in policy if cluster-wide enforcement is the primary consumer.

## Anti-patterns (never do these)

- ❌ **Runtime config (model, prompts, tools) in tfvars.** Breaks portability — `agent.yaml` must produce the same behavior on local/remote/AWS without invoking terraform.
- ❌ **Infrastructure config (ports, egress IPs, SSH host) in `agent.yaml`.** The CLI/terraform provisioning flow won't see these and the rendered config can disagree with the actual host state.
- ❌ **Secret VALUES in `agent.yaml`.** OpenClaw issue #9627 — secrets in disk-resident config files. Always go through the secrets store; reference by name (e.g. `openai-api-key`) and let `SecretNameToEnvVar` inject `OPENAI_API_KEY`.
- ❌ **A new YAML file per concern** (`tools.yaml`, `memory.yaml`, ...). Extend `agent.yaml` with a new versioned top-level key instead. The schema is designed to absorb growth (see `specs/2026-05-19_feature_local-model-routing/spec.md`).
- ❌ **Editing files under `~/.conga/agents/<name>.json` by hand.** That file is materialized by the provider; manual edits get clobbered on the next refresh. Use the provider's API or its source-of-truth (tfvars on AWS, CLI on local/remote).
- ❌ **Committing real agent definitions.** Only `agents/_example/`, `terraform.tfvars.example`, and `backend.tf.example` go in git. The gitignore already enforces this; do not bypass.
- ❌ **Changing the location or format of an existing layer** without a deprecation cycle. This document is the contract; the cost of moving is high.

## Worked examples

### Example 1: "I want agent X to use a custom LLM endpoint"
Walk the rule:
1. Affects AWS infra? **No.** The endpoint URL is application-layer, but its *reachability* (egress allowlist) IS infra → that part goes in tfvars `agents.<name>.egress_allowed_domains`. The URL itself does not.
2. Security/policy decision? **No** (the choice of *which* model is not a policy decision; the choice of which models are *allowed* would be).
3. Runtime-consumed, operator-authored, provider-agnostic? **Yes.** → **`agents/<name>/agent.yaml`** with `model: { provider, name, base_url }`.
4. (also) Credential? **Yes**, the API key → secrets store (`openai-api-key`). The `base_url` and `name` go in overlay; the key goes in secrets. Two homes, deliberately.

### Example 2: "I want to restrict agent X to a single Slack channel"
Walk the rule:
1. Affects AWS infra (channel bindings drive the router's `routing.json` and the security group's allowed messaging endpoints)? **Yes.** → **tfvars `channels.slack.bindings`**, not the overlay.

### Example 3: "I want a per-agent token budget cap"
Walk the rule:
1. Affects AWS infra? **No.**
2. Security/policy decision with global default? Possibly — if the cap is a uniform policy with per-agent overrides, → `conga-policy.yaml`. If it's purely per-agent runtime config without policy lifecycle, → `agent.yaml`. Both could be argued; pick `agent.yaml` for now (simpler), revisit if cost-policy enforcement becomes a thing (likely with Bifrost).

### Example 4: "I want agent X to use a custom prompt"
Walk the rule:
1. Affects AWS infra? No.
2. Policy decision? No.
3. Runtime-consumed, operator-authored, provider-agnostic? Yes. → **`agents/<name>/SOUL.md`** (or `AGENTS.md`, `USER.md`). Already the established pattern; don't duplicate in `agent.yaml`.

## Why three layers (overlay vs policy vs infra) instead of one?

This was reviewed in the architect deep-dive (`specs/2026-05-19_feature_local-model-routing/README.md`). Summary:

- **Infrastructure** is AWS-specific and terraform-driven; collapsing it into a portable layer would lose the declarative AWS resource model.
- **Cluster policy** has unique lifecycle (validate/deploy/drift) that runtime overlay doesn't need.
- **Runtime overlay** is hand-edited per-agent without any global default; collapsing into policy would force operators to express "this agent uses this model" as a per-agent override of a non-existent global, which is awkward.

Each layer earns its place. The cost is more places to look; the taxonomy doc is the compensation.

## Extending this document

When adding a new per-agent concern:
1. Update the **decision rule** if the new concern doesn't fit cleanly (rare — usually it slots into runtime overlay).
2. Update the **taxonomy table** with the new column entry if you're adding a new layer (very rare; requires deliberate architectural decision).
3. Add a **worked example** for non-obvious concerns.
4. Cross-link from the relevant spec.

Do **not** create a "config-taxonomy-v2.md" or similar. Update this file in place; git history preserves the rationale.
