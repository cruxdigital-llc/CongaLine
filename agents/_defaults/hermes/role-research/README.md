# role-research (Hermes)

Research agent. DM-driven (`type: user`). Suggested for web research, doc digests, competitive intel, and "go find / read / summarize" work.

## Important: model selection

The OpenClaw sibling uses an `agent.yaml` `model:` block to route through a cheap Qwen endpoint. **On Hermes, the per-agent `model:` overlay is not yet implemented** — see `product-knowledge/standards/upstream-openclaw-issues.md` (CRIT-A entry).

Until the spec lands, this Hermes agent uses whatever was set as the runtime default during `conga admin setup`. To override per-agent, edit Hermes's `cli-config.yaml` directly on the container after provisioning.

## Egress

The model `base_url` host (whichever you wire up via Hermes's `cli-config.yaml`) must be in the agent's egress allowlist, plus any search/scrape endpoints the agent uses for web fetches.
