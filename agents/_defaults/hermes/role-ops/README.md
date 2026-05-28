# role-ops (Hermes)

Operations agent. DM-driven (`type: user`). Suggested for monitoring queries, infra status checks, and short health reports.

## Important: model selection

The OpenClaw sibling of this role uses an `agent.yaml` `model:` block to route through a cheap Qwen endpoint. **On Hermes, the per-agent `model:` overlay is not yet implemented** — see `product-knowledge/standards/upstream-openclaw-issues.md` (CRIT-A entry) for the gap and the planned fix.

Until the spec lands, the Hermes agent uses whatever was set as the runtime default during `conga admin setup`. To route this role through a cheaper model:

- **Option A** (operator action): set the cheap model as your Hermes runtime default at setup time, accept that all Hermes agents share it.
- **Option B** (per-agent override): edit Hermes's `cli-config.yaml` directly on the container after provisioning. Conga's overlay won't touch this file; you own it manually until the spec ships.

If you set a `model:` block in `agents/<your-agent>/agent.yaml` regardless, Conga will emit a one-time stderr warning at refresh time, and `cfg.model` in the rendered config will reflect your overlay's intent — but Hermes won't actually be able to address a custom `base_url`.

## Egress

If the model endpoint you choose is external (LiteLLM proxy, hosted Qwen, etc.), add it to the agent's egress allowlist (`terraform.tfvars` `agents.<name>.egress_allowed_domains` on AWS, or `~/.conga/conga-policy.yaml` `agents.<name>.egress.allowed_domains` on local/remote).
