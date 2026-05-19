# Specification: Multi-Channel Team Agents

Detailed technical specification. Assumes `requirements.md` and `plan.md` have been read.

## 1. Data Models

### 1.1 `channels.ChannelBinding` (unchanged)

```go
// pkg/channels/channels.go:6-10 (no change)
type ChannelBinding struct {
    Platform string `json:"platform"`
    ID       string `json:"id"`
    Label    string `json:"label,omitempty"`
}
```

### 1.2 `provider.AgentConfig.Channels` (unchanged)

```go
// pkg/provider/provider.go:28-36 (no change)
type AgentConfig struct {
    // ... existing fields
    Channels []channels.ChannelBinding `json:"channels,omitempty"`
}
```

`Channels` is already a slice. The single-binding constraint was enforced at bind-time, not at the schema level. Persisted agent JSON files on disk (local, remote, aws) stay in the same shape — no migration required.

### 1.3 `provider.AgentConfig.ChannelBinding` (unchanged behavior)

```go
// pkg/provider/provider.go:38-46 (no change)
// Returns the first binding matching the platform, or nil.
func (a *AgentConfig) ChannelBinding(platform string) *channels.ChannelBinding {
    for i := range a.Channels {
        if a.Channels[i].Platform == platform {
            return &a.Channels[i]
        }
    }
    return nil
}
```

Kept for callers that only need "is this platform bound at all?" semantics.

### 1.4 `provider.AgentConfig.ChannelBindings` (new)

```go
// pkg/provider/provider.go (new method)
// Returns all bindings matching the platform. Empty slice if none.
// Preserves insertion order (the order of conga channels bind calls).
func (a *AgentConfig) ChannelBindings(platform string) []channels.ChannelBinding {
    var out []channels.ChannelBinding
    for _, b := range a.Channels {
        if b.Platform == platform {
            out = append(out, b)
        }
    }
    return out
}
```

Insertion order is preserved so that `routing.json` and `openclaw.json` regeneration produces stable output for a given agent state (deterministic test snapshots).

### 1.5 `channels.MultiBindingChannel` (new interface)

```go
// pkg/channels/channels.go (added)
// MultiBindingChannel is implemented by platforms that support multiple
// bindings of the same platform on a single agent (e.g. one agent on
// several Slack channels). Platforms that do not support this continue
// to implement only the base Channel interface.
type MultiBindingChannel interface {
    Channel
    OpenClawChannelConfigMulti(bindings []ChannelBinding) map[string]any
}
```

Compile-time assertion elsewhere to prevent drift:
```go
// pkg/channels/slack/slack.go
var _ channels.MultiBindingChannel = (*Slack)(nil)
```

### 1.6 `RoutingConfig` (unchanged schema, new invariant)

```go
// pkg/common/routing.go
type RoutingConfig struct {
    Channels map[string]string `json:"channels"`
    Members  map[string]string `json:"members"`
    // ... (dm-agent-routing may add fields here independently)
}
```

**New invariant**: `Channels` may contain multiple keys (channel IDs) mapped to the same URL value, when a team agent has multiple bindings. No other consumer of `RoutingConfig` needs to change — the router already iterates the map by channel-ID lookup.

## 2. API / Interface Changes

### 2.1 Bind guard (three providers, identical change)

**Before** (`pkg/provider/localprovider/channels.go:187-191`, and parity in remote/aws):
```go
if a.ChannelBinding(binding.Platform) != nil {
    return fmt.Errorf("agent %q already has a %s binding: %w",
        agentName, binding.Platform, provider.ErrBindingExists)
}
```

**After** (per-agent check):
```go
for _, existing := range a.Channels {
    if existing.Platform == binding.Platform && existing.ID == binding.ID {
        if binding.Label != "" && existing.Label != binding.Label {
            return fmt.Errorf(
                "binding %s:%s already exists on agent %q with a different label (%q); "+
                "unbind first to relabel",
                binding.Platform, binding.ID, agentName, existing.Label)
        }
        // Idempotent: exact binding already present (same label, or no new label supplied).
        return nil
    }
}
```

**Plus a cross-agent uniqueness check** added before persisting the new binding:

```go
// Catch collisions that would silently overwrite routing.json entries.
others, err := p.ListAgents(ctx)
if err != nil {
    return fmt.Errorf("failed to check binding uniqueness: %w", err)
}
for _, other := range others {
    if other.Name == agentName {
        continue
    }
    for _, b := range other.Channels {
        if b.Platform == binding.Platform && b.ID == binding.ID {
            return fmt.Errorf(
                "channel %s:%s is already bound to agent %q; unbind it there first",
                binding.Platform, binding.ID, other.Name)
        }
    }
}
```

