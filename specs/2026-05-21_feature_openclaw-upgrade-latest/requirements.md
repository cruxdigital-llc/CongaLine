# Requirements: OpenClaw Upgrade Latest

## Goal

Bump the deployed OpenClaw container image pin from `ghcr.io/openclaw/openclaw:v2026.3.11`
to `ghcr.io/openclaw/openclaw:v2026.5.18`, across all three providers
(local, remote, AWS), and verify that nothing the existing fleet depends on
regresses.

The bump is **scope-locked** to the pin change. No defaults reconciliation,
no schema migrations, no dependent feature work — those are separate specs
that may follow once this bump has soaked.

## Motivation

The current pin (`v2026.3.11`) is a held-back pin, not a deliberate
long-term choice. It was set to dodge openclaw/openclaw#45311 — a Slack
socket-mode regression introduced in `v2026.3.12` that caused agents to
connect but receive zero inbound events.

**Status as of 2026-05-21**:
- Issue #45311 is **CLOSED** (2026-04-25).
- Fix shipped in **`v2026.3.22`** via PR #45953 ("harden @slack/bolt import
  interop across current bundled runtime shapes"), with regression coverage
  in `extensions/slack/src/monitor/provider.interop.test.ts`.
- Reporter validated the fix on `v2026.3.22`; maintainer closed the issue
  citing release notes + on-main test coverage.
- Latest upstream stable is **`v2026.5.20`** (released today). The previous
  stable **`v2026.5.18`** (3 days old) is our target — chosen for soak time
  vs. recency, and was already named as the desirable target in the deferred
  Phase 1 of `specs/2026-05-19_feature_local-model-routing/`.

Holding the pin any longer means building new features on top of an image
that is two months and ~14 minor releases behind upstream, where the
runtime/config schema may have shifted in ways we'd rather not back-port
into.

## In Scope

1. Updating the **default image tag** wherever it is hard-coded in this
   repo (Terraform variable defaults, Go provider setup defaults, JSON
   schema example).
2. Updating the **deployed image tag** in each existing environment's
   runtime configuration (SSM on AWS, `~/.conga/local-config.json` for
   local, `~/.conga/remote-config.json` for remote — to the extent the
   operator running the bump can reach those environments).
3. Cycling containers on each verified environment so the new image is
   actually running.
4. Updating documentation: `CLAUDE.md` OpenClaw pin note, and the
   `project-openclaw-pin-revisit` auto-memory.

## Out of Scope

- Reconciling `agents/_defaults/<runtime>/<type>/*` drift against the new
  image. Known tech debt (`product-knowledge/PROJECT_STATUS.md` "Known
  Issues") — separate spec.
- Adopting new OpenClaw config fields exposed by versions between
  `2026.3.11` and `2026.5.18`. If something in the rendered `openclaw.json`
  needs to change to even *boot* on the new image, that's a blocker we
  surface and stop; otherwise we ship the bump and treat new-field adoption
  as its own follow-up.
- Bumping to `v2026.5.20`. Could be done as a fast-follow once `v2026.5.18`
  has soaked; not in this spec.
- Hermes runtime image (different runtime, different image, not pinned for
  the same reason).
- OpenClaw plugin/skill enablement decisions (separate open question in
  `PROJECT_STATUS.md`).

## Success Criteria

**Functional** — verified post-deploy on at least one environment:
1. **Slack inbound works.** A user agent receives a Slack DM and replies
   end-to-end. A team agent receives a channel mention and replies
   end-to-end. (The specific regression the pin was guarding against.)
2. **Gateway-only mode works.** An agent provisioned without Slack
   configuration serves the web UI via `conga connect` — HTTP 200 from the
   gateway, no "origin not allowed" errors, `/model` and chat both
   functional.
3. **Per-agent model overlay still works.** An agent with a non-empty
   `agents/<name>/agent.yaml` (per the 2026-05-19 Local Model Routing
   feature) boots on the new image, the configured non-default model is
   selectable, and `/model` switching to/from the runtime default still
   works.
4. **Egress proxy still works.** Envoy enforcement still drops disallowed
   domains; allowed domains succeed. No iptables / DOCKER-USER chain
   regressions.

**Operational**:
5. **All three providers smoked.** Local, remote, AWS each go through:
   `admin setup` → `add-user` (or `add-team`) → container healthy → `conga
   connect` succeeds. Interface parity is a Must standard
   (`product-knowledge/standards/`).
6. **CLAUDE.md and memory updated.** The OpenClaw pin paragraph in
   `CLAUDE.md` reflects the new tag and the resolution of #45311. The
   `project-openclaw-pin-revisit` memory is either updated (still a watched
   pin, just at a new version) or retired (no longer a held-back pin).

## Constraints (carried in from memory / standards)

- **Bisectable**: the pin bump lands as a single commit, separate from any
  feature that depends on the new version. (`project-openclaw-pin-revisit`
  memory.)
- **Interface parity**: all three providers (local, remote, AWS) must end
  up on the same image tag and the same code path. No provider-specific
  forks. (`product-knowledge/standards/` Must.)
- **Rollback shape**: revert is a `git revert` of the pin commit plus a
  per-environment `conga admin setup` (or equivalent SSM/JSON config edit)
  to re-pin the old tag and cycle. No schema migration in either direction.
- **Pin everywhere**: no place that today says `2026.3.11` may end up
  saying `:latest` or be left stale at `2026.3.11`. The two existing
  `:latest` fallbacks (`pkg/runtime/openclaw/container.go`,
  `pkg/provider/remoteprovider/provider.go`) are pre-existing and out of
  scope for this spec — they are reachable only when SSM/config lookups
  fail, which the bump does not change.

## Open Questions for Spec Phase

- Does anything in the rendered `openclaw.json` schema need to change to
  boot on `v2026.5.18`? Local Model Routing live-tested on AWS with the
  current renderer (sibling spec, 2026-05-19) but with a `2026.3.11`
  container — has not been tested with renderer-output-as-is on the new
  image. The pin-bump spec must include a smoke step where an existing
  config is fed to the new image *before* shipping the bump.
- Are there any `v2026.3.12` → `v2026.5.18` changelog entries calling out
  required-action operator migrations (auth, on-disk format, env vars)?
  Spec phase reads the upstream changelog and surfaces any.
- AWS bootstrap reads `$CONGA_IMAGE` from SSM. Confirm the operator
  workflow for updating the SSM parameter — is it `conga admin setup`,
  or a direct SSM write, or both?
