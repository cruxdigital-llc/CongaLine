# Spec — Delegation Routing

**Goal**: Two-tier delegation. Tier 1 — every Conga agent can declare a
**subagent** (single secondary model) in its overlay; the runtime delegates
mechanical work to it autonomously via the runtime's native subagent
mechanism. Tier 2 — five canonical **role agents** ship as overlay packages
under `agents/_defaults/<runtime>/<role>/`, provisioned via
`conga admin add-user --role <name>`.

**Prerequisite**: `upstream-capability.md` (Phase 1 findings — confirms
OpenClaw `sessions_spawn` + `agents.defaults.subagents` and Hermes
`delegate_task` + `delegation:` both natively support this).

## Data model

### `agent.yaml` schema v2

Schema bumps from v1 → v2. Loader handles **both** explicitly; v1 documents
continue to parse with zero behavioral change. Adding the new `subagents:`
top-level key to a v1 document fails strict-key parsing (correct behavior).

```yaml
version: 2

# v1 model: block — unchanged.
model:
  provider: anthropic            # or "ollama", "openai" (existing v1 enum)
  name: claude-opus-4-7
  base_url: ""                   # empty = hosted Anthropic API
  # context_window, max_tokens optional

# NEW in v2 — top-level peer of `model:`. Optional. Absent = no subagent
# configured; runtime falls back to inheriting the primary model (which
# defeats the cost optimization but is a safe default).
subagents:
  model:
    provider: openai             # same enum as model.provider; ollama or openai
    name: qwen-2.5-72b-instruct
    base_url: https://litellm.lan/v1
    # context_window, max_tokens optional, same semantics as v1 model.*

  # OpenClaw-only: prompt-level nudge. Hermes ignores this field but the
  # loader does not reject it (forward-compat across runtimes).
  delegation_mode: prefer        # "prefer" or "suggest" (default: "suggest")

  # Universal: concurrency cap. Mapped per-runtime
  # (OpenClaw maxConcurrent, Hermes max_concurrent_children).
  max_concurrent: 4              # optional; 0/absent = runtime default

  # Hermes-only: nesting depth (range 1-3, default 1). OpenClaw ignores
  # this field — its nesting is implicit/policy-driven.
  max_spawn_depth: 1             # optional
```

**Strict-key rules unchanged**: unknown top-level keys still fail loudly.
`delegation_mode`, `max_concurrent`, `max_spawn_depth` are recognized in
v2 only; setting them in a v1 document is rejected.

