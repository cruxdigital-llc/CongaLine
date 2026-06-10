# Implementation Tasks — Infrastructure-Only Simplification

> Derived from `spec.md`. Ordered so each phase is independently testable. `[ ]` = todo.
> All code under `pkg/` (→ requires a `terraform-provider-conga` release on completion).

## Phase 1 — Foundation: generator emits `$include` ✅ DONE
- [x] **T1.1** `config.go` injects `config["$include"] = []string{AgentCustomConfigFile}`; const in
  `container.go`. Added `Runtime.CustomConfigFileName()` (openclaw→"agent-custom.json", hermes→"").
- [x] **T1.2** `TestGenerateConfig_IncludesAdminCustomFile` (user/team/overlay); existing suite is
  the no-regression guard ($include is purely additive). Passing.

## Phase 2 — Provider write paths ✅ DONE (tests pending in P6)
- [x] **T2.1** Const lives in `openclaw` runtime; each provider has its own `ensureAgentCustomConfig`
  (FS / SSH / SSM) — gated on `rt.CustomConfigFileName() != ""`. **C1 verified**: a missing $include
  target invalidates the config, so the helper runs after **every** root write (self-healing).
- [x] **T2.2** Local: helper + calls at provision, refresh, and bind-regenerate. Creates `{}` (0644)
  only if absent; never clobbers.
- [x] **T2.3** Remote: helper (`test -e || printf '{}'` via SSH) + calls at provision, refresh,
  bind-regenerate.
- [x] **T2.4** AWS: create-if-absent in `regenerateAgentConfigOnInstance`; **re-protect root:root
  0444** after the recursive chown (read-only to agent uid on the hardened provider).
- [ ] **T2.5** Tests per provider (deferred to P6).
- [ ] **T2.6 (NEW)** AWS bootstrap `user-data.sh.tftpl`: the boot-time bash-generated openclaw.json
  does not yet emit `$include`/create the include. Self-heals on first `conga refresh` (Go path);
  add `$include` + include creation to the bootstrap bash for fresh-deploy correctness. (tftpl =
  no provider release.)

## Phase 3 — Integrity: hash root + validate effective channel allowlist (security-critical)
- [ ] **T3.1** Keep the existing root-hash baseline (`saveConfigBaseline`/`checkConfigIntegrity`)
  unchanged (target stays `openclaw.json`). Confirm `agent-custom.json` is **not** hashed.
- [ ] **T3.2** Add **effective-allowlist validation**: resolve the merged `channels.*` (in-container
  `openclaw config get channels`, or compare merged file) and assert every channel/section maps to
  a binding in the agent record (`AgentConfig.Channels`, platform-agnostic). Alert on any extra.
  Wire into the integrity check path (local/remote `integrity.go`; AWS `check-config-integrity.sh`
  + `user-data.sh.tftpl`). Reuse the existing violation log/journal path.
- [ ] **T3.3** Security regression test: an include injecting `channels.<id>` is flagged by T3.2.

## Phase 4 — `conga agent rebaseline` (Interface Parity: CLI + JSON + MCP)
- [ ] **T4.1** Provider interface (`pkg/provider/provider.go`): add
  `ResetAgentCustomConfig(ctx, name) error`; implement in all three providers (backup
  `agent-custom.json.bak.<unixtime>`, rewrite `{}`).
- [ ] **T4.2** CLI (`internal/cmd/`): `conga agent rebaseline <name>` → reset + `RefreshAgent`;
  prints backup path; `--yes`/`--force` skips confirm.
- [ ] **T4.3** JSON schema (`internal/cmd/json_schema.go`) + MCP tool
  (`internal/mcpserver/tools*.go`: `conga_rebaseline_agent`, `DestructiveHint: true`). Consistent
  defaults across all three.
- [ ] **T4.4** Tests: reset backs up + empties; idempotent; refresh reloads.

## Phase 5 — Migration, perms, validation hook
- [ ] **T5.1** Migration (regenerate-fresh): first refresh/provision for an existing agent adds
  `$include` to the root and creates an empty include if absent; never clobbers a non-empty include.
- [ ] **T5.2** One-time operator advisory (stderr notice on first refresh + release-note text):
  "Direct edits to openclaw.json are no longer preserved; use agent-custom.json."
- [ ] **T5.3** `agent-custom.json` perms: target `0444`, not writable by agent uid; ownership per
  provider (root:root AWS; operator-owned read-only-to-container local/remote). Document the
  dir-writable caveat (defense-in-depth; T3.2 is the load-bearing control).
- [ ] **T5.4** Validation hook (§9): optional `openclaw config validate` of the generated root;
  warn-only by default, `--strict-validate` makes it fatal. (May defer per open checkpoint #4.)

## Phase 6 — Integration / live / docs
- [ ] **T6.1** Integration (acceptance): add `mcp.servers.linear` to include → `conga refresh` →
  persists AND a channel bind still applies to root.
- [ ] **T6.2** Migration integration: pre-existing agent → first refresh → root gains `$include`,
  empty include created, healthy.
- [ ] **T6.3** Live verify (AWS, in verify-feature): MCP-server-in-include survives `systemctl
  restart` (mechanics already proven on `aaron`).
- [ ] **T6.4** Docs: update `product-knowledge/standards/config-taxonomy.md` (new `agent-custom.json`
  locus — resolves the gate's `should` warning); operator note on the `config set` fail-closed UX.

## Open checkpoints to resolve during impl (from spec §14)
- [ ] **C1** OpenClaw behavior when `$include` target is **absent** (drives "always create `{}`").
- [ ] **C2** Atomic-write parity (AWS has it; add to local/remote root writes).
- [ ] **C3** Array-merge semantics under `$include` (`tools.allow`, `allowFrom`) — replace vs union.
- [ ] **C4** Per-provider mechanism for the T3.2 effective-allowlist read.

## Release (post-merge)
- [ ] **R1** `terraform-provider-conga` release (tag congaline → bump provider go.mod → tag/publish).
