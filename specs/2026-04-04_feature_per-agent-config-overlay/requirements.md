# Requirements: Per-Agent Config Overlay

## Problem

Congaline's behavior seeding (see `2026-03-20_feature_behavior-management`)
composes workspace files from `behavior/base/`, `behavior/{user,team}/`, and
`behavior/overrides/<agent>/`. The override layer is limited to a fixed set
of filenames (`SOUL.md`, `AGENTS.md`, `USER.md`) and is a **replacement**, not
an **addition**.

This prevents the common case of seeding an agent with extra project-specific
reference material — e.g. a `CLIENT.md`, `PROJECT.md`, or `TEAM.md` — without
also rewriting the shared baseline.

## Goals

1. **Arbitrary per-agent markdown files.** An operator can drop any number of
   `.md` files into a per-agent directory and have them seeded into the
   agent's workspace on provision/refresh, alongside the composed baseline.
2. **Additive by default.** The per-agent layer adds files; it does not
   implicitly delete baseline files.
3. **Explicit replacement still supported.** If a per-agent file shares a
   name with a baseline-composed file (`SOUL.md`, `AGENTS.md`, `USER.md`),
   it replaces that file verbatim — preserving today's `overrides/` semantics.
4. **Never clobber agent-mutable state.** Files the agent writes to during
   normal operation (`MEMORY.md`, anything under `memory/`, agent-authored
   notes) must not be overwritten or deleted by seeding.
5. **Works on all three providers.** Local, remote (SSH), and AWS must all
   deliver the same overlay with consistent semantics.
6. **Survives refresh.** Re-running `conga admin refresh-all` (or per-agent
   refresh) re-applies the overlay from source-of-truth, so operators can
   edit an overlay file and push it out.
7. **Immediate use case.** Tailor a team agent to a client engagement: ship
   `CLIENT.md`, `TEAM.md`, `PROJECT.md` alongside the standard team baseline.

## Non-Goals

- **Not a secrets mechanism.** Overlay files are plain markdown, version-
  controlled, and world-readable inside the workspace. Secrets continue to
  flow through the env/secrets path.
- **Not a templating engine rewrite.** The existing `{{AGENT_NAME}}` +
  channel-binding template variables are sufficient; we are not introducing
  Go templates, partials, or conditionals.
- **Not bidirectional sync.** We seed from source to workspace only. The
  agent may edit overlay files at runtime, but those edits are considered
  ephemeral and will be overwritten on the next refresh. (See "Protected
  file list" below for the exceptions that are never touched.)
- **Not a per-agent override of non-markdown config** (`openclaw.json`,
  env files, policy). Those have their own dedicated paths.

## Constraints

- **C1: Protected files.** The seeding code must have a hard-coded deny list
  that includes at minimum `MEMORY.md` and any path under `memory/`,
  `logs/`, or `agents/`. These are never written, deleted, or chown'd by
  the overlay mechanism.
- **C2: Additive merge.** Seeding writes/updates overlay files but does not
  delete files in the workspace that are no longer present in the overlay
  source (with one narrow exception — see C3).
- **C3: Managed-file tracking.** To avoid leaving stale overlay files behind
  when an operator removes a file from the overlay source, the provider
  writes a manifest (`.conga-overlay-manifest.json`) listing the files it
  placed. On the next refresh, any file in the previous manifest that is no
  longer in the new overlay source is deleted — but only if the file still
  matches the content the manifest recorded. If the agent (or a human) has
  modified it, it is left alone and a warning is logged.
- **C4: File-type allowlist.** Only `.md` files are seeded from the overlay
  tree in the initial cut. Non-markdown files are ignored with a warning,
  keeping the blast radius small and preventing accidental binary/script
  drops. (Expandable later.)
- **C5: Same composition priority as today.** For the three
  composition-managed files (`SOUL.md`, `AGENTS.md`, `USER.md`), a file in
  the per-agent overlay replaces the composed baseline, exactly matching
  current `overrides/` behavior.
- **C6: Validation up front.** `conga admin refresh` / `refresh-all` must
  fail fast with a clear error if the overlay contains files that violate
  the protected list, rather than silently skipping them.

## Success Criteria

- An operator can run `conga agent overlay add <agent> ./path/to/CLIENT.md`
  (or equivalent — CLI surface is specified in `spec.md`), refresh the
  agent, and see `CLIENT.md` in the agent's workspace alongside `SOUL.md`,
  `AGENTS.md`, `USER.md`.
- Running `refresh-all` after editing `CLIENT.md` in the overlay source
  updates the file in the workspace on every provider.
- `MEMORY.md` and the `memory/` tree are byte-identical before and after a
  refresh with an overlay present — verified by test.
- Removing a file from the overlay source and refreshing removes it from
  the workspace **iff** the agent has not modified it; otherwise it is
  preserved and a warning is printed.
