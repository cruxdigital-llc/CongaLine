# Per-Agent Behavior: AGENTS.md

<!--
  OVERRIDE NOTICE
  Placing this file in behavior/agents/<your-agent>/AGENTS.md completely
  REPLACES behavior/default/AGENTS.md for that agent. The default will
  not be used.

  Getting started:
    1. mkdir behavior/agents/<your-agent>
    2. cp behavior/default/AGENTS.md behavior/agents/<your-agent>/AGENTS.md
       — or write your own from scratch
    3. Edit to add agent-specific context (client, team, project, rules)
    4. conga refresh --agent <your-agent>

  This example file is a reference, not a starting point. Clone the
  default and extend it, or write something purpose-built for your agent.
-->

## What goes here

AGENTS.md is loaded into every session as operating instructions. The
default version covers session startup, red lines, memory rules, and
tool guidance. When you override it for a specific agent, you own the
entire file — include everything the agent needs to operate.

## Tips

- Start by copying `behavior/default/AGENTS.md` so you inherit the
  baseline rules, then append your agent-specific sections.
- Keep total file size under 150 lines. OpenClaw concatenates all
  workspace markdown into a single system prompt — brevity matters.
- Use `conga agent show <name> AGENTS.md` to inspect the deployed version.
- Use `conga agent diff <name>` to compare source vs workspace.
