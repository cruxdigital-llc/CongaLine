# role-writing

Opus-orchestrated writing agent with a Qwen subagent for mechanical text work. Channel-driven (`type: team`). Suggested for drafts, edits, content strategy — work where voice and judgment matter, with Qwen handling the predictable text transformations (translation, formatting, word counts).

## After provisioning

Edit `agents/<your-agent>/agent.yaml` and set `subagents.model.base_url` to your LLM proxy. The default is a placeholder. Then `conga refresh --agent <your-agent>`.

The primary stays at the runtime default (Opus from `openclaw-defaults.json`). See `role-code-dev/agent.yaml` comments for primary override notes.

## Egress

- **Anthropic** (`api.anthropic.com`) — Opus primary. Already in default allowlist.
- **Your LLM proxy** (`subagents.model.base_url` host) — Qwen subagent.

Add the proxy host to the agent's egress allowlist. The provisioning flow warns if it's missing.

## Channels

Suggested binding: `#writing`, `#content`, or a similar shared editorial channel.
