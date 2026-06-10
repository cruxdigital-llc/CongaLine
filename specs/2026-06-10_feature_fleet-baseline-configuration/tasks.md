# Implementation Tasks — Fleet Baseline (+ Per-Agent Declarative) Configuration

> From `spec.md`. All `pkg/` → requires a `terraform-provider-conga` release on completion.
> Verified foundation: `$include`-array later-wins, includes union, root-wins-over-all.

## Phase 1 — Generator emits the `$include` array ✅ DONE
- [x] **T1.1/1.2** consts + 3-element `$include` array (order=precedence). **T1.3** include test updated. Green.

## Phase 3 — Source resolver ✅ DONE
- [x] `common.ResolveCustomConfigSources` + test (fleet `_defaults/<runtime>/fleet-custom.json`,
  per-agent `agents/<name>/custom.json`; nil if absent).

## Phase 4 (Go paths) ✅ DONE — `deployManagedCustomConfig` on local/remote/AWS-regenerate
- [x] **T4.1/4.2/4.3** Each provider deploys fleet-custom.json + agent-managed-custom.json from
  sources (or `{}`) at every Go write path (provision/refresh/bind), beside the #30
  `ensureAgentCustomConfig`. AWS re-protects all three managed includes root:root 0444. Build+tests green.
- [ ] **T4.4 (remaining)** AWS **boot tftpl** + **provision scripts** (add-user/add-team): deploy the
  managed include files from the S3-synced `/opt/conga/agents/` sources (fresh-deploy path).
- [ ] **T4.5 (remaining)** Per-provider deploy tests (provision deploys layers; refresh re-syncs 2–3 not 4).

## Phase 2 — De-embed `openclaw-defaults.json` (with embedded fallback) — REMAINING
- [ ] **T2.1** Keep `//go:embed openclaw-defaults.json` as a **fallback**; add a loader that prefers
  an on-disk `<config-dir>/openclaw-defaults.json` and falls back to the embed if absent/unreadable.
- [ ] **T2.2** Sync the editable file: S3 on AWS bootstrap; local/remote write it from the repo source.
- [ ] **T2.3** Tests: file present → file used; absent → embedded fallback; malformed → fallback + warn.

## Phase 5 — Integrity: guard + hash all managed layers
- [ ] **T5.1** Run `common.ValidateAgentCustomConfig` (reserved-key guard) on **all three** include
  files in local/remote `RunIntegrityCheck` + AWS `check-config-integrity.sh`.
- [ ] **T5.2** Hash the managed include files (fleet-custom, agent-managed-custom) vs deployed baseline;
  `agent-custom.json` stays un-hashed.
- [ ] **T5.3** Tests: reserved key in each layer flagged.

## Phase 6 — Pre-deploy validation + egress (fleet blast-radius)
- [ ] **T6.1** Validate the merged result before deploy (operator-side `openclaw config validate` or
  schema); **fail closed on a bad fleet file** so it never reaches the host.
- [ ] **T6.2** Emit the #30-style egress-gap warning for endpoints declared in fleet/per-agent custom.

## Phase 7 — Effective-config view (architect-recommended; Interface Parity if shipped)
- [ ] **T7.1** `conga agent show-config <name>` → render the effective merged config (root + 3 includes).
  CLI + JSON schema + MCP tool. (May fast-follow if scope is tight — decide in review.)

## Phase 8 — Migration + docs
- [ ] **T8.1** First refresh under this feature rewrites `$include` from #30's 1-element to the
  3-element array; deploy empty managed files; `agent-custom.json` untouched.
- [ ] **T8.2** `product-knowledge/standards/config-taxonomy.md`: add the fleet + per-agent layers
  (resolves the gate's `should` warning).

## Phase 9 — Integration / live / release
- [ ] **T9.1** Integration: fleet file → lands on all agents on refresh; per-agent overrides fleet;
  admin overrides per-agent; bad fleet file rejected pre-deploy.
- [ ] **T9.2** Live (in `/glados:verify-feature`): fleet propagation + override precedence on the fleet.
- [ ] **R1** `terraform-provider-conga` release after merge.

## Open checkpoints (spec §12)
- [ ] **C1** Final file names (avoid `custom.json` / `agent-custom.json` / `agent-managed-custom.json`
  confusion).
- [ ] **C2** De-embed S3 path + first-boot ordering + integrity of the synced defaults.
- [ ] **C3** Source-removed reconciliation (deploy `{}` vs drop the include).
