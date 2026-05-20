# Plan: local-model-routing

High-level implementation approach. Detailed file:line work and interface signatures land in `spec.md` after the pre-spec spike.

## Strategy

**Extend existing pipes; do not add new ones.** The behavior overlay already discovers a per-agent directory and merges its contents into runtime config. We add one more file type (`agent.yaml`) to that overlay and teach the OpenClaw runtime config generator to honor one new field block (`model:`). Everything else — secrets, egress, terraform, CLI — stays as-is.

## Sequence

### Phase 0 — Pre-spec spike (gates everything)

1. **Check upstream pin status.**
   - `gh issue view openclaw/openclaw#45311` — is the Slack socket mode regression resolved?
   - If **fixed**: choose the latest stable that includes the fix as the new pin target.
   - If **open**: stay on `2026.3.11`.
2. **Verify OpenClaw's OpenAI-compatible config shape against the chosen image.**
   - `docker pull ghcr.io/openclaw/openclaw:<chosen-tag>`.
   - Inspect schema docs in the image or run a minimal config with `OPENAI_BASE_URL`/`OPENAI_API_KEY` env + an `openai/<name>` model declaration. Observe what shape OpenClaw accepts.
3. **Write findings** → `specs/2026-05-19_feature_local-model-routing/spike-openclaw-openai.md` with: pin decision, verified `openclaw.json` schema for OpenAI providers, and any edge cases (e.g. does OpenClaw require both env var and config field?).
4. If the spike shows OpenClaw does **not** natively support OpenAI-compatible: stop. Re-plan as a Bifrost-blocked feature; do not proceed with this implementation.

### Phase 1 — Image pin bump (only if Phase 0 decided to bump)

1. Update the OpenClaw image tag in `terraform.tfvars` (and any other places that pin it — search `2026.3.11` to enumerate).
2. Update the `CLAUDE.md` note about the pin and the linked issue.
3. `terraform apply`; verify Slack still works and the regression is actually gone.
4. Commit separately from the feature so the bump is bisectable.

### Phase 2 — Overlay loader extension

**Touchpoints:** `pkg/common/` — likely `overlay.go` or `behavior.go` (confirmed during spec).

1. Define typed structs:
   ```go
   type AgentOverlayConfig struct {
       Model *ModelConfig `yaml:"model,omitempty"`
   }
   type ModelConfig struct {
       Provider string `yaml:"provider"`
       Name     string `yaml:"name"`
       BaseURL  string `yaml:"base_url,omitempty"`
   }
   ```
2. Extend the per-agent overlay discovery to look for `agent.yaml` alongside the existing prompt files. Parse with `gopkg.in/yaml.v3` (or whichever YAML lib the repo already uses; check `go.mod`).
3. Validation:
   - `Provider` ∈ `{"openai", "anthropic"}` (extensible).
   - `Name` non-empty when `Provider` set.
   - `BaseURL` parses via `net/url.Parse`; reject empty scheme or host.
