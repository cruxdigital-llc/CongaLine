# Technical Specification — Infrastructure-Only Simplification

> Status: Spec (GLaDOS `spec-feature`). Builds on `requirements.md`, `plan.md`,
> `research-openclaw-config.md`. Implementation gated on the security standards review (§12).

## 1. Summary

Stop treating `openclaw.json` as a disposable, fully-regenerated artifact. Conga keeps full
authority over a **managed root** `openclaw.json` but delegates everything else to an
**admin-owned include file** that Conga never reads or overwrites. OpenClaw's native `$include`
deep-merges the two at load time, with the **root taking precedence** on conflicts. Admin edits
(e.g. an `mcp.servers.linear` block) survive restarts, refreshes, binds, and re-baselines.

## 2. Chosen architecture (decisions locked)

| Decision | Choice | Source |
|---|---|---|
| Approach | **C — `$include` layering** (validated live, §research 5b) | confirmed |
| Root ownership | **Conga owns root `openclaw.json`**; admin owns `agent-custom.json` | operator, 2026-06-09 |
| Merge precedence | **Root wins on scalar conflicts; objects union (include can ADD keys)** | verified live (probe3/probe4) |
| Re-baseline | **`conga agent rebaseline <name>`** (backs up include, resets) | operator |
| Migration | **Regenerate fresh** (managed root + empty include; no carry-over of ad-hoc edits) | operator |
| Integrity | **Hash managed root + validate effective channel allowlist** (union finding, §5.5/§12) | operator + security gate |
| `agent-custom.json` perms | **Read-only to agent uid (0444)**; admins edit via privileged path | security gate |
| `openclaw` CLI | **Validation only** (`config validate`), not mutation (§research 5c) | confirmed |

### Verified mechanics (image `2026.5.26`, on `aaron`)
- `$include` merges + validates; survives restart + hot-reload.
- On conflicting keys, **the root file wins** (`gateway.port`, `session.dmScope` probes).
- OpenClaw **fails closed** on owned-writes (`config set`/`patch`) when root has `$include` —
  never flattens. Gateway does **not** owned-write at startup.

## 3. File layout & ownership

Per agent, in the agent data dir (`~/.conga/data/<name>/`, `/opt/conga/data/<name>/`):

```
openclaw.json        # CONGA-OWNED. Regenerated wholesale on provision/refresh/bind.
                     # Identical content to today + one added directive:
                     #   "$include": ["agent-custom.json"]
agent-custom.json    # ADMIN-OWNED. Created once (empty {}). Conga NEVER reads or writes it
                     # after creation (except `rebaseline`, which backs it up + resets to {}).
```

- **Path constraint** (upstream): includes must stay inside the top-level config dir — satisfied
  (same dir). Use a **relative** include path (`"agent-custom.json"`), not absolute.
- The include is JSON5 (admins may use comments / trailing commas). Conga treats it as **opaque**
  and never parses or rewrites it → comments preserved.

## 4. The Conga-owned key set (the contract)

Conga writes exactly these into the managed root (unchanged from today's generator — see
`research-openclaw-config.md` §2). Everything NOT in this set is admin territory.

- `$include` (new — the only structural addition)
- `gateway.*` — `port`, `mode`, `bind`, `controlUi.allowedOrigins`, `auth.{mode,token}`
- `channels.<bound-platform>.*` — from bindings (slack today)
- `plugins.entries.<channel>.enabled`
- `agents.defaults.{model, models, subagents}` — when an `agent.yaml` overlay is present
- `models.providers.*` — overlay-derived
- `messages.groupChat.visibleReplies`, `tools.alsoAllow` — team agents only
- Static shipped defaults: `agents.defaults.{workspace,heartbeat,contextPruning,compaction}`,
  `tools.profile`, `commands.*`, `session.dmScope`, `hooks.internal.*`, `skills.install.*`,
  `update.*`

