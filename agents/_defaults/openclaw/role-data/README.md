# role-data

Qwen-backed data/reporting agent. DM-driven (`type: user`). Suggested for CSV crunching, metrics summarization, format conversion, and scheduled reports — mechanical work where a cheap model is the right tool.

## After provisioning

Edit `agents/<your-agent>/agent.yaml` to point `model.base_url` at your actual LLM proxy or Qwen endpoint. The default `https://litellm.internal/v1` is a placeholder. Then `conga refresh --agent <your-agent>`.

## Egress

Add the `base_url` host to the agent's egress allowlist (`terraform.tfvars` agents.<name>.egress_allowed_domains on AWS, or `~/.conga/conga-policy.yaml` agents.<name>.egress.allowed_domains on local/remote). The provisioning flow warns if it's missing.
