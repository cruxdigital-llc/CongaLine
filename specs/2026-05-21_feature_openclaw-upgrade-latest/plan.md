# Plan: OpenClaw Upgrade Latest

**Target tag**: `ghcr.io/openclaw/openclaw:v2026.5.18`
**Current pin**: `ghcr.io/openclaw/openclaw:v2026.3.11`

## Approach

A pin bump is structurally trivial — find the version string, change it,
redeploy. The risk is not in the find-and-replace; it's in the **two
months and ~14 minor releases** of upstream change between the two tags.
The plan therefore frames this as a small, reversible commit gated by a
disciplined verification pass on each provider, not as a routine
mechanical edit.

Phases run sequentially because each gates the next: changelog review
either kills the bump or shapes verification; the code change can't be
written safely without that review; verification can't run until the
code change exists; the rollout can't happen until verification passes.

## Phase 0 — Pre-flight: upstream changelog review

Owner: Architect.

Read the OpenClaw `CHANGELOG.md` window `v2026.3.12` … `v2026.5.18` and
classify every entry as one of:

- **Blocking** — requires an `openclaw.json` schema change, an env var
  change, or a host-side migration before the container will boot or
  function correctly with our current rendered config.
- **Adjacent** — touches Slack, gateway, model overlay, or egress code
  paths we depend on. Flag as a focus area for Phase 3 verification.
- **Irrelevant** — features/fixes we don't exercise.

**Stop condition**: if any entry is classified Blocking, this spec stops
and a separate spec is opened to address the migration first. The pin
bump itself does not absorb schema migrations (see "Out of Scope" in
requirements).

Deliverable: a `changelog-review.md` artifact alongside `plan.md`
listing every Adjacent entry and the verification step it maps to.

## Phase 1 — Code change

Owner: Architect.

Single commit. Updates the **default** tag in every place this repo
hard-codes a version string, but does not touch the two pre-existing
`:latest` fallbacks (those activate only when SSM/config lookup fails
and are out of scope per requirements). Specifically:

1. `terraform/environments/production/variables.tf` — production env default.
2. `terraform/modules/congaline/variables.tf` — module default.
3. `terraform/modules/infrastructure/variables.tf` — module default
   inside the agents object/map.
4. `pkg/provider/remoteprovider/setup.go` — both occurrences (interactive
   prompt default + non-interactive default).
