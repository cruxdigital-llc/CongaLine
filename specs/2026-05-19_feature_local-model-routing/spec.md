# Spec: local-model-routing

**Goal**: Per-agent model override via `behavior/agents/<name>/agent.yaml`, provider-agnostic across AWS/local/remote, with the `aaron`-to-Spark-Qwen flow as the first production use case. Detailed implementation contract.

**Prerequisite**: Phase 0 spike findings in `spike-openclaw-providers.md`. Pin bumps to `v2026.5.18`. Provider path is **Ollama native** (not OpenAI-compatible) for the Spark case.

## Data model

### New file: `behavior/agents/<name>/agent.yaml`

Operator-authored YAML. Lives next to `SOUL.md` / `AGENTS.md` / `USER.md` in the per-agent overlay directory. Gitignored at the directory level (existing `.gitignore` rule; only `_example/` is committed).

#### Top-level schema (v1)

```yaml
version: 1                    # required; current schema version
model:
  provider: ollama            # required if `model:` block is present
  name: qwen3:6b              # required when `provider` is set
  base_url: http://192.168.181.97:11434  # required for self-hosted providers
```

#### Schema versioning contract

`version` is **required** and gates how the loader interprets the rest of the document.

- `version: 1` — current. The loader understands this version.
- Missing `version` — accepted as `version: 1` for backwards-compatibility during onboarding. The loader emits a one-time warning at refresh: *"agent.yaml missing `version:` key; assumed 1. Add `version: 1` to silence this warning."*
- `version: 2+` (or any non-1 value) — **hard failure** with the message: *"agent.yaml requires conga >= <minimum-version> to read schema version <N>; this binary is <current-version>."* This guarantees that a config authored for a future binary refuses to load on an older one rather than partially parsing.

When this feature evolves to require schema changes (e.g. Bifrost-era fallback chains), we bump to `version: 2` and ship a loader that handles both 1 and 2 explicitly. **Never** silently accept a higher version.

#### Strict key parsing

The loader uses `yaml.Decoder` with `KnownFields(true)`. Unknown top-level keys (or unknown keys inside `model:`) cause a hard parse error.

Rationale: this file controls security-relevant behavior (which model the agent talks to, on which endpoint). A typo like `bare_url:` instead of `base_url:` should fail loudly, not silently fall back to the Anthropic default. The trade-off is that adding a new top-level key is a deliberate, breaking change for any existing config that uses it — but for a file that explicitly carries `version:`, that's the right trade.

#### Reserved keyspace (forward-compatibility)

The following top-level keys are **reserved** for future versions and **MUST NOT** be set in `version: 1` documents (they will trigger the strict-key parse failure):

