# `agents/`

Per-agent configuration tree. Each agent's prompts and runtime overlay live in their own subdirectory under here. **Real agent directories are gitignored** — only the two underscore-prefixed entries (`_defaults/` and `_example/`) are committed.

## What lives where

```
agents/
├── README.md                       # this file
├── _defaults/                      # COMMITTED — shipped defaults, indexed by runtime + agent type
│   ├── openclaw/
│   │   ├── user/    SOUL.md  AGENTS.md  USER.md.tmpl
│   │   └── team/    SOUL.md  AGENTS.md  USER.md.tmpl
│   └── hermes/
│       └── ...
├── _example/                       # COMMITTED — annotated template you copy from
│   ├── SOUL.md
│   ├── AGENTS.md
│   └── agent.yaml.example
└── <agent-name>/                   # GITIGNORED — your real agents
    ├── SOUL.md                     # optional; overrides _defaults
    ├── AGENTS.md                   # optional; overrides _defaults
    ├── USER.md                     # optional; overrides the templated USER.md.tmpl
    └── agent.yaml                  # optional; per-agent runtime overlay (model, …)
```

`<agent-name>` must match the agent name in your tfvars / `conga admin add-user` / `conga admin add-team`. Names cannot start with `_` (so the underscore-prefixed entries above can't collide with real agents).

## What goes in each file

| File | Purpose | Required? |
|---|---|---|
| `SOUL.md` | Agent personality and high-level guidance. Read at every agent turn. | Optional — falls back to `_defaults/<runtime>/<type>/SOUL.md` |
| `AGENTS.md` | Domain knowledge, project context, operating procedures. | Optional — falls back to `_defaults/<runtime>/<type>/AGENTS.md` |
| `USER.md` | Per-conversation user context. Authored as a static file. | Optional — falls back to rendering `_defaults/<runtime>/<type>/USER.md.tmpl` with channel-binding variables substituted in |
| `agent.yaml` | Schema-versioned runtime overlay: model provider/name/endpoint, future memory/tools/limits. | Optional — defaults apply when absent. See `_example/agent.yaml.example` for the full schema. |

The runtime resolves files in this order: **agent override (one of the files above) → `_defaults/<runtime>/<type>/` → nothing**. There is no recursive merging; each file is either taken whole from the agent directory or whole from defaults.

## Creating a new agent's overlay

```bash
# Pick a name that matches what's declared in terraform.tfvars (or what
# you'll pass to `conga admin add-user/add-team` on local/remote).
NAME=myagent

# Copy the example as a starting point.
cp -r agents/_example agents/$NAME

# Edit to taste. The .example suffix is dropped — the file the loader
# reads is agent.yaml, not agent.yaml.example.
mv agents/$NAME/agent.yaml.example agents/$NAME/agent.yaml
# Edit prompts:
vim agents/$NAME/SOUL.md agents/$NAME/AGENTS.md

# Apply.
conga refresh --agent $NAME            # local / remote
# or, on AWS, after `terraform apply` to sync to S3:
conga --provider aws refresh --agent $NAME
```

## What does NOT belong here

This directory is for **per-agent runtime configuration** authored by hand. It is NOT a home for:

- **Infrastructure** (ports, egress IPs, channel bindings, secret values) → `terraform/environments/<env>/terraform.tfvars`
- **Cluster policy** (egress allow/deny, posture, drift) → `~/.conga/conga-policy.yaml`
- **Runtime persistence** (allocated port, channel binding state) → `~/.conga/agents/<name>.json` (materialized by the provider; don't hand-edit)
- **Secrets** (API keys, tokens) → secrets store (AWS Secrets Manager on AWS; mode-0400 files on local/remote)
- **New file types per concern** (e.g. `memory.yaml`, `tools.yaml`) → extend `agent.yaml` with a new top-level key in a versioned schema bump

The full taxonomy and decision rule for "which layer does my new config concern belong in?" lives in [`product-knowledge/standards/config-taxonomy.md`](../product-knowledge/standards/config-taxonomy.md).

## Cross-references

- [`_example/agent.yaml.example`](_example/agent.yaml.example) — annotated template with the full schema, provider matrix, and reserved keyspace
- [Root README — Per-Agent Model Routing](../README.md#per-agent-model-routing) — schema v1, additive allowlist semantics, end-to-end walkthrough
- [`product-knowledge/standards/config-taxonomy.md`](../product-knowledge/standards/config-taxonomy.md) — the canonical "where does this concern live?" map
- [`pkg/common/behavior.go`](../pkg/common/behavior.go) `resolveBehaviorFiles` — the resolver implementation
- [`pkg/common/overlay_agent.go`](../pkg/common/overlay_agent.go) `LoadAgentOverlay` — the `agent.yaml` loader