5. `internal/cmd/json_schema.go` — JSON schema example.
6. `CLAUDE.md` — OpenClaw pin paragraph: new tag, new "why" (was Slack
   regression, now pinned for stability; #45311 RESOLVED in v2026.3.22).

Out of scope for this commit:
- `pkg/runtime/openclaw/container.go:23` (`:latest` fallback — unchanged).
- `pkg/provider/remoteprovider/provider.go:255, 651` (`:latest` fallbacks
  — unchanged).
- `terraform/environments/production/.terraform/...` (Terraform-managed
  cache, regenerated on `terraform init`).

The commit message will explicitly note: pin bump only, no schema or
behavior change in this repo, bisectable revert is `git revert <sha>` +
per-env `conga admin setup` re-pin.

## Phase 2 — Provider parity audit

Owner: Architect.

Verify the same code path runs on all three providers post-bump:
- **AWS**: image tag comes from SSM (`/conga/admin/image`), written at
  `conga admin setup` time. After Phase 1, fresh installs default-write
  the new tag; existing fleets need an explicit SSM update + container
  cycle.
- **Local**: image tag in `~/.conga/local-config.json`. Same shape.
- **Remote**: image tag in `~/.conga/remote-config.json`. Same shape.

Deliverable: a short matrix in the spec showing, per provider:
- where the tag is read from at container-start time,
- the operator command to update it for an existing environment,
- the operator command to cycle the running container.

If any provider's update path is more than one operator step, that gets
flagged in the spec for a usability follow-up — but does not block this
spec.

## Phase 3 — Verification

Owner: QA. Performed on a non-production environment first (local, then
remote), then on production (AWS) only after both clear.

Five verification scenarios, each maps to a success criterion from
requirements:

1. **Slack inbound — user agent.** Provision a user agent, DM it, confirm
   reply. (Maps to: regression we're clearing.)
2. **Slack inbound — team agent.** Provision a team agent, mention it
   in a channel, confirm reply.
3. **Gateway-only.** Provision an agent with no Slack config. `conga
   connect`. Confirm HTTP 200, working chat, `/model` works.
4. **Model overlay.** Pick an agent with a non-empty
   `agents/<name>/agent.yaml` (per Local Model Routing spec). Confirm:
   boot succeeds, non-default model is reachable, `/model default` flips
   back to the runtime default.
5. **Egress proxy.** Confirm Envoy is up, an allowed domain reaches its
   destination, a disallowed domain is dropped at the proxy AND at
   iptables (AWS only, where the dual-layer is configured).

Each provider runs scenarios 1–4. AWS additionally runs scenario 5
(remote runs Envoy too but iptables enforcement is fully validated only
on AWS per the egress feature spec).

**Gate**: any scenario failure stops the rollout and opens a bug spec.
The Phase 1 commit may stay on `main` (it's a default-only change for
fresh installs and is safe) but existing-environment updates pause.

## Phase 4 — Rollout

Owner: Operator.

Three independent, opt-in updates, one per existing environment:
1. Local: re-run `conga admin setup` selecting the new tag (or accept
   the default), then `conga admin refresh-all` to cycle containers.
2. Remote: same flow, on the remote-configured machine.
3. AWS: same flow against the SSM-backed config, then either `conga
   admin refresh-all` (which now restarts containers with the new image
   per `RefreshAgent` semantics in CLAUDE.md) or a `conga admin
   cycle-host` for a clean reboot.

No coordinated cutover. Each environment is independent and reversible.

## Phase 5 — Memory + docs hygiene

Owner: Architect.

- Update `CLAUDE.md` (done in Phase 1).
- Update auto-memory `project-openclaw-pin-revisit`:
  - If we expect to keep tracking upstream stability, update the body to
    reflect the new tag and a fresh "re-check periodically" cadence.
  - If the pin is no longer a held-back pin (just normal version pinning),
    retire the memory.
- Add a line to `PROJECT_STATUS.md` "Recent Changes" with the bump date,
  the from/to tags, and the verification summary.

## Risks & Mitigations

| Risk | Likelihood | Mitigation |
|---|---|---|
| Rendered `openclaw.json` rejected by the new image | Low | Phase 0 changelog review surfaces schema changes; Phase 3 scenario 3 + 4 catches runtime rejection before rollout. |
| Slack regression not actually fixed in our deployment shape | Low | Phase 3 scenarios 1+2 directly exercise the failure mode; rollout is paused on any failure. |
| Model overlay (`agent.yaml`) behavior differs on new image | Low | Phase 3 scenario 4 covers this; the overlay is renderer-side, image-agnostic by design. |
| Operator runs partial rollout (some envs new, some old) and forgets | Medium | Acceptable for this fleet — environments are operator-managed and independent. CLAUDE.md note + memory update give future-us a paper trail. |
| Upstream silently bumps required env vars between 3.11 and 5.18 | Low | Phase 0 changelog review explicitly checks for env/config-format changes. |

## Rollback

Single-step revert per environment:
1. `git revert <pin-commit>` and merge (restores in-repo defaults to
   `v2026.3.11`).
2. Per environment: `conga admin setup` accepting the now-restored
   `v2026.3.11` default, then refresh / cycle to redeploy the old image.

No schema or data-format migration in either direction — the rendered
config and on-disk state are unchanged by the bump itself.

## Open Questions Carried Into Spec Phase

(See requirements §"Open Questions" — re-listed here so spec phase has
them in one place.)

1. Any blocking changelog entries `v2026.3.12` … `v2026.5.18`?
2. Operator command for updating the AWS SSM image parameter — is it
   `conga admin setup`, direct SSM write, or both? Resolved by Phase 2
   parity audit but worth confirming with a manual test.
3. Do we want to fast-follow to `v2026.5.20` after a soak period, or
   treat `v2026.5.18` as the new stable resting place?
