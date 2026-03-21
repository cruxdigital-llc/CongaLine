# Trace Log: Conga Line Rename

**Feature**: Rename app from "OpenClaw"/"CruxClaw" to "Conga Line" / CLI from `cruxclaw` to `conga`
**Date**: 2026-03-20
**Status**: Spec complete — ready for implementation

## Session Log

### 2026-03-20 — Planning Session
- Feature requested: comprehensive rename of app branding
- "Conga Line" — tagline: "a line of lobsters"
- CLI binary: `cruxclaw` → `conga`
- All SSM, Secrets Manager, S3 paths included in rename
- Upstream Open Claw references preserved

### 2026-03-20 — Spec Session
- Detailed spec written with file-by-file change list
- Config key `openclaw-image` renamed to `image` (our key, not upstream)
- Intermediate config files renamed: `$AGENT_NAME-openclaw.json` → `$AGENT_NAME-config.json`
- CloudWatch namespace: `OpenClaw` → `CongaLine`

### Artifacts
- [requirements.md](requirements.md) — naming convention table, do-not-rename list, success criteria
- [plan.md](plan.md) — 9-phase bottom-up rename plan
- [spec.md](spec.md) — file-by-file change specification

## Active Capabilities
- File search (Glob/Grep) for comprehensive reference discovery

## Active Personas
- **Architect** — structural integrity, infrastructure path consistency
- **QA** — verification checklist, missed-reference detection

## Key Decisions
1. **Naming**: `conga` for everything — project name, infra prefix, CLI binary, Docker container prefix
2. **Full path rename**: SSM `/openclaw/` → `/conga/`, Secrets Manager, S3 prefixes all change
3. **Upstream preserved**: `ghcr.io/openclaw/openclaw:*`, `github.com/openclaw/openclaw/*`, `openclaw.json` config file name — all stay
4. **Bottom-up order**: Go code → CI → Terraform → bootstrap → router → docs → specs → product-knowledge
5. **Deployment migration out of scope**: live AWS resources need manual migration (state mv, re-setup)
6. **Config key rename**: `openclaw-image` → `image` (cleaner, it's our naming not upstream's)

## Persona Review

### Architect — ✅ Approved
- Pure rename, no architectural concerns
- Terraform resource identifiers are local-only; AWS names from `var.project_name`
- Go module path is a clean break (not a library)

### QA — ✅ Approved with notes
- Add broader case-insensitive grep to verification (CruxClaw, crux-claw, etc.)
- Watch sed patterns in templates — prefix stripping must match new paths
- Config key rename (`openclaw-image` → `image`) affects SSM + bootstrap + variables.tf

## Standards Gate Report (Pre-Implementation)

| Standard | Scope | Severity | Verdict |
|---|---|---|---|
| Zero trust the AI agent | all | must | ✅ PASSES |
| Immutable configuration | all | must | ✅ PASSES |
| Least privilege | iam | must | ✅ PASSES |
| Secrets never touch disk | secrets | must | ✅ PASSES |
| Isolated Docker networks | containers | must | ✅ PASSES |
| IMDSv2 enforced | compute | must | ✅ PASSES |

**Gate: PASS** — pure rename, no security implications.
