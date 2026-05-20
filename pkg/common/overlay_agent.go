package common

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sync"

	"github.com/cruxdigital-llc/conga-line/pkg/provider"
	"github.com/cruxdigital-llc/conga-line/pkg/runtime"
	"gopkg.in/yaml.v3"
)

// agentOverlayFileName is the operator-authored per-agent runtime config file.
// Lives in behavior/agents/<name>/ alongside SOUL.md / AGENTS.md / USER.md.
// See specs/2026-05-19_feature_local-model-routing/ and
// product-knowledge/standards/config-taxonomy.md for the schema and rationale.
const agentOverlayFileName = "agent.yaml"

// reservedTopLevelKeys are keys claimed by future schema versions. Setting
// any of these in a v1 document is rejected with a friendlier message than
// the generic strict-key parse error so operators understand they're not
// typos. Keep in sync with the reserved-keyspace section of
// behavior/agents/_example/agent.yaml.example.
var reservedTopLevelKeys = map[string]string{
	"memory": "future schema version (per-agent memory backend)",
	"tools":  "future schema version (per-agent tool/MCP allowlist)",
	"limits": "future schema version (per-agent token/cost limits)",
	"images": "future schema version (per-agent image model)",
	"pdf":    "future schema version (per-agent PDF model)",
	"video":  "future schema version (per-agent video model)",
}

// overlayWarningOnce dedupes "missing version" and "non-standard base_url"
// warnings so they fire at most once per file path per process. Refresh loops
// would otherwise spam stderr.
var overlayWarningOnce sync.Map // map[string]struct{}

// LoadAgentOverlay reads behavior/agents/<agent.Name>/agent.yaml if present.
//
// Return semantics:
//   - File missing: (nil, nil). The agent has no overlay; defaults apply.
//   - File present but malformed / fails validation: (nil, err) wrapped with
//     the file path so operators can find the offending file quickly.
//   - File present and valid: (overlay, nil).
//
// Strict-key parsing is enabled. Unknown top-level or inner keys (e.g. typo
// `bare_url:` instead of `base_url:`) are rejected with the decoder's
// line/key message. See spec § "Strict key parsing" for rationale.
func LoadAgentOverlay(behaviorDir string, agent provider.AgentConfig) (*runtime.AgentOverlay, error) {
	path := filepath.Join(behaviorDir, "agents", agent.Name, agentOverlayFileName)

	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	// Pre-pass: detect reserved top-level keys before strict-key parsing so we
	// can emit a friendlier "reserved for future version" error.
	if err := checkReservedKeys(path, data); err != nil {
		return nil, err
	}

	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)

	var overlay runtime.AgentOverlay
	if err := dec.Decode(&overlay); err != nil {
		// An empty file decodes to io.EOF; treat that as the version-0 case
		// (empty overlay, warn-and-accept).
		if errors.Is(err, io.EOF) {
			emitMissingVersionWarning(path)
			return &overlay, nil
		}
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}

	if err := overlay.Validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}

	if overlay.Version == 0 {
		emitMissingVersionWarning(path)
	}

	if overlay.Model != nil &&
		overlay.Model.Provider == runtime.ProviderOpenAI &&
		runtime.OpenAIBaseURLLooksNonstandard(overlay.Model.BaseURL) {
		emitNonStandardBaseURLWarning(path, overlay.Model.BaseURL)
	}

	return &overlay, nil
}

// checkReservedKeys decodes the raw YAML into a generic map and reports
// whether any top-level keys are reserved for future schema versions.
// Returns nil if no reserved keys are present (including for entirely empty
// or unparseable input — the main decode path handles those).
func checkReservedKeys(path string, data []byte) error {
	var raw map[string]any
	if err := yaml.Unmarshal(data, &raw); err != nil {
		// Defer to the main decoder for syntax errors.
		return nil
	}
	for key := range raw {
		if reason, reserved := reservedTopLevelKeys[key]; reserved {
			return fmt.Errorf("%s: key %q is reserved for a %s; not supported in v1. See behavior/agents/_example/agent.yaml.example for the current schema",
				path, key, reason)
		}
	}
	return nil
}

func emitMissingVersionWarning(path string) {
	key := "missing-version:" + path
	if _, loaded := overlayWarningOnce.LoadOrStore(key, struct{}{}); loaded {
		return
	}
	fmt.Fprintf(os.Stderr, "warning: %s missing `version:` key; assumed %d. Add `version: %d` to silence this warning.\n",
		path, runtime.CurrentOverlaySchemaVersion, runtime.CurrentOverlaySchemaVersion)
}

func emitNonStandardBaseURLWarning(path, baseURL string) {
	key := "nonstandard-base-url:" + path + ":" + baseURL
	if _, loaded := overlayWarningOnce.LoadOrStore(key, struct{}{}); loaded {
		return
	}
	fmt.Fprintf(os.Stderr, "warning: %s declares openai base_url %q which does not look like an OpenAI-compatible /v1 path; proceeding but the endpoint may reject requests.\n",
		path, baseURL)
}