| Reserved key | Anticipated content | Tracked by |
|---|---|---|
| `model.fallbacks` | Fallback chain — `[provider/name, provider/name, ...]` | Bifrost / Model Routing (ROADMAP #22) |
| `memory` | Per-agent memory backend (provider, endpoint, retention) | Foreseeable based on `concepts/models.md` upstream |
| `tools` | Per-agent tool/MCP allowlist | Future — no spec yet |
| `limits` | Token budgets, daily caps, max-turn-tokens | Future — no spec yet |
| `images` / `pdf` / `video` | Per-agent multi-modal model refs | OpenClaw supports these upstream |

Adding any of these in a future version means:
1. Bump schema `version`.
2. Update the loader to recognize the new top-level key under that version.
3. Add a `pkg/runtime/<rt>/apply<Concern>Overlay` function that translates it.
4. Document the migration path from `version: N-1`.

This list is **forward-looking guidance, not a commitment**. The reserved-keys mechanism only forbids accidental misuse; it doesn't require any of these features to exist.

Forward-compatibility note: top-level keys other than `version:` and `model:` are reserved for future versions and rejected today by strict-key parsing.

#### Field semantics

| Field | Type | Required | Validation | Notes |
|---|---|---|---|---|
| `model.provider` | string | when `model:` is present | enum: `ollama`, `openai` | Determines which `models.providers.<id>` block the generator writes |
| `model.name` | string | when `model.provider` is set | non-empty after trim | Exact model tag — e.g. `qwen3:6b` for Ollama (output of `ollama list`), `gpt-5.5` or `qwen-2.5-72b-instruct` for OpenAI-compatible |
| `model.base_url` | string | for `ollama` (LAN) and `openai` (non-default endpoints) | parseable URL with non-empty scheme + host | **Ollama: NO `/v1` suffix.** OpenAI-compatible: include `/v1` per the provider's docs. The generator does NOT auto-append or strip `/v1` |

#### Provider matrix (v1)

| Overlay `provider` | OpenClaw provider id | Required secret | `base_url` shape |
|---|---|---|---|
| `ollama` | `ollama` | none (LAN sentinel `ollama-local`) | `http://host:port` — no `/v1` |
| `openai` | `openai` | `openai-api-key` per-agent secret | `https://api.openai.com/v1` or `http://host:port/v1` |

Unknown `provider` values are rejected at overlay load with a clear error.

### Go type additions

**Type definition**: `pkg/runtime/overlay.go` (new file; defined here to avoid a `common → runtime` import cycle, since `pkg/runtime` is the leaf package that everyone imports):

```go
package runtime

type AgentOverlay struct {
    Version int           `yaml:"version"`
    Model   *ModelOverlay `yaml:"model,omitempty"`
}

type ModelOverlay struct {
    Provider string `yaml:"provider"`
    Name     string `yaml:"name"`
    BaseURL  string `yaml:"base_url,omitempty"`
}
```

**Loader**: `pkg/common/overlay_agent.go` (new file) returns `*runtime.AgentOverlay`.

**`pkg/runtime/runtime.go`** — extend `ConfigParams`:

```go
type ConfigParams struct {
    Agent        provider.AgentConfig
    Secrets      provider.SharedSecrets
    GatewayToken string
    Model        string         // EXISTING — currently consumed by Hermes (pkg/runtime/hermes/config.go); preserved
    Overlay      *AgentOverlay  // NEW — provider-agnostic per-agent overlay
}
```

**Hermes `params.Model` field is intentionally preserved.** `pkg/runtime/hermes/config.go:50` reads `params.Model` to set the YAML `model:` key in Hermes's config. It is NOT dead code. A future spec can migrate Hermes to consume `params.Overlay` and deprecate `params.Model`, but that migration is out of scope here. This spec only wires `params.Overlay` for OpenClaw.

## API / Interface contracts

### Overlay loader

New exported function in `pkg/common/`:

```go
// LoadAgentOverlay reads behavior/agents/<agent>/agent.yaml if present.
// Returns (nil, nil) when the file does not exist (not an error).
// Returns (nil, err) when the file exists but parsing or validation fails.
func LoadAgentOverlay(behaviorDir string, agent provider.AgentConfig) (*runtime.AgentOverlay, error)
```

Behavior:
1. Resolve path: `filepath.Join(behaviorDir, "agents", agent.Name, "agent.yaml")`.
2. `os.ReadFile`. On `os.IsNotExist` → return `(nil, nil)`. On any other error → wrap and return.
3. Construct a `yaml.NewDecoder(bytes.NewReader(data))` and call `dec.KnownFields(true)` (**strict-key parsing**). Decode into `runtime.AgentOverlay`.
   - On parse error (including unknown-key error) → wrap with file path and the raw decoder message. Unknown keys produce a message like `"yaml: unmarshal errors: line 4: field bare_url not found in type runtime.ModelOverlay"` which already names the typo.
4. Validate (see Validation Rules above). On failure → wrap with file path.
5. Return the populated overlay.

**Why strict-key parsing**: this file controls security-relevant behavior. A typo (`bare_url:` instead of `base_url:`) must fail loudly, not silently fall through to the Anthropic default. The `version:` mechanism makes future schema extensions deliberate, so strict-key has no compatibility cost.

The function is called once per agent per refresh cycle, alongside `resolveBehaviorFiles`. Callers pass the result into `ConfigParams.Overlay`.

### Validation rules

Implemented in `pkg/runtime/overlay.go` `(*AgentOverlay).Validate()`:

0. **Schema version**: `Version` must be `1`. Empty `Version` is accepted as `1` (with the one-time warning above). Any non-1 value is rejected with the "binary too old" message. Validate this **before** validating any inner block.
1. If `Overlay == nil` → no-op, return nil. (Allows `LoadAgentOverlay` to call `Validate` unconditionally.)
2. If `Overlay.Model != nil`:
   - `Provider` must be one of `{"ollama", "openai"}`. Reject empty or unknown.
   - `Name` must be non-empty after `strings.TrimSpace`.
   - If `BaseURL` is set: `url.Parse` must succeed AND the result must have a non-empty `Scheme` ∈ `{"http", "https"}` AND a non-empty `Host`.
   - **`ollama` AND `BaseURL` ends with `/v1`** → reject with the message: `"ollama provider requires baseUrl without /v1 suffix; the OpenAI-compatible endpoint breaks tool calling (see openclaw docs/providers/ollama.md)"`.
   - **`ollama` AND `BaseURL` is empty** → reject with `"ollama provider requires base_url"`. (We do not fall back to `127.0.0.1:11434` because the agent runs inside a container with no Ollama daemon.)
   - **`openai` AND `BaseURL` set AND not ending in `/v1`** → soft warning logged at refresh time, not a hard reject. Some OpenAI-compatible servers expose `/openai/v1` or similar paths.

3. Future providers extend the enum; unknown values stay rejected.
4. **Provider strings are case-sensitive lowercase.** `Ollama` or `OPENAI` are rejected as unknown providers. The error message names the canonical form (`ollama`, `openai`) so operators can fix the typo without consulting docs.

### Runtime config generator (`pkg/runtime/openclaw/config.go`)

Extend `GenerateConfig` to honor `params.Overlay` when non-nil:

```go
func (r *Runtime) GenerateConfig(params runtime.ConfigParams) ([]byte, error) {
    var config map[string]any
    if err := json.Unmarshal(openclawDefaults, &config); err != nil {
        return nil, fmt.Errorf("failed to parse openclaw-defaults.json: %w", err)
    }

    // EXISTING gateway/channels/plugins logic unchanged...

    if params.Overlay != nil && params.Overlay.Model != nil {
        if err := applyModelOverlay(config, params.Overlay.Model); err != nil {
            return nil, fmt.Errorf("apply model overlay: %w", err)
        }
    }

    return json.MarshalIndent(config, "", "  ")
}
```

`applyModelOverlay` (new private function in `pkg/runtime/openclaw/config.go`):

1. Compute the model ref: `modelRef := m.Provider + "/" + m.Name` (e.g. `"ollama/qwen3:6b"`).
2. Walk `config["agents"]["defaults"]` (created by defaults JSON). Replace `model.primary` with `modelRef`. Clear `model.fallbacks` to `[]any{}` (overlay'd agents shouldn't fall back to Anthropic; explicit fallback support is a future feature).
3. Replace the `models` allowlist entirely: `{modelRef: {}}` — removes the hardcoded `anthropic/claude-opus-4-6` entry.
4. Create or merge `config["models"]["providers"][m.Provider]`:
   - For `ollama`: `{"baseUrl": m.BaseURL, "apiKey": "ollama-local", "api": "ollama"}`.
   - For `openai`: `{"baseUrl": m.BaseURL}`. The `apiKey` reference is via env (`OPENAI_API_KEY`); we do NOT write the key value into the JSON config (which would write it to disk — see CLAUDE.md note on issue #9627). The env path is the existing `openai-api-key` secret → `OPENAI_API_KEY` env var.
5. Return mutated config.

### No changes to

- `pkg/runtime/openclaw/env.go` — `openai-api-key` (or any other `<provider>-api-key`) already flows via `SecretNameToEnvVar`.
- `pkg/runtime/openclaw/openclaw-defaults.json` — kept as the Anthropic default. The overlay's job is to *replace* keys, not to bake new defaults.
- `pkg/policy/` — egress allowlist already accepts IP literals (the running `terraform.tfvars` proves this with `192.168.181.97`).
- `terraform/modules/` — no new variables.
- `terraform-provider-conga` — no new attributes; `go get` bump for the `pkg/` changes per existing release flow.
- `internal/cmd/` — no new commands. Operators edit `agent.yaml` directly, identical to editing `SOUL.md`.

### Caller wiring

Three call sites construct `ConfigParams` for OpenClaw. Each must populate `Overlay`:

| Provider | Call site (current) | Change |
|---|---|---|
| Local | `pkg/provider/localprovider/provider.go` `RefreshAgent` (or equivalent) | Call `common.LoadAgentOverlay(behaviorDir, agent)` before `runtime.GenerateConfig`; pass result via `ConfigParams.Overlay`. |
| Remote | `pkg/provider/remoteprovider/provider.go` `RefreshAgent` | Same. `behaviorDir` is the local repo path that gets SFTP-pushed. |
| AWS | `pkg/provider/awsprovider/provider.go` `RefreshAgent` AND the bootstrap-time discovery path in `terraform/modules/infrastructure/user-data.sh.tftpl` | The Go side (RefreshAgent) follows the same pattern. The bash bootstrap reads behavior files from S3 (`deploy-behavior.sh`); it must be extended to also pull `agent.yaml` and pass it to the config generator via a CLI flag or env var. |

The AWS bootstrap wiring is the most invasive piece because of the shell layer. **Sub-decision**: rather than parsing YAML in bash, have the Go side render the full `openclaw.json` server-side at refresh time and ship the final JSON via SSM/S3, so the bootstrap shell only needs to drop the file. This matches how `openclaw.json` is already produced for AWS today.

## Edge cases & failure modes

| Scenario | Behavior |
|---|---|
| `agent.yaml` missing | No-op. Identical to today's behavior. (`LoadAgentOverlay` returns `(nil, nil)`.) |
| `agent.yaml` is empty file | Parses to `AgentOverlay{Version: 0, Model: nil}`. Version 0 is accepted as 1 with a one-time warning; Model nil = no-op. Net result: no overlay applied, warning logged. |
| `agent.yaml` is malformed YAML | Hard error at refresh. Message includes file path and YAML parser error. Container is not restarted; the prior good config remains in place. |
| `agent.yaml` has unknown top-level key (e.g. `memry:` typo) | Hard error from `KnownFields(true)`. Decoder message names the bad key and line number. |
| `agent.yaml` has unknown inner key (e.g. `model.bare_url`) | Hard error from `KnownFields(true)`. Decoder message names the bad key, surfacing the typo immediately. |
| `agent.yaml` declares `version: 2` (or any non-1) | Hard error: `"agent.yaml version 2 requires a newer conga binary; this binary supports version 1 only"`. |
| `agent.yaml` omits `version:` entirely | Accepted as version 1, with a one-time warning at refresh: *"agent.yaml missing `version:` key; assumed 1. Add `version: 1` to silence this warning."* |
| `provider:` unknown (e.g. `azure`) | Hard error: `"unknown model provider \"azure\" in <path>: supported: ollama, openai"`. |
| `provider: ollama` + `base_url` ends with `/v1` | Hard error per validation rule. Message cites OpenClaw docs URL. |
| `provider: openai` + `base_url` missing | Allowed (defaults to OpenAI's hosted API at `api.openai.com/v1`). |
| `provider: openai` + no `openai-api-key` secret | Container starts, hits upstream auth failure, error surfaces in `conga logs`. Not a refresh-time gate (would require coupling overlay loader to secret store). Surfaced in operator docs. |
| `provider: openai` + `base_url` set but doesn't end in `/v1` | Soft warning logged at refresh time. Some OpenAI-compatible servers use non-standard paths; warn but allow. |
| Egress not allowed to `base_url` host | Container starts, network call fails, Envoy proxy logs show the deny. Operator must add the host/IP to `egress_allowed_domains` in tfvars (or `~/.conga/conga-policy.yaml` for local/remote). Validated separately by `conga policy validate` if the host is added. |
| `agent.yaml` exists for an agent that doesn't exist as an `AgentConfig` | Cannot happen via normal flow — overlay is only loaded when an agent is being refreshed. Defensive: if `LoadAgentOverlay` is called for an unknown agent, the file read returns "no such file or directory" which we already handle as no-op. |
| Two refreshes race | `GenerateConfig` is stateless; both produce the same JSON. The Docker container is restarted at most once per refresh — last writer wins on the config file. Existing concurrency model unchanged. |
| Spark unreachable at container start (VPN down) | Container starts, model preflight (if any) fails, agent surfaces error on first turn. Self-healing — when VPN recovers, next turn succeeds. (OpenClaw's cron-skip preflight at `docs/providers/ollama.md` applies for cron jobs; chat turns surface the error.) |

## Tests

### Unit tests

#### `pkg/common/overlay_agent_test.go` (new)
1. `LoadAgentOverlay` — file missing → returns `(nil, nil)`.
2. `LoadAgentOverlay` — empty file → returns `(&AgentOverlay{Version: 0}, nil)` and emits the missing-version warning exactly once.
3. `LoadAgentOverlay` — minimal valid `version: 1` Ollama overlay → returns populated struct.
4. `LoadAgentOverlay` — minimal valid `version: 1` OpenAI overlay → returns populated struct.
5. `LoadAgentOverlay` — `version: 2` → returns error matching `/version 2 requires a newer conga binary/`.
6. `LoadAgentOverlay` — missing `version:` key with other content → succeeds, model populated, missing-version warning emitted once.
7. `LoadAgentOverlay` — unknown top-level key (e.g. `tools:` or typo `mdoel:`) → returns error citing the line number and key name.
8. `LoadAgentOverlay` — unknown inner key (`model.bare_url`) → returns error citing the typo.
9. `LoadAgentOverlay` — malformed YAML → returns error wrapping file path.
10. `LoadAgentOverlay` — unknown `provider:` → returns error citing supported set.
11. `LoadAgentOverlay` — Ollama with `/v1` in `base_url` → returns error citing the tool-calling regression.
12. `LoadAgentOverlay` — Ollama with empty `base_url` → returns error.
13. `LoadAgentOverlay` — `base_url` with no scheme → returns error.
14. `LoadAgentOverlay` — OpenAI with non-`/v1` `base_url` → succeeds (warning logged via test capture).
15. `LoadAgentOverlay` — provider casing mismatch (`Ollama`) → returns error naming canonical `ollama`.

#### `pkg/runtime/overlay_test.go` (new)
- Same validation rules, but unit-tested at the `Validate()` level for the type itself (independent of file I/O).

#### `pkg/runtime/openclaw/config_test.go` (extend)
1. **Golden file (no overlay)**: `GenerateConfig` output with `params.Overlay == nil` is byte-identical to the existing committed golden (no regression for default agents).
2. **Golden file (Ollama overlay)**: explicit Ollama overlay produces:
   - `agents.defaults.model.primary` = `"ollama/qwen3:6b"`
   - `agents.defaults.model.fallbacks` = `[]`
   - `agents.defaults.models` = `{"ollama/qwen3:6b": {}}`
   - `models.providers.ollama` = `{"baseUrl": "http://192.168.181.97:11434", "apiKey": "ollama-local", "api": "ollama"}`
   - All other keys (`workspace`, `heartbeat`, `contextPruning`, etc.) preserved from defaults.
3. **Golden file (OpenAI overlay with self-hosted endpoint)**: produces `models.providers.openai = {"baseUrl": "http://192.168.181.97:8000/v1"}` and no API key written to the JSON.
4. **Channels coexistence**: Overlay + Slack channel together → both sections appear correctly.

### Integration tests

Add to existing CLI integration test harness (see `specs/2026-04-07_feature_cli-integration-tests/`):

1. **Local provider**: provision agent with `behavior/agents/<test-agent>/agent.yaml` declaring an Ollama overlay against a mock HTTP server. Verify `openclaw.json` inside the container reflects the overlay. Verify the container's first outbound HTTP request goes to the mock, not to `api.anthropic.com`.
   - **Mock requirements**: stub `/api/chat` (Ollama native endpoint, returns a canned chat-completion response shape) and `/api/tags` (returns the declared model in the response). Critically, the mock must NOT expose `/v1/chat/completions` — exposing it would mask the wrong-path footgun. If the agent's outbound request hits `/v1/*` paths, the test fails, proving the overlay correctly routed through the Ollama-native API.
2. **Local provider, no overlay**: provision agent without `agent.yaml`. Verify `openclaw.json` is byte-identical to today's output (regression guard).
3. **Local provider, broken overlay**: provision with malformed `agent.yaml`. Verify refresh fails with a clear error and the prior good config is preserved on disk.
4. **Remote provider**: same as #1 but via SSH. Confirms overlay loads correctly when behavior dir is the repo working tree and gets SFTP-pushed.

### Manual AWS verification (not automated — captured in `verify-feature` step)

Per the plan's "End-to-end verification" section, run against the production `aaron` agent on AWS.

## Data Safety (per architecture standards)

This feature **does not touch agent data**. No changes to:
- Agent data directory paths.
- Container volume mounts.
- `RefreshAgent` / `CycleHost` data semantics.
- `Teardown` flow.

The only file written by this feature is the generated `openclaw.json` at refresh time, which has always been overwritten on every refresh. No new file is written to the agent data directory. No file is removed.

`agent.yaml` lives in the **repo working tree** (`behavior/agents/<name>/`), not in the agent's data directory. It is operator-authored, not agent-generated. The agent has no read or write access to it.

## Provider parity (per architecture standards)

The `Provider` interface is unchanged. All three providers (`aws`, `local`, `remote`) consume the same `pkg/runtime/openclaw/GenerateConfig` function with the same `ConfigParams` shape. The new `Overlay` field is populated identically by each provider's `RefreshAgent` codepath via `common.LoadAgentOverlay`.

The same `behavior/agents/aaron/agent.yaml` file produces identical `openclaw.json` on all three providers (golden test #2 above verifies this property).

## Interface Parity (per architecture standards)

**This feature adds no new CLI command, no JSON input field, no MCP tool.** It is implemented entirely as an extension to the existing config generation pipeline. Operators interact with the feature by editing a file (`agent.yaml`), exactly as they do for `SOUL.md` / `AGENTS.md` today. No interface parity concerns arise.

If a future iteration wants CLI ergonomics for the overlay (`conga agent set-model <name> ...`), that's a separate spec that would need to satisfy the parity rule by adding CLI + JSON + MCP simultaneously.

## Config Format Boundary (per architecture standards)

`agent.yaml` is operator-authored, so YAML is the correct choice per the existing rule: "JSON for machine-generated, YAML for hand-authored." The standards doc currently says "`conga-policy.yaml` is the only YAML file — this is intentional"; this spec expands that to two YAML files. The principle (machine vs. operator) is preserved; only the descriptive sentence becomes stale.

**Standards update**: as part of this feature's implementation, update `product-knowledge/standards/architecture.md` to reflect that `agent.yaml` is the second operator-authored YAML, sharing the policy file's rationale.

## Documentation deltas

| File | Change |
|---|---|
| `behavior/agents/_example/agent.yaml.example` | **NEW**. Template with `version: 1`, both Ollama and OpenAI provider examples, inline comments explaining each field, and a commented-out **reserved keyspace** block (`memory:`, `tools:`, `limits:`, `model.fallbacks`) so operators understand what's coming and won't try to use those keys yet. |
| `product-knowledge/standards/config-taxonomy.md` | **NEW**. Single source of truth for per-agent config homes: infrastructure → tfvars, cluster policy → conga-policy.yaml, runtime overlay → agent.yaml, runtime persistence → JSON/SSM, secrets → secrets store. Includes decision rule and anti-patterns. Per architect deep-dive in trace `README.md`. |
| `CLAUDE.md` "Behavior files" section | Add a paragraph: `agent.yaml` is the optional per-agent config overlay sitting alongside `SOUL.md`. Real overlays are gitignored. Currently supports `model:` block; see spec for schema. Link to `config-taxonomy.md` for the broader picture. |
| `product-knowledge/standards/architecture.md` | (a) Update Config Format Boundary table note to reflect two YAML files (`conga-policy.yaml` + `agent.yaml`). (b) Add a "see also" reference to `config-taxonomy.md` for the full taxonomy. |
| `product-knowledge/ROADMAP.md` | Cross-link this spec from the Bifrost / Model Routing row (Pipeline Phase 2) as the minimal precursor. |
| `product-knowledge/PROJECT_STATUS.md` | Already updated (#27 entry from plan-feature). |
| `terraform.tfvars.example` | Add a commented example showing `agents.<name>.secrets = { "openai-api-key" = "..." }` for OpenAI-compatible flows. (Ollama-local needs no secret.) |
| `terraform.tfvars` | (Operator's responsibility, not committed): add the per-agent `openai-api-key` secret entry when using the `openai` overlay provider. Not needed for Ollama. |

### Operator recipe (goes in CLAUDE.md and the `_example/agent.yaml.example` comments)

```bash
# 1. On the Spark, see what's installed:
ollama list
# NAME              SIZE      MODIFIED
# qwen3:6b          4.2 GB    2 days ago
# ...

# 2. Use the exact NAME column value as `model.name` in agent.yaml:
cat > behavior/agents/aaron/agent.yaml <<'YAML'
version: 1
model:
  provider: ollama
  name: qwen3:6b
  base_url: http://192.168.181.97:11434   # NO /v1
YAML

# 3. (AWS) terraform apply, then conga refresh-agent aaron.
# 3. (local/remote) conga refresh-agent aaron.
```

## Implementation phases (mapping plan.md → concrete order)

1. **Phase 0 (done in spike)**: pin status checked, OpenClaw schema confirmed, this spec written.
2. **Phase 1 — Pin bump** (separate commit/PR): bump image tag to `v2026.5.18` in tfvars + CLAUDE.md note; verify Slack still works. Block on this before Phase 2.
3. **Phase 2 — Type definitions** (`pkg/runtime/overlay.go`): `AgentOverlay`, `ModelOverlay`, `Validate()`.
4. **Phase 3 — Overlay loader** (`pkg/common/overlay_agent.go`): `LoadAgentOverlay` + tests.
5. **Phase 4 — Config generator** (`pkg/runtime/openclaw/config.go`): `applyModelOverlay` + golden tests.
6. **Phase 5 — Provider wiring** (local, remote, aws): each `RefreshAgent` calls `LoadAgentOverlay` and threads `ConfigParams.Overlay`.
7. **Phase 6 — AWS bootstrap shell** (`deploy-behavior.sh` and friends): pull `agent.yaml` from S3 (or render JSON server-side per sub-decision above) so the bootstrap path mirrors the Go path.
8. **Phase 7 — Example + docs**:
   - `behavior/agents/_example/agent.yaml.example` (with `version: 1` + reserved keyspace block).
   - `product-knowledge/standards/config-taxonomy.md` (NEW, per architect review).
   - `CLAUDE.md` "Behavior files" paragraph + link to taxonomy.
   - `product-knowledge/standards/architecture.md` cross-link + Config Format Boundary update.
   - `product-knowledge/ROADMAP.md` cross-link to this spec.
9. **Phase 8 — Provider release**: tag congaline, bump `terraform-provider-conga`, release per `reference_provider_release_flow.md` (memory).
10. **Phase 9 — Verification**: AWS end-to-end on `aaron`; local provider parity; control agent regression check.

## Open Questions (still open, to revisit during implement-feature)

1. **AWS bootstrap path** (Phase 6) — render JSON server-side (preferred, simpler bootstrap) vs. ship `agent.yaml` to S3 and parse on the box. Decision deferred until we touch the bootstrap; both paths are viable. Default: render server-side.
2. **YAML library** — verify the project already uses `gopkg.in/yaml.v3` (per `architecture.md` Config Format Boundary table). If yes, reuse. If no, add it.
3. **Fallback chain support** — overlay v1 forcibly clears `fallbacks` to `[]`. Operators wanting "Qwen primary, Claude fallback" need a future enhancement. Documented in non-goals; flag in operator docs.
4. **Per-runtime overlay applicability** — Hermes runtime exists alongside OpenClaw. This spec targets OpenClaw only. Hermes overlay support is deferred to a separate spec (Hermes has a different model-config shape).