**Reserved keys still reserved**: `memory`, `tools`, `limits`, `images`,
`pdf`, `video` continue to be future-version reservations (now under v3+).
`subagents` is removed from the reserved set (it's claimed by v2).

### Go type additions

`pkg/runtime/overlay.go`:

```go
const CurrentOverlaySchemaVersion = 2  // bump from 1

// AgentOverlay gains the Subagents field.
type AgentOverlay struct {
    Version   int               `yaml:"version"`
    Model     *ModelOverlay     `yaml:"model,omitempty"`
    Subagents *SubagentsOverlay `yaml:"subagents,omitempty"`  // NEW
}

// SubagentsOverlay configures the runtime's native subagent system.
// nil = no subagent configured. When set, Model is required.
type SubagentsOverlay struct {
    Model          *ModelOverlay `yaml:"model"`           // required when block present
    DelegationMode string        `yaml:"delegation_mode,omitempty"`
    MaxConcurrent  int           `yaml:"max_concurrent,omitempty"`
    MaxSpawnDepth  int           `yaml:"max_spawn_depth,omitempty"`
}
```

`ModelOverlay` is reused — no separate "subagent model" type. This means
sub-agent models go through the same validation as primary models
(provider enum, base_url shape, sane token caps).

### Schema versioning contract — v1 ↔ v2

Per `specs/2026-05-19_feature_local-model-routing/spec.md` §
"Schema versioning contract":

| Document `version:` | Behavior |
|---|---|
| absent | Accepted as v2 with a one-time "missing version" warning (existing behavior, just current-version updated from 1 to 2) |
| `1` | Accepted; only `model:` block recognized. Setting `subagents:` in a v1 doc = strict-key rejection |
| `2` | Accepted; `model:` + `subagents:` recognized |
| `3+` | Hard rejection: "agent.yaml requires conga >= <version-shipping-3> to read schema version <N>" |

**Loader implementation**: a v1 doc that contains a `subagents:` key
must fail with a clear message — *"subagents: requires schema version 2;
bump `version:` to 2 to use this key"* — not a generic strict-key error.
This is a friendlier failure mode borrowed from the existing reserved-key
mechanism (`pkg/common/overlay_agent.go` `checkReservedKeys`).

### Role package shape

A role is a directory under `agents/_defaults/<runtime>/<role>/`. The
directory layout mirrors the existing user/team default directories.

```
agents/_defaults/
  openclaw/
    user/                  # existing — DM-only agent type
    team/                  # existing — channel-based agent type
    role-ops/              # NEW — Qwen-backed operations role
      SOUL.md
      AGENTS.md
      USER.md.tmpl
      agent.yaml           # v2, model=Qwen, no subagents block
    role-data/             # NEW — Qwen-backed reporting role
    role-research/         # NEW — Qwen-backed research role
    role-code-dev/         # NEW — Opus-backed dev role w/ Qwen subagent
      SOUL.md
      AGENTS.md
      USER.md.tmpl
      agent.yaml           # v2, model=Opus, subagents.model=Qwen
    role-writing/          # NEW — Opus-backed writing role w/ Qwen subagent
  hermes/
    user/
    team/
    role-ops/              # mirrors openclaw/role-ops with hermes-specific tweaks
    ...
```

**Role naming convention**: `role-<kebab-case>`. The `role-` prefix
disambiguates from the existing `user` and `team` agent-type directories.
This means `agents/_defaults/<runtime>/role-code-dev/` not
`agents/_defaults/<runtime>/code-dev/`.

**`agent.yaml` in role defaults is concrete, not a template.** The
generator processes `USER.md.tmpl` (existing behavior), but `agent.yaml`
in a role package is loaded as-is. Operators who want to customize
override per-agent in `agents/<name>/agent.yaml` (existing per-agent
overlay path), which fully replaces the role's overlay.

### Role catalog (initial five)

| Role | Directory slug | Primary model | Subagent model | Default channels | AGENTS.md emphasis |
|---|---|---|---|---|---|
| Operations | `role-ops` | qwen-2.5-72b-instruct (or operator-chosen Qwen) | — | DMs, #ops | Monitoring, infra status, runbook execution |
| Data | `role-data` | qwen-2.5-72b-instruct | — | DMs, #data | CSV analysis, metrics reporting, format work |
| Research | `role-research` | qwen-2.5-72b-instruct | — | DMs, #research | Web research, doc digests, competitive intel |
| Code/Dev | `role-code-dev` | anthropic/claude-opus-4-7 | qwen-2.5-72b-instruct | DMs, #engineering | Code review, architecture, debugging |
| Writing | `role-writing` | anthropic/claude-opus-4-7 | qwen-2.5-72b-instruct | DMs, #writing | Drafts, edits, content strategy |

**The three Qwen roles have no `subagents:` block.** They're already
cheap; sub-agent overhead isn't worth it. The two Opus roles have a Qwen
sub-agent because that's where the cost wins live.

**Model names in the table are defaults** — operators can override
per-agent. The role package ships a sensible default; the per-agent
overlay can replace it.

### Provider/runtime config impact: zero

`AgentConfig` (`pkg/provider/provider.go`) is **unchanged**. No `Role`
field, no `Subagent` field. Role is encoded in the prompt files +
overlay; the rest of the system stays oblivious. This protects all
three providers and the JSON/SSM persistence layer from a schema
change.

## Runtime config generator changes

### OpenClaw (`pkg/runtime/openclaw/config.go`)

A new helper `applySubagentsOverlay(config, sub)` runs after the
existing `applyModelOverlay`. It mutates `config` to emit:

```json5
{
  "agents": {
    "defaults": {
      "model": { "primary": "anthropic/claude-opus-4-7", "fallbacks": [] },
      "models": {
        "anthropic/claude-opus-4-7": {},
        "openai/qwen-2.5-72b-instruct": {}    // subagent model also goes here
      },
      "subagents": {
        "model": "openai/qwen-2.5-72b-instruct",
        "delegationMode": "prefer",
        "maxConcurrent": 4
      }
    }
  },
  "models": {
    "providers": {
      "anthropic": { /* existing */ },
      "openai": {                              // subagent's provider config also added
        "baseUrl": "https://litellm.lan/v1",
        "models": [{ "id": "qwen-2.5-72b-instruct", "name": "qwen-2.5-72b-instruct" }]
      }
    }
  }
}
```

**Subagent model is merged into the existing models allowlist** so the
orchestrator can also `/model` switch to it manually. This preserves the
"additive allowlist" property Feature #27 established.

**`models.providers` block accumulates** both the primary's and the
subagent's provider config. If primary and subagent share a provider
(e.g. both `openai` but at different endpoints), the **subagent's
provider config wins** (the operator deliberately chose two different
providers; one provider entry maps to one endpoint). Spec note:
re-using the same provider with two endpoints is **not supported in v2**
— validation rejects it. Workaround: use the existing v1 `model:` block
for the primary's endpoint and put the subagent on a different provider
key (e.g. `ollama` for one, `openai` for the other).

### Hermes (`pkg/runtime/hermes/config.go`)

A new helper `applySubagentsOverlay` emits the `delegation:` block:

```yaml
# Hermes config.yaml fragment
delegation:
  model: "openai/qwen-2.5-72b-instruct"   # provider/name pair
  max_concurrent_children: 4
  max_spawn_depth: 1
  # delegation_mode is OpenClaw-only — skipped here
```

**Hermes provider-enum mismatch** (called out in upstream-capability.md):
Hermes' `delegation.provider` accepts a fixed enum (`openrouter`, `nous`,
`zai`, `kimi-coding`, `minimax`). Our overlay's enum is `ollama` |
`openai`. The mismatch is real but workable:

