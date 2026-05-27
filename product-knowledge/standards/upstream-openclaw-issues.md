<!--
GLaDOS-MANAGED DOCUMENT
Last Updated: 2026-05-27
To modify: Edit directly. Add new entries to "Active workarounds" as
upstream bugs surface. Move entries to "Resolved upstream" when the fix
ships in a version we've pinned to.
-->

# Upstream OpenClaw Issues — Active Workarounds

> Conga Line pins to a specific OpenClaw image tag for bisectability
> (see `CLAUDE.md`, image pin section). When an upstream bug bites us
> before it's fixed in a pinned version, we add a workaround in our own
> code or config and track it here. This document is the operator's
> answer to "why does Conga emit *this* unusual config?"

## Active workarounds

### #25592 — Team agents leak preamble text to Slack channels

**Upstream:** [openclaw/openclaw#25592](https://github.com/openclaw/openclaw/issues/25592) — open since 2026-02; still open at v2026.5.26.

**Symptom.** Bare `text` content blocks emitted by Claude *before* a tool call — preamble narration, "let me think about this" prose, decision-not-to-reply commentary, inter-tool acknowledgements — are delivered to the channel as visible Slack messages. Real example from `nvidia-team` on 2026-05-27 (on v2026.5.18, before our fix):

> *"Nathan is posting status updates — Linear tickets filed and Phase 1 MR up. Not directed at me, no question to answer. Just a team update. Let me capture the ticket references to memory and stay quiet."*

The leaked content is **not** an Anthropic `thinking` block. Those are tagged `isReasoning: true` and are suppressed by [#84319](https://github.com/openclaw/openclaw/issues/84319) (closed in v2026.5.20). What leaks here is plain assistant text that the model uses as a "scratchpad" before its tool calls. It bypasses every reasoning-suppression code path because it's not flagged as reasoning.

**Why it bites team agents specifically.** Team agents post in shared Slack channels with multiple humans watching. Any inter-message narration is professional embarrassment. User agents in DMs are 1:1 and a touch of preamble is acceptable.

**Conga workaround.** Two coordinated changes, both team-agents only:

1. **Config-side** — `applyTeamChannelDiscipline()` in `pkg/runtime/openclaw/config.go`:
    - `messages.groupChat.visibleReplies: "message_tool"` — gates delivery on an explicit `message()` tool call.
    - `tools.alsoAllow: ["message"]` — restores the `message` tool that `tools.profile: "coding"` (set in `openclaw-defaults.json`) strips out. Without this the agent would have no tool with which to deliver replies and every turn would silently drop.

2. **Prompt-side** — "Channel Discipline" section in every team-agent `AGENTS.md`:
    - `agents/_defaults/openclaw/team/AGENTS.md` (generic team default)
    - `agents/_defaults/openclaw/role-code-dev/AGENTS.md`
    - `agents/_defaults/openclaw/role-writing/AGENTS.md`
    - Per-agent `agents/<name>/AGENTS.md` for any team agent that has its own (e.g. `agents/nvidia-team/AGENTS.md`).

    Tells the model: *only `message(...)` posts; bare text is internal; if you finish a turn without calling `message()` when you meant to reply, that reply is lost*.

**Why both.** Config alone causes silent drops when the model forgets to call the tool (see [#85384](https://github.com/openclaw/openclaw/issues/85384) and closed-not-planned [#77320](https://github.com/openclaw/openclaw/issues/77320)). Prompt alone doesn't suppress preamble because the model isn't perfectly disciplined. Together: config enforces, prompt teaches.

**Scope discipline.** The branch fires only when `params.Agent.Type == provider.AgentTypeTeam` in `GenerateConfig`. User agents stay on the looser defaults — silent-drop risk is higher in a 1:1 DM (a missed reply is noticed immediately) and the preamble cost is lower.

**Validation.** After refreshing a team agent, inside the container:
- `cat /home/node/.openclaw/openclaw.json | jq '.messages.groupChat.visibleReplies'` → `"message_tool"`
- `cat /home/node/.openclaw/openclaw.json | jq '.tools'` → has `profile: "coding"` AND `alsoAllow: ["message"]`
- `grep -c "Channel Discipline" /home/node/.openclaw/data/workspace/AGENTS.md` → `1`
- Logs should NOT contain `[agents/tool-policy] tool policy removed ... message` (that line indicates `coding` profile stripped the tool — meaning `alsoAllow` didn't land).

**Escape conditions — remove the workaround when:**
1. Upstream #25592 is fixed in a version we pin to. The fix could be (a) a config knob like `suppressInterToolText: true`, (b) an OpenClaw behavior change that treats bare-text-before-tool-call as internal, or (c) something else entirely — the issue lists three possibilities and the maintainers haven't picked one.
2. We move team agents off the `coding` profile. If we adopt `messaging` profile (which preserves `message` natively per [delegation-routing spec § upstream-capability](../../specs/2026-05-22_feature_delegation-routing/upstream-capability.md)), the `tools.alsoAllow` half becomes redundant — but `visibleReplies` still matters.

**Related open issues to watch:**
- [#85384](https://github.com/openclaw/openclaw/issues/85384) — *"message_tool_only group chats can go silent when final reply is emitted instead of message tool"*. The silent-drop side of our workaround.
- [#80458](https://github.com/openclaw/openclaw/issues/80458) — *"buildEmbeddedRunPayloads leaks 'commentary' phase text to channel delivery"*. Adjacent leak path, codex/phase-tagged providers only — doesn't currently bite us with Claude.

---

### #73182 — Claude thinking default silently flipped to `medium` (cost regression)

**Upstream:** [openclaw/openclaw#73182](https://github.com/openclaw/openclaw/issues/73182) — open since 2026-04-28.

**Symptom.** OpenClaw v2026.4.22 raised the implicit default thinking level for reasoning-capable models (Claude Opus, Sonnet) from `off` to `medium`. Every turn now requests extended thinking from the Anthropic API even when the operator never asked for it. Anthropic spend doubles overnight. The boot banner shows it: `[gateway] agent model: anthropic/claude-opus-4-7 (thinking=medium, fast=off)`.

**Status.** We have NOT applied a config workaround for this yet. The leak symptom from #25592 was the user-visible issue; the cost symptom from #73182 is silent (you only notice it on the bill). The upstream fix discussion is ongoing and may add an `agents.defaults.reasoningDefault` knob (PR by deepujain landed partially — commit `0c9f84451a9f`).

**Mitigation options if cost becomes a blocker:**
- Set `agents.defaults.reasoningDefault: "off"` in `pkg/runtime/openclaw/openclaw-defaults.json` once the schema is finalized upstream.
- Or per-agent in `agents/<name>/agent.yaml` once that overlay supports the field.

**Watch:** Anthropic monthly spend on team-agent accounts. If the marginal cost is acceptable (thinking does measurably improve Opus output), do nothing; if not, apply the workaround above.

---

## Resolved upstream

History of upstream bugs that bit us, with the fix commit/release. These
no longer need workarounds at the current pin but inform future operator
expectations.

| Issue | Symptom | Fixed in | Conga action taken |
|---|---|---|---|
| [#45311](https://github.com/openclaw/openclaw/issues/45311) | Slack socket-mode regression | v2026.3.22 (PR [#45953](https://github.com/openclaw/openclaw/pull/45953), Slack Bolt import-interop hardening) | Held image pin at v2026.3.11 until fix, then bumped. |
| [#84319](https://github.com/openclaw/openclaw/issues/84319) | Claude `thinking` blocks leaked to Slack via non-streaming delivery paths | v2026.5.20 (PR [#84322](https://github.com/openclaw/openclaw/pull/84322), `b05c6158`) | Bumped pin from v2026.5.18 (which had the leak) to v2026.5.26 on 2026-05-27. |

## Adding a new entry

When a new upstream OpenClaw bug bites us:

1. **File or find the upstream issue.** If filing, link the Conga-side reproduction. If finding, add a comment with our reproduction context.
2. **Add an "Active workaround" entry** here with: upstream link + state, symptom (concrete example), Conga workaround (code/config/prompt locations), validation steps, and escape conditions (what would let us remove the workaround).
3. **Update CLAUDE.md** with a one-paragraph entry in *OpenClaw Behavioral Issues* that points at this document for full context.
4. **Match scope to the bug.** If the bug affects only team agents, gate the workaround on `provider.AgentTypeTeam`. Don't apply a costly workaround fleetwide for a narrow upstream bug.
5. **When the bug is fixed upstream and we've pinned to the fix**, move the entry to *Resolved upstream*, remove the workaround code/config (with a separate PR — don't bundle removal with pin bumps), and update validation/tests.