Rationale: `routing.json` is a flat `channels[id] → url` map. Without this guard, binding the same channel ID to two different agents silently overwrites the first mapping — the earlier agent stops receiving events with no error surfaced to the operator. This check is explicitly not about multi-binding (it's a preexisting bug with a narrow attack surface in the single-binding world), but becomes materially more likely once multi-binding exists, so it lands in the same PR.

Summary table of the new bind-time semantics:

| Case | Behavior |
|------|----------|
| New `(platform, id)` on this agent | Append, return nil. |
| Exact `(platform, id)` on this agent, same label (or empty label provided) | No-op, return nil. |
| Exact `(platform, id)` on this agent, different non-empty label | Error: relabel-requires-unbind. |
| Same `(platform, id)` on a *different* agent | Error: already bound to `<other-agent>`. |

**`provider.ErrBindingExists` is removed.** Verified external usage at `terraform-provider-conga/internal/terraform/binding_resource.go:110` — the Terraform provider previously used it to distinguish "already bound" from "other error" and to reconcile idempotent `terraform apply`. Under the new semantics, `BindChannel` returns `nil` for both cases the Terraform provider cared about (exact duplicate = no-op success; different ID = valid new binding). The `if errors.Is(err, ErrBindingExists)` block and the "Conflicting binding exists" diagnostic at lines 110-127 are deleted. The sentinel var and the test file `pkg/provider/errors_test.go` are deleted as part of this change.

### 2.2 Routing generation

**Before** (`pkg/common/routing.go:62-71`):
```go
case "team":
    binding := a.ChannelBinding("slack")
    if binding == nil {
        continue
    }
    cfg.Channels[binding.ID] = url
```

**After**:
```go
case "team":
    for _, binding := range a.ChannelBindings("slack") {
        cfg.Channels[binding.ID] = url
    }
```

Iteration is on the `ChannelBindings` slice which preserves insertion order (see 1.4). Repeated bindings of the same ID cannot occur (guarded at bind-time, 2.1).

### 2.3 Slack channel config (multi path)

**New method on `slack.Slack`** (matches the interface in 1.5):

```go
// pkg/channels/slack/slack.go
func (s *Slack) OpenClawChannelConfigMulti(bindings []channels.ChannelBinding) map[string]any {
    if len(bindings) == 0 {
        return nil
    }

    channelsMap := make(map[string]any, len(bindings))
    allowlist := make([]string, 0, len(bindings))

    for _, b := range bindings {
        channelsMap[b.ID] = map[string]any{
            // Same per-channel values as the existing singular path.
            // Extract into a helper to keep singular + multi in sync.
        }
        allowlist = append(allowlist, b.ID)
    }

    return map[string]any{
        "signingSecret": s.signingSecret(),
        "botToken":      s.botToken(),
        "mode":          "http",
        "groupPolicy":   "allowlist",
        "channels":      channelsMap,
        "allowlist":     allowlist,
        // Any other top-level fields produced by the singular path.
    }
}
```

**Refactor**: The singular `OpenClawChannelConfig` should be rewritten as a one-line wrapper:
```go
func (s *Slack) OpenClawChannelConfig(b channels.ChannelBinding) map[string]any {
    return s.OpenClawChannelConfigMulti([]channels.ChannelBinding{b})
}
```

This guarantees byte-identical output for single-binding agents (Success Criteria #8) without maintaining two parallel code paths.

### 2.4 Runtime config generator call site

**`pkg/runtime/openclaw/config.go`** — where channels are assembled into `openclaw.json`:

```go
for _, platform := range knownPlatforms {
    ch := channels.Get(platform) // existing registry lookup
    if ch == nil {
        continue
    }

    var cfg map[string]any
    if multi, ok := ch.(channels.MultiBindingChannel); ok {
        cfg = multi.OpenClawChannelConfigMulti(params.Agent.ChannelBindings(platform))
    } else {
        binding := params.Agent.ChannelBinding(platform)
        if binding == nil {
            continue
        }
        cfg = ch.OpenClawChannelConfig(*binding)
    }

    if cfg == nil {
        continue
    }
    channelsCfg[platform] = cfg
}
```

The type assertion is the single decision point. Implementations that only satisfy `Channel` fall through the `else` branch and behave exactly as today.

### 2.5 `Provider` interface: `UnbindChannel` signature change

**Before** (`pkg/provider/provider.go:193`):
```go
UnbindChannel(ctx context.Context, agentName string, platform string) error
```

**After**:
```go
UnbindChannel(ctx context.Context, agentName string, platform string, id string) error
```

Rationale: the old signature addressed the sole binding implicitly. With N bindings per platform allowed, the caller must name which one to remove. All implementations and call sites update in lockstep:

**Implementations updated:**
- `pkg/provider/localprovider/channels.go:235`
- `pkg/provider/remoteprovider/channels.go:214`
- `pkg/provider/awsprovider/channels.go:240`

Each implementation's body changes from "find-by-platform, remove" to "find-by-(platform, id), remove". When `id == ""` and the agent has exactly one binding for the platform, remove it (legacy compatibility for internal callers that may not plumb the ID yet). When `id == ""` and the agent has 2+ bindings for the platform, return a new sentinel error:

```go
// pkg/provider/errors.go (replaces ErrBindingExists)
var ErrAmbiguousUnbind = errors.New("multiple bindings exist; specify binding id")
```

This is the only new sentinel introduced by this spec. Callers (CLI, MCP, Terraform provider) each decide how to present the ambiguity — the CLI enumerates bindings and exits non-zero, the Terraform provider never hits this path because its Delete always has the ID from state.

**Internal call sites updated:**
- `internal/cmd/channels.go:276` — passes the parsed `id` portion of `<platform>:<id>`.
- `internal/mcpserver/tools_channels.go:217` — accepts a new `id` input field on the `conga_channels_unbind` tool.
- `internal/mcpserver/server_test.go:143` — mock signature updated.

**External call site updated (Terraform provider):**
- `internal/terraform/binding_resource.go:176` — passes `state.BindingID.ValueString()`.

### 2.6 CLI surface

#### `conga channels bind <agent> <platform>:<id> [--label <label>]`

- **Behavior**: Adds a binding. Idempotent for exact `(platform, id)` match.
- **Exit code**: 0 on success or no-op; non-zero on validation failure (bad format, unknown agent, etc.).
- **Output on idempotent no-op**: `binding already exists for <agent>: <platform>:<id>` (stderr), exit 0.
- **Output on success**: `bound <agent> to <platform>:<id>` (stdout), exit 0.

#### `conga channels unbind <agent> <platform>:<id>`

- **Behavior**: Removes the exact `(platform, id)` binding.
- **ID is now mandatory when multiple bindings exist for the platform.** Prior to this change, an agent had at most one binding per platform; CLI UX may have allowed `conga channels unbind <agent> slack` (platform-only) as a convenience.
- **Error when ID omitted and 2+ bindings exist**:
  ```
  agent "acme" has 3 slack bindings; specify the ID to remove.
  Current bindings:
    slack:C0123456789
    slack:C0234567890
    slack:C0345678901
  Example: conga channels unbind acme slack:C0123456789
  ```
  Exit code: non-zero (validation error).
- **When exactly one binding exists**: platform-only form may remain accepted for backward compatibility with existing automation; no behavior change for that case.

#### `conga channels list [--agent <name>]`

- **Output (table)**:
  ```
  AGENT    PLATFORM  ID            LABEL
  acme     slack     C0123456789   #legal-vendor
  acme     slack     C0234567890   #sales-deals
  acme     slack     C0345678901
  payroll  slack     C0456789012   #finance
  ```
- **Sort order**: by agent name (asc), then platform (asc), then ID (asc). Stable, deterministic.
- **One row per binding.** Agent name is repeated rather than using a ditto mark — easier for downstream scripting.
- **With `--agent <name>`**: filters to one agent's bindings. Header still printed.
- **Empty**: prints the header only; exit 0.

#### MCP tools (`internal/mcpserver/tools_channels.go`)

Tool descriptions updated to mention multi-binding support. Behavioral parity with the CLI:
- `conga_channels_bind`: idempotent, accepts repeated calls.
- `conga_channels_unbind`: requires ID when multiple bindings present, same error message format as the CLI.
- `conga_channels_list`: returns JSON array of `{agent, platform, id, label}` objects; sort order matches CLI.

### 2.7 Router — unbound channel ephemeral notice

#### `resolveTarget` (extended return shape)

```javascript
// router/slack/src/index.js (modified)
function resolveTarget(payload) {
  const channel = extractChannel(payload);

  if (channel) {
    if (channel.startsWith('D')) {
      const userId = extractUser(payload);
      if (userId && config.members[userId]) {
        return { target: config.members[userId], reason: `dm:${userId}` };
      }
      return null; // DM with no mapping — unchanged
    }
    if (config.channels[channel]) {
      return { target: config.channels[channel], reason: `channel:${channel}` };
    }
    // NEW: known to be a channel (C… or G…) but not in routing.json
    if (channel.startsWith('C') || channel.startsWith('G')) {
      return {
        target: null,
        reason: `unbound:${channel}`,
        channelId: channel,
        userId: extractUser(payload),
      };
    }
  }

  // Fallback: user-based routing (app_home, etc.) — unchanged
  const userId = extractUser(payload);
  if (userId && config.members[userId]) {
    return { target: config.members[userId], reason: `user:${userId}` };
  }

  return null;
}
```

#### Event handler

After `const route = resolveTarget(body)` (line 177 in current file):

```javascript
if (!route) return;

// Existing path: route.target is set → forward.
if (route.target) {
  forwardEvent(route.target, body).catch(err =>
    console.error(`[router] Async forward error:`, err.message));
  return;
}

// New path: unbound channel → maybe notify the sender.
if (route.reason?.startsWith('unbound:')) {
  unboundNotice.notify(route.channelId, route.userId, body)
    .catch(err => console.error(`[router] Ephemeral dispatch error:`, err.message));
  return;
}
```

#### `unbound-notice.js` (new module)

```javascript
// router/slack/src/unbound-notice.js
const RATE_LIMIT_MS = 24 * 60 * 60 * 1000;
const MAX_ENTRIES = 5000;
const rateLimit = new Map(); // key: `${channelId}:${userId}` → expiry ms

async function notify(channelId, userId, payload) {
  if (!channelId || !userId) return;
  if (userId.startsWith('B')) return;              // bot sender
  if (payload?.event?.subtype === 'bot_message') return;

  const key = `${channelId}:${userId}`;
  const now = Date.now();
  const expiry = rateLimit.get(key);
  if (expiry && expiry > now) return;

  // Lazy eviction on write when map is full
  if (rateLimit.size >= MAX_ENTRIES) {
    for (const [k, exp] of rateLimit) {
      if (exp <= now) rateLimit.delete(k);
    }
  }
  rateLimit.set(key, now + RATE_LIMIT_MS);

  const text = `I'm not configured for this channel yet. ` +
    `Ask your Conga admin to run: \`conga channels bind <agent> slack:${channelId}\``;

  await postEphemeral(channelId, userId, text);
}

async function postEphemeral(channel, user, text) {
  const token = process.env.SLACK_BOT_TOKEN;
  if (!token) {
    console.warn('[router] SLACK_BOT_TOKEN not set; skipping ephemeral notice');
    return;
  }
  const res = await fetch('https://slack.com/api/chat.postEphemeral', {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json; charset=utf-8',
      Authorization: `Bearer ${token}`,
    },
    body: JSON.stringify({ channel, user, text }),
  });
  if (!res.ok) {
    console.error(`[router] postEphemeral HTTP ${res.status}`);
    return;
  }
  const body = await res.json().catch(() => null);
  if (body && !body.ok) {
    // Common, non-fatal: not_in_channel, user_not_in_channel, channel_not_found
    console.warn(`[router] postEphemeral not ok: ${body.error}`);
  }
}

