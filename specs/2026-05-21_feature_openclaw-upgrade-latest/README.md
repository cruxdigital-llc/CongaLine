# Feature Trace: OpenClaw Upgrade Latest

**Feature**: `openclaw-upgrade-latest`
**Started**: 2026-05-21
**Status**: Planning
**Lead**: TBD (pending persona selection)

## Purpose

Bump the deployed OpenClaw image pin from `v2026.3.11` → the current upstream
stable (`v2026.5.20` at time of writing), now that the Slack socket-mode
regression that held the pin back has been resolved upstream.

This is the deferred Phase 1 of the Local Model Routing feature
(spec `2026-05-19_feature_local-model-routing`), promoted into its own
spec so the pin bump lands as a discrete, bisectable change ahead of any
future feature that depends on the newer OpenClaw schema or runtime
behavior.

## Upstream Status Check (per `project-openclaw-pin-revisit` memory)

- **Holdback issue**: [openclaw/openclaw#45311](https://github.com/openclaw/openclaw/issues/45311)
- **State**: CLOSED (2026-04-25)
- **Fix release**: `v2026.3.22` via PR #45953 (Slack Bolt import-interop hardening)
- **Confirmation**: Reporter validated end-to-end on `v2026.3.22`; maintainer
  closed citing release-note evidence and on-main regression coverage in
  `extensions/slack/src/monitor/provider.interop.test.ts`.
- **Current latest stable**: `v2026.5.20` (released 2026-05-21).
- **Conclusion**: Pin holdback is no longer justified — proceed with bump.

## Active Personas

- **Architect** — schema/runtime compatibility, provider parity (local/remote/AWS), rollback strategy
- **QA** — verification plan covering Slack inbound, gateway, per-agent model overlay, egress proxy interaction post-bump

## Session Log

- **2026-05-21**: Session started; spec dir scaffolded.
- **2026-05-21**: Verified upstream Slack regression #45311 is closed; fix
  shipped in `v2026.3.22`; pin bump unblocked.
- **2026-05-21**: Personas selected — Architect, QA.
- **2026-05-21**: Requirements drafted. Scope = pin bump only; target =
  `v2026.5.18` (3-day soak, named as deferred Phase 1 target by sibling
  Local Model Routing spec). Success criteria: Slack inbound (user +
  team), gateway-only, model overlay, egress proxy, three-provider
  parity smoke, CLAUDE.md + memory hygiene.
- **2026-05-21**: Plan drafted. 6 phases — changelog review → single
  code-change commit → provider parity audit → 5-scenario verification
  per provider → opt-in per-environment rollout → memory/docs hygiene.
  Pin lives in 6 in-repo locations (3 Terraform, 2 Go provider, 1 JSON
  schema example) plus CLAUDE.md; two `:latest` fallbacks are explicitly
  out of scope.

## Persona Review (Spec Phase)

### Architect

**Verdict**: ✅ APPROVE.

- **No new dependencies**, no data-model change, no API contract change.
  An image-tag bump touched in six pre-existing locations.
- **Three-provider parity**: spec correctly identifies the AWS
  `refresh-all`-vs-`cycle-host` asymmetry (the SSM-baked-into-systemd-
  unit path) and routes the operator down the right command per
  provider. ✓
- **Bisectability**: single commit, six-line diff plus one paragraph in
  `CLAUDE.md`. Revert is trivial. ✓
- **Schema-change gate**: Phase 0 (upstream changelog audit) is the
  right guard against silent breakage. The artifact requirement
  (`changelog-review.md`) gives implementation a paper trail. ✓

**Notes for follow-up (not blocking)**:
- The `refresh-all`-vs-`cycle-host` asymmetry on AWS is documented in
  Phase 2 but knowingly not fixed. Worth a small follow-up spec to
  unify the rollout command across providers.
- The two `:latest` fallback paths
  (`pkg/runtime/openclaw/container.go:23`,
  `pkg/provider/remoteprovider/provider.go:255, :651`) are correctly
  out of scope. Latent risk noted; not introduced by this bump.
- GHCR tag immutability assumption: OpenClaw releases are immutable in
  practice; not codified in policy. Acceptable risk.

### QA

**Verdict**: ✅ APPROVE with three requests.

- **Unhappy path coverage** present: pull failure, mid-rollout
  container failure, mixed-version fleet, wrong-command operator
  mistake. ✓
- **Verifiable outputs** defined for all five scenarios. Each scenario
  has a concrete observable (HTTP 200, reply within 30s, `docker
  inspect` shows new tag, etc.). ✓
- **Rollout ordering** (local → remote → AWS) is correct: smallest
  blast radius first, gated on prior passes.

**Requests (folded into spec)**:
1. **S5 (Egress) should run on remote too**, not AWS-only. Remote runs
   Envoy in enforce mode per the egress feature spec; the iptables
   half is AWS-only, but the proxy half is verifiable on remote.
2. **Define a soak window**. A concrete "≥7 days of normal use on at
   least one production agent" gate before any further bumps or
   building features on top of `v2026.5.18`.
3. **Mid-rollout rollback rehearsal on AWS**: before
   `conga admin cycle-host` on production, do one deliberate rollback
   round-trip on a non-critical agent (or a clone host) to confirm the
   revert-SSM + restart-image-refresh path works as described.

**Note**: no automated tests added by this bump. Acceptable for a
one-time pin change; flagged as a recombobulate-worthy observation
(integration tests against the real runtime image catch this class of
bug earlier — already on the radar from the `2026-05-19` Local Model
Routing spec).

## Standards Gate Report (Pre-implementation)

| Standard | Scope | Severity | Verdict |
|---|---|---|---|
| `architecture.md` — Agent Data Safety | all (lifecycle ops) | must | ✅ PASSES — image bump does not touch volume mounts, data directories, or refresh-data paths. The "Refresh operations rebuild config, not data" rule is preserved (refresh-all and cycle-host both leave `/opt/conga/data/<name>/` untouched). |
| `architecture.md` — Interface Parity | all (cli/json/mcp) | must | ✅ PASSES — no new CLI flag, JSON field, or MCP parameter introduced. The change is purely in default values; existing surfaces unchanged. |
| `architecture.md` — Module Structure | pkg/internal split | must | ✅ PASSES — no code moves between `pkg/` and `internal/`. Edits are localized to one `internal/` file (`json_schema.go`) and one `pkg/` file (`remoteprovider/setup.go`); both already existed in their respective trees. |
| `architecture.md` — Provider Contract is API Boundary | all providers | should | ✅ PASSES — bump applies uniformly to all three providers via their respective config-storage mechanisms (SSM/local-config.json/remote-config.json). |
| `architecture.md` — Config Format Boundary | config files | should | ✅ PASSES — no new config files or formats. |
| `architecture.md` — Channel Abstraction (no deeper Slack coupling) | new code | should | ✅ PASSES — no Slack-specific logic introduced. The bump benefits Slack handling (clears #45311) but the change itself is platform-agnostic. |
| `config-taxonomy.md` — Decision rule for new per-agent concerns | per-agent config | must | ✅ PASSES — the image tag is global infrastructure config (already in tfvars on AWS), not a per-agent concern. No taxonomy slot is created or violated. |
| `egress-controls.md` — iptables active in ALL modes; defense-in-depth layers | egress | must | ✅ PASSES — no change to egress proxy, iptables rules, or DOCKER-USER chain. Verification scenario S5 explicitly re-tests the egress controls on the new image. |
| `security.md` — Pinned image baseline | universal | must | ⚠️ **WARNING → resolved by spec**. The security standard at line 41 hardcodes the old tag in the rationale. The spec's Phase 1 catalog (entry B6) updates this file in the same commit, so the standard stays in sync with reality. Without the standards-gate catch, this would have drifted. |
| `security.md` — Secrets via env vars, never config | universal | must | ✅ PASSES — no secret-handling change. |
| `security.md` — Config integrity monitoring | universal | must | ✅ PASSES — the integrity baseline is recomputed when `openclaw.json` regenerates as part of the bump. No change to the integrity-check mechanism. |
| `security.md` — Pinned to known-good version (rationale rewrite) | universal | should | ✅ PASSES — the spec rewrites the rationale from "avoid v2026.3.12 Slack regression" to "stability/bisectability against arbitrary upstream drift". More durable framing; reviewed and approved as part of B1/B6. |

**Result**: ✅ Gate PASSES. No `must` violations remain after spec
adjustments (the one `must` warning on `security.md:41` is closed by
spec entry B6). One blocking gap caught — nine additional hardcoded
version-string locations the initial enumeration missed (B2–B7, C2–C6)
— is fully absorbed into the spec's Phase 1 catalog.

## Spec adjustments from review

Folded into `spec.md`:
- Scenario S5 extended to include remote provider (proxy half only;
  iptables enforcement still AWS-only).
- Phase 5 expanded with a "soak window" item: ≥7 days at
  `v2026.5.18` on at least one production agent before any further
  bumps or dependent feature work.
- Phase 4 (AWS rollout) prepended with a "rehearsal" sub-step.
- **Phase 1 catalog expanded** (caught by standards gate): from 6
  files to 14 files / ~20 single-line edits, broken into three
  categories: A. in-repo defaults (5), B. docs + standards (7),
  C. tests + CI (6). The original 6 were correct but incomplete —
  README/TECH_STACK/security.md docs, integration test const,
  manifest test fixture, and CI cache key were all missed.

## Decisions

- **Scope**: pin bump only — no defaults reconciliation, no schema
  migration, no dependent feature work. (Memory: bisectable.)
- **Target version**: `v2026.5.18` over `v2026.5.20` (latest) for
  soak time, and over `v2026.3.22` (minimum-viable) for currency.
- **Stop condition**: any "blocking" changelog entry between
  `v2026.3.12` and `v2026.5.18` pauses this spec and spawns a separate
  migration spec.
- **Rollback**: `git revert` of pin commit + per-env `conga admin setup`
  re-pin. No data migration.
- **AWS rollout**: `cycle-host`, not `refresh-all` — image is baked
  into per-agent systemd ExecStart lines and only the boot-time
  `conga-image-refresh.service` rewrites them. `refresh-all` refreshes
  env files only (pre-existing asymmetry; not fixed here).

## Session Log (continued)

- **2026-05-21**: Spec drafted. Persona review (Architect + QA) passed
  with three QA asks folded into spec. Standards gate caught nine
  additional hardcoded version-string locations beyond the initial
  six — total Phase 1 catalog is now 14 files / ~20 line edits, split
  into A. defaults (5), B. docs/standards (7), C. tests/CI (6).
- **2026-05-21**: PROJECT_STATUS.md updated to reflect spec'd /
  reviewed / gate-passed state.
- **2026-05-21 (implementation)**: `tasks.md` drafted and approved.
  Phase 0 audit complete — `changelog-review.md` produced; 0 Blocking
  entries across 35+ releases / 7,173 lines (only `### Breaking`
  section was BlueBubbles iMessage removal, irrelevant to us). Two
  belt-and-suspenders Phase 3 checks added: `allowedOrigins` doctor
  seeding (#83286) and `fallbacks` warn (#79369).
- **2026-05-21 (implementation)**: Phase 1 commit prepared — all 14
  edits applied:
  - A1 `terraform/environments/production/variables.tf:60`
  - A2 `terraform/modules/congaline/variables.tf:4`
  - A3 `terraform/modules/infrastructure/variables.tf:49`
  - A4–A5 `pkg/provider/remoteprovider/setup.go:190, :193`
  - B1 `CLAUDE.md:92` (paragraph rewritten)
  - B2–B5 `README.md:67, :717, :720, :723` (last is a deletion)
  - B6 `product-knowledge/standards/security.md:41` (rationale rewritten)
  - B7 `product-knowledge/TECH_STACK.md:38`
  - C1 `internal/cmd/json_schema.go:43`
  - C2 `internal/cmd/integration_helpers_test.go:26`
  - C3–C4 `pkg/manifest/manifest_test.go:14, :55`
  - C5–C6 `.github/workflows/ci.yml:58, :65, :67`
- **2026-05-21 (implementation)**: Verification clean — `go build ./...`,
  `go vet ./...`, `gofmt -l .` all silent; `go test ./... -count=1`
  passes (20 packages, ~80s including pkg/aws). One intentional
  `2026.3.11` reference remains in `CLAUDE.md` (historical note about
  the previous pin) plus two in `PROJECT_STATUS.md` (feature-#28
  description and Local Model Routing deferred-Phase-1 note); both
  documented as intentional history per spec "Out of bounds".

- **2026-05-21 (live S3 smoke on local provider)**: **BLOCKING REGRESSION
  FOUND.** Provisioned a gateway-only test agent against the new image
  in an isolated `~/.conga-verify` data dir. Image pulled fine; the
  container exited with status 78 a few seconds after startup. Logs:
  ```
  [gateway] loading configuration…
  [gateway] resolving authentication…
  Gateway start blocked: set gateway.mode=local (current: remote) or
  pass --allow-unconfigured.
  Config write audit: /home/node/.openclaw/logs/config-audit.jsonl
  ```
  Traced to `/app/dist/run-B3RJX50x.js` in the new image:
  ```js
  if (params.allowUnconfigured || params.mode === "local") return [];
  // …
  return [`Gateway start blocked: set gateway.mode=local (current: ${params.mode}) or pass --allow-unconfigured.`, …];
  ```
  Our rendered `openclaw.json` emits `gateway.mode: "remote"`
  deliberately (CLAUDE.md line 88) — we need 0.0.0.0 bind inside the
  container so Docker `-p 127.0.0.1:<host>:18789` can deliver traffic.
  The new image treats `mode=remote` as a configuration requiring
  explicit opt-in. **My Phase 0 changelog audit missed this entirely.**
  My grep rubric scanned for "schema change / breaking / env var / data
  dir layout" — this validation was added without any of those
  signal-words in the changelog entry.
- **2026-05-21 (live S3 smoke)**: Verification environment torn down
  cleanly (`conga admin teardown --delete-data --force`); no leftover
  containers or networks. The repo's Phase 1 commit `685649e` on
  `chore/upgrade-openclaw` remains intact — not pushed, decision pending.

## Phase 0 Audit Gap — Postmortem

The Phase 0 rubric in spec.md asked four questions to identify Blocking
entries: schema change? env var change? on-disk layout? required
host-side migration? **None caught a new runtime validation on an
existing-and-still-accepted config field.** The field is unchanged in
shape — `gateway.mode` is still a string, still accepts `"remote"`,
still accepts `"local"`. What changed is that startup now rejects
`"remote"` without `--allow-unconfigured` (or with `--allow-unconfigured`).

**Concrete rubric fix for future bumps**: extend Phase 0 to include a
*live boot test* before signing off the audit — pull the candidate image,
mount the current rendered config, run the container, watch for any
non-zero exit within the first 30 seconds. This would have caught the
regression in seconds rather than after the commit landed.

(This rubric fix is itself a follow-up; not part of this bump.)

## Migration applied (in-scope expansion of the bump)

After consulting the user, the bump was expanded to absorb the
migration needed to keep our deployment compatible with the new image's
stricter validation. Two parallel issues surfaced during live testing:

### Migration 1 — gateway.mode "remote" → "local"

The OpenClaw schema doc in the new image is explicit:
> `gateway.mode`: "local" runs channels and agent runtime on this host,
> while "remote" connects through remote transport. Keep "local" unless
> you intentionally run a split remote gateway topology.

Our deployment runs gateway + agent runtime in the same container — we
are literally `mode=local`. The 0.0.0.0 binding (required for Docker
`-p` port forwarding) comes from `gateway.bind: "lan"`, not from `mode`.
CLAUDE.md's claim that "Gateway mode is always 'remote' (binds 0.0.0.0)"
conflated the two settings — a long-standing doc bug, not a behavior
need.

**Files changed (in-scope expansion):**
- `pkg/runtime/openclaw/config.go` — `mode: "remote"` → `"local"`,
  removed the now-dead `gateway.remote.url` block.
- `terraform/modules/infrastructure/user-data.sh.tftpl` — same edit in
  both bash JSON heredocs (lines 364, 453).
- `CLAUDE.md` line 73 — paragraph rewritten to explain
  mode-vs-bind distinction.

### Migration 2 — gateway auth token at provision time

The new image refuses to bind a non-loopback gateway without auth:
> Refusing to bind gateway to lan without auth.
> Container environment detected — the gateway defaults to bind=auto
> (0.0.0.0) for port-forwarding compatibility.
> Set OPENCLAW_GATEWAY_TOKEN or OPENCLAW_GATEWAY_PASSWORD, or pass
> --token/--password […]

**Root cause in our code**: both `ProvisionAgent` paths (local and
remote) were passing `""` as the gateway token to the config generator
on initial provision — so fresh agents booted with an unauthenticated
gateway config. `RefreshAgent` correctly generates/preserves a token,
but only after the first refresh. Old image silently allowed this; new
image refuses.

This was a latent security bug exposed by the new image. The fix is
also a real improvement.

**Files changed (in-scope expansion):**
- `pkg/provider/localprovider/provider.go` ProvisionAgent — generate
  token via `generateToken()`, pass to `GenerateConfig`.
- `pkg/provider/remoteprovider/provider.go` ProvisionAgent — same
  pattern, parity with local.

### Files NOT yet fixed (deferred to a follow-up spec)

- `terraform/modules/infrastructure/user-data.sh.tftpl` —
  `setup_user_agent` and `setup_team_agent` bash heredocs emit the
  initial AWS agent config with NO `gateway.auth` block. Same latent
  bug. Fix is bash-side (generate UUID, inject into heredoc + env
  file). Not part of this commit — needs operator validation on AWS
  before landing, which the spec's Phase 4 rollout will exercise.
- The `regenerateAgentConfigOnInstance` Go path on AWS DOES thread the
  token (it uses the same `pkg/runtime/openclaw/config.go` generator
  we just fixed), so the AWS gap is bootstrap-only.

### Live verification result (S3, local provider)

After both migrations:
- Container healthy on v2026.5.18 within ~5s of provisioning.
- `docker inspect` confirms image tag = `ghcr.io/openclaw/openclaw:2026.5.18`.
- `curl http://127.0.0.1:18789/` returns HTTP 200 (three probes:
  `/`, `/healthz`, and `/` with explicit `Origin: http://localhost:18789`).
- `docker logs conga-verify-gw | grep -iE 'origin not allowed|fallbacks'`
  is empty — both audit belt-and-suspenders checks clean (#83286 and
  #79369).
- `conga connect` returns a usable URL+token. Browser handoff still
  available to the operator if they want to finish the chat-reply +
  `/model` parts of S3.
- Verification env will be torn down after the operator decides on
  next steps.