4. Surface result via whatever struct already conveys overlay data to the runtime (extend it; don't add a parallel channel).
5. Unit tests:
   - Missing `agent.yaml` → nil `Model`.
   - Valid file → typed struct populated.
   - Malformed YAML → error with file path.
   - Invalid `Provider` value → error.
   - `base_url` not a URL → error.

### Phase 3 — OpenClaw config overlay

**Touchpoints:** `pkg/runtime/openclaw/config.go` (`GenerateConfig`).

1. Accept the new `Model` field from the overlay struct.
2. When `Model` is set, mutate the generated `openclaw.json` according to the schema confirmed in Phase 0. Provisional shape (validated by spike):
   ```jsonc
   {
     "agents": {
       "defaults": {
         "model": { "primary": "openai/qwen-3.6", "fallbacks": [] },
         "models": {
           "openai/qwen-3.6": {
             "baseURL": "http://192.168.181.97:11434/v1",
             "params": {}
           }
         }
       }
     }
   }
   ```
3. When `Model` is nil, output is byte-identical to today.
4. Unit tests:
   - Golden file: no-overlay generates today's output exactly.
   - Golden file: overlay produces the expected merged JSON.
   - Property: extra unrelated fields in `openclaw-defaults.json` are preserved.

### Phase 4 — Example file + docs

1. Create `behavior/agents/_example/agent.yaml.example`:
   ```yaml
   # Optional per-agent runtime config overlay. Sits next to SOUL.md / AGENTS.md.
   # Copy to behavior/agents/<your-agent>/agent.yaml and edit.
   # Real agent overlays are gitignored — only this _example file is committed.

   model:
     # Provider for outbound model calls. Default (no overlay) = Anthropic.
     # Supported: "openai" (any OpenAI-compatible endpoint), "anthropic" (no-op default).
     provider: openai

     # Model name as the provider expects it.
     name: qwen-3.6

     # Required for "openai" when targeting a self-hosted endpoint.
     # Omit for OpenAI's hosted API.
     base_url: http://192.168.181.97:11434/v1
   ```
2. Add a paragraph to `CLAUDE.md` "Behavior files" section: what `agent.yaml` is, that real overlays are gitignored, and a link to this spec.
3. Cross-link from `product-knowledge/ROADMAP.md` to this spec under the Bifrost / Model Routing row (item #22 in PROJECT_STATUS) — this feature is the minimal precursor.

### Phase 5 — Provider release

Per `CLAUDE.md`: changes to `pkg/` require:
1. Tag congaline with the new version.
2. In `terraform-provider-conga` repo: `go get github.com/cruxdigital-llc/congaline@<tag>` + `go mod tidy`.
3. Tag the provider; GoReleaser publishes to registry.
4. Delete `~/.terraform.d/plugins/registry.terraform.io/cruxdigital-llc/conga/` locally and `terraform init -upgrade` to pick up the new version.

### Phase 6 — End-to-end verification

#### AWS (primary path)
1. `behavior/agents/aaron/agent.yaml` populated with Spark URL.
2. `terraform/environments/production/terraform.tfvars`: add `"openai-api-key" = "<key-or-dummy>"` under `agents.aaron.secrets`.
3. `terraform apply` from `terraform/environments/production/`.
4. `conga refresh-agent aaron`.
5. `mcp__conga__conga_container_exec --agent aaron` running `cat /home/node/.openclaw/openclaw.json` — verify the model block.
6. DM `aaron` in Slack: "hello" → expect Qwen response.
7. `mcp__conga__conga_get_logs --agent aaron` — outbound URL is the Spark.
8. `mcp__conga__conga_get_proxy_logs --agent aaron` — Envoy connect target is `192.168.181.97:11434`.
9. Control: DM `zach` → Anthropic response. Their `openclaw.json` is unchanged (diff is empty).

#### Local provider parity
1. Fresh `conga admin setup --provider local` on a dev machine with `behavior/agents/aaron/agent.yaml` present.
2. `conga secrets set openai-api-key --agent aaron --value dummy`.
3. `docker exec conga-aaron cat /home/node/.openclaw/openclaw.json` — same model overlay as on AWS.

#### Remote provider parity
1. Same overlay, `conga admin setup --provider remote` against a Raspberry Pi or VPS.
2. Same `openclaw.json` shape.

## Touchpoint summary

| Change kind | Files |
|---|---|
| **New code** | `pkg/common/overlay.go` (or `behavior.go`) — parse `agent.yaml`; `pkg/runtime/openclaw/config.go` — overlay model fields |
| **New tests** | Unit tests next to the above; golden files for `openclaw.json` generation |
| **New committed asset** | `behavior/agents/_example/agent.yaml.example` |
| **Doc updates** | `CLAUDE.md` (Behavior files section), `product-knowledge/ROADMAP.md` (cross-link), `product-knowledge/PROJECT_STATUS.md` (new entry) |
| **Possibly changed** | Image pin in `terraform.tfvars` (depends on Phase 0); `CLAUDE.md` pin note |
| **Untouched** | `terraform/modules/`, `terraform-provider-conga` resource definitions, `internal/cmd/`, `pkg/policy/`, `pkg/runtime/openclaw/env.go`, `.gitignore` |

## Risks

| Risk | Mitigation |
|---|---|
| OpenClaw 2026.3.11 (or chosen target) doesn't natively support OpenAI-compatible | Spike catches this in Phase 0; we stop before writing code and re-plan around Bifrost |
| Spark IP changes | Documented as a manual update in both `terraform.tfvars` and `agent.yaml`; surface in the spec's operational notes |
| `aaron` opts in but `openai-api-key` is missing | Container starts, upstream auth fails, visible in `conga logs`. Acceptable failure mode for v1; can add a validator later |
| Image pin bump (Phase 1) introduces unrelated regressions | Apply separately, commit separately, verify Slack + existing agents before merging the feature |

## Open Questions (deferred to `spec.md`)

1. Exact YAML library: confirm whether the repo already depends on `gopkg.in/yaml.v3` or something else; if neither, choose one.
2. Whether to expose `provider: anthropic` as a no-op (explicit default declaration) or reject it as redundant.
3. Whether to support a `fallbacks: []` array in `agent.yaml.model` for users who want OpenAI primary + Anthropic fallback. Lean **no** for v1 — keeps the overlay minimal; revisit when Bifrost lands.
4. Telemetry: should the rendered `openclaw.json` log which path it took (Anthropic default vs overlay) at refresh time? Lean **yes** — helps diagnose silent overlay misses.
