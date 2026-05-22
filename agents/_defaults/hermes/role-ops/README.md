# role-ops

Qwen-backed operations agent. DM-driven (`type: user`). Suggested for monitoring queries, infra status checks, and short health reports — the work where a cheap, fast model is plenty and Opus would be overkill.

## After provisioning

The default `agent.yaml` points at `https://litellm.internal/v1` — a placeholder. Edit `agents/<your-agent>/agent.yaml` to set:

- `model.base_url` — your actual LLM proxy or Qwen endpoint
- `model.name` — the Qwen variant you've deployed
- Optionally `context_window` / `max_tokens` if your proxy has tighter caps

Then run `conga refresh --agent <your-agent>` to apply.

## Egress

This role talks to one external endpoint (the `base_url` from `agent.yaml`). Add that host to the agent's egress allowlist either in `terraform.tfvars` (AWS) or `~/.conga/conga-policy.yaml` (local/remote). The `conga admin add-user` flow will warn if the endpoint is missing.