- If the operator's overlay says `subagents.model.provider: ollama` —
  Hermes inherits the parent's Ollama setup transparently; we emit
  only `delegation.model`, not `delegation.provider`.
- If the operator's overlay says `subagents.model.provider: openai` and
  `base_url` matches Hermes' supported endpoints — we emit
  `delegation.provider: openrouter` (most common case).
- If `base_url` doesn't match any known Hermes provider, we emit only
  `delegation.model` (leaving Hermes to fall back to the parent's
  provider) and emit a one-time warning at generation time:
  *"Hermes runtime does not natively support the overlay's openai
  base_url; subagent will inherit the parent's provider config."*

This degraded-mode behavior is intentional. Loud failure > silent wrong.

### Test surface for generators

Per `pkg/runtime/openclaw/config_test.go` style:

| Test | Input | Expected |
|---|---|---|
| `TestSubagentsOverlay_OpenClaw_Basic` | v2 overlay with subagents.model=Qwen | `agents.defaults.subagents.model == "openai/qwen-2.5-72b-instruct"`; subagent in models allowlist |
| `TestSubagentsOverlay_OpenClaw_DelegationMode` | overlay sets `delegation_mode: prefer` | `agents.defaults.subagents.delegationMode == "prefer"` |
| `TestSubagentsOverlay_OpenClaw_NoBlock` | v2 overlay with no `subagents:` | identical output to v1 overlay (no `subagents` key in JSON) |
| `TestSubagentsOverlay_OpenClaw_SameProviderConflict` | primary=openai/A@URL-1, subagent=openai/B@URL-2 | validation error: "subagent provider config conflicts with primary" |
| `TestSubagentsOverlay_Hermes_DegradedNoProvider` | overlay with openai + non-Hermes base_url | `delegation.model` emitted, `delegation.provider` omitted, warning logged |
| `TestSubagentsOverlay_Hermes_OllamaInherit` | overlay with ollama + base_url | `delegation.model` only; Hermes inherits parent ollama config |
| `TestV1OverlayWithSubagentsKey_FailsLoudly` | v1 doc with `subagents:` key | parse error mentions "schema version 2 required" |
| `TestV2OverlayMissingSubagentsModel` | v2 doc with `subagents:` block but no inner `model:` | validation error: "subagents.model is required when subagents block is present" |

## CLI changes

### `conga admin add-user --role <slug>`

