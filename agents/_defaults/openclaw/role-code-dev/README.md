# role-code-dev

Opus-orchestrated dev agent with a Qwen subagent for mechanical work. Channel-driven (`type: team`). Suggested for code review, architecture discussions, debugging sessions — work where Opus's reasoning matters and Qwen handles the lookups/file-ops/log-parsing it would be wasteful to spend Opus tokens on.

## After provisioning

The default `agent.yaml` declares a subagent pointing at `https://litellm.internal/v1`. Edit `agents/<your-agent>/agent.yaml` and set:

- `subagents.model.base_url` — your actual LLM proxy or Qwen endpoint
- `subagents.model.name` — the Qwen variant you've deployed
- Optionally adjust `subagents.delegation_mode` (`prefer` nudges the orchestrator harder; `suggest` is the default) and `subagents.max_concurrent`

Then `conga refresh --agent <your-agent>` to apply.

The primary model stays at the runtime default (Opus from `openclaw-defaults.json`). If you want a different Opus revision, update the defaults file or override `/model` per session.

## Egress

This role talks to:
- **Anthropic** (`api.anthropic.com`) — for the Opus primary. Already in the default allowlist for new agents via `conga bootstrap`.
- **Your LLM proxy** (`subagents.model.base_url` host) — for Qwen delegation.

Add the proxy host to the agent's egress allowlist. The provisioning flow warns if it's missing.

## Channels

Suggested bindings:
- `#code-review` or `#engineering` — primary channel for Opus-driven review and architecture discussions
