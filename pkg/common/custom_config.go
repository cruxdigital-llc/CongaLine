package common

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/cruxdigital-llc/conga-line/pkg/provider"
	"github.com/cruxdigital-llc/conga-line/pkg/runtime"
)

// CustomConfigSources holds the committed declarative custom-config layers for an
// agent (feature #31): the fleet baseline (applies to all agents) and the
// per-agent file. Each field is the raw source content, or nil if the source is
// absent — in which case the provider deploys "{}" (the $include target must
// exist or the whole config is invalid). These are deployed beside openclaw.json
// as fleet-custom.json / agent-managed-custom.json and merged via $include.
type CustomConfigSources struct {
	Fleet    []byte // agents/_defaults/<runtime>/fleet-custom.json (all agents)
	PerAgent []byte // agents/<name>/custom.json (one agent)
}

// FleetCustomSourceName / PerAgentCustomSourceName are the committed source file
// names (distinct from the deployed names in pkg/runtime/openclaw).
const (
	FleetCustomSourceName    = "fleet-custom.json"
	PerAgentCustomSourceName = "custom.json"
)

// RuntimeDefaultsSourceName is the committed, operator-editable OpenClaw runtime
// baseline (feature #31's de-embed). It lives at
// agents/_defaults/openclaw/openclaw-defaults.json — runtime-level, NOT
// type-specific — beside the fleet-custom.json source, and is synced to the AWS
// host via the existing `aws s3 sync conga/agents/` path. The repo file
// pkg/runtime/openclaw/openclaw-defaults.json remains the canonical seed and the
// binary's embedded fallback.
const RuntimeDefaultsSourceName = "openclaw-defaults.json"

// ResolveRuntimeDefaults reads the operator-editable runtime baseline for an
// agent's runtime from behaviorDir (the operator's agents/ tree — resolve with
// ResolveOperatorBehaviorDir). It returns the raw bytes to thread into
// runtime.ConfigParams.RuntimeDefaults, or nil if no on-disk file exists (the
// generator then uses its embedded fallback). The bytes are returned unvalidated;
// the generator validates and falls back on malformed input (tamper-safe).
//
// Only OpenClaw ships a de-embedded baseline today; other runtimes yield nil.
func ResolveRuntimeDefaults(behaviorDir string, agent provider.AgentConfig) []byte {
	rtName := runtime.ResolveRuntime(agent.Runtime, "")
	if rtName != runtime.RuntimeOpenClaw {
		return nil
	}
	if data, err := os.ReadFile(filepath.Join(behaviorDir, defaultsSubdir, string(rtName), RuntimeDefaultsSourceName)); err == nil {
		return data
	}
	return nil
}

// ResolveCustomConfigSources reads the committed declarative custom-config
// sources for an agent from behaviorDir (the operator's agents/ tree — resolve
// with ResolveOperatorBehaviorDir). Missing sources yield nil (deploy "{}").
// Fleet config is per-runtime and NOT type-specific (applies to every agent of
// that runtime).
func ResolveCustomConfigSources(behaviorDir string, agent provider.AgentConfig) CustomConfigSources {
	rtName := string(runtime.ResolveRuntime(agent.Runtime, ""))
	var s CustomConfigSources
	if data, err := os.ReadFile(filepath.Join(behaviorDir, defaultsSubdir, rtName, FleetCustomSourceName)); err == nil {
		s.Fleet = data
	}
	if data, err := os.ReadFile(filepath.Join(behaviorDir, agent.Name, PerAgentCustomSourceName)); err == nil {
		s.PerAgent = data
	}
	return s
}

// ReservedCustomConfigKeys are the top-level openclaw.json keys Conga owns and
// that the admin-owned customization file (agent-custom.json, the "$include"
// target) must NOT declare:
//   - channels: the channel allowlist is a declared security boundary
//     (security.md §Configuration Integrity). OpenClaw deep-merges objects by
//     union, so an include could ADD a channel binding even though it cannot
//     overwrite Conga's scalar values — extending the allowlist undetected.
//   - gateway: carries the auth token and bind/port.
//   - plugins: gates channel plugins.
//   - $include: prevents nested-include chains escaping Conga's managed root.
var ReservedCustomConfigKeys = []string{"$include", "channels", "gateway", "plugins"}

