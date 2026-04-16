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
3. **Team-only access**: Users without a personal agent but enrolled in team agent DM access can DM the bot and reach their team agent (Scenarios D, E).
4. **Thread continuity**: Once a DM thread is routed to an agent, all replies in that thread stay with the same agent.
5. **Graceful uncertainty**: When the classifier cannot confidently determine which agent should handle a message, the system asks the user for clarification and pins the session to the chosen agent.
6. **Fallback resilience**: If the classifier is unavailable (API down, no key configured), messages still route to a default agent — never dropped.
7. **Explicit enrollment**: Admin explicitly grants DM access per-user per-team-agent via CLI. No implicit access from channel membership.
8. **Backward compatible**: Deployments without an Anthropic API key behave identically to today. The `dmRouting` config section is additive and ignored by older routers.

## Non-Goals (v1)

- Automatic enrollment from Slack channel membership (requires Slack API calls)
- Batch enrollment CLI (per-user is sufficient)
- LLM routing for channel messages (channels already map 1:1 to team agents)
- Personal agent as orchestrator/mediator pattern (may come in v2)
- Cross-platform support beyond Slack (Telegram DM routing is a future extension)

## Constraints

- Router must remain lightweight — the classifier is a single API call, not a new service
- No new npm dependencies in the router (use native `fetch` for Anthropic API)
- Team agents currently have `dmPolicy: "disabled"` — must be conditionally enabled for enrolled users
- Changes to `pkg/` require a Terraform provider release
- The Slack app needs `chat:write` scope for ephemeral clarification messages (already in the recommended scopes)

## Enrollment Model

- Admin runs `conga channels enroll <team-agent> <user-id>` to grant DM access
- Admin runs `conga channels unenroll <team-agent> <user-id>` to revoke
- Stored as `DMAccess []string` on the team agent's `AgentConfig`
- Enrollment triggers: regenerate team agent's `openclaw.json` (enable `dmPolicy: "allowlist"`), regenerate `routing.json` (populate `dmRouting` section), refresh containers

## Personas

- **Architect**: Data model changes, routing config schema, Channel interface impact, provider parity
- **QA**: Classifier failure modes, thread cache edge cases, enrollment validation, test coverage
- **PM**: Enrollment UX, clarification flow UX, scope boundaries
