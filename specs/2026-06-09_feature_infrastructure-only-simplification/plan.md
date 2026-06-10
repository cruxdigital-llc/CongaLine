# High-Level Plan — Infrastructure-Only Simplification

> Plan altitude: approach + decisions, not implementation detail. The detailed design
> lands in `/glados:spec-feature`.

## The core tension

Conga can't simply "stop touching `openclaw.json`." Some of what it writes is genuinely
operational and must keep updating after first deploy:

- **Conga-owned** (must stay authoritative): `gateway.bind`/port, `allowedOrigins`,
  `channels.*` (from bindings), secret/token wiring, `agents.defaults.model` + `subagents`
  (from `agent.yaml`), team channel-discipline keys.
- **Admin-owned** (must survive): `mcpServers.*`, and any other section an operator adds for
  the agent's specific users/workload.

The feature is fundamentally about drawing — and enforcing — that boundary.

## Approaches considered

> **Updated after `research-openclaw-config.md`.** Upstream supports native `$include`
> deep-merge, and the config is JSON5 — both change the trade-offs below.

| | Approach | Drift survives? | Conga ops still work? | Risk |
|---|---|---|---|---|
| A | **Provision-once, never re-touch** | ✅ | ❌ bind/refresh/secret-rotation stop updating config | Low complexity, high operational regression |
| B | **Conga-owned-keys deep-merge** (read-merge-write existing file) | ✅ | ✅ | Medium — robust merge needed; **strips JSON5 comments** on rewrite (4c) |
| **C** | **Layered config via `$include`** (Conga owns a managed file, admin owns an included file; OpenClaw deep-merges) | ✅ | ✅ | Low — **validated live** (§5b); fails closed, never flattens |
| D | **Conga drives `openclaw config patch`** (CLI-managed merge) | ✅ | ✅ | Medium — validated/version-correct + `null`-deletes (§5c), but **strips admin comments** and **needs in-container exec** per change |

### Recommended: **Approach C** — validated on `aaron` (2026-06-09)

The original recommendation was B. Config research (§4) plus a live experiment on the pinned
`2026.5.26` image (`research-openclaw-config.md` §5b) make **C the recommendation**:

- **`$include` is real and robust** — merges deeply, validates, survives restart + hot-reload,
  and **fails closed (never flattens)**. Confirmed live.
- **Conga owns the root `openclaw.json`**, regenerated wholesale, with `$include` → an
  admin-owned file the admin edits directly. Conga never parses the admin file → the JSON5
  comment-stripping problem of B disappears, and the integrity monitor gets a clean target
  (hash only the Conga-managed root).
- **Against B**: read-merge-write must parse admin **JSON5** and would strip comments on every
  rewrite — surprising data loss.

**Known trade-off** (from §5b): with a root `$include`, OpenClaw refuses in-container
`openclaw config set`/`configure` for root keys (fails closed). Operators edit the include file
directly or use the Conga CLI — acceptable for an infra-managed deployment, but must be documented.

**On using the `openclaw` CLI** (Approach D, §5c): evaluated and rejected *for mutation* — `config
patch` is validated and version-correct but **strips admin JSON5 comments** and requires
in-container execution per change, which works against this feature's goal. **Adopt the CLI for
read-only validation instead**: shell out to `openclaw config validate`/`schema` to check Conga's
generated managed file against the exact image version (no write → no fail-closed conflict),
removing the hand-maintained-key-spelling risk. Net: **Approach C for ownership + CLI for validation.**

## Affected components (current-state anchored)

- `pkg/runtime/openclaw/config.go` — factor `GenerateConfig()` so the **set of Conga-owned
  paths** is explicit and a merge-into-existing path exists alongside full generation.
- `pkg/common/config.go` — `RuntimeGenerateAgentFilesWithOverlay()` gains a baseline-vs-merge mode.
- Provider write paths — `localprovider/provider.go`, `remoteprovider/provider.go`,
  `awsprovider/channels.go:regenerateAgentConfigOnInstance` — switch refresh/bind from
  overwrite to read-merge-write (provision stays full-write).