// ErrCustomConfigUnparseable indicates agent-custom.json could not be parsed as
// strict JSON (e.g. it uses JSON5 comments/trailing commas). Callers should warn
// and recommend manual review rather than treat it as a pass — naive comment
// stripping is unsafe because URLs (e.g. mcp.servers) contain "//".
var ErrCustomConfigUnparseable = errors.New("agent-custom.json is not strict JSON (possibly JSON5); cannot validate reserved keys automatically")

// ValidateAgentCustomConfig returns nil if the admin-owned customization file is
// safe (empty, "{}", or declares no Conga-owned key). It returns a descriptive
// error if it declares any ReservedCustomConfigKeys, or ErrCustomConfigUnparseable
// if it cannot be parsed as strict JSON. This is the load-bearing control behind
// the "Conga owns the channel allowlist" invariant: it detects an include that
// tries to inject or extend channel bindings.
//
// Equivalent to ValidateCustomConfigKeys("agent-custom.json", data); prefer the
// generic form for the fleet / per-agent managed layers (feature #31) so the
// error message names the offending file.
func ValidateAgentCustomConfig(data []byte) error {
	return ValidateCustomConfigKeys("agent-custom.json", data)
}

// ValidateCustomConfigKeys is the filename-generic reserved-key guard applied to
// EVERY $include layer (feature #31): the admin-owned agent-custom.json plus the
// Conga-deployed fleet-custom.json / agent-managed-custom.json. fname is used in
// the error message so operators see which layer is offending. Returns
// ErrCustomConfigUnparseable for non-strict-JSON (JSON5) input.
func ValidateCustomConfigKeys(fname string, data []byte) error {
	if len(strings.TrimSpace(string(data))) == 0 {
		return nil
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(data, &m); err != nil {
		return ErrCustomConfigUnparseable
	}
	var found []string
	for _, k := range ReservedCustomConfigKeys {
		if _, ok := m[k]; ok {
			found = append(found, k)
		}
	}
	if len(found) > 0 {
		sort.Strings(found)
		return fmt.Errorf("%s declares Conga-owned key(s) %v; these are managed by Conga (channels via `conga channels bind`) and must not appear in the include", fname, found)
	}
	return nil
}

// ValidateManagedConfigSources is the pre-deploy, fail-closed guard on the
// Conga-deployed declarative layers (feature #31 T6.1). A reserved-key violation
// in the FLEET file would break or compromise EVERY agent (blast radius), so the
// deploy path calls this BEFORE writing anything and aborts on violation — the
// bad file never reaches a host. Both fleet and per-agent sources are checked.
//
// An unparseable (JSON5) source is tolerated here (returns nil for that layer):
// OpenClaw accepts JSON5, our strict-JSON reserved-key check cannot, and the
// on-host openclaw load + periodic integrity check are the backstop. See spec §6.
func ValidateManagedConfigSources(srcs CustomConfigSources) error {
	layers := []struct {
		name string
		data []byte
	}{
		{FleetCustomSourceName, srcs.Fleet},
		{PerAgentCustomSourceName, srcs.PerAgent},
	}
	for _, l := range layers {
		if l.data == nil {
			continue
		}
		if err := ValidateCustomConfigKeys(l.name, l.data); err != nil {
			if errors.Is(err, ErrCustomConfigUnparseable) {
				continue // JSON5 tolerated; on-host load + integrity check backstop
			}
			return fmt.Errorf("pre-deploy validation failed for %s (fix the committed source before deploy): %w", l.name, err)
		}
	}
	return nil
}

// ClassifyIncludeValidation runs the reserved-key guard on one $include layer and
// classifies the result for integrity reporting, shared by the local and remote
// providers' RunIntegrityCheck. A reserved-key violation is a hard error
// ("CONFIG INTEGRITY VIOLATION (<fname>): ..."); an unparseable (JSON5) file is a
// non-fatal warn left to the authoritative in-container check; OK returns ("","").
func ClassifyIncludeValidation(fname string, data []byte) (warn string, err error) {
	if verr := ValidateCustomConfigKeys(fname, data); verr != nil {
		if errors.Is(verr, ErrCustomConfigUnparseable) {
			return fmt.Sprintf("%s could not be validated (not strict JSON); manual review advised", fname), nil
		}
		return "", fmt.Errorf("CONFIG INTEGRITY VIOLATION (%s): %w", fname, verr)
	}
	return "", nil
}
