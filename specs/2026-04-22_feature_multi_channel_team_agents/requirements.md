# Requirements: Multi-Channel Team Agents

## Problem Statement

A team agent today can be bound to at most one channel per platform. The bind-time guard at `pkg/provider/localprovider/channels.go:187-191` (and the identical guards in `remoteprovider/channels.go:164-168` and `awsprovider/channels.go:186-190`) rejects a second Slack binding with `ErrBindingExists`. Downstream code assumes the singular: `AgentConfig.ChannelBinding(platform)` returns only the first match, `pkg/common/routing.go:62-71` emits one channel-ID entry for team agents, and `pkg/channels/slack/slack.go:73-80` builds a single-entry allowlist.

Operators who want one team agent to serve multiple related channels (e.g. one agent serving three Slack channels whose members coordinate on a shared workstream) must provision N separate agents instead. N agents means N containers, N× memory, N separate memory stores — fragmenting what is logically one team's context.

## Goal

Allow a single team agent to be bound to multiple channels **on the same platform**, with the router forwarding events from any bound channel to the same container. Binding is additive and explicitly admin-gated via `conga channels bind`; merely inviting the bot to a channel in Slack does not grant routing.

## User Scenarios

| Scenario | Setup | Expected behavior |
|----------|-------|-------------------|
| A | Admin binds one team agent to channels `C1`, `C2`, `C3` via three `conga channels bind` calls | All three bindings persist. `routing.json` maps each of `C1/C2/C3` to the same agent URL. The agent's `openclaw.json` allowlist contains all three IDs. |
| B | Admin runs `conga channels list <agent>` after multiple binds | All bindings displayed, grouped by platform |
| C | Admin runs `conga channels unbind <agent> slack:C2` | Only `C2` removed; `C1` and `C3` remain bound. Routing and allowlist regenerated accordingly. |
| D | User posts a message in `C1` | Event forwarded to the agent container. Agent responds in-channel. |
| E | Someone invites the bot to an unbound channel `C4` | Router detects the unknown channel ID on the next message event. An ephemeral message is posted to the sender: *"I'm not configured for this channel. Ask an admin to run `conga channels bind <agent> slack:C4`."* Message is rate-limited to at most once per (channel, user) per 24 hours. |
| F | Admin attempts `conga channels bind <agent> slack:C1` when `C1` is already bound to this agent with the same label | Idempotent: no error, no duplicate. Exit 0 with a "binding already exists" notice. |
| F' | Admin attempts to bind `slack:C1` to this agent with a new label when it's already bound with a different label | Error: "binding exists with different label — unbind first". Exit non-zero. |
| F'' | Admin attempts to bind `slack:C1` to agent `acme` when it's already bound to agent `payroll` | Error: "channel slack:C1 is already bound to agent \"payroll\"; unbind it there first". Exit non-zero. |
| G | Admin provisions a fresh team agent and binds zero channels | Current gateway-only behavior preserved. `routing.json` has no entries for this agent's Slack platform. |
| H | A team agent that was previously bound to one channel is upgraded to multi-channel | Existing single binding continues to work unchanged after the upgrade (backward compatibility for pre-upgrade `AgentConfig.Channels` shape — no migration needed since the field is already a slice). |

## Success Criteria

