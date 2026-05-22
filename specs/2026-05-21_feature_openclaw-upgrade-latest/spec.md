# Spec: OpenClaw Upgrade Latest

**From**: `ghcr.io/openclaw/openclaw:v2026.3.11`
**To**: `ghcr.io/openclaw/openclaw:v2026.5.18`

This spec is the implementation detail for the bump. Requirements live in
`requirements.md`; high-level phases live in `plan.md`. This document is
the operator-facing checklist plus the exact edits.

## Non-Goals (re-stated, load-bearing)

- No `openclaw.json` schema migration. If the new image refuses our
  rendered config, this spec **stops** and a separate migration spec is
  filed.
- No change to the two `:latest` fallbacks
  (`pkg/runtime/openclaw/container.go:23`,
  `pkg/provider/remoteprovider/provider.go:255, :651`). Those activate
  only when both the persisted config value and the user-set
  configuration are absent — a state this bump does not produce.
- No reconciliation of `agents/_defaults/<runtime>/<type>/*` against
  whatever the new upstream image bundles.

## Phase 0: Upstream changelog audit (BLOCKING)

Before any code change. Read OpenClaw `CHANGELOG.md` between `v2026.3.12`
and `v2026.5.18`.

Classification rubric:

- **Blocking** → STOP. Open a separate migration spec.
  - Required `openclaw.json` schema change (renamed field, removed
    field, new required field).
  - Required environment variable change (renamed, removed,
    different format).
  - Required on-disk layout change inside the container's data dir
    (mount path, file naming, permission expectations).
- **Adjacent** → continue, but Phase 3 verification adds a focused
  step for this area.
  - Changes to Slack monitor, socket-mode handling, app bootstrap.
  - Changes to the gateway / HTTP server / `allowedOrigins`.
  - Changes to model-selection logic, `/model` command, or model
    config loading.
  - Changes to plugin / skill loading that affect what we ship.
- **Irrelevant** → ignore.

**Artifact**: `specs/2026-05-21_feature_openclaw-upgrade-latest/changelog-review.md`
containing every Adjacent entry and the verification scenario it maps
to. The file is the precondition for Phase 1.

**Source**: pull the changelog directly from upstream during
implementation —
`https://raw.githubusercontent.com/openclaw/openclaw/v2026.5.18/CHANGELOG.md`
— rather than relying on release-notes summaries on the releases page.

## Phase 1: Repository changes (single commit)

> **Note**: the pre-implementation standards gate caught nine additional
> hardcoded version-string locations that the initial enumeration
> missed (CI cache, integration tests, manifest test fixture, README,
> security standard, TECH_STACK). Updated table below is the complete
> set.

Replace every hardcoded reference to `2026.3.11` with `2026.5.18`,
across three categories:

### A. In-repo defaults (5 files)

| # | File | Line | Notes |
|---|---|---|---|
| A1 | `terraform/environments/production/variables.tf` | 60 | Production env image default |
| A2 | `terraform/modules/congaline/variables.tf` | 4 | Module-level default |
| A3 | `terraform/modules/infrastructure/variables.tf` | 49 | Module-level default inside the agents object/map |
| A4 | `pkg/provider/remoteprovider/setup.go` | 190 | Non-interactive image default |
| A5 | `pkg/provider/remoteprovider/setup.go` | 193 | Interactive prompt default |

### B. Documentation + standards (6 files / paragraphs)

| # | File | Line(s) | Edit |
|---|---|---|---|
| B1 | `CLAUDE.md` | 92 | Rewrite the OpenClaw-pin paragraph: new tag, note that #45311 was resolved in `v2026.3.22` (PR #45953, Slack Bolt import-interop hardening), explain we pin specific minors for stability/bisectability not because of the specific regression. |
| B2 | `README.md` | 67 | TECH_STACK table image cell. Tag only. |
| B3 | `README.md` | 717 | "Pinned to **v2026.3.11**..." sentence — rewrite to drop the regression-reasoning and reference current pin. |
| B4 | `README.md` | 720 | Code-block tag. |
| B5 | `README.md` | 723 | The "Once the bug ... we'll update this" note — **delete entirely** (the bug is fixed and we ARE updating). |
| B6 | `product-knowledge/standards/security.md` | 41 | Universal Baseline table row: "(currently v2026.3.11)" → "(currently v2026.5.18)"; rationale rewritten to "Avoids regressions; pinned to a specific minor for bisectability". Drop the "Slack socket mode bug in v2026.3.12" specifics — that's history, not a current rationale. |
| B7 | `product-knowledge/TECH_STACK.md` | 38 | Tech-stack table image cell. Tag only. |