- Integrity — `localprovider/integrity.go`, `remoteprovider/integrity.go`,
  `scripts/check-config-integrity.*` + AWS `user-data.sh.tftpl` timer — re-scope from whole-file
  hash to **Conga-owned-subset** validation (or drop hard reversion entirely; today it already
  doesn't auto-revert except token regen).
- CLI — an explicit `conga ... --reset-config` / `conga agent rebaseline` affordance (name TBD)
  for opt-in return to generated baseline.

## Phased delivery (high level)

1. **Foundation** — define the Conga-owned key set as a single source of truth; implement a
   deep-merge that overwrites owned paths and preserves all others. Unit-test the merge in
   isolation (admin keys preserved, owned keys updated, missing-file → baseline).
2. **Local provider** — wire refresh/bind to merge-mode; prove restart survival end-to-end.
3. **Integrity re-scope** — adapt the monitor to the owned-key boundary; security review.
4. **Remote + AWS parity** — same merge semantics over SSH and on-host; AWS timer + atomic write.
5. **Re-baseline affordance + migration** — explicit reset command; safe first-refresh-after-upgrade.

## Key decisions to resolve in spec

1. **Approach C confirmed; decide root ownership** — `$include` layering is validated (§5b).
   Remaining call: Conga-owns-root + admin-include (recommended) vs the inverse, and document the
   in-container owned-write trade-off.
2. **Owned-key set** — the exact JSON-path list Conga claims authority over. Everything else is
   admin territory. This list *is* the contract. (Confirmed footprint: `research-openclaw-config.md` §2.)
3. **Merge semantics for collections** — `channels` and `allowedOrigins`: full-replace the
   Conga-managed entries while preserving admin-added ones, vs. replace-the-whole-array. Define
   precisely (esp. how to delete a binding without nuking admin entries). (Largely OpenClaw's
   deep-merge job under C, but our managed-side shape still needs definition.)
4. **Integrity monitor's new job** — hash the Conga-managed root file only (clean under C), vs.
   validate-JSON + owned-keys present, vs. retire hard checks. Security sign-off hinges on this.
5. **Re-baseline UX** — flag vs. subcommand; whether it backs up the drifted file first.
6. **Migration** — behavior of the *first* refresh after operators upgrade to this version, given
   some may already have hand-edited (and lost) config in the field.
7. **`.last-good` / `.bak.N` origin** — confirm whether OpenClaw or Conga creates these; decide if
   Conga should snapshot before merge.

## Risks

- **Merge correctness** — a buggy deep-merge silently corrupts agent config. Mitigation:
  isolated, heavily-tested merge function; atomic write + pre-merge backup on all providers
  (today only AWS backs up).
- **Security regression** — loosening integrity could mask real tampering. Mitigation: owned-key
  validation + explicit security.md review (gating).
- **Three-provider drift** — merge implemented inconsistently across providers. Mitigation: merge
  logic lives in `pkg/common`/`pkg/runtime`, providers only choose mode (Interface Parity must).
- **Hot-reload races** — merging while OpenClaw is hot-reloading `.tmp` files. Mitigation: write
  atomically; reuse the AWS atomic-write pattern everywhere.

## Out of scope (recap)

Typed `mcpServers:` overlay schema; storage changes for agent records/secrets; layered
multi-file config (Approach C); any application code.

## Testing strategy (QA persona)

- **Unit**: merge function — owned updated, admin preserved, deletions, missing file, malformed
  on-disk JSON; byte-equality regression on provision baseline vs. today.
- **Integration**: per-provider — add `mcpServers.linear` by hand → `conga refresh` → assert it
  persists and a channel bind still applies.
- **Live (verify phase)**: AWS fleet — hand-add an MCP server to one agent, `systemctl restart`,
  `conga refresh`, confirm via `conga_container_exec` that the server survives and the agent runs.