New flag on the existing `add-user` and `add-team` commands. Currently
agents are typed via `--type user|team` (existing) and `--runtime
openclaw|hermes` (existing).

Behavior:
1. **Resolve role**: look up `agents/_defaults/<runtime>/role-<slug>/`. If
   missing, error with the list of available roles for that runtime.
2. **Copy role defaults to the agent's overlay dir**: at
   `agents/<agent-name>/`, write SOUL.md, AGENTS.md, USER.md.tmpl (process
   the template), and agent.yaml. Existing per-agent overrides
   (`agents/<name>/<file>` that already exist) are **preserved** —
   files in the destination are not overwritten. This lets an operator
   `--role X` an existing customized agent without losing their work.
3. **Continue with normal provisioning**: the rest of `add-user` is
   unchanged.

**The `--role` flag is mutually exclusive with `--type`** at the CLI
level. A role implies an agent type — `role-ops`/`role-data`/`role-research`
default to `--type user` (DM-driven), `role-code-dev`/`role-writing`
default to `--type team` (channel-driven). If the operator passes both
`--role` and `--type`, error: *"--role implies --type; pass one or the
other."*

**Inferring type from role**: encoded in `agents/_defaults/<runtime>/role-<slug>/role.meta`
(a tiny single-line file with one key: `type: user|team`). Generator
reads this file at `--role` resolution time. No `AgentConfig.Role` field
needed — type stays the user-visible concept.

### Interface parity

Per the **must-severity** Interface Parity standard
(`product-knowledge/standards/architecture.md`), every CLI flag must
exist in all three interfaces:

| Interface | Surface | Field |
|---|---|---|
| CLI | `--role <slug>` on `add-user` / `add-team` | new flag |
| JSON input | `"role": "code-dev"` on the AddUser command schema | new field; mutex with `"type"` |
| MCP | `conga_provision_agent` tool gains `role` string parameter | mutex with `type` |

JSON schema update: `internal/cmd/json_schema.go`. MCP tool registration:
`internal/mcpserver/tools_lifecycle.go`.

### `conga agent` subcommand impact

The `agent list / show / diff` commands already show per-agent overlay
content. No change needed — they'll naturally show the v2 `subagents:`
block in the agent's overlay directory.

`conga agent show <name>` should print which role the agent was
provisioned with **IF** the agent's overlay directory is byte-identical
to one of the role defaults (a comparison done at print time). If the
operator has modified anything, show "custom (originally role-X)" or
just "custom." Optional polish — not blocking.

## Egress integration

Per `requirements.md` success criterion #6 and Aaron's confirmed
**Option 3 (auto-derive + warn)** strategy.

### Provisioning-time check

`conga admin add-user` / `add-team` and `conga bootstrap` perform a
**non-blocking check** after the overlay is loaded:

1. Collect all `base_url` host values from the overlay
   (`model.base_url` + `subagents.model.base_url`).
