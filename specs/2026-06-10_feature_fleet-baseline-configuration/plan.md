# High-Level Plan — Fleet Baseline (+ Per-Agent Declarative) Configuration

> Altitude: approach + decisions. Detailed design (incl. the empirical precedence check) → `/glados:spec-feature`.

## The layering model

Today there are two effective layers (Conga-managed root, on-host admin drift). This feature adds
**two declarative, version-controlled layers in between**, all composed by OpenClaw at load time:

| # | Layer | Home | Owner | Applies to | Driftable? |
|---|---|---|---|---|---|
| 1 | Runtime defaults | `openclaw-defaults.json` (de-embedded → host/S3) | Conga | all | no (authoritative) |
| 2 | **Fleet custom (NEW)** | committed, e.g. `agents/_defaults/<runtime>/fleet-custom.json` | operator (repo) | all | no (re-synced on refresh) |
| 3 | **Per-agent custom (NEW)** | committed, e.g. `agents/<name>/custom.json` | operator (repo) | one agent | no (re-synced) |
| 4 | Admin drift | `agent-custom.json` (on host) | admin | one agent | yes (never overwritten) |
| — | Conga-owned keys (gateway/channels/auth/overlays) | the managed `openclaw.json` root | Conga | — | wins over all includes |

**Precedence (lowest → highest):** defaults → fleet → per-agent-declarative → admin-drift, with the
**managed root always winning** for Conga-owned keys (verified in #30). Composition is OpenClaw's
deep-merge of the `$include` array.

## How it's wired (Approach C, extended)

Conga writes into the managed root:
```jsonc
"$include": ["fleet-custom.json", "agent-managed-custom.json", "agent-custom.json"]
```
- `fleet-custom.json` and `agent-managed-custom.json` are **Conga-deployed** to the agent data dir
  from the committed sources (`agents/_defaults/…`, `agents/<name>/…`) on provision/refresh — the
  same way behavior files (`SOUL.md`, etc.) are already deployed.
- `agent-custom.json` is the unchanged #30 admin-drift file (created `{}` if absent, never clobbered).
- OpenClaw deep-merges them under the root; **array order defines fleet-vs-per-agent precedence**.

This reuses everything #30 verified (single `$include`, root-wins, fail-closed) and only adds:
deploying 1–2 managed include files + extending the `$include` to an array.

## Why not the alternatives (recap from discussion)

- **A — bake into `openclaw-defaults.json` only**: fleet-only (no per-agent declarative layer),
  and Conga-owned/authoritative (no per-agent drift of those keys). Good for true universals; we
  de-embed it (layer 1) but it's not the per-agent answer.
- **B — seed `agent-custom.json` from a committed template at provision**: new-agents-only;
  baseline changes don't propagate to existing agents. Rejected as the primary mechanism.
- **C (chosen)**: managed include files + array `$include` → propagates fleetwide on refresh AND
  supports per-agent declarative config, while keeping the admin drift hatch.

## De-embed `openclaw-defaults.json` (folded in)

Move it out of `//go:embed` to an editable file synced like other bootstrap assets (S3 on AWS,
local file on local/remote). Lets fleet defaults change without a binary rebuild + provider release.
Must preserve first-boot availability (defaults present before the gateway starts).

## Phased delivery (high level)

1. **De-embed defaults** — `openclaw-defaults.json` as a synced file + loader change; no behavior change.
2. **Array `$include` + deploy managed include files** — extend the generator + per-provider deploy
   of `fleet-custom.json` / `agent-managed-custom.json` from committed sources.
3. **Precedence + integrity** — verify array order live; extend the reserved-key guard to all layers;
   hash the managed include files (admin drift file stays un-hashed).
4. **Fleet egress/secrets + docs** — additive egress allowlist story; shared-secret for fleet MCP auth;
   update `config-taxonomy.md` with the new layers.
5. **Three-provider parity + tests + live verify.**

## Key decisions to resolve in spec

1. **`$include`-array precedence** — confirm order semantics live (does a *later* include win? we need
   per-agent > fleet, and admin-drift > both). This is the load-bearing unknown (like #30's root-wins).
2. **File names + locations** — `agents/_defaults/<runtime>/fleet-custom.json`? per-agent
   `agents/<name>/custom.json` vs a clearer name; how they map to deployed include filenames.
3. **Managed vs admin precedence** — does repo-declared per-agent config out-rank on-host admin drift,
   or vice-versa? (Drift-wins is more permissive; managed-wins is more governable.)
4. **De-embed delivery** — S3 sync (AWS) / file (local/remote); first-boot guarantee; where the
   canonical defaults file lives in the repo.
5. **Integrity** — hash the Conga-managed include files; confirm the reserved-key guard runs on every
   layer; how a fleet-file change reconciles with per-agent baselines.
6. **Runtime applicability** — OpenClaw-only (Hermes has no `$include`); behavior for Hermes agents.

## Risks
- **Array-merge precedence unverified** — must live-check on the pinned image before committing (cheap,
  per #30 pattern). If array order isn't controllable, the layering model needs rework.
- **De-embed regressions** — first-boot/air-gapped defaults availability; keep a safe fallback.
- **Fleet blast radius** — a bad fleet-custom file breaks *every* agent at once → validate before
  deploy (reuse `openclaw config validate`), stage/roll carefully.
- **Precedence confusion** — four layers is a lot; docs + a `conga agent diff`-style "effective config"
  view would help operators reason about it.

## Out of scope (recap)
Typed schema for custom config; `agent.yaml` schema changes; removing the admin drift hatch; app code.

## Testing strategy (QA)
- **Unit**: generator emits the `$include` array; reserved-key guard on each layer; de-embed loader.
- **Live (verify)**: array-order precedence on the pinned image; a fleet MCP server lands on all
  agents via refresh; a per-agent committed file overrides the fleet one; admin drift still wins.
- **Integration**: provision seeds managed include files; refresh re-syncs fleet/per-agent but not
  admin drift; integrity flags a reserved key in any layer.
