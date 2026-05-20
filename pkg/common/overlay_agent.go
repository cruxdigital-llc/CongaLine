package common

import (
	"bytes"
	"errors"
	"fmt"
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

	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)

	var overlay runtime.AgentOverlay
	if err := dec.Decode(&overlay); err != nil {
		// An empty file decodes to io.EOF; treat that as the version-0 case
		// (empty overlay, warn-and-accept).
		if errors.Is(err, fs.ErrInvalid) || isEmptyYAMLError(err) {
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

// isEmptyYAMLError detects the yaml.v3 sentinel returned for empty input.
// yaml.v3 wraps io.EOF in a TypeError; check the message as a fallback.
func isEmptyYAMLError(err error) bool {
	return err != nil && err.Error() == "EOF"
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