2. Filter out empty/null hosts (Anthropic hosted = no base_url; doesn't
   need an explicit entry, it's `api.anthropic.com`).
3. Compare each derived host against the agent's effective egress
   allowlist (provider-specific resolution):
   - **AWS**: `tfvars` `agents.<name>.egress_allowed_domains`
     (operator-managed; we cannot mutate it).
   - **Local/Remote**: `conga-policy.yaml` `agents.<name>.egress.allowed_domains`
     merged with global.
4. For each derived host **not** in the effective allowlist, emit a
   one-line warning to stderr:
   ```
   warning: agent X overlay declares subagent endpoint litellm.lan but
   it is not in the egress allowlist. The agent will provision, but
   subagent requests will be denied at runtime (HTTP 403 via egress
   proxy). Add "litellm.lan" to:
     - terraform.tfvars: agents.X.egress_allowed_domains  (AWS)
     - ~/.conga/conga-policy.yaml: agents.X.egress.allowed_domains  (local/remote)
   ```
5. Provisioning **continues**. The operator can address the gap; the
   warning makes it visible.

**Anthropic auto-allowance**: when the overlay primary is `anthropic`
(no `base_url`), the implicit endpoint `api.anthropic.com` is treated
as already allowed — Conga's bootstrap manifest already adds Anthropic
to the default allowlist for new agents (per
`pkg/manifest/manifest.go`). The check skips it for primary models. For
subagents, same rule applies if `subagents.model.provider` is anthropic
(currently disallowed by validation — see below).

### `subagents.model.provider == anthropic`?

**Validation rejects** anthropic as a subagent provider in v2. Reason:
the entire point of the subagent block is to point at a *cheaper* model.
If the operator wants Anthropic-on-Anthropic, that's already
expressible by `/model`-switching mid-conversation between two
Anthropic models — no subagent block needed. The validation message:
*"subagents.model.provider must be 'ollama' or 'openai'; for
Anthropic-only fleets, use the runtime's native /model switching"*.

This is a deliberate scope-narrowing for v2. A future v3 may relax it.

### Egress check helper

New function in `pkg/common/`:

```go
// CheckOverlayEgress returns a list of endpoint hosts from the overlay
// that are NOT present in the agent's effective egress allowlist. The
// returned slice is suitable for emitting one warning line per missing
// host. Returns nil when everything is allowed.
func CheckOverlayEgress(overlay *runtime.AgentOverlay, effectiveAllowlist []string) []string
```

Provider-specific resolution of `effectiveAllowlist` lives in the
provider (it knows where to look — tfvars vs policy file).
`add-user`/`add-team` orchestrate the call. Bootstrap calls it as part
of its provisioning pipeline.

## Secrets handling for subagent providers

Per `product-knowledge/standards/config-taxonomy.md` anti-pattern *"Secret
VALUES in `agent.yaml`"* and Worked Example #1, the subagent's API key
follows the **same secrets-store pattern** as the primary's:

- **`subagents.model.provider: ollama`** — uses the LAN sentinel
  `ollama-local` (same as primary ollama); no per-agent secret required.
- **`subagents.model.provider: openai`** — requires the per-agent secret
  `openai-api-key` (already a recognized secret name in Feature #27).
  If the agent already has an openai-api-key (because the primary is also
  openai-compatible), the subagent shares it — there's one API key per
  agent, not per model.

**Implication**: an agent with `model.provider: anthropic` + a
`subagents.model.provider: openai` block needs the `openai-api-key`
secret set before its first subagent invocation. Provisioning does NOT
block on this; the secret can be set later via `conga secrets set`. At
runtime, the first subagent spawn fails with a clear runtime error if
the secret is missing.

**No new secret names introduced.** Reuses `openai-api-key` (existing)
and `anthropic-api-key` (existing). The secret-name → env-var mapping
(`SecretNameToEnvVar` in `pkg/runtime/runtime.go`) is unchanged.

## Channel × Runtime × Role compatibility

The existing Channel × Runtime matrix (CLAUDE.md):

| Channel | OpenClaw | Hermes |
|---|---|---|
| `slack` | ✅ | ✅ |
| `telegram` | ❌ | ✅ |

**Role × Runtime**: all five roles ship for **both** runtimes
(`agents/_defaults/openclaw/role-*/` and `agents/_defaults/hermes/role-*/`).
A role does not constrain runtime — operators can run a Code/Dev role on
either OpenClaw or Hermes.

**Role × Channel**: no new constraint. The default channels in the role
catalog table are suggestions, not enforced. `conga channels bind`
remains the way to attach a channel to any agent.

**Role × Egress**: roles don't ship infrastructure. The role's
`agent.yaml` declares the model endpoints (LiteLLM, Anthropic, etc.) —
the operator separately ensures those endpoints are in the egress
allowlist (per Egress integration above).

**Combinations that auto-fail**:
- Role using Hermes runtime + Telegram channel: continues to work (Telegram
  is Hermes-only — that's the existing valid combination).
- Role using OpenClaw runtime + Telegram channel: continues to fail per
  the existing channel × runtime matrix; the role doesn't override that.
- v2 overlay against Hermes with non-supported subagent provider config:
  degrades to "subagent inherits parent" + warning (see Hermes generator
  section).

## Agent Data Safety

Per the **must-severity** Agent Data Safety standard
(`product-knowledge/standards/architecture.md`):

**This feature does not touch agent data directories.** All changes are
in:
- Config generation paths (`openclaw.json`, Hermes `config.yaml`) which
  are regenerated on every `RefreshAgent` and never touch
  `~/.conga/data/<name>/` (local) / `/opt/conga/data/<name>/` (remote, AWS).
- Overlay loading paths (`agents/<name>/agent.yaml`) which are operator-
  authored and outside the data directory.
- Role packages (`agents/_defaults/<runtime>/role-*/`) which are
  workspace-level defaults, not agent-specific data.

**`--role` provisioning preserves existing overlay files** (point 2 of
the CLI section) — operators who add `--role X` to an existing agent
don't lose customizations.

**No volume mount changes. No data deletion. No agent data
restructuring.** ✅ Data safety preserved.

## Edge cases

| Scenario | Expected behavior |
|---|---|
| v1 overlay (existing Feature #27 agents) | Unchanged — no new fields recognized; output is byte-identical to today |
| v2 overlay, no `subagents:` block | Validation passes; output identical to v1 (no `subagents` key in generated config) |
| v2 overlay, `subagents:` block without `model:` | Validation error: "subagents.model is required when block present" |
| Subagent endpoint unreachable at provision time | Provisioning succeeds (the runtime starts; sub-agent invocations fail at first call with a 503-style error from the runtime, surfaced in chat). Same self-healing semantics as Feature #27 primary endpoint |
| Subagent endpoint missing from egress allowlist | Warning at provision; subagent invocations 403 at runtime via egress proxy. Operator action required |
| Operator passes `--role X` to an agent that already has `agent.yaml` | Existing files preserved; only missing files copied from role defaults. `agent.yaml` is NOT overwritten if it exists — the operator's customization wins |
| Operator passes `--role X` + `--type Y` | CLI rejects with mutex error |
| Operator passes a role slug that doesn't exist for the chosen runtime | Error: lists available roles for that runtime |
| v2 + Hermes runtime + subagent with openai/non-supported base_url | Warning at config generation; `delegation.provider` omitted; sub-agent inherits parent's provider (degraded but non-crashing) |
| v2 + Hermes runtime + subagent with ollama provider | Works cleanly; `delegation.model` emitted; Hermes inherits parent ollama setup |
| Operator sets primary=openai/A@URL-1 and subagent=openai/B@URL-2 | Validation error: "subagent and primary cannot share provider key with different base_urls; use one provider per agent in v2" |
| Operator sets `subagents.model.provider: anthropic` | Validation error: "subagents.model.provider must be 'ollama' or 'openai'" |
| Per-agent overlay (`agents/<name>/agent.yaml`) is v2 but `agents/_defaults/<runtime>/role-X/agent.yaml` is v1 | Two-version coexistence is fine — defaults parse as v1, per-agent parses as v2. When `--role` copies the default to an agent dir, the version in the file is preserved |
| Egress proxy down during subagent invocation | Same as primary-model proxy-down: chat surface shows runtime error; self-heals when proxy recovers |
| Subagent model OOM / context-window-exceeded | Runtime's responsibility — surfaces 400 from the LiteLLM/vLLM endpoint. Operator-actionable via `context_window` / `max_tokens` on `subagents.model.*` |

## Phased implementation contract

Phases are sized for **small, independently-verifiable commits**. Each
must build, vet, gofmt clean, and pass `go test ./...` before moving on.

### Phase 1 — Schema bump + types (no behavior yet)

- `pkg/runtime/overlay.go`:
  - `CurrentOverlaySchemaVersion = 2`
  - Add `SubagentsOverlay` struct + `AgentOverlay.Subagents` field
  - Add `(s *SubagentsOverlay) Validate()` with the rules from the
    Edge cases table (required model when block present, no anthropic
    for subagent provider, no same-provider conflict, etc.)
- `pkg/common/overlay_agent.go`:
  - Remove `subagents` from `reservedTopLevelKeys`
  - Add the helpful v1-with-subagents error message
- Tests: `pkg/runtime/overlay_test.go` — add cases for the validation
  rules; v1 docs still pass; v2 doc with subagents parses; v1 doc with
  subagents fails loudly.

**Acceptance**: `go test ./pkg/runtime/... ./pkg/common/...` passes.
No generator changes yet.

### Phase 2 — OpenClaw generator

- `pkg/runtime/openclaw/config.go`:
  - New `applySubagentsOverlay(config, sub *runtime.SubagentsOverlay)`
    helper called after `applyModelOverlay` in `GenerateConfig`.
  - Emit `agents.defaults.subagents.{model, delegationMode, maxConcurrent}`.
  - Merge subagent model into `agents.defaults.models` allowlist.
  - Merge subagent provider into `models.providers.<id>` (rejecting
    same-provider conflicts per validation).
- Tests: `pkg/runtime/openclaw/config_test.go` — add the table from
  "Test surface for generators" above.

**Acceptance**: live-test against the existing Feature #27 production
agent on AWS: the agent's overlay stays v1, output is byte-identical.
A new test agent with a v2 overlay + subagent block produces the
expected JSON shape (capture via a `conga refresh` dry-run and diff).

### Phase 3 — Hermes generator

- `pkg/runtime/hermes/config.go`:
  - New `applySubagentsOverlay` emitting the `delegation:` block.
  - Implement the degraded-mode logic for unsupported providers.
  - One-time warning helper for the unsupported-provider case (reuse
    the `overlayWarningOnce sync.Map` pattern from
    `pkg/common/overlay_agent.go`).
- Tests: covering the degraded paths from the table above.

**Acceptance**: Hermes runtime startup succeeds with a v2 overlay
declaring an `openai` subagent; runtime logs show the delegation config
applied; degraded-mode warning visible.

### Phase 4 — Egress check helper + provisioning integration

- `pkg/common/egress_check.go` (new):
  - `CheckOverlayEgress(overlay, effectiveAllowlist) []string`
- `internal/cmd/admin_provision.go`:
  - Call `CheckOverlayEgress` after overlay load, before provisioning
    proceeds. Emit warnings; don't block.
- Provider resolution of `effectiveAllowlist`:
  - Local provider: read `~/.conga/conga-policy.yaml` per-agent egress.
  - Remote provider: read remote `/opt/conga/conga-policy.yaml`.
  - AWS provider: read tfvars-derived SSM parameter
    (existing pattern from policy enforcement reports).

**Acceptance**: provisioning a v2 agent with a missing subagent endpoint
in egress emits a clear warning and continues; same agent with the
endpoint present provisions silently.

### Phase 5 — Role packages

- Create five new directories under `agents/_defaults/openclaw/` and
  five under `agents/_defaults/hermes/`:
  - `role-ops/`, `role-data/`, `role-research/`,
    `role-code-dev/`, `role-writing/`
- Each contains: `SOUL.md`, `AGENTS.md`, `USER.md.tmpl`, `agent.yaml`,
  `role.meta` (the one-line type indicator).
- Content authored per the Role catalog table — Qwen roles get model-only
  overlays, Opus roles get model + subagents.
- Light README.md per role explaining the role's purpose (one paragraph).

**Acceptance**: directory tree matches; each `agent.yaml` parses
through the v2 loader without error.

### Phase 6 — CLI `--role` flag

- `internal/cmd/admin_provision.go` (and the `add-team` equivalent):
  - Add `--role` flag, mutex with `--type`.
  - Resolve role → copy defaults to `agents/<name>/` (preserving
    existing files).
  - Read `role.meta` to infer type.
- `internal/cmd/json_schema.go`:
  - Add `role` field to AddUser/AddTeam JSON schemas; mutex with `type`.
- `internal/mcpserver/tools_lifecycle.go`:
  - Add `role` parameter to `conga_provision_agent`; mutex with `type`.

**Acceptance**:
- `conga admin add-user --role code-dev --runtime openclaw` provisions an
  Opus + Qwen-subagent agent end-to-end.
- JSON input variant works identically.
- MCP tool variant works identically.
- **Idempotency test** (per QA persona review): running
  `conga admin add-user --role code-dev` against an *existing* agent
  whose `agents/<name>/agent.yaml` was customized must preserve the
  customization. Only missing files from the role default are added.

### Phase 7 — Docs

- `agents/_example/agent.yaml.example`: bump to v2; show a `subagents:`
  block in the comment + worked example. Keep a v1 example as a
  reference.
- `product-knowledge/standards/config-taxonomy.md`: extend the runtime
  overlay row to mention subagents; add Worked Example #5 (role package).
- `CLAUDE.md`: add a "Delegation Model" section with the five-role
  catalog and a one-paragraph on the v2 overlay. Add the upstream
  vocabulary mapping (subagent = `sessions_spawn` / `delegate_task`).
- `README.md`: brief mention if the README already describes Feature #27
  routing.

**Acceptance**: docs pass spelling/link-check (existing pre-commit
hooks). Manual review by the architect.

### Phase 8 — Verification

- Re-run `go test ./...` with `-count=1`; all packages pass.
- Live smoke on local provider: provision `--role code-dev`; chat
  through the agent; confirm subagent spawn via OpenClaw runtime logs
  (`docker logs conga-X | grep subagent`).
- Live smoke on AWS: provision `--role ops` on the existing test
  environment; confirm refresh-all still works.
- `/glados:verify-feature` workflow.

**Acceptance**: clean test suite + live smoke logs attached to spec.

## Test plan summary

| Layer | Tests | New / Modified |
|---|---|---|
| Overlay types & validation | `pkg/runtime/overlay_test.go` | ~8 new test cases |
| OpenClaw generator | `pkg/runtime/openclaw/config_test.go` | ~6 new test cases |
| Hermes generator | `pkg/runtime/hermes/config_test.go` | ~4 new test cases |
| Overlay loader (reserved keys, v1↔v2) | `pkg/common/overlay_agent_test.go` | ~3 new test cases |
| Egress check helper | `pkg/common/egress_check_test.go` (new file) | ~5 test cases |
| CLI `--role` flag | `internal/cmd/admin_provision_test.go` | ~4 new test cases (incl. `--role` idempotency on existing agent) |
| JSON input schema | `internal/cmd/json_schema_test.go` | ~2 new test cases |
| MCP tool | `internal/mcpserver/tools_lifecycle_test.go` | ~2 new test cases |
| Integration (CLI lifecycle) | `internal/cmd/integration_*_test.go` | 1 new subtest for `--role` |

Existing regression suite (Feature #27 tests, channel tests, runtime
tests) must remain green.

## Out-of-scope (carried to backlog)

These were debated during planning and explicitly deferred:

- **Bifrost-style cost-routing proxy** (ROADMAP #22) — request-time
  model selection by cost. Stays separate from this feature.
- **Per-role token budget caps** — `subagents.limits: {...}` v3 key.
- **Cross-agent invocation as MCP tools** — Code/Dev agent calls a
  Research agent. Promising but needs MCP server maturity.
- **`AgentConfig.Role` field** — Phase 3 Route B from plan.md. Revisit
  when introspection demand materializes.
- **Subagent observability** — counts, costs, latency per delegation.
  Owned by Bifrost work when it lands.
- **Multiple named subagent models** — list shape in overlay. Bump to
  v3 when a real second-subagent use case appears (e.g. vision).

## Open questions closed by this spec

All seven from plan.md, plus the naming flag raised in
upstream-capability.md:

1. ✅ Upstream mechanism: `sessions_spawn` + `agents.defaults.subagents`
   (OpenClaw); `delegate_task` + `delegation:` (Hermes). See
   upstream-capability.md.
2. ✅ Hint or no hint: `delegationMode` is a prompt nudge, not a routing
   hint. Exposed in overlay as `delegation_mode`.
3. ✅ Egress strategy: Option 3 — auto-derive + warn at provision.
4. ✅ Role catalog: ship all five (Ops, Data, Research, Code/Dev,
   Writing).
5. ✅ `AgentConfig.Role`: not added. Route A (overlay packages) wins.
6. ✅ Hermes parity: degraded but non-crashing; documented in spec.
7. ✅ Compat matrix: no new entries needed; roles don't override
   channel × runtime constraints.
8. ✅ Naming: Tier 1 = "subagent" (not "delegate" — upstream collision);
   Tier 2 = "role agent" (unchanged).

## Handoff

- `/glados:implement-feature` is the next step.
- Recommend implementing Phases 1 → 8 in order; each phase is a small
  reviewable commit.
- Phase 2 (OpenClaw generator) should be live-smoked against the
  existing Feature #27 production agent on AWS before Phase 5 (role
  packages) lands.
