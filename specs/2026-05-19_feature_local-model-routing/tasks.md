# Tasks: local-model-routing

Canonical implementation checklist. Mirrors phases in `spec.md` § Implementation phases. Phases 1, 6, 8, and 9 are intentionally **out of scope for this PR** (see "Out of scope" below).

## In scope (this PR)

### Phase 2 — Overlay types
- [ ] **Create `pkg/runtime/overlay.go`** with:
  - `AgentOverlay { Version int; Model *ModelOverlay }`
  - `ModelOverlay { Provider, Name, BaseURL string }`
  - `(o *AgentOverlay).Validate() error` — version gate, provider enum, name/base_url checks, ollama+`/v1` reject, casing-sensitive provider strings.
  - Exported constants for supported provider names (`ProviderOllama`, `ProviderOpenAI`).
  - Sentinel `OllamaLocalAPIKey = "ollama-local"`.
- [ ] **Create `pkg/runtime/overlay_test.go`** — unit tests for `Validate()` covering every rule in the spec (version 0/1/2, missing provider, unknown provider, empty name, casing mismatch, `/v1` footgun, empty base_url for ollama, etc.).

### Phase 3 — Overlay loader
- [ ] **Create `pkg/common/overlay_agent.go`** with:
  - `LoadAgentOverlay(behaviorDir string, agent provider.AgentConfig) (*runtime.AgentOverlay, error)`
  - File path: `<behaviorDir>/agents/<name>/agent.yaml`
  - `os.IsNotExist` → `(nil, nil)`; other read errors → wrap.
  - `yaml.Decoder` with `KnownFields(true)` for strict-key parsing.
  - After parse: call `(*AgentOverlay).Validate()`; wrap errors with file path.
  - Emit missing-version warning to stderr exactly once per file path (use a `sync.Map` to dedupe per process).
- [ ] **Create `pkg/common/overlay_agent_test.go`** — the 15 cases from `spec.md` § Tests:
  1. Missing file → `(nil, nil)`
  2. Empty file → version-0 + warning
  3. Valid Ollama v1
  4. Valid OpenAI v1
  5. `version: 2` rejected
  6. Missing version + valid body → warning + accepted
  7. Unknown top-level key → strict-key error
  8. Unknown `model.bare_url` → strict-key error
  9. Malformed YAML → error with file path
  10. Unknown `provider:` → error citing supported set
  11. Ollama + `/v1` suffix → error
  12. Ollama + empty base_url → error
  13. base_url no scheme → error
  14. OpenAI + non-`/v1` base_url → succeeds, warning logged
  15. Provider casing (`Ollama`) → error citing canonical

### Phase 4 — OpenClaw config overlay
- [ ] **Modify `pkg/runtime/runtime.go`**: add `Overlay *AgentOverlay` to `ConfigParams` (additive, after existing fields). Keep `Model string` for Hermes compatibility.
- [ ] **Modify `pkg/runtime/openclaw/config.go`**:
  - Add private `applyModelOverlay(config map[string]any, m *ModelOverlay) error` that mutates:
    - `agents.defaults.model.primary` → `<provider>/<name>`
    - `agents.defaults.model.fallbacks` → `[]`
    - `agents.defaults.models` → `{<provider>/<name>: {}}` (full replace; removes Anthropic default)
    - `models.providers.<provider>` → block with `baseUrl`, `apiKey` (ollama sentinel), `api` (ollama only)
  - In `GenerateConfig`: when `params.Overlay != nil && params.Overlay.Model != nil`, call `applyModelOverlay`.
- [ ] **Extend `pkg/runtime/openclaw/config_test.go`** — golden file tests:
  1. No overlay → byte-identical to today's output (regression guard)
  2. Ollama overlay → expected `models.providers.ollama` block
  3. OpenAI overlay with self-hosted endpoint → expected `models.providers.openai.baseUrl`
  4. Overlay + Slack channel coexistence

### Phase 5 — Provider wiring
- [ ] **Modify `pkg/common/config.go`**: extend `RuntimeGenerateAgentFiles` signature to accept an overlay parameter; pass through to `GenerateConfig`. Add a sibling `RuntimeGenerateAgentFilesWithOverlay(rtName, cfg, shared, perAgent, overlay)` to preserve the existing signature (back-compat).
- [ ] **Modify `pkg/provider/localprovider/provider.go`**: `ProvisionAgent` (line ~192) and `RefreshAgent` (line ~681) call `common.LoadAgentOverlay(p.behaviorDir(), cfg)` before `rt.GenerateConfig`, then pass `Overlay` in `ConfigParams`.
- [ ] **Modify `pkg/provider/remoteprovider/provider.go`**: same pattern at the analogous call site(s) using the remote provider's behavior dir (`filepath.Join(repoPath, "behavior")`).
- [ ] **Modify `pkg/provider/awsprovider/channels.go`**: `regenerateAgentConfigOnInstance` calls `common.LoadAgentOverlay` using the operator's repo path (from `p.cfg.RepoPath` or equivalent), then passes via `RuntimeGenerateAgentFilesWithOverlay`. No bootstrap shell changes needed — overlay is consumed at config-gen time on the operator's machine.

### Phase 7 — Example + docs
- [ ] **Create `behavior/agents/_example/agent.yaml.example`** with `version: 1`, Ollama + OpenAI provider examples, inline comments, and a commented-out reserved keyspace block.
- [ ] **Modify `CLAUDE.md`** — add a short paragraph to the "Behavior files" section pointing at `agent.yaml` + the taxonomy doc.
- [ ] **Modify `product-knowledge/ROADMAP.md`** — cross-link from the Bifrost / Model Routing row to this spec as the minimal precursor.

### Verification
- [ ] `go test ./...` passes.
- [ ] `go vet ./...` clean.
- [ ] `gofmt -l .` clean.
- [ ] Manual smoke: build the CLI, point a throwaway local agent at a mock HTTP server via `agent.yaml`, confirm the rendered `openclaw.json` contains the expected `models.providers.<id>` block.

## Out of scope for this PR

| Phase | Reason | Tracked where |
|---|---|---|
| Phase 1 — Image pin bump | Requires running the new image and exercising Slack DM round-trip in production. That's an ops step that should land as its own focused PR with explicit acceptance testing. | Separate PR (prereq for production deploy of this feature) |
| Phase 6 — AWS bootstrap shell | Discovered during implementation that the overlay is consumed entirely at config-gen time on the operator's machine; the resulting `openclaw.json` carries the effect via the existing `regenerateAgentConfigOnInstance` upload path. No `deploy-behavior.sh` or `user-data.sh.tftpl` changes needed. **This is a scope reduction**, not a punt. |
| Phase 8 — Provider release | Release management is a deploy concern; happens after merge, not as part of the implementation PR. | Operator-driven post-merge |
| Phase 9 — End-to-end verification | That's `/glados:verify-feature`. Happens after this PR is reviewed. | Next workflow step |

## Status

| Phase | Status |
|---|---|
| 2 — Types | ✅ complete (`pkg/runtime/overlay.go` + tests) |
| 3 — Loader | ✅ complete (`pkg/common/overlay_agent.go` + tests) |
| 4 — Generator | ✅ complete (`pkg/runtime/openclaw/config.go` `applyModelOverlay` + 5 golden tests) |
| 5 — Provider wiring | ✅ complete (local, remote, AWS) |
| 7 — Docs | ✅ complete (`_example/agent.yaml.example`, CLAUDE.md, ROADMAP.md; taxonomy doc + architecture.md landed during spec phase) |
| Verification | ✅ go test / go vet / gofmt all clean |
