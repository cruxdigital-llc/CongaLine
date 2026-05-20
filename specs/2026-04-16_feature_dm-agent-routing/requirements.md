# Requirements: DM Agent Routing

## Problem Statement

When a user DMs the Slack bot, the message currently routes to their personal agent (via the 1:1 `members` map in `routing.json`) or is dropped if they don't have one. Users who belong to client teams (e.g., project1-internal, project2-internal) may need to query team agents via DM, and not every team member has a personal agent.

There is no mechanism today for:
1. A user with a personal agent + team agent access to DM the team agent
2. A team-only user (no personal agent) to DM any agent at all

## Goal

Enable transparent, intelligent DM routing so that when any enrolled user messages the Slack bot, the right agent responds directly — without prefixes, menus, or any special syntax.

## User Scenarios

| Scenario | User has | Expected behavior |
|----------|----------|-------------------|
| A | Personal agent only | DM routes to personal agent (unchanged) |
| B | Personal agent + 1 team agent | Classifier determines which agent handles the DM |
| C | Personal agent + N team agents | Classifier determines which agent handles the DM |
| D | 1 team agent only (no personal) | DM routes directly to team agent (no classification) |
| E | N team agents only (no personal) | Classifier determines which agent handles the DM |

## Success Criteria

1. **Transparent routing**: Multi-agent user DMs the bot and the correct agent responds without any special syntax or user action.
2. **No regressions**: Single-agent users (personal-only, Scenario A) see zero behavioral change.
3. **Team-only access**: Users who are members of a channel bound to a team agent can DM the bot and reach that team agent (Scenarios D, E).
4. **Thread continuity**: Once a DM thread is routed to an agent, all replies in that thread stay with the same agent.
5. **Graceful uncertainty**: When the classifier cannot confidently determine which agent should handle a message, the system asks the user for clarification and pins the session to the chosen agent.
6. **Fallback resilience**: If the classifier is unavailable (endpoint down, no key configured), messages still route to a default agent — never dropped.
7. **Automatic access from channel membership**: DM access is derived from Slack channel membership — no manual enrollment. When a user joins/leaves a channel bound to a team agent, their DM routing updates automatically.
8. **Backward compatible**: Deployments without a classifier configured behave identically to today.

## Non-Goals (v1)

- Manual enrollment CLI (channel membership is the source of truth)
- LLM routing for channel messages (channels already map 1:1 to team agents)
- Personal agent as orchestrator/mediator pattern (may come in v2)
- Cross-platform support beyond Slack (Telegram DM routing is a future extension)

## Constraints

- Router must remain lightweight — the classifier is a single API call, not a new service
- No new npm dependencies in the router (use native `fetch` for both Anthropic and OpenAI-compatible endpoints; use the built-in `node:test` runner for router tests)
- Team agents currently have `dmPolicy: "disabled"` (`pkg/channels/slack/slack.go`) — must be conditionally enabled for router-forwarded DMs
- Changes to `pkg/` require a Terraform provider release
- `routing.json` schema is restructured (single `agents` table, name-indexed `channels`/`members`). Writer (Go) and reader (router) ship from this repo and deploy together.
- Slack app scopes already in `SetupGuide()`: `chat:write`, `channels:read`, `groups:read` — no scope additions required
- Slack app **manifest** additions are required and are NOT documented today:
  - Event Subscriptions: `member_joined_channel`, `member_left_channel`
  - Interactivity & Shortcuts: enabled (Socket Mode delivery — no Request URL)
- Pinned OpenClaw image is `ghcr.io/openclaw/openclaw:2026.3.11`. The empty-`allowFrom` DM acceptance form must be validated against this image (Phase 0).

## Operational Limitations (v1)

- **Per-process router state.** The thread-routing cache and pending-clarification map live in router memory. A router restart (config reload, container restart, host cycle) wipes them. Recovery is graceful: new DMs in an existing thread re-classify on the next message; pending picks past TTL fall back to the default agent.
- **Membership rebuilds on startup.** Channel membership is reconstructed via `conversations.members` when the router starts. If Slack's API is unavailable for a given channel at startup, that channel's users temporarily fall back to personal-only DM routing. Full state recovers on the next 30-minute re-poll. The router does not refuse to start when Slack is partially unreachable.

## DM Access Model

DM access is derived automatically from Slack channel membership:
- If a user is a member of a channel bound to a team agent, they can DM that agent
- The router resolves membership at startup via `conversations.members` API and maintains it via `member_joined_channel` / `member_left_channel` events
- No admin enrollment commands needed — channel membership is the source of truth
- Bot must be a member of each bound channel to query its members

## Classifier Model

The classifier determines which agent should handle a DM when a user has access to multiple agents.

- **Default**: Anthropic Haiku via the Anthropic Messages API (requires `ANTHROPIC_API_KEY` in router env)
- **Self-hosted option**: Set `CLASSIFIER_URL` to any OpenAI-compatible endpoint (e.g. Ollama on a local GPU server). When set, the router uses this endpoint instead of Anthropic — no API key needed.
- **Neither configured**: Multi-agent DMs fall back to the default agent. Single-agent DMs still route directly.

## Personas

- **Architect**: Data model changes, routing config schema, Channel interface impact, provider parity
- **QA**: Classifier failure modes, thread cache edge cases, enrollment validation, test coverage
- **PM**: Enrollment UX, clarification flow UX, scope boundaries
