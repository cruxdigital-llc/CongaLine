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
- [x] **T4.4** AWS **boot tftpl** + **provision scripts** (add-user/add-team): the fresh-deploy path
  now emits the 3-element `$include` array and `deploy-agents.sh` deploys fleet-custom.json +
  agent-managed-custom.json from the S3-synced `/opt/conga/agents/` sources (or `{}`), root:root 0444,
  openclaw-gated. Centralized the file deploy in `deploy-agents.sh.tmpl` (runs in all 3 fresh-deploy
  paths after the s3 sync); updated the `$include` jq at all 3 config-write sites
  (`add-user.sh.tmpl`, `add-team.sh.tmpl`, `user-data.sh.tftpl`). Tests: render assertions
  (add-user/add-team 3-element `$include`) + `deploy-agents.sh.tmpl` content test. Green.
- [ ] **T4.5 (remaining)** Per-**Go-provider** deploy tests (provision deploys layers; refresh
  re-syncs 2–3 not 4). The bash fresh-deploy path is covered by T4.4's tests; this is the Go side
  (local/remote/AWS-regenerate) — fold into P9.

## Phase 2 — De-embed `openclaw-defaults.json` (with embedded fallback) ✅ DONE (Go scope)
> **Scope decision (operator, 2026-06-10):** Go de-embed now; the AWS **bash boot/provision**
> path unification is a tracked follow-up (T2.4), not in this pass. The conga Go binary never runs
> on the AWS host (`user-data.sh.tftpl:1367`), so the embed is read only by the Go paths
> (operator-side `conga refresh` on AWS, + local/remote). The AWS fresh-boot + add-user/add-team
> bash scripts still hardcode the defaults inline; a fresh boot uses those until the first
> `conga refresh` regenerates from the file.
>
> **File location (resolves C2):** `agents/_defaults/openclaw/openclaw-defaults.json` — runtime-level
> (NOT type-specific), beside `fleet-custom.json`. Rides the existing `aws s3 sync conga/agents/`
> to `/opt/conga/agents/` on AWS and the local/remote behavior-dir snapshot — **no new terraform/S3
> wiring**. Repo `pkg/runtime/openclaw/openclaw-defaults.json` stays the canonical seed + embed.
- [x] **T2.1** Embed retained as fallback; `GenerateConfig` prefers `ConfigParams.RuntimeDefaults`
  (valid JSON) else embed; malformed → warn + embed. (`pkg/runtime/openclaw/config.go`.)
- [x] **T2.2** Sync: `common.ResolveRuntimeDefaults(behaviorDir, agent)` reads the on-disk file,
  threaded at all 3 Go gen sites (local provision+regenerate; `RuntimeGenerateAgentFilesWithOverlay`
  covering remote+AWS). Reuses fleet-custom's sync — file is present wherever fleet-custom is.
- [x] **T2.3** Tests: file present → file used; absent → embedded fallback; malformed → fallback
  (no error). (`config_test.go`, `custom_config_test.go`.) Build + tests green.
- [ ] **T2.4 (tracked follow-up — do NOT lose)** Unify the AWS **bash boot/provision** path:
  refactor `user-data.sh.tftpl` (×2 heredocs) + `add-user.sh.tmpl` + `add-team.sh.tmpl` to layer
  gateway/channels over the S3-synced `openclaw-defaults.json` (jq) with a minimal inline fallback,
  so a fresh AWS boot also reflects operator edits without waiting for a `conga refresh`. Large bash
  refactor; re-verifies the fresh-boot config. Closes the long-standing bash/Go defaults divergence.

## Phase 5 — Integrity: guard + hash all managed layers ✅ DONE
- [x] **T5.1** Reserved-key guard on **all three** include files. Refactored `common` to a
  filename-generic `ValidateCustomConfigKeys(fname, data)` + `ClassifyIncludeValidation(fname, data)`
  (shared warn/err classification); added runtime method `ManagedCustomConfigFiles()`
  (openclaw → [fleet-custom, agent-managed-custom]; hermes → nil). local/remote `RunIntegrityCheck`
  now iterate admin + managed layers; AWS `check-config-integrity.sh` loops the jq reserved-key check
  over all three.
- [x] **T5.2** Hash the managed include files vs deployed baseline (`<agent>.<fname>.sha256` local/remote,
  `<agent>-<fname>.sha256` AWS), written at every baseline-save point (local/remote `saveConfigBaseline`
  → `saveManagedIncludeBaselines`; AWS `deploy-agents.sh`). `agent-custom.json` stays un-hashed.
  Missing baseline self-heals.
- [x] **T5.3** Tests: reserved key flagged in each layer (local `checkIncludeReservedKeys`); managed-include
  on-host tamper detected (`checkManagedIncludeIntegrity`); `common` generic validator/classifier;
  deploy-agents baseline-write content test. Green across local/remote/scripts/common.

