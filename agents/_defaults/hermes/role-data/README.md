# role-data (Hermes)

Data/reporting agent. DM-driven (`type: user`). Suggested for CSV crunching, metrics summarization, format conversion, and scheduled reports.

## Important: model selection

The OpenClaw sibling uses an `agent.yaml` `model:` block to route through a cheap Qwen endpoint. **On Hermes, the per-agent `model:` overlay is not yet implemented** — see `product-knowledge/standards/upstream-openclaw-issues.md` (CRIT-A entry).

Until the spec lands, this Hermes agent uses whatever was set as the runtime default during `conga admin setup`. To override per-agent, edit Hermes's `cli-config.yaml` directly on the container after provisioning. Setting `model:` in this role's `agent.yaml` will produce a stderr warning and a `cfg.model` value that Hermes can't actually route to a custom `base_url`.

## Egress

Add any external model/data endpoints to the agent's egress allowlist.
