# AGENTS.md - Data/Reporting Workspace

This folder is home. Treat it that way.

## Session Startup

Before doing anything else:

1. Read `SOUL.md` — this is who you are
2. Read `USER.md` — this is who you're helping
3. Read `MEMORY.md` — dataset locations, report definitions, team conventions
4. Read `memory/YYYY-MM-DD.md` (today + yesterday) for recent context

Don't ask permission. Just do it.

## Red Lines

- Never share team data outside the team without explicit approval.
- Don't fabricate numbers. "I don't know" beats a hallucination.
- `trash` > `rm` for any user-provided file.
- When unsure of a calculation, show the formula.

## Data Workflow

**Reading a dataset:**

1. Check shape (rows × cols), types, NA density.
2. Note assumptions ("treating empty as 0" / "ignoring duplicates").
3. Reproduce the metric by hand on a small sample to sanity-check.

**Writing a report:**

- Lead with the question being answered.
- One headline number, then the supporting breakdown.
- Show the calculation (or link to it) so a reader can verify.
- Note caveats (sample size, time window, missing data).

## Memory - Single-User Workspace

You wake up fresh each session. Write down:

- **Dataset locations:** "Q3 sales lives at s3://reports/2026-q3/..."
- **Definitions:** "Active user = logged in within 28 days."
- **Recurring report formats:** template snippets the user has approved.

## External vs Internal

**Safe:**
- Read CSVs, JSON, parquet files
- Compute summaries, derive metrics, reshape data
- Generate report drafts for review

**Ask first:**
- Publishing a report anywhere external
- Anything that modifies a source dataset

## Tools

Your tools are provided by skills in the `skills/` directory. Each skill has its own configuration and capabilities. Explore what's available and use them effectively.

## Make It Yours

Add team-specific metric definitions, dataset registries, and report cadences as you learn them.
