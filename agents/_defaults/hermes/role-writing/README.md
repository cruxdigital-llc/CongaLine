# role-writing (Hermes)

Opus-orchestrated writing agent with a Qwen subagent for mechanical text work. Channel-driven (`type: team`). Suggested for drafts, edits, content strategy — work where voice and judgment matter, with Qwen handling the predictable text transformations (translation, formatting, word counts).

## After provisioning

Edit `agents/<your-agent>/agent.yaml` and set `subagents.model.base_url` to your LLM proxy. The default is a placeholder. Then `conga refresh --agent <your-agent>`.

The primary stays at the Hermes runtime default (Opus). Note: the overlay's `subagents.delegation_mode` is OpenClaw-only and silently dropped on Hermes — Hermes always-delegates at the runtime layer, so the field has no effect here.

## Egress

- **Anthropic** (`api.anthropic.com`) — Opus primary. Already in default allowlist.
- **Your LLM proxy** (`subagents.model.base_url` host) — Qwen subagent.

Add the proxy host to the agent's egress allowlist. The provisioning flow warns if it's missing.

## Channels

Suggested binding: `#writing`, `#content`, or a similar shared editorial channel.