## Phase 6 — Pre-deploy validation + egress (fleet blast-radius) ✅ DONE
- [x] **T6.1** `common.ValidateManagedConfigSources` runs the reserved-key guard on the fleet +
  per-agent sources **before any write**, in all 3 deploy paths (local/remote `deployManagedCustomConfig`,
  AWS `RegenerateAgent`). A reserved-key violation **fails the deploy closed** (blast radius — the bad
  fleet file never reaches a host). JSON5/unparseable is tolerated pre-deploy (on-host openclaw load +
  integrity check backstop, per §6). No openclaw binary needed operator-side.
- [x] **T6.2** `common.WarnCustomConfigEgressGaps` walks `mcp.servers.*.url` in fleet + per-agent and
  emits the #30-style egress-gap warning for any host not allowlisted. Wired beside all 6
  `WarnOverlayEgressGaps` sites (local/remote/AWS × provision+refresh), reusing the resolved allowlist.
- [x] **T6.3 (tests)** `ValidateManagedConfigSources` (fleet/per-agent reserved-key → fail; JSON5 →
  tolerated; clean → pass); `CheckCustomConfigEgress` (missing host flagged, wildcard match, cross-layer
  dedup, no-MCP/unparseable → nil). Green.

## Phase 7 — Effective-config view ✅ DONE (layered view; operator-chosen approach)
- [x] **T7.1** `conga agent show-config <agent>` renders the **layered view** (operator decision
  2026-06-10): the 4 deployed layers read live from the container via `ContainerExec`, each labeled
  with precedence rank + role + owner — NOT a synthesized merge (no risk of diverging from OpenClaw).
  Interface Parity: **CLI** (`agent show-config`) + **JSON** (`--output json`) + **MCP**
  (`conga_agent_show_config`, ReadOnlyHint). Shared pure builder `common.EffectiveConfigSpecs` +
  `common.BuildConfigLayers` (decoupled from openclaw; string-keyed role map). Tests:
  spec ordering (openclaw 4 layers, hermes root-only), layer build (precedence/present/content
  classification), MCP tool registered + callable. Green.

## Phase 8 — Migration + docs ✅ DONE
- [x] **T8.1** Migration is **automatic, no code**: the generator unconditionally emits the 3-element
  `$include` array (tested in `config_test.go`) and the deploy paths always create the empty managed
  files, so the first refresh under this feature rewrites #30's 1-element form wholesale;
  `agent-custom.json` is never touched. Backward-compatible by construction.
- [x] **T8.2** `config-taxonomy.md` updated (resolves the gate's `should` warning): split the
  customization row into declarative (fleet + per-agent) vs admin-drift; added a "custom-config layers
  + precedence" subsection (root > admin-drift > per-agent > fleet, `show-config` pointer, which-layer
  guidance, fail-closed/hash notes); documented the de-embedded `openclaw-defaults.json`; rewrote
  Example 6 (Linear MCP) to present the layer choice. Header date bumped.

## Phase 9 — Integration / live / release
- [x] **T9.1** Provider deploy test (`TestDeployManagedCustomConfig`): fleet + per-agent deployed from
  sources (or `{}`), re-synced each call (propagation), admin-drift agent-custom.json untouched,
  **bad fleet source fails closed** (no partial write). **Plus `TestFleetAndPerAgentConfig`**
  (integration-tagged, Docker) — codifies the full T9.2 flow end-to-end against real OpenClaw: include
  array order, layers-from-sources, **union**, **per-agent > fleet**, **admin-drift > per-agent**,
  fleet propagation on refresh, show-config layers, reserved-key **fail-closed**, managed-baseline
  cleanup on removal. **13/13 subtests pass** (42s live run).
- [x] **Composition contract (unit)** `TestCompositionPrecedenceContract` (`pkg/common`): pins that the
  generator's deployed `$include` array (low→high) and the `show-config` precedence view (high→low) are
  exact reverses and match the documented model root > admin-drift > per-agent > fleet — so the
  operator-facing precedence can't silently drift from what OpenClaw merges. (The merge *result* is
  verified live by the integration test.)
- [x] **T9.2 (verify-feature)** ✅ Live-verified on local Docker (OpenClaw 2026.5.26): deployed
  `$include` array; layers deployed from the live repo (`overlayBehaviorDir`); OpenClaw effective
  merge shows **union** + **per-agent > fleet** + **admin-drift > per-agent > fleet**; fleet
  propagation on `refresh` with baseline rewritten in lockstep (no false violation); pre-deploy
  **fail-closed** on a reserved-key fleet source (never reached host); egress-gap warning on
  `mcp.servers.*.url`; orphan baselines cleaned on removal; `show-config` CLI+JSON render the 4 layers.
  Torn down after. See README "T9.2 Live verification" table.
- [ ] **R1 (post-merge)** `terraform-provider-conga` release (this PR touches `pkg/`).

## Open checkpoints (spec §12)
- [ ] **C1** Final file names (avoid `custom.json` / `agent-custom.json` / `agent-managed-custom.json`
  confusion).
- [ ] **C2** De-embed S3 path + first-boot ordering + integrity of the synced defaults.
- [ ] **C3** Source-removed reconciliation (deploy `{}` vs drop the include).
