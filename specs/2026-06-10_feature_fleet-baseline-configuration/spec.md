# Technical Specification — Fleet Baseline (+ Per-Agent Declarative) Configuration

> Status: Spec (GLaDOS `spec-feature`). Builds on `requirements.md`, `plan.md`, and feature #30
> (`specs/2026-06-09_feature_infrastructure-only-simplification/`). Implementation gated on the
> security standards review (§11).

## 1. Summary

Add two **declarative, version-controlled** custom-config layers in the repo — a **fleet** layer
(all agents) and a **per-agent** layer (`agents/<name>/`) — that Conga deploys to each agent and
references from the managed `openclaw.json` via a `$include` **array**. They compose with feature
#30's on-host admin-drift `agent-custom.json`. Also de-embed `openclaw-defaults.json` so the fleet
runtime baseline is editable without a binary/provider release.

## 2. Verified merge model (the foundation)

Live-verified on `aaron`/`2026.5.26` (probe, §research/README 2026-06-10):
- **Root wins** over all includes (Conga-owned `gateway`/`channels`/`auth` stay authoritative).
- Within the `$include` **array, later wins**.
- Distinct keys **union** across all layers.

**Layering (lowest → highest precedence):**

| # | Layer | Committed source | Deployed file (data dir) | Owner | Scope | Hashed? |
|---|---|---|---|---|---|---|
| 1 | Runtime defaults | `pkg/runtime/openclaw/openclaw-defaults.json` → **de-embedded**, S3/file-synced | (folded into managed root) | Conga | all | (root) |
| 2 | **Fleet custom** | `agents/_defaults/<runtime>/fleet-custom.json` | `fleet-custom.json` | operator (repo) | all | yes |
| 3 | **Per-agent custom** | `agents/<name>/custom.json` | `agent-managed-custom.json` | operator (repo) | one | yes |
| 4 | Admin drift (#30) | — (created on host) | `agent-custom.json` | admin | one | no |

Managed root sets: `"$include": ["fleet-custom.json", "agent-managed-custom.json", "agent-custom.json"]`
→ effective precedence **root > admin-drift > per-agent > fleet**, distinct keys union.

**Decision (admin-drift wins over per-agent-declarative):** admin drift is last → highest among
includes. This preserves #30's "admin drift is sacrosanct / never clobbered" philosophy: a host
hotfix overrides repo-declared per-agent config. Alternative (repo wins) was considered and rejected
as less operationally forgiving; documented in §11a.

## 3. Code changes

### 3.1 Generator — `pkg/runtime/openclaw/config.go`
- Replace the single `config["$include"] = []string{AgentCustomConfigFile}` with the **array**
  above. Add consts `FleetCustomConfigFile = "fleet-custom.json"`,
  `AgentManagedCustomConfigFile = "agent-managed-custom.json"` (next to `AgentCustomConfigFile`).
- A missing `$include` target invalidates the whole config (verified #30/C1), so **all three files
  must always exist** on disk — providers ensure that (§3.3).

### 3.2 De-embed `openclaw-defaults.json`
- Replace `//go:embed` with a loader that reads the defaults from a known path
  (`<config-dir>/openclaw-defaults.json`), **falling back to an embedded copy** if the file is
  absent (first-boot / air-gap safety — do NOT drop the embed entirely; keep it as the fallback).
- Sync the editable copy like other bootstrap assets: S3 (`s3://<state-bucket>/conga/...`) on AWS,
  local file on local/remote. The repo file remains the canonical source.

### 3.3 Source resolution + deploy (the new machinery)
- **Resolver** (`pkg/common`): given an agent + runtime, resolve the fleet source
  (`agents/_defaults/<runtime>/fleet-custom.json`) and per-agent source (`agents/<name>/custom.json`).
  Model after `resolveBehaviorFiles` / the behavior-deploy path.
- **Deploy**, per provider, on provision **and** refresh **and** bind (every root write — same hook
  points as #30's `ensureAgentCustomConfig`):
  - Write `fleet-custom.json` from the fleet source (or `{}` if no source).
  - Write `agent-managed-custom.json` from the per-agent source (or `{}` if none).
  - Ensure `agent-custom.json` exists (`{}` if absent) — **unchanged #30 behavior, never clobbered.**
  - Perms: managed files (1,2-deployed) follow the same per-provider model as the root
    (AWS root:root 0444; local/remote per #30). Admin-drift file unchanged.
- **Validate before deploy**: run `openclaw config validate` (or schema) on the merged result where
  feasible; a bad **fleet** file breaks *every* agent, so fail closed on fleet validation in the
  generating path (operator-side), not silently on the host.

### 3.4 Integrity — extend the #30 guard
- `common.ValidateAgentCustomConfig` already forbids reserved keys
  (`$include`/`channels`/`gateway`/`plugins`). Run it on **all three** include files
  (fleet-custom, agent-managed-custom, agent-custom) in local/remote `RunIntegrityCheck` and the
  AWS `check-config-integrity.sh`.
- **Hash** the managed include files (fleet-custom, agent-managed-custom) against their deployed
  baseline (detects on-host tampering of the Conga-deployed copy); `agent-custom.json` stays
  un-hashed (admin drift).

### 3.5 Operator visibility (recommended, may phase)
- `conga agent show-config <name>` (or extend `agent diff`): render the **effective merged config**
  (root + 3 includes) so operators can reason about 4-layer precedence. Strongly aids debugging;
  CLI+JSON+MCP parity if shipped. Could be a fast follow if scope is tight.

## 4. Data model
- **Fleet source**: `agents/_defaults/<runtime>/fleet-custom.json` — free-form OpenClaw config
  subset, committed (joins the existing committed `_defaults/` tree). JSON (JSON5 tolerated, but
  strict-JSON gets full validation per #30).
- **Per-agent source**: `agents/<name>/custom.json` — same shape, gitignored per the
  no-committed-agents rule (only `_defaults`/`_example` are committed).
- **Agent record / secrets**: unchanged.
- **Manifest**: track the deployed managed include files (like behavior-file manifest tracking).

## 5. Lifecycle
- **Provision/refresh/bind**: deploy layers 2–3 from sources, ensure layer 4 exists, write the
  managed root with the `$include` array. Layers 2–3 **re-sync** each time (propagation); layer 4
  never touched.
- **Fleet change**: edit `agents/_defaults/<runtime>/fleet-custom.json` → `conga refresh` (or
  refresh-all) → propagates to every agent. No `rebaseline`, no host editing.
- **Per-agent change**: edit `agents/<name>/custom.json` → `conga refresh --agent <name>`.
- **`rebaseline`** (#30) still resets only `agent-custom.json` (the admin layer).

## 6. Edge cases
- **Empty/absent source** → deploy `{}` (the include target must exist or config is invalid).
- **Bad fleet file** → caught by pre-deploy validation; never reaches the host (fleet blast radius).
- **Hermes agents** → no `$include`; fleet/per-agent custom is a no-op (gated on
  `CustomConfigFileName() != ""`, as #30).
- **Reserved key in any layer** → integrity violation (§3.4).
- **JSON5 in a managed source** → same `ErrCustomConfigUnparseable` warn path as #30.

## 7. Three-provider parity
Deploy + perms + integrity wired identically across local (FS), remote (SSH), AWS (SSM + boot tftpl
+ provision scripts — all three #30 AWS write paths must learn the new include files).

## 8. Migration / backward-compat
- Existing agents (post-#30) have `"$include": ["agent-custom.json"]`. First refresh under this
  feature rewrites it to the 3-element array and deploys empty `fleet-custom.json` /
  `agent-managed-custom.json` (no behavior change until sources are populated). `agent-custom.json`
  untouched.

## 9. Testing
- **Unit**: generator emits the 3-element `$include`; resolver picks correct sources; de-embed
  loader (file present → file; absent → embedded fallback).
- **Live (verify)**: array precedence (done); a fleet MCP server lands on all agents via refresh; a
  per-agent `custom.json` overrides the fleet entry; admin drift still wins; integrity flags a
  reserved key in each layer.
- **Integration**: provision deploys all layers; refresh re-syncs 2–3 but not 4; bad fleet file
  rejected pre-deploy.

## 10. Data Safety (architecture.md §Agent Data Safety, must)
Config-only. New files (`fleet-custom.json`, `agent-managed-custom.json`) live next to
`openclaw.json` in the data dir — **config, not agent data**. No reads/writes to
`agents/main/sessions`, `workspace`, memory. Refresh re-syncs config layers, never data. ✅

## 11. Security considerations (GATES implementation — security.md review)
- **Reserved-key guard on all layers** (§3.4) keeps `channels`/`gateway`/etc. Conga-owned across
  fleet + per-agent + admin. Root-wins (verified) means no layer can override security keys.
- **Fleet blast radius** is the new risk: one file affects every agent. Mitigations: pre-deploy
  validation (fail closed), staged rollout (refresh per-agent first), and the effective-config view.
- **Egress**: fleet MCP endpoints → global `allowed_domains` (additive); per-agent → per-agent
  egress. Emit the #30-style egress-gap warning at deploy for any declared endpoint not allowlisted.
- **Secrets**: never in custom files (#9627); fleet MCP auth → shared secret.
- **Perms**: managed include files root:root 0444 on AWS (read-only to agent), as the root.
- **De-embed**: the synced defaults file must be integrity-covered (it feeds every root); keep the
  embedded fallback so a missing/tampered file fails safe to known-good.

### 11a. Resolved decisions
- Precedence **root > admin-drift > per-agent > fleet** (verified). Admin-drift highest among
  includes (sacrosanct, per #30). Alternative (repo per-agent wins over host drift) rejected.
- De-embed keeps an **embedded fallback** (not a pure file) for first-boot/air-gap safety.

## 12. Open implementation checkpoints
1. Final file names/paths (`agents/<name>/custom.json` vs `agent-custom.source.json`; deployed
   `agent-managed-custom.json` name) — avoid confusion with the host admin `agent-custom.json`.
2. De-embed delivery details (S3 path, first-boot ordering, integrity of the synced defaults).
3. Whether `conga agent show-config` ships in this feature or as a fast follow.
4. Manifest/reconciliation when a fleet/per-agent source is removed (deploy `{}` vs delete include).

## 13. Handoff
`/glados:implement-feature` — implement §3 (generator array, de-embed w/ fallback, resolver +
per-provider deploy across all #30 write paths incl. AWS tftpl + provision scripts, integrity
extension), tests per §9; then `/glados:verify-feature` (live fleet-propagation + override checks)
and the security gate (§11). `pkg/` change → `terraform-provider-conga` release.

## 14. Implementation reconciliation (verify-feature, 2026-06-10)

Final implementation vs. this spec — divergences, all intentional and traced in README:

- **§3.2 de-embed location/scope.** The operator-editable defaults live at
  `agents/_defaults/openclaw/openclaw-defaults.json` (committed, runtime-level), reusing the existing
  `agents/` → `/opt/conga/agents/` S3 sync — **no new terraform/S3 path** (resolves C2). Scope is the
  **Go generation paths only** (operator-side `conga refresh` on AWS + local/remote); the AWS bash
  fresh-boot heredocs still hardcode the baseline inline — unifying them is tracked follow-up **T2.4**.
  On local, the #31 sources + defaults resolve via `overlayBehaviorDir()` (live repo), so edits
  propagate on `conga refresh` without re-running `admin setup`.
- **§3.5 effective-config view → layered view (not synthesized merge).** `conga agent show-config`
  renders the 4 **deployed layers** read live from the container, each labeled by precedence/role/owner,
  rather than computing a merged config in Go. Operator decision: a synthesized merge could diverge
  from OpenClaw's actual deep-merge and mislead; show the source-of-truth + precedence contract instead.
  Shipped this feature with full Interface Parity: CLI + `--output json` (+ `json_schema.go` contract) +
  MCP `conga_agent_show_config`. (Resolves C3-adjacent §12.3.)
- **§3.4 integrity baseline lifecycle.** Managed-include baselines (`<name>.<file>.sha256` /
  `<name>-<file>.sha256` on AWS) are written at every deploy point AND on the AWS Go refresh path
  (`regenerateAgentConfigOnInstance`), and cleaned up on `RemoveAgent` across all 3 providers — keeping
  deploy + baseline + guard in sync (caught + fixed in the two PR-review passes).
- **§12 checkpoints resolved.** C1: source `agents/<name>/custom.json` → deployed
  `agent-managed-custom.json` (distinct from admin `agent-custom.json`); fleet source
  `_defaults/<runtime>/fleet-custom.json` → deployed `fleet-custom.json`. C2: reuse the `agents/` sync
  (above). C3: a removed source deploys `{}` on next refresh (the `$include` target must exist); the
  include array is never trimmed.
- **T9.2 live-verified** (verify-feature, local Docker / OpenClaw 2026.5.26): union + full precedence
  chain (root > admin-drift > per-agent > fleet), fleet propagation + baseline freshness, pre-deploy
  fail-closed, egress-gap warning, orphan-baseline cleanup, show-config. See README T9.2 table.
- **Still open (not blockers for this PR):** T2.4 (AWS bash boot-path de-embed unification),
  R1 (provider release, post-merge).
