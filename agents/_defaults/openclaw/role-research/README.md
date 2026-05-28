# role-research

Qwen-backed research agent. DM-driven (`type: user`). Suggested for web research, doc digests, competitive intel, and other "go find / read / summarize" work — fast and cheap.

## After provisioning

Edit `agents/<your-agent>/agent.yaml` to point `model.base_url` at your LLM proxy or Qwen endpoint. The default is a placeholder. Then `conga refresh --agent <your-agent>`.

If the agent needs to fetch web pages, make sure your egress allowlist includes the search/scrape endpoints you intend to use, in addition to the model `base_url` host.
