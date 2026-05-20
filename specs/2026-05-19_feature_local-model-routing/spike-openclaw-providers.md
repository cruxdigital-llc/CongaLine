# Spike: OpenClaw pin status + provider config shape

**Goal**: Resolve the two open questions from `plan.md` before writing `spec.md`:

1. Is openclaw/openclaw#45311 still open? If fixed, what's the latest stable pin?
2. Exact `openclaw.json` schema for routing the `aaron` agent to Qwen on the DGX Spark.

## Q1: Pin status

- **Issue #45311**: `[Bug] 2026.3.12: Slack socket mode connects but receives zero inbound events (regression from 2026.3.11)` — **CLOSED 2026-04-25**.
- **Latest stable release** (as of 2026-05-19): `v2026.5.18` (released 2026-05-18).
- **Caveats**: The Slack stack had multiple subsequent regressions and fixes, with the most recent batch (#81846, #81852, #79027) closed on **2026-05-13 → 2026-05-17**. The Slack story has been turbulent. Verification post-bump should explicitly exercise socket-mode inbound DM dispatch and event ACK, not just process health.

### Decision
**Bump the image pin from `2026.3.11` to `2026.5.18`** in a separate commit before the feature lands. Reasons:
- The original blocking issue is fixed.
- Building against `2026.3.11` means accepting whatever provider/model schema that older version uses, which differs materially from current (see Q2).
- The recent Slack fixes are valuable on their own merit.

### Acceptance test for the bump
1. Apply the pinned `2026.5.18` image.
2. `conga refresh-agent` an existing agent (e.g. `zach`).
3. Send a Slack DM. Confirm the agent receives and responds.
4. Tail container + router logs for any "no inbound events" / "ACK" warnings for at least 15 minutes.
5. Check `openclaw doctor` output if exposed; otherwise inspect `/health` endpoints.

If any Slack regression resurfaces, roll back the pin and proceed with the feature against `2026.3.11` (deferring some `2026.5.x` config features that depend on the newer schema).

## Q2: Routing aaron to Qwen on the Spark

### Critical finding — the planning answer was wrong
The plan-feature decision was "Native OpenAI-compatible support" (`OPENAI_BASE_URL` + `openai/<model>`). **This path is broken for our case.** From `docs/providers/ollama.md` in `v2026.5.18`:

> **Remote Ollama users**: Do not use the `/v1` OpenAI-compatible URL (`http://host:11434/v1`) with OpenClaw. This breaks tool calling and models may output raw tool JSON as plain text. Use the native Ollama API URL instead: `baseUrl: "http://host:11434"` (no `/v1`).

The Spark exposes Ollama on port `11434` (Ollama's default). Routing it through OpenClaw's `openai/*` path would compile, connect, and *appear* to work — but tool calls would break, producing a silently degraded agent. **The correct path is OpenClaw's native `ollama` provider**, not OpenAI-compatible.

### Confirmed config schema (v2026.5.18)

Top-level structure of `openclaw.json` for this case:

```json5
{
  // Active model selection
  "agents": {
    "defaults": {
      "model": {
        "primary": "ollama/qwen3:6b",   // exact tag from `ollama list` on the Spark
        "fallbacks": []                 // empty = no auto-fallback to Anthropic
      },
      // Optional allowlist; if present, narrows /model picker. Omit to keep auto-discovery.
      "models": {
        "ollama/qwen3:6b": {}
      }
    }
  },
  // Provider endpoint configuration
  "models": {
    "providers": {
      "ollama": {
        "baseUrl": "http://192.168.181.97:11434",   // NO /v1 path
        "apiKey":  "ollama-local",                  // sentinel for LAN/loopback; no real auth needed
        "api":     "ollama"
      }
    }
  }
}
```

### Key rules (from `docs/concepts/model-providers.md` and `docs/providers/ollama.md`)

1. **Model refs use `<provider>/<model>` format.** Setting `primary` to `ollama/<tag>` is what tells OpenClaw to use the Ollama provider for chat turns.
2. **`baseUrl` is canonical**; `baseURL` is accepted for compatibility. Use `baseUrl`.
3. **No `/v1` suffix** for Ollama — that's the OpenAI-compatible endpoint, which OpenClaw warns against.
4. **`apiKey: "ollama-local"`** is a recognized sentinel for local/LAN hosts (loopback, private subnets, `.local`, bare hostnames). No real bearer token needed. Setting `OLLAMA_API_KEY=ollama-local` as env also works.
5. **Setting `models.providers.ollama` explicitly disables auto-discovery.** With explicit config, OpenClaw will NOT call `/api/tags` to discover what's installed — models must be listed manually or referenced as bare `ollama/<model>:tag` refs. For an IaC-deployed config this is desirable (deterministic startup), but it means we must know the exact Ollama model tag at deploy time.
6. **Auto-discovery is the alternative**: omit `models.providers.ollama` entirely, set `OLLAMA_API_KEY=ollama-local` in env, and let OpenClaw discover models at runtime. Requires the container to query `192.168.181.97:11434/api/tags` at startup — the egress allowlist already permits this, so it works. Less deterministic than explicit config but no upfront knowledge of model tags needed.

### Decision: explicit config, not auto-discovery
For IaC reproducibility, generate **explicit** `models.providers.ollama` config from the overlay. The overlay carries `provider`, `name`, and `base_url`; the runtime config generator writes both the `agents.defaults.model.primary` and the `models.providers.<provider>` blocks.

### Generalizing the overlay
The overlay schema should NOT hardcode "ollama" — Aaron may swap the Spark from Ollama to vLLM, llama.cpp (with `llama-server`), or a Bedrock proxy later. Supported provider values (v1):

| `provider` value in overlay | OpenClaw provider key | Notes |
|---|---|---|
| `ollama` | `ollama` | Native Ollama API. Use `baseUrl` without `/v1`. `apiKey` defaults to `"ollama-local"` for LAN hosts. |
| `openai` | `openai` | Native OpenAI / OpenAI-compatible endpoints (vLLM, llama.cpp `--api`, OpenAI itself). Use `baseUrl` ending in `/v1`. Requires `openai-api-key` secret. |
| (others) | (deferred) | Add when needed. The overlay loader rejects unknown providers. |

**For Aaron's Spark today**: `provider: ollama`, `name: qwen3:6b` (or whatever `ollama list` shows), `base_url: http://192.168.181.97:11434` — **no `/v1`**.

If Aaron later switches to vLLM serving Qwen at a different port: `provider: openai`, `name: qwen3-6b`, `base_url: http://192.168.181.97:8000/v1` — **with `/v1`**.

## Verification against the running image

(Not yet executed — to be done as the first step of `implement-feature` Phase 0):

```bash
docker pull ghcr.io/openclaw/openclaw:2026.5.18

# Sanity: does the explicit ollama config work end-to-end against the Spark?
docker run --rm -e OPENCLAW_CONFIG_PATH=/tmp/openclaw.json \
  -v "$PWD/tmp-config":/tmp:ro \
  --network host \
  ghcr.io/openclaw/openclaw:2026.5.18 \
  openclaw infer model run --local --model ollama/qwen3:6b \
    --prompt "Reply with exactly: pong" --json
```

A `pong`-shaped reply confirms the wire format. Tool-call behavior must be tested separately by invoking a tool-using agent flow (e.g. file_search).

## Open questions resolved → spec can proceed
- Pin: bump to `v2026.5.18`.
- Provider: `ollama` (not `openai`), with no `/v1` in `baseUrl`.
- Auth: `ollama-local` sentinel for LAN; real `openai-api-key` only when overlay declares `provider: openai`.
- Schema: write to both `agents.defaults.model.primary` and `models.providers.<id>` in the generated `openclaw.json`.
- Genericity: overlay's `provider` field gates which `models.providers.*` block is generated.