1. **N bindings per platform allowed**: `conga channels bind` succeeds for a second, third, Nth Slack binding on the same agent. The bind guard's duplicate-platform rejection is removed. New guards enforce: (a) idempotent success on exact `(platform, id)` match with same label; (b) explicit error on exact `(platform, id)` match with a different non-empty label; (c) explicit error when the same `(platform, id)` is already bound to a different agent (cross-agent uniqueness).
2. **Fan-out routing**: `routing.json` contains one `channels[<id>]` entry per bound channel, all pointing at the same agent webhook URL.
3. **Policy coverage**: `openclaw.json` `channels.slack.groupPolicy` is `allowlist` and `channels.slack.channels` includes all bound channel IDs. Messages from bound channels pass the allowlist; messages from unbound channels are rejected.
4. **Unbound-channel UX**: When the bot receives an event from a Slack channel not present in `routing.json`, the router posts exactly one ephemeral message per (channel, user) per 24 hours instructing the sender to ask an admin to bind the channel. The message names the exact CLI command.
5. **CLI parity**: `channels bind`, `channels unbind`, and `channels list` all handle N bindings correctly. `unbind` addresses a specific `(platform, id)`; omitting the ID is an error when multiple bindings exist.
6. **Provider parity**: Behavior is identical across `localprovider`, `remoteprovider`, and `awsprovider`. All three bind guards are updated in the same change.
7. **Idempotency**: Binding the same `(platform, id)` twice is a no-op, not an error.
8. **No regression for single-binding agents**: Agents with exactly one binding see byte-identical `routing.json` and `openclaw.json` output before and after the feature lands (allowlist with one entry, one `channels[<id>]` routing entry).
9. **Production-grade release coordination**: The change is shipped across two repos (`congaline` and `terraform-provider-conga`) in a sequenced release. `AgentConfig.Channels` wire format is unchanged, but the `Provider.UnbindChannel` Go interface signature is a breaking change (adds `id string`). The Terraform resource schema bumps from v0 to v1; existing `.tfstate` files upgrade automatically via a state upgrader. Existing single-binding operators see no observable change after upgrade; multi-binding is opt-in. Breaking changes are enumerated in the release notes.

## Non-Goals (v1)

- Cross-platform multi-binding (e.g. Slack + Telegram on the same agent) — out of scope, orthogonal feature. `ChannelBinding` already allows different platforms; this spec only removes the *same-platform* duplicate constraint.
- Enforced memory isolation between channels. Memory segmentation inside the container is an OpenClaw/behavior concern, addressed by the per-agent SOUL.md where relevant. The framework does not police cross-channel recall.
- DM routing for multi-channel team agents. That is the concern of `specs/2026-04-16_feature_dm-agent-routing/`. This spec does not change DM handling.
- Automatic bind when the bot is invited to a channel. Bindings remain explicitly admin-gated for access curation.
- Channel-scoped memory persistence APIs. Advisory-only via SOUL.md guidance.
- Per-channel policy overrides (different behavior per bound channel). Single policy applies to all bindings.

## Constraints

- `AgentConfig.Channels` is already `[]channels.ChannelBinding`; no wire-format or storage format change is needed. The constraint was purely enforced at bind-time.
- Changes to `pkg/` require a Terraform provider release per the repo's release flow (see CLAUDE.md § Terraform Provider). All changes here are additive optional behavior — backward compatible.
- Router changes must not require a new npm dependency. The unbound-channel ephemeral message uses the existing bot token and the Slack Web API `chat.postEphemeral` endpoint via native `fetch`.
- The Slack app must have `chat:write` scope for the ephemeral message (already in the recommended scope list).
- The router already reloads `routing.json` via `fs.watch`; the additive entries produced by this feature require no change to the reload mechanism.
- Rate-limit state for the unbound-channel ephemeral is in-memory only (router restart clears it). Acceptable — at worst, a restart triggers one duplicate notice per user in newly invited channels.
- Per-agent SOUL.md guidance for channel-aware memory is documented in a standards note but not required for the feature to ship.

## Personas

- **Architect**: Schema compatibility; routing generation correctness; provider parity; interaction with the (pending) dm-agent-routing changes to the same files.
- **QA**: Idempotency under repeat binds; unbind semantics with N bindings; ephemeral rate-limit correctness; regression test that single-binding agents produce byte-identical config.
- **PM**: Admin mental model for curated access; clarity of the unbound-channel ephemeral message; whether the exact CLI command should be surfaced (potential info leak about agent names) or kept generic.