**Invariant** (verified, with an important nuance):
- **Scalar/leaf conflicts: root wins.** An include cannot *change* a Conga-set value
  (`gateway.port`, `gateway.auth.token`, `gateway.bind`, `session.dmScope`, `tools.profile` all
  verified root-wins). ✅
- **Objects deep-merge (union): the include CAN ADD sibling keys** to a Conga-owned object, and
  can add entirely new sections. **Verified**: an include added `channels.slack.channels.<id>`
  and a new `channels.telegram` section to the effective config. ❌ This means an include can
  **extend** the channel allowlist even though it cannot overwrite Conga's entries.

**Security consequence**: the channel allowlist is a security boundary (security.md §Configuration
Integrity, "channel allowlist is security-critical"). Because the include can add channel entries
via deep-merge union, and the integrity monitor hashes only the root, an injected channel would be
**undetected**. This is mitigated in §5.5 (effective-allowlist validation) and §12. The
arrays-replace-vs-union behavior (e.g. `tools.allow`) is less clear from probing and is an
implementation checkpoint (§14).

## 5. Code changes

### 5.1 Config generation — `pkg/runtime/openclaw/config.go`
- In `GenerateConfig()`, after building the config map, set the include directive:
  `cfg["$include"] = []string{"agent-custom.json"}`. No other generator logic changes — the root
  is still the full deterministic generation it is today (no regression to the baseline content).
- Add a post-generation **validation hook** (§9): optionally shell out to `openclaw config
  validate` against the generated root to catch schema drift; non-fatal warning by default.

### 5.2 Provider write paths (local / remote / aws)
For each provider, two behaviors change; everything else (token read-back, env file, router
reconnect) is untouched:

1. **ProvisionAgent**: after writing `openclaw.json`, **create `agent-custom.json` if absent**
   with content `{}`, **readable but not writable by the agent uid** (target `0444`; ownership
   `root:root` on AWS, operator-owned/read-only-to-container on local/remote — per §12 mitigation
   2). Never overwrite an existing `agent-custom.json`. (File perms are defense-in-depth; the
   load-bearing control against channel injection is the §5.5 effective-allowlist check.)
2. **RefreshAgent / Bind / Unbind / RemoveChannel**: regenerate and write **`openclaw.json` only**
   (as today). **Never touch `agent-custom.json`.** This is the core behavioral change —
   refresh/restart no longer wipes admin customization (it lives in the untouched include).

Touch points (from research §2):
- Local: `provider.go` ProvisionAgent (~248), RefreshAgent (~761), `channels.go` regenerate (~346).
- Remote: `provider.go` ProvisionAgent (~251 SSH upload), RefreshAgent (~661).
- AWS: `channels.go` `regenerateAgentConfigOnInstance` (~514), provision/refresh.

### 5.3 Provider interface — `pkg/provider/provider.go`
Add one method: `ResetAgentCustomConfig(ctx, name) error` — backs up `agent-custom.json` to
`agent-custom.json.bak.<unixtime>` and rewrites it to `{}`. Implemented per provider (file ops via
local FS / SFTP / SSM atomic-write). Used by `rebaseline`.

### 5.4 CLI — `conga agent rebaseline <name>`
- New subcommand under `agent`. Calls `prov.ResetAgentCustomConfig(name)` then `prov.RefreshAgent`
  (so the gateway reloads). Prints the backup path. Idempotent.
- **Interface Parity (must, architecture.md §Interface Parity)**: ship CLI + JSON-input field +
  MCP tool together — `internal/cmd/` command, `internal/cmd/json_schema.go` field, and
  `internal/mcpserver/tools*.go` `conga_rebaseline_agent` with `DestructiveHint: true`. Consistent
  defaults across all three.
- Confirmation prompt unless `--yes`/`--force` (it discards admin drift); MCP skips the prompt
  (LLM is the user) but uses the destructive hint.