module.exports = { notify, _rateLimit: rateLimit }; // _rateLimit exposed for tests
```

**Agent-name privacy**: message uses the literal placeholder `<agent>` — never substitutes a real agent name. This is intentional (see Decisions in README.md).

## 3. Edge Cases & Error Handling

### 3.1 Bind edge cases

| Case | Expected behavior |
|------|-------------------|
| Bind same `(platform, id)` twice | No-op, exit 0, notice on stderr. |
| Bind same `id` with different label | No-op on the binding itself; label from the first bind wins. (Future: separate `conga channels label` command; out of scope here.) |
| Bind to an agent that does not exist | Error: `agent %q not found`, non-zero exit. Unchanged. |
| Bind 1000 channels to one agent | Allowed. Allowlist grows. Performance: openclaw.json regeneration is O(N) over bindings; N < ~100 is realistic, N = 1000 still completes in ms. No artificial cap. |
| Bind during router hot-reload | Router uses `fs.watch`; the reload handler in `router/slack/src/index.js:29-30` falls back to the prior config on parse errors. The current writer at `pkg/provider/localprovider/provider.go:1663` uses `os.WriteFile` (non-atomic), so a partial read is theoretically possible but benign — the next change event triggers another reload. **Pre-existing behavior, not introduced by this spec.** A separate hardening follow-up should convert routing.json writes to atomic rename; out of scope here. |

### 3.2 Unbind edge cases

| Case | Expected behavior |
|------|-------------------|
| Unbind non-existent `(platform, id)` | Error: `agent %q has no slack:C… binding`, non-zero exit. |
| Unbind last remaining binding | `openclaw.json` regenerates without the Slack section. Agent reverts to gateway-only mode (same as a never-bound agent). |
| Unbind without ID when 2+ bindings exist | Validation error with the enumerated list (see 2.5). |
| Unbind without ID when exactly 1 binding exists | Legacy form preserved: removes the sole binding. |
| Unbind the same ID twice | Second call errors per row 1. |

### 3.3 Routing regeneration timing

- `conga channels bind`/`unbind` writes the agent JSON file, then triggers `RefreshAgent()` (or provider equivalent) which regenerates `routing.json` and `openclaw.json`.
- Router picks up `routing.json` via its existing `fs.watch`; there is no additional wiring.
- Container picks up `openclaw.json` via OpenClaw's hot-reload; see CLAUDE.md for `.tmp` file considerations. Already handled by current code.

### 3.4 Unbound-channel ephemeral edge cases

| Case | Expected behavior |
|------|-------------------|
| Bot newly invited to a channel, no messages yet | No ephemeral (we only act on message events). |
| Bot invited; user posts a message | One ephemeral to that user. |
| Same user posts 10 more times | No additional ephemerals for 24h. |
| Different user posts | One ephemeral to the new user. |
| User posts in channel already bound | No ephemeral (route.target is set). |
| Channel becomes bound mid-window | Subsequent messages route normally; rate-limit entries for that channel become irrelevant (never checked again). Old entries age out. |
| Router restarts | Rate-limit map clears. At-most one duplicate ephemeral per user per channel after restart. Acceptable. |
| `chat.postEphemeral` returns `not_in_channel` | Logged at warn; rate-limit entry already set; no retry. Admin must invite the bot properly. |
| DM to the bot (`D…` channel) | Handled by the DM code path; never hits unbound logic. |
| `app_mention` in unbound channel | Dropped at the `eventType === 'app_mention'` check earlier in the handler (line 166). To surface ephemerals on mentions too, the check would need to allow app_mention through `resolveTarget`. **Decision for v1: mentions in unbound channels are silently dropped, matching current behavior.** (Deferred: enabling ephemeral on mention requires small event handler reorder; flag in follow-up.) |
| Private channel (`G…` ID) | Treated identically to public. Bot must be a member to see the event anyway. |
| Multi-party DM (`mpim`) | Out of scope — uses `G…` or `D…` depending on Slack era; if it lands in resolveTarget's channel path and ID doesn't match routing, falls through to user-based routing as today. No ephemeral. |

### 3.5 Provider parity (within congaline)

The bind guard change (2.1) and `UnbindChannel` signature change (2.5) must be applied identically in all three providers. A shared helper in `pkg/provider/` is tempting but historically each provider has its own `channels.go` with subtle file-write semantics (local uses `os.WriteFile`, remote uses SFTP, aws uses SSM). Keep the guard copy-pasted across the three files. Test matrix includes all three.

### 3.6 Terraform provider — coordinated changes (`terraform-provider-conga`)

The Terraform provider repo has tight coupling to the singular-binding assumption and must be updated as part of this feature's coordinated release. Production operators have `.tfstate` files with the old resource identity scheme that must migrate cleanly.

**Files in `terraform-provider-conga`:**

#### `internal/terraform/binding_resource.go`

**Resource identity scheme** (line 129, 181-189):
- **Before**: `plan.ID = agentName + "/" + binding.Platform`
- **After**: `plan.ID = agentName + "/" + binding.Platform + "/" + binding.ID`

**Schema version bump** (new, line 45):
```go
resp.Schema = schema.Schema{
    Version: 1, // was 0
    // ... attributes unchanged
}
```

**State upgrader** (new method required by the framework):
```go
func (r *channelBindingResource) UpgradeState(ctx context.Context) map[int64]resource.StateUpgrader {
    return map[int64]resource.StateUpgrader{
        0: {
            PriorSchema: &schema.Schema{ /* v0 schema, same attributes */ },
            StateUpgrader: func(ctx context.Context, req resource.UpgradeStateRequest, resp *resource.UpgradeStateResponse) {
                var prior channelBindingResourceModel
                resp.Diagnostics.Append(req.State.Get(ctx, &prior)...)
                if resp.Diagnostics.HasError() { return }
                // Recompose ID to the new scheme using binding_id from state.
                prior.ID = types.StringValue(fmt.Sprintf("%s/%s/%s",
                    prior.Agent.ValueString(),
                    prior.Platform.ValueString(),
                    prior.BindingID.ValueString()))
                resp.Diagnostics.Append(resp.State.Set(ctx, prior)...)
            },
        },
    }
}
```

Existing `.tfstate` files with IDs like `acme/slack` are rewritten to `acme/slack/C0123456789` on the next `terraform plan`/`apply` automatically. No user action required.

**Create method** (lines 93-134) — simplifies after `ErrBindingExists` removal:
```go
func (r *channelBindingResource) Create(...) {
    // ... plan extraction unchanged
    if err := r.prov.BindChannel(ctx, agentName, binding); err != nil {
        resp.Diagnostics.AddError("Failed to bind channel", err.Error())
        return
    }
    plan.ID = types.StringValue(fmt.Sprintf("%s/%s/%s",
        agentName, binding.Platform, binding.ID))
    // ... label handling unchanged
    resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}