### C. Tests + CI (4 files / 6 occurrences)

| # | File | Line(s) | Edit |
|---|---|---|---|
| C1 | `internal/cmd/json_schema.go` | 43 | JSON-schema example string. |
| C2 | `internal/cmd/integration_helpers_test.go` | 26 | `const testImage` — bump so integration tests provision real containers against the new image. |
| C3 | `pkg/manifest/manifest_test.go` | 14 | YAML fixture string. |
| C4 | `pkg/manifest/manifest_test.go` | 55 | Expected assertion value (must match the fixture above). |
| C5 | `.github/workflows/ci.yml` | 58 | `key: docker-openclaw-2026.3.11` → `key: docker-openclaw-2026.5.18`. **Cache-busting**: bumping the key avoids loading a stale tarball under the new tag. |
| C6 | `.github/workflows/ci.yml` | 65, 67 | `docker pull` + `docker save` lines. Both occurrences. |

**Why C3/C4 matter even though `manifest_test.go` is "just a string"**: the manifest test's contract is that arbitrary image tags parse correctly. Bumping the fixture to the current production tag keeps the test aligned with what the operator actually sees today; an integration regression on the parser would have a clearer signal.

**Out of bounds** (do NOT touch):

- `pkg/runtime/openclaw/container.go:23` — `:latest` fallback (out-of-scope).
- `pkg/provider/remoteprovider/provider.go:255, :651` — `:latest` fallbacks (out-of-scope).
- `terraform/environments/production/.terraform/...` — Terraform cache, regenerated on `terraform init`.
- `agents/_defaults/...` — agent prompt templates (separate concern).
- `specs/2026-05-19_feature_local-model-routing/...` and `product-knowledge/PROJECT_STATUS.md:210` — historical references in completed/in-flight specs; do not rewrite history.

**Commit message** (suggested):

> bump openclaw image pin: v2026.3.11 -> v2026.5.18
>
> #45311 closed; fix shipped in v2026.3.22 via #45953 (Slack Bolt
> import-interop hardening). This commit moves the default tag in all
> hardcoded locations: 5 in-repo defaults (terraform vars, remote
> provider setup), 7 doc/standards references (CLAUDE.md, README.md,
> security.md, TECH_STACK.md), 4 test/CI updates (json schema example,
> integration test const, manifest test fixture, CI cache key + pull).
>
> No schema, no behavior change in this repo. The two `:latest`
> fallbacks in pkg/runtime/openclaw/container.go and
> pkg/provider/remoteprovider/provider.go are intentionally not touched.

**Expected diff size**: ~20 single-line edits + one `CLAUDE.md` paragraph rewrite + one `README.md` sentence rewrite + one `README.md` note deletion.

**Out of bounds** (do NOT touch):

- `pkg/runtime/openclaw/container.go:23` — `:latest` fallback.
- `pkg/provider/remoteprovider/provider.go:255, :651` — `:latest`
  fallbacks.
- `terraform/environments/production/.terraform/...` — Terraform
  internal cache; regenerated by `terraform init`.
- `agents/_defaults/...` — agent prompt templates, out of scope.

**Commit message** (suggested):

> bump openclaw image pin: v2026.3.11 -> v2026.5.18
>
> #45311 is closed (fix in v2026.3.22 via #45953, Slack Bolt
> import-interop hardening). This commit moves the default tag in all
> six in-repo locations plus the CLAUDE.md pin note. No schema, no
> behavior change in this repo. The two `:latest` fallbacks in
> `pkg/runtime/openclaw/container.go` and
> `pkg/provider/remoteprovider/provider.go` are intentionally not
> touched.

## Phase 2: Provider parity table

Confirmed during the open-questions probe at spec-time:

| Provider | Image storage | Operator update path | Container cycle path |
|---|---|---|---|
| Local | `~/.conga/local-config.json` field `image` (`getConfigValue`/`setConfigValue` in `pkg/provider/localprovider/provider.go`) | `conga admin setup --provider local` → re-prompts image, accept default or enter | `conga admin refresh-all` → `RefreshAgent` does `docker stop/rm/run` with new image |
| Remote | `~/.conga/remote-config.json` field `image` (same Go pattern in `pkg/provider/remoteprovider/provider.go`) | `conga admin setup --provider remote` → re-prompts image | `conga admin refresh-all` → SSH into remote, `docker stop/rm/run` |
| AWS | SSM parameter `/conga/config/image` (written by `awsutil.PutParameter` in `pkg/provider/awsprovider/provider.go:779`) | `conga admin setup --provider aws --profile openclaw --region us-east-2` → writes SSM | **`conga admin cycle-host`** — NOT `refresh-all`. The image is baked into each systemd unit's `ExecStart`; only `conga-image-refresh.service` rewrites those at boot. `refresh-all` only refreshes secrets/env files. |