### 5.5 Integrity monitor — hash root **+ validate the effective channel allowlist**
- Local `integrity.go`, Remote `integrity.go`, AWS `check-config-integrity.sh` +
  `user-data.sh.tftpl`: continue to SHA256-hash `openclaw.json` (the managed root) as the operator
  chose. `agent-custom.json` is **not** hashed (it is expected to drift).
- **NEW, required by the §4 union finding**: the integrity check MUST additionally validate the
  **effective merged channel allowlist** against Conga's authorized binding set. Concretely, after
  resolving the merge (e.g. `openclaw config get channels` in-container, or comparing the merged
  `channels.*` to the agent record's `Channels`), alert if the effective allowlist contains any
  channel/section Conga did not author. This closes the deep-merge-union gap whereby
  `agent-custom.json` could inject a channel binding undetected. Treated as a security-boundary
  check, not advisory.
- Baseline (root hash) is recomputed after each root write, as today.
- Rationale: hashing the root alone is necessary but **not sufficient** once an include can union
  new keys into security-critical objects; the effective-allowlist check restores the security
  boundary the operator's "hash root only" choice would otherwise weaken.

## 6. Data models

- **Agent record** (`~/.conga/agents/<name>.json`, `/opt/conga/agents`, SSM): **unchanged**.
- **`agent-custom.json`**: free-form OpenClaw config subset, admin-authored, opaque to Conga. No
  schema enforced by Conga (OpenClaw validates the merged result at load). Conga only ever writes
  `{}` (provision/rebaseline) or backs it up.
- **Manifest** (`pkg/common` manifest tracking): record that `agent-custom.json` exists for an
  agent (so teardown/reconcile know about it); do not track its contents.

## 7. Lifecycle walk-throughs

- **Provision**: write managed root (with `$include`) + create empty `agent-custom.json`. Start
  container. Admin edits `agent-custom.json` → restart/hot-reload merges it.
- **Refresh / bind / secret rotation**: regenerate root only; include untouched → admin keys
  persist. Owned keys (channels/token) update correctly.
- **Rebaseline**: back up + reset `agent-custom.json` to `{}`, refresh. Admin drift discarded
  intentionally.
- **Teardown**: remove both files with the data dir (existing behavior).

## 8. Migration (regenerate-fresh)

On first refresh/provision after upgrade for an existing agent:
- Regenerate the managed root from defaults+overlays+bindings (adds `$include`).
- Create `agent-custom.json` = `{}` if absent.
- **No automatic carry-over** of ad-hoc keys an operator may have hand-added directly to
  `openclaw.json` (those are wiped today anyway). 
- **One-time operator advisory** (release notes + a stderr notice on first refresh): "Direct edits
  to `openclaw.json` are no longer preserved; move custom config into `agent-custom.json`."
- Idempotent: re-running does not clobber an existing non-empty `agent-custom.json`.

## 9. CLI-for-validation (read-only)

After generating the managed root, Conga MAY run `openclaw config validate` (in-container or a
throwaway `docker run` of the pinned image) to verify the generated root against the exact image
schema. Read-only → no `$include` fail-closed conflict, no comment loss. Default: warn-only
(non-fatal) to avoid coupling provisioning to container availability; a `--strict-validate` flag
can make it fatal. This closes the hand-maintained-key-spelling risk (research open Q#4).

## 10. Edge cases & error handling

- **`agent-custom.json` missing at runtime**: `$include` of a missing file — must verify OpenClaw
  behavior (likely ignores or errors). Mitigation: Conga always ensures it exists (`{}`) before
  starting the container; provision/refresh re-create it if deleted.
- **Admin writes invalid JSON5 in the include**: OpenClaw fails to load/merge → gateway may not
  start or keeps last-good. Surface via `conga status`/logs; document that the admin owns
  correctness of their file. (Optional: `conga agent validate <name>` wrapping `openclaw config
  validate`.)
- **Admin tries to override an owned key**: silently loses (root wins) — document this so admins
  aren't surprised their `gateway.*`/`channels.*` override "does nothing."
- **Hot-reload during write**: keep writes atomic (reuse AWS atomic write pattern; add to
  local/remote where missing) to avoid OpenClaw reading a half-written root.
- **`$include` blocks `openclaw config set`**: documented operator UX — edit `agent-custom.json`
  directly or use Conga CLI.

## 11. Testing strategy

- **Unit**: generator emits `$include: ["agent-custom.json"]`; byte-equality of the rest of the
  root vs today (no regression); owned-key set unchanged.
- **Provider unit/integration** (per provider): provision creates empty `agent-custom.json`;
  refresh/bind rewrite root but leave a non-empty `agent-custom.json` untouched; rebaseline backs
  up + resets.
- **Integration (the acceptance test)**: add `mcp.servers.linear` to `agent-custom.json` →
  `conga refresh` → assert it persists AND a channel bind still applies to the root.
- **Migration**: existing agent → first refresh → root gains `$include`, empty include created,
  agent healthy.
- **Security regression (QA-required)**: an `agent-custom.json` that injects a `channels.*` entry
  must be **detected** by the effective-allowlist check (§5.5) — assert the integrity/validation
  path flags a channel not present in the agent record. Guards the deep-merge-union finding (§4).
- **Live (verify-feature)**: on AWS, add an MCP server to one agent's include, `systemctl restart`,
  confirm via `conga_container_exec` that it survives and the agent runs. (Mechanics already proven
  on `aaron`.)

## 11a. Data Safety (required — architecture.md §Agent Data Safety, must)

This feature touches config files only; it must not touch agent data (memory, workspace, sessions,
canvas, identity).

- **What changes**: `openclaw.json` (regenerated, as today) and `agent-custom.json` (created once,
  reset only by explicit `rebaseline`). Both are **config**, not data.
- **Data dir untouched**: `agents/main/sessions/*`, `workspace/`, memory, and logs under the data
  dir are never read or written by this feature. Refresh continues to "rebuild config, not data."
- **`rebaseline` scope**: backs up + resets **only** `agent-custom.json`; never the data dir. The
  backup (`agent-custom.json.bak.<ts>`) stays in place.
- **Migration**: regenerate-fresh rewrites the managed root and creates an empty include — **no
  data dir mutation**. Existing sessions/memory are preserved.
- **Volume-mount contract**: unchanged (`-v <data>:<ContainerDataPath>:rw`).

Conclusion: agent data integrity is preserved. ✅ Not a blocking concern.

## 12. Security considerations (GATES implementation)

Per CLAUDE.md, security-relevant → `product-knowledge/standards/security.md` review required.
- **Owned-key protection (scalars)**: verified root-wins means an include cannot *overwrite*
  `gateway.auth.token`, `gateway.bind/port`, or `session.dmScope`. ✅
- **Channel-allowlist injection (deep-merge union) — the key risk**: an include CAN **add**
  `channels.*` entries/sections (verified §4). The channel allowlist is a declared security
  boundary. Threat: a prompt-injected agent (uid 1000) that can write `agent-custom.json` adds
  itself to other channels, escalating reach — exactly Principle 1/2's threat model.
  **Mitigations** (all in scope):
  1. **Effective-allowlist validation** (§5.5) — detect any channel not authorized by the agent
     record. Primary control.
  2. **`agent-custom.json` ownership** — set `root:root` mode `0444` (agent reads, cannot write);
     admins edit via their privileged path (local FS / SSH / SSM as root). Reduces the
     agent-as-attacker vector. Caveat: the config dir is writable by uid 1000 for OpenClaw
     hot-reload `.tmp` files (existing accepted residual risk, CLAUDE.md), so file perms are
     defense-in-depth, not a hard guarantee — hence (1) is the load-bearing control.
  3. Document that admins must not place secrets in `agent-custom.json` (mode 0444, not a secret
     store; secrets stay in `.env`, Issue #9627).
- **Integrity**: root hash preserved (tamper detection on Conga-owned root) **plus** the
  effective-allowlist check (§5.5). Admin drift in non-security sections (mcp/skills/etc.) is out
  of scope by design.
- **Secrets**: continue to flow via `.env`, never the JSON (Issue #9627). Admins must not put
  secrets in `agent-custom.json` in cleartext — document; the include is mode 0644 like the root.
- **Egress**: an admin-added `mcp.servers` remote endpoint is subject to the existing egress
  allowlist. Document that new endpoints need allowlisting (mirror the overlay egress-gap warning).
- **`agent-custom.json` permissions**: 0644 owned by uid 1000 (OpenClaw must read it; it is not a
  secret store).

## 13. Out of scope

Typed `mcp:`/`skills:` overlay schema in `agent.yaml`; storage changes for agent records/secrets;
Approach B/D (rejected); application code. `.bak.N`/`.last-good` rotation ownership (confirm during
implementation but no change planned).

## 14. Open implementation checkpoints

1. Verify OpenClaw behavior when the `$include` target file is **absent** (drives the "always
   create `{}`" guarantee).
2. Confirm atomic-write parity across providers (AWS has it; add to local/remote).
3. Confirm `pkg/` change → `terraform-provider-conga` release needed (yes per CLAUDE.md).
4. Decide whether `conga agent validate` (read-only `openclaw config validate` wrapper) ships now
   or later.
5. **Confirm array-merge semantics** under `$include` (`tools.allow`, `allowFrom`, fallbacks):
   replace vs union. Probe was inconclusive; affects whether arrays need the same effective-value
   guard as `channels`.
6. **Implement the effective-allowlist check (§5.5) per provider** — how to read the merged
   `channels` cheaply in each (local/remote: in-container `openclaw config get`; AWS: SSM exec or
   compare merged file). Define the alert path (reuse the integrity-violation log/journal).

## 14a. Implementation notes (verify-feature retrospection, 2026-06-09)

Divergences from this spec, all intentional and verified:

- **§5.5 integrity — stricter than specified.** Spec called for "validate the effective merged
  channel allowlist against the agent record." Implemented instead as
  `common.ValidateAgentCustomConfig`: the include must not declare ANY Conga-owned top-level key
  (`$include`/`channels`/`gateway`/`plugins`). This is simpler and stronger (forbids the whole
  security-boundary surface, not just channel diffs) and avoids resolving merges in Go. Wired into
  local + remote `RunIntegrityCheck` and the AWS `check-config-integrity.sh` (jq).
- **New seam:** `Runtime.CustomConfigFileName()` (openclaw→`agent-custom.json`, hermes→`""`) gates
  all include behavior cleanly per runtime (not in the original §5 plan).
- **Self-healing on every root write** (not just provision) — driven by the verified C1 finding
  that a missing `$include` target invalidates the whole config. Each provider's
  `ensureAgentCustomConfig` runs after provision/refresh/bind.
- **Perms reality:** AWS re-protects `agent-custom.json` root:root 0444 (verified). Local creates
  0644 (operator-owned); remote ends up uid-1000-owned after its recursive chown — so the §5.5
  detection control is load-bearing there, as anticipated.
- **JSON5 limitation (documented residual):** the key-name check strict-parses and surfaces
  `ErrCustomConfigUnparseable` / a WARN rather than risk unsafe comment-stripping (URLs contain
  `//`). An attacker writing JSON5 evades the key-name check (WARN, not blocked); compensated by
  AWS perms. Optional hardening: in-container `openclaw config get channels` (tasks T3.5).
- **Verified live** on `aaron`/`2026.5.26`: MCP-in-include survives restart; the integrity jq guard
  flags an injected `channels` and passes a clean mcp include.

## 15. Handoff

`/glados:implement-feature` — implement §5 across `pkg/runtime/openclaw`, the three providers, the
CLI, and integrity; land tests per §11; then `/glados:verify-feature` (incl. the live MCP-survival
check) and the security gate (§12).