```

The entire `errors.Is(err, ErrBindingExists)` block (lines 110-127) is deleted. The "Conflicting binding exists" diagnostic is deleted — that condition (different ID for same platform) is now valid.

**Read method** (line 153):
- **Before**: `binding := agent.ChannelBinding(state.Platform.ValueString())` (first match)
- **After**: iterate `agent.ChannelBindings(state.Platform.ValueString())` and find by `state.BindingID.ValueString()`. Return "not found" → `RemoveResource` if no exact match.

**Delete method** (line 176):
- **Before**: `UnbindChannel(ctx, agent, platform)`
- **After**: `UnbindChannel(ctx, agent, platform, state.BindingID.ValueString())`

**ImportState method** (lines 181-189):
- **Before**: expects `agent/platform`, parses 2 parts.
- **After**: expects `agent/platform/id`, parses 3 parts. Sets all three attributes + computed ID.
- **Backward compatibility**: for a deprecation window, also accept the 2-part form and error with a helpful message: `"import format changed to agent/platform/id; derive the ID from your existing binding configuration"`. Not a hard error — let users self-correct.

#### `internal/terraform/helpers_test.go` / other tests

Update any fixture IDs from `agent/platform` to `agent/platform/id`.

#### No data source changes

The provider's data sources (if any read bindings) would need matching updates. Audit at implementation time: `grep -rn "ChannelBinding\b" ~/Development/crux/terraform-provider-conga`.

### 3.7 External SDK compatibility

**Scope of external callers**: only `terraform-provider-conga` imports `pkg/provider/` from this repo (confirmed via release flow in CLAUDE.md; the repo is the sole documented external consumer). No other Go modules, scripts, or CLI wrappers depend on these exports.

**API break summary** (for release notes):
- `Provider.UnbindChannel` signature adds `id string`.
- `ErrBindingExists` removed.
- `ErrAmbiguousUnbind` added.
- `AgentConfig.ChannelBindings(platform)` added (additive).
- `channels.MultiBindingChannel` interface added (additive).

Per SemVer, the `UnbindChannel` signature change and `ErrBindingExists` removal are breaking. The congaline module tag bumps the minor version (pre-1.0) or major version (post-1.0) accordingly. Terraform provider release is gated on the congaline tag being published.

### 3.8 Backward compatibility and upgrade path

| Operator scenario | Experience |
|-------------------|------------|
| Single-binding user, CLI only | `conga` upgrade: zero observable change. Bindings, routing, policy all byte-identical. |
| Single-binding user, Terraform | `terraform apply` after provider upgrade: state upgrader rewrites resource ID from `acme/slack` to `acme/slack/C…`; no diff on resource attributes; no actual bind/unbind action. |
| Multi-binding desired | Add more `conga_channel_binding` resources or additional `conga channels bind` calls. Each gets its own terraform state entry keyed on the new ID scheme. |
| Scripted `terraform import` using old format | Command still works for a deprecation window; emits a warning suggesting the new format. |
| Operator skips provider upgrade | CLI multi-binding still works; Terraform-managed bindings remain limited to one per platform until upgrade. No hybrid breakage. |

## 4. Test Plan

### 4.1 Unit tests

- `pkg/provider/provider_test.go`:
  - `TestChannelBindings_ReturnsAllMatches`
  - `TestChannelBindings_EmptyWhenNone`
  - `TestChannelBindings_PreservesInsertionOrder`
  - `TestChannelBinding_UnchangedForSingle`
- `pkg/provider/{local,remote,aws}provider/channels_test.go` (three files):
  - `TestBindChannel_MultipleSlackChannels_Succeeds`
  - `TestBindChannel_ExactDuplicate_Idempotent`
  - `TestBindChannel_ExactDuplicate_DifferentLabel_Errors` — label mismatch surfaces explicit error.
  - `TestBindChannel_ExactDuplicate_EmptyLabel_NoOp` — caller supplies no label, existing label preserved, returns nil.
  - `TestBindChannel_CrossAgentCollision_Errors` — same `(platform, id)` on a different agent fails with `"already bound to agent <name>"`.
  - `TestBindChannel_DifferentPlatforms_Succeeds` (regression)
  - `TestUnbindChannel_NoID_SingleBinding_Succeeds` — legacy path.
  - `TestUnbindChannel_NoID_MultipleBindings_ErrAmbiguous` — returns `ErrAmbiguousUnbind`.
  - `TestUnbindChannel_WithID_RemovesOnlyThatBinding`.
- `pkg/common/routing_test.go`:
  - `TestGenerateRoutingJSON_TeamAgentSingleChannel_ByteIdentical` (snapshot)
  - `TestGenerateRoutingJSON_TeamAgentMultipleChannels`
  - `TestGenerateRoutingJSON_TeamAgentMixedWithOtherAgents`
- `pkg/channels/slack/slack_test.go`:
  - `TestOpenClawChannelConfigMulti_SingleBinding_MatchesSingular` (snapshot)
  - `TestOpenClawChannelConfigMulti_MultipleBindings`
  - `TestOpenClawChannelConfigMulti_Empty_ReturnsNil`
  - Compile-time: `var _ channels.MultiBindingChannel = (*Slack)(nil)`
- `pkg/runtime/openclaw/config_test.go`:
  - `TestOpenClawConfig_TeamAgentMultiBinding_AllowlistIncludesAll`
  - `TestOpenClawConfig_TeamAgentSingleBinding_Unchanged` (snapshot)

### 4.2 Router tests

- `router/slack/src/unbound-notice.test.js`:
  - First call for key → `chat.postEphemeral` invoked once.
  - Second call within 24h → not invoked.
  - Different user, same channel → invoked.
  - After simulated 24h+ → invoked again.
  - Missing `userId` → skipped silently.
  - `B…` user → skipped.
  - `bot_message` subtype → skipped.
  - Map eviction: fill to 5000, add one more → expired entries pruned, new entry accepted.
  - `chat.postEphemeral` returns `not_in_channel` → warn logged, rate-limit entry still set, no retry on next event.
  - `chat.postEphemeral` returns `missing_scope` → warn logged, rate-limit entry still set.
- `router/slack/src/index.test.js` (if present; else add):
  - `resolveTarget` returns `unbound:*` for channel not in routing map.
  - `resolveTarget` prefers bound mapping over unbound path.
  - Event handler calls `unboundNotice.notify` on unbound path.

### 4.3 CLI tests

- `internal/cmd/channels_test.go` (or parity test file):
  - Bind same binding twice → second call exits 0 with idempotent notice.
  - Bind N different Slack channels to same agent → all N persisted.
  - Unbind without ID when multiple → validation error with enumerated list.
  - Unbind specific ID → only that one removed.
  - `channels list` output contains one row per binding, sorted as specified.

### 4.4 MCP tool tests

- `internal/mcpserver/tools_channels_test.go`:
  - Same assertions as CLI, via MCP tool invocation.
  - `conga_channels_list` JSON shape: array of `{agent, platform, id, label}`.

### 4.5 Integration

- `test/integration/local/multi_channel_test.go` (new):
  1. Provision a team agent.
  2. Bind three synthetic Slack channel IDs.
  3. Read `routing.json` and `openclaw.json`; assert shape.
  4. Unbind one channel with the new ID-aware signature; re-read; assert updated shape.
  5. Attempt unbind with empty ID when 2 bindings remain → expect `ErrAmbiguousUnbind`.
  6. Unbind all remaining; assert Slack section absent from `openclaw.json`.
- Remote and AWS: add equivalent cases to the existing provider integration harness. For AWS, the SSM round-trip is the same as today's single-binding path.

### 4.6 Terraform provider tests (`terraform-provider-conga`)

- `internal/terraform/binding_resource_test.go`:
  - `TestChannelBinding_Create_SingleBinding` — regression; resource ID matches new format.
  - `TestChannelBinding_Create_MultipleBindings_SameAgentPlatform` — two HCL resources for same agent+platform with different IDs both succeed, distinct state entries.
  - `TestChannelBinding_Read_MultipleBindings_FindsCorrectID` — Read method scans all platform bindings and returns the right one.
  - `TestChannelBinding_Delete_WithID_RemovesOnlyThatBinding` — unaffected bindings remain.
  - `TestChannelBinding_Import_NewFormat` — `agent/platform/id`.
  - `TestChannelBinding_Import_OldFormat_Deprecated` — warns, still works.
- `internal/terraform/state_upgrader_test.go` (new):
  - `TestStateUpgrader_V0ToV1_Succeeds` — v0 state with ID `acme/slack` migrates to v1 state with ID `acme/slack/C0123456789` given v0 state's `binding_id` attribute.
  - `TestStateUpgrader_V0_MissingBindingID_ErrorsHelpfully` — v0 state that somehow lacks `binding_id` fails with a clear diagnostic pointing the operator at `terraform state rm <resource> && terraform import <resource> <agent>/<platform>/<id>`.
  - `TestStateUpgrader_AlreadyV1_NoOp` — idempotent when state is already current.

### 4.6 Manual E2E

Executed once before PR merges:

1. Provision a fresh team agent.
2. Bind three real Slack channels the bot is a member of.
3. Post in each channel; verify same container handles all three (`conga logs --agent <name>` shows three channel IDs as origins).
4. Invite the bot to an unbound channel; post a message; verify ephemeral appears to the sender; verify the channel ID in the suggested command matches the invited channel.
5. Post again within five minutes; verify no second ephemeral.
6. Another team member posts; verify they get an ephemeral (different user, same channel).
7. Unbind one of the three originally-bound channels; verify routing stops within ~5s (fs.watch reload); verify openclaw.json allowlist updated.
8. Run `conga channels list`; verify table output matches spec 2.5.

## 5. Data Safety

Per `product-knowledge/standards/architecture.md` § Agent Data Safety (severity: must), this section explicitly confirms the spec's data impact.

**Data paths (per provider):**

| Provider | Agent Data Path | Touched by this spec? |
|---|---|---|
| AWS | `/opt/conga/data/<name>/` (dedicated encrypted EBS volume) | **No** |
| Remote | `/opt/conga/data/<name>/` | **No** |
| Local | `~/.conga/data/<name>/` | **No** |

**Config paths (per provider):**

| Provider | Agent Config Path | Modified? |
|---|---|---|
| AWS | SSM parameter `/conga/agents/<name>` + `/opt/conga/config/<name>.env` | **Yes** (adds `channels` entries to agent JSON; regenerates env file on channel change, unchanged schema) |
| Remote | `/opt/conga/agents/<name>.json` + `/opt/conga/config/<name>.env` | **Yes** (same) |
| Local | `~/.conga/agents/<name>.json` + `~/.conga/config/<name>.env` | **Yes** (same) |

**Routing / policy paths:**

| Path | Modified? |
|---|---|
| `routing.json` | **Yes** (additional `channels[id]` entries for multi-bound agents) |
| `openclaw.json` (per agent) | **Yes** (allowlist and `channels` map expand with additional IDs) |

**Claims:**

1. **No agent data directory is read, written, deleted, or re-created by any code path in this spec.** Bind/unbind operations modify agent *config* JSON, regenerate routing/openclaw config, and reload the router. They never touch `~/.conga/data/<agent>/` or its AWS/remote equivalents. This matches the standard's rule 3 ("Refresh operations rebuild config, not data").
2. **No volume-mount change.** Container `-v` flags (for the data path) are unchanged. The `RefreshAgent` code path this spec extends already only rewrites config files and restarts containers with the same mounts.
3. **Teardown unchanged.** This spec does not touch `admin teardown` or its `--delete-data` flag. Existing data-preservation semantics hold.
4. **Persistence across bind/unbind.** Adding or removing a channel binding triggers `RefreshAgent`, which restarts the agent container. Container restart preserves the data volume mount. An agent accumulates memory from `slack:C1`, is additionally bound to `slack:C2`, the resulting restart preserves the `C1`-era memory, and the agent begins handling `C2` messages against that same memory store.

**Integration test coverage** (already in §4.5, reaffirmed here for this standard): the local integration test provisions a team agent, binds 3 channels, regenerates config, unbinds channels — no step of that flow references the agent data directory. A dedicated assertion should be added to the integration test: `stat` the data directory before and after the bind/unbind operations and confirm unchanged `mtime` on the directory itself (content `mtime` changes are acceptable — containers may write memory during the test).

## 6. Observability & Logging

- CLI: existing log lines sufficient. Bind/unbind already logs to the agent-config provider's write path.
- **Adoption signal** (new, required): every time `routing.json` is regenerated, emit a structured log line at INFO if any agent has >1 binding on any single platform:
  ```
  [router-config] multi-binding agent: name=<agent> platform=slack bindings=<N> channel_ids=[<id>,<id>,...]
  ```
  This is the operational proxy for feature adoption. Operators grepping their logs can see which agents are using multi-binding without requiring a metrics system. Emitted from the routing-regen code path in each provider after `GenerateRoutingJSON` returns. The line does not include agent secrets or message content.
- Router log lines:
  - `[router] forward ${channel} → ${target}` (existing, unchanged)
  - `[router] unbound channel ${channelId} from user ${userId} → ephemeral sent` (new, on first send within rate-limit window)
  - `[router] unbound channel ${channelId} from user ${userId} → suppressed (rate-limited)` at debug level only (avoid log spam)
  - `[router] postEphemeral ${error}` at warn when Slack returns a non-fatal error (`not_in_channel`, `user_not_in_channel`, etc.). Rate-limit entry is still set — do not retry.
- No new Prometheus-style metrics. If/when metrics are added project-wide, candidate counters: `unbound_channel_events_total`, `ephemeral_sent_total`, `ephemeral_suppressed_total`, `multi_binding_agents_gauge`.

## 7. Security Considerations

- **Agent-name privacy**: ephemeral message uses `<agent>` placeholder; no internal agent names leak to channel members.
- **No change to the bind access-control model**: bindings remain admin-gated. The ephemeral notice explicitly instructs the user to route the request through an admin, not self-serve.
- **Slack token scope**: `chat:write` is required for `chat.postEphemeral`; already in the recommended scope list per CLAUDE.md. No new scope asks.
- **Rate-limit memory**: in-memory map bounded at 5000 entries with lazy eviction. Worst-case memory ≈ 5000 × (~50 bytes key + number) ≈ sub-megabyte. No amplification vector (keys are user-supplied channel/user IDs, bounded by Slack's ID length).
- **No new egress endpoints**: `chat.postEphemeral` uses the same `slack.com/api/*` surface already exercised by the router. Existing egress allowlist covers it.

## 8. Rollout

Coordinated across two repos: `congaline` and `terraform-provider-conga`.

### 7.1 Sequence

1. **Merge congaline PR.** All phases together in `congaline/main`. Single PR.
2. **Tag congaline.** Pick the version per SemVer (minor bump pre-1.0, major post-1.0). This is the required `go get` target for the provider.
3. **Update terraform-provider-conga.** Separate PR in the provider repo:
   - `go get github.com/cruxdigital-llc/conga-line@<new-tag> && go mod tidy`
   - Apply the code changes detailed in §3.6.
   - Schema version bump + state upgrader.
   - Tests updated.
4. **Merge terraform-provider-conga PR; tag it.** GoReleaser publishes to the Terraform Registry.
5. **Delete local plugin cache on operator machines before upgrading.** Per CLAUDE.md § Terraform Provider: `rm -rf ~/.terraform.d/plugins/registry.terraform.io/cruxdigital-llc/conga/` then `terraform init -upgrade`. Without this, Terraform serves the stale cached provider and the state upgrader never runs.
6. **Operator guide updates.** Out of spec scope; track as a follow-up doc issue.

### 7.2 Release notes must cover

- Breaking API changes (§3.7 summary).
- State migration is automatic; no manual action required for standard single-binding users.
- Import format change (`agent/platform` → `agent/platform/id`); old format accepted with warning for one minor version.
- Multi-binding behavior: same `conga_channel_binding` HCL resource, used multiple times with different `binding_id` values for the same agent/platform, now allowed.

### 7.3 Rollback plan

If an operator must roll back after upgrading:
- Terraform provider: pin the previous version in `required_providers` and run `terraform init -upgrade`. The state file now has schema version 1, but a v0 provider will fail to read it with a clear schema mismatch error — state cannot roll back cleanly. **Operators who added multi-binding must remove those extra bindings via HCL first, then roll back.** Document this prominently.
- Congaline: downgrading only affects the CLI/MCP/router; agents that already have multi-bindings persist on disk (local) or in SSM (aws) as JSON. An older congaline will ignore the extra entries for some operations (`ChannelBinding` returns the first) — not destructive but not functional either. Clean rollback requires `conga channels unbind` for each extra binding before downgrading.

This is a one-way door for customers who actually use multi-binding. Call that out in the release notes.

## 9. Open Questions (none blocking)

- **Phase 2 follow-up**: enabling unbound ephemerals on `app_mention` events. Current spec: not handled (mention events short-circuit before `resolveTarget`). Reasonable extension if operator feedback indicates confusion when users @-mention the bot in an unbound channel.
- **Telegram parity**: this spec only touches Slack. Telegram channels (if they support multi-binding semantics — typically they do not, single chat ID per agent is the norm) can opt in later by implementing `MultiBindingChannel`. No blocking dependency.
- **Resolved**: `ErrBindingExists` is removed (verified external usage in `terraform-provider-conga/internal/terraform/binding_resource.go`; that code path is deleted as part of the coordinated provider update — see §3.6).
- **Admin-discovery UX for unbound-channel ephemeral**: current v1 uses a generic `<agent>` placeholder with no admin-discovery hint. Operators inviting the bot must already know which admin owns Conga. Acceptable for v1; revisit if operator feedback indicates confusion. Possible v2 iterations: (a) mention a configurable admin Slack channel env var, (b) list known agent names (reverses the privacy decision).
- **SSM concurrent-write protection for AWS provider**: the AWS provider writes agent JSON via SSM parameters without optimistic-concurrency checks. Two simultaneous `conga channels bind` calls against the same agent could last-write-wins. Preexisting, not introduced by this spec, but amplified by the cross-agent uniqueness check (which reads all agents before writing). Tracked as a separate hardening issue.
- **Atomic write for `routing.json`**: `pkg/provider/localprovider/provider.go:1663` uses `os.WriteFile` directly. The secrets writer at `pkg/provider/localprovider/secrets.go:26` uses the atomic temp+rename pattern — `routing.json` should match. Preexisting issue; hardening follow-up.

## 10. Coordinated-change Summary

Quick reference for the reviewer:

**In `congaline`:**
- `pkg/provider/provider.go` — `AgentConfig.ChannelBindings(platform)` added; `UnbindChannel` interface signature gains `id string`.
- `pkg/provider/errors.go` — `ErrBindingExists` removed; `ErrAmbiguousUnbind` added.
- `pkg/provider/errors_test.go` — deleted.
- `pkg/provider/{local,remote,aws}provider/channels.go` — bind guard → idempotent-on-exact-match; `UnbindChannel` takes ID; find-by-(platform, id) internally.
- `pkg/channels/channels.go` — `MultiBindingChannel` interface added.
- `pkg/channels/slack/slack.go` — implements `MultiBindingChannel`; singular method becomes a wrapper.
- `pkg/common/routing.go:62-71` — loops over all Slack bindings.
- `pkg/runtime/openclaw/config.go` — call-site type-asserts for `MultiBindingChannel`.
- `internal/cmd/channels.go` — bind/unbind/list CLI updated per §2.6.
- `internal/mcpserver/tools_channels.go` — MCP parity.
- `internal/mcpserver/server_test.go` — mock signature updated.
- `router/slack/src/index.js` — `resolveTarget` extended; event handler dispatches unbound notice.
- `router/slack/src/unbound-notice.js` (new).
- Tests throughout.

**In `terraform-provider-conga`:**
- `go.mod` — congaline tag bumped.
- `internal/terraform/binding_resource.go` — resource ID scheme → `agent/platform/id`; Schema version 1; state upgrader; `Create` simplified; `Read` iterates bindings; `Delete` passes ID; `ImportState` handles 3-part format with legacy fallback.
- `internal/terraform/binding_resource_test.go` — extended cases.
- `internal/terraform/state_upgrader_test.go` (new).

**Release order:** congaline tag → terraform-provider-conga PR → terraform-provider-conga tag. Operator upgrade flow documented in release notes per §7.2.
