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
- [x] **T2.6** AWS bootstrap `user-data.sh.tftpl`: after the cp to the data dir, inject `$include`
  via `jq` and create `agent-custom.json` (root:root 0444); re-baseline the integrity hash from the
  post-`$include` `openclaw.json`. Fresh AWS deploys now layer correctly (not just post-refresh).

## Phase 3 — Integrity (security-critical) ✅ Go-side DONE; AWS-bash remaining
- [x] **T3.1** Root-hash baseline unchanged (target `openclaw.json`); `agent-custom.json` not hashed.
- [x] **T3.2** `common.ValidateAgentCustomConfig` (pkg/common/custom_config.go): forbids the include
  from declaring Conga-owned keys (`$include`,`channels`,`gateway`,`plugins`) — stricter than
  "validate merged allowlist" and robust (no unsafe JSON5 comment-stripping; surfaces
  `ErrCustomConfigUnparseable` instead). Wired into local + remote `RunIntegrityCheck` (new
  `checkAgentCustomConfig`, ALERT on reserved key, WARN on unparseable). Kept separate from the
  refresh-time hash check.
- [x] **T3.3** Security regression test: `custom_config_test.go` flags injected `channels` (+ gateway,
  plugins, $include) and the JSON5-unparseable case.
- [x] **T3.4** AWS: `check-config-integrity.sh` now also validates each agent's `agent-custom.json`
  via jq — CONFIG_INTEGRITY_VIOLATION (systemd-cat warning) if it declares
  `$include/channels/gateway/plugins`, WARN if it's not valid JSON. jq fragments verified locally
  (literal `$include` key, no false positives on `{}`/mcp, lists injected keys, WARN on invalid).
  **Still to re-audit at the post-implementation security gate.**
- [ ] **T3.5 (hardening, optional)** Authoritative JSON5-aware variant: `openclaw config get channels`
  in-container compared to the agent record, closing the JSON5-evasion gap noted in T3.2.

## Phase 4 — `conga agent rebaseline` ✅ DONE
- [x] **T4.1** `provider.Provider.ResetAgentCustomConfig` + impls (local FS, remote SSH, AWS SSM with
  root:root 0444 re-protect). Backs up `.bak.<unixtime>`, rewrites `{}`.
- [x] **T4.2** CLI `conga agent rebaseline <name>` (`agent_rebaseline.go`, registered in
  `agent_behavior.go`) → reset + RefreshAgent; `--yes` skips confirm.
- [x] **T4.3** JSON schema `agent.rebaseline` + MCP `conga_rebaseline_agent` (DestructiveHint).
- [x] **T4.4** Tests: `customconfig_test.go` (ensure create/preserve; reset backs up + empties).
  All provider/mcp/cmd suites pass.

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
- [x] **T6.4** Docs: `config-taxonomy.md` updated — new "Runtime customization (admin)" row +
  Example 6 (add Linear MCP) + the `config set` fail-closed operator note. Resolves the gate's
  `should` warning.

## Open checkpoints to resolve during impl (from spec §14)
- [ ] **C1** OpenClaw behavior when `$include` target is **absent** (drives "always create `{}`").
- [ ] **C2** Atomic-write parity (AWS has it; add to local/remote root writes).
- [ ] **C3** Array-merge semantics under `$include` (`tools.allow`, `allowFrom`) — replace vs union.
- [ ] **C4** Per-provider mechanism for the T3.2 effective-allowlist read.

## Release (post-merge)
- [ ] **R1** `terraform-provider-conga` release (tag congaline → bump provider go.mod → tag/publish).