This asymmetry (refresh-all vs cycle-host) is pre-existing and is **not**
fixed by this spec — it's noted here so the operator follows the right
command per provider. A follow-up could close the asymmetry by having
AWS `RefreshAll` invoke the image-refresh path; out of scope for this
bump.

## Phase 3: Verification scenarios

Five scenarios. Each is a "given / when / then" that produces a binary
pass/fail. All five must pass on a given provider before that provider
is marked smoked.

### Scenario S1 — Slack inbound, user agent

- **Given**: a user agent provisioned via `conga admin add-user` with a
  `--channel slack:<member-id>` binding.
- **When**: an authorized Slack user sends the agent a DM.
- **Then**: the agent replies via the same DM thread within ~30s. No
  errors in `docker logs conga-<name>` related to socket-mode or Bolt
  initialization.

### Scenario S2 — Slack inbound, team agent

- **Given**: a team agent provisioned via `conga admin add-team` with
  `--channel slack:<channel-id>` binding.
- **When**: an authorized Slack user `@mentions` the agent in the bound
  channel.
- **Then**: the agent replies in-channel within ~30s.

### Scenario S3 — Gateway-only mode

- **Given**: an agent provisioned **without** any Slack channel binding.
- **When**: operator runs `conga connect --agent <name>`.
- **Then**: a browser session to the printed URL returns HTTP 200, the
  chat UI renders, sending a message produces a reply, no "origin not
  allowed" errors. `/model` slash-command shows available models.

### Scenario S4 — Per-agent model overlay

- **Given**: an agent whose `agents/<name>/agent.yaml` is non-empty
  (per spec `2026-05-19_feature_local-model-routing`), pointing to a
  reachable non-default model (e.g. ollama or LiteLLM endpoint).
- **When**: the agent container starts and the operator opens the
  gateway / DMs the agent.
- **Then**: the configured model is the active one (visible via
  `/model`), and `/model default` flips back to the runtime default
  without restart.

### Scenario S5 — Egress proxy (remote + AWS)

- **Given**: an agent with `conga-policy.yaml` egress in `enforce` mode,
  one allowed domain (e.g. `api.anthropic.com`), one disallowed domain
  (e.g. `example.com`).
- **When**: from inside the container,
  `curl -x http://localhost:<envoy-port> https://api.anthropic.com` and
  `curl -x http://localhost:<envoy-port> https://example.com`.
- **Then**:
  - **Both remote and AWS**: the first returns 200/401 (Envoy allows
    on policy match), the second is blocked at the Envoy proxy.
  - **AWS only (additional)**: from host, `iptables -L DOCKER-USER`
    shows DROP rules for the agent's egress chain (the dual-layer
    enforcement is AWS-only per the egress feature spec).

## Phase 4: Rollout order

Strict ordering for safety:

1. **Local** (operator's dev machine; cheapest blast radius).
   - `git pull`, `make build`, `conga admin setup --provider local`,
     accept new image default.
   - `conga admin refresh-all`.
   - Run S1, S2 (if Slack credentials configured locally — optional),
     S3, S4.

2. **Remote** (lab Raspberry Pi or equivalent; medium blast radius).
   - `conga admin setup --provider remote`, accept new image default.
   - `conga admin refresh-all`.
   - Run S1, S2, S3, S4.

3. **AWS production** (highest blast radius — gated on prior two clean).
   - **Rollback rehearsal first** (QA-required). On a non-critical or
     clone agent, do one deliberate revert round-trip: write
     `v2026.3.11` back to SSM `/conga/config/image`, SSM-session into
     the host, `systemctl restart conga-image-refresh.service` then
     `systemctl restart conga-<agent>`, verify
     `docker inspect conga-<agent> | jq '.[0].Config.Image'` shows
     `v2026.3.11`. Then re-set SSM to `v2026.5.18` and repeat the
     restarts to confirm forward path also works.
   - `conga admin setup --provider aws --profile openclaw --region us-east-2`,
     accept new image default. (Writes SSM `/conga/config/image`.)
   - `conga admin cycle-host`. Wait for instance to come back; verify
     `/opt/conga/.bootstrap-complete` exists via SSM session.
   - Run S1, S2, S3, S4, S5.

A failure at any step pauses rollout. The Phase 1 commit can stay on
`main` (it only changes defaults — fresh installs and tfvars consumers
get the new tag, existing environments are untouched until their
operator runs the steps above).

## Phase 5: Post-rollout hygiene

After all three providers smoked clean:

1. `CLAUDE.md` already updated in Phase 1 — re-read and confirm the new
   pin paragraph is accurate.
2. Update auto-memory `project-openclaw-pin-revisit`:
   - Body: "Pin currently at `v2026.5.18`. #45311 resolved in
     `v2026.3.22`. Continue periodic check of OpenClaw release cadence
     before features that depend on schema/behavior — no specific
     known regression to watch right now."
   - **Why**: pin tracking is still useful as a practice (the next
     held-back pin won't announce itself), but the specific blocker is
     gone.
3. Add a `PROJECT_STATUS.md` "Recent Changes" entry with the bump date,
   the from/to tags, and a one-line verification summary.
4. **Soak window** (QA-required). Treat `v2026.5.18` as the resting
   pin for at least **7 calendar days of normal use on at least one
   production agent** before:
   - Bumping further (e.g. fast-follow to `v2026.5.20+`), or
   - Landing any feature spec that depends on schema/runtime behavior
     introduced between `v2026.3.11` and `v2026.5.18`.
   The soak window starts the day production cycle-host completes
   cleanly. Track on the `PROJECT_STATUS.md` "Recent Changes" entry.
5. (Optional) Open a follow-up issue for the `RefreshAll`-vs-`CycleHost`
   asymmetry on AWS, if the operator experience during this bump made
   it feel rough.

## Edge cases

- **Pull failure on the new tag**: `docker pull
  ghcr.io/openclaw/openclaw:v2026.5.18` returns auth error or
  manifest-not-found. → Treat as a Phase 1 prereq failure; do not
  commit until a manual `docker pull` succeeds with no auth and from
  a clean machine. (Public image; should be straightforward.)
- **Mid-rollout failure on AWS after `cycle-host`**: the new image is
  pulled but a container fails to start. → SSH into instance via
  SSM, `journalctl -u conga-<name>`. If root cause is image-incompat
  (rendered config rejected), execute the "Stop condition" from
  Phase 0 retroactively: revert the SSM image value to `2026.3.11`,
  run `systemctl restart conga-image-refresh.service`, then
  `systemctl restart conga-<name>`. File a migration spec.
- **Mixed-version fleet during partial rollout**: explicitly accepted.
  Slack regression fix means even the old image works as a Slack
  client; we're just bumping for currency. No coordination needed
  between providers.
- **Operator runs `conga admin refresh-all` instead of `cycle-host` on
  AWS**: secrets/env get refreshed but image stays old. → Diagnostic:
  `docker inspect conga-<name> | jq '.[0].Config.Image'` shows old
  tag. Fix: run `cycle-host`.

## Operator commands (copy-paste reference)

```bash
# Local
git pull && make build
conga admin setup --provider local
conga admin refresh-all

# Remote
conga admin setup --provider remote
conga admin refresh-all

# AWS
conga admin setup --provider aws --profile openclaw --region us-east-2
conga admin cycle-host

# Post-cycle health check (AWS)
aws ssm start-session --target $(aws ec2 describe-instances \
  --profile openclaw --region us-east-2 \
  --filters 'Name=tag:Name,Values=conga-*' 'Name=instance-state-name,Values=running' \
  --query 'Reservations[].Instances[].InstanceId' --output text) \
  --profile openclaw --region us-east-2
# Inside session:
test -f /opt/conga/.bootstrap-complete && echo READY || echo NOT READY
docker inspect conga-<agent> | jq '.[0].Config.Image'  # should show v2026.5.18
```

## Open questions explicitly closed by this spec

- **Q (from plan)**: any blocking changelog entries `v2026.3.12` …
  `v2026.5.18`? → Resolved by Phase 0 (gates the rest of the spec).
- **Q (from plan)**: operator command for updating the AWS SSM image
  parameter? → Resolved: `conga admin setup` (writes SSM via
  `awsutil.PutParameter` at `pkg/provider/awsprovider/provider.go:779`)
  + `conga admin cycle-host` (triggers the boot-time
  `conga-image-refresh.service`).
- **Q (from plan)**: fast-follow to `v2026.5.20`? → Deferred. Decide
  after `v2026.5.18` has soaked one to two weeks. Not in this spec.
