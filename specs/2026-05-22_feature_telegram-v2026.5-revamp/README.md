# Feature Trace: Telegram v2026.5 Revamp

**Feature**: `telegram-v2026.5-revamp`
**Started**: 2026-05-22
**Status**: **Planning only — no implementation in this trace**
**Lead**: TBD (pending operator pickup)

## Purpose

Make Telegram a fully-supported messaging channel for **OpenClaw** agents on
v2026.5.18+. Today the Telegram channel implementation in this codebase is
in a half-finished state:

- `pkg/channels/telegram/telegram.go` emits an OpenClaw channel config that is
  pre-v2026.5.18 and would be rejected at gateway startup by the new strict-
  additional-properties schema if anyone tried to use it.
- `router/telegram/src/index.js` was built for **Hermes Agent**, not
  OpenClaw — it forwards Telegram events to `/v1/chat/completions` (port
  8642, Hermes' OpenAI-compatible endpoint) rather than to an OpenClaw
  webhook receiver. So even if the channel config were correct, inbound
  Telegram events wouldn't actually reach an OpenClaw agent.
- The OpenClaw Telegram plugin ships **bundled-but-disabled** in v2026.5.18
  (`openclaw plugins list` shows it as `disabled` by default), unlike the
  Slack plugin which was externalized into `@openclaw/slack`. Our generator
  emits `plugins.entries.telegram.enabled: true`, but that's untested
  against the new image.

The production fleet does not currently use Telegram. This is dormant code.
This spec scopes what would need to change to make it functional under
OpenClaw v2026.5.18+, so the next operator who wants Telegram support has a
runnable starting point.

## Discovery context

Surfaced during the `chore/upgrade-openclaw` PR review (2026-05-21/22). The
[pr-review-toolkit:code-reviewer](https://github.com/cruxdigital-llc/CongaLine/pull/51#issuecomment-4516560155)
agent flagged `pkg/channels/telegram/telegram.go:73` as the same `allow: true`
→ `enabled: true` migration we did for Slack in round 6 of that PR. Deeper
investigation revealed the broader architectural mismatch documented here.

## Active Personas

- **Architect** — topology choice (router vs per-agent bot), v2026.5.18
  schema compliance, OpenClaw plugin enablement path
- **QA** — live Telegram bot smoke test plan, multi-account scenarios,
  long-polling vs webhook trade-offs

## Session Log

- **2026-05-22**: Spec dir scaffolded. Requirements + plan drafted from the
  PR #51 investigation. No code changes — implementation deferred to a
  future spec session.
