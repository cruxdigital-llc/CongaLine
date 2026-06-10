package common

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

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
func ValidateAgentCustomConfig(data []byte) error {
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
		return fmt.Errorf("agent-custom.json declares Conga-owned key(s) %v; these are managed by Conga (channels via `conga channels bind`) and must not appear in the include", found)
	}
	return nil
}
