package common

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"

	"github.com/cruxdigital-llc/conga-line/pkg/runtime"
)

// CheckOverlayEgress returns the list of hostnames declared in the overlay
// (Model.BaseURL + Subagents.Model.BaseURL) that are NOT covered by the
// effective egress allowlist. Empty / null base_urls are skipped — hosted
// Anthropic primaries don't carry one and don't need an explicit entry
// (Conga's bootstrap manifest adds api.anthropic.com for new agents).
//
// Matching mirrors policy.MatchDomain semantics: exact + "*.suffix"
// wildcard, case-insensitive. The wildcard matches subdomains only
// (*.openai.com matches api.openai.com but not openai.com itself), matching
// the Envoy Lua filter at egress-time.
//
// Returns nil when every declared endpoint is allowed. The returned slice
// preserves insertion order (primary first, then subagent) so the caller
// can emit warnings in a predictable sequence.
//
// The returned hostnames are de-duplicated: if primary and subagent
// resolve to the same host (e.g. both on litellm.lan) and it is missing
// from the allowlist, the slice contains a single entry.
func CheckOverlayEgress(overlay *runtime.AgentOverlay, allowlist []string) []string {
	if overlay == nil {
		return nil
	}

	var hosts []string
	if overlay.Model != nil {
		if h := extractHost(overlay.Model.BaseURL); h != "" {
			hosts = append(hosts, h)
		}
	}
	if overlay.Subagents != nil && overlay.Subagents.Model != nil {
		if h := extractHost(overlay.Subagents.Model.BaseURL); h != "" {
			hosts = append(hosts, h)
		}
	}
	if len(hosts) == 0 {
		return nil
	}

	seen := make(map[string]bool, len(hosts))
	var missing []string
	for _, h := range hosts {
		lower := strings.ToLower(h)
		if seen[lower] {
			continue
		}
		seen[lower] = true
		if !hostMatchesAllowlist(h, allowlist) {
			missing = append(missing, h)
		}
	}
	return missing
}

// FormatEgressGapWarning returns the multi-line operator-facing warning
// for a single missing endpoint. Format matches
// specs/2026-05-22_feature_delegation-routing/spec.md § "Egress
// integration / Provisioning-time check." Caller writes it to stderr.
func FormatEgressGapWarning(agentName, host string) string {
	return fmt.Sprintf(`warning: agent %s overlay declares endpoint %s but it is not in the egress allowlist. The agent will provision, but requests to this host will be denied at runtime (HTTP 403 via egress proxy). Add %q to:
  - terraform.tfvars: agents.%s.egress_allowed_domains  (AWS)
  - ~/.conga/conga-policy.yaml: agents.%s.egress.allowed_domains  (local/remote)`,
		agentName, host, host, agentName, agentName)
}

// WarnOverlayEgressGaps emits one FormatEgressGapWarning line group per
// missing host via Warn — the context's WarningSink if one is attached
// (so MCP can surface them), or stderr otherwise. No-op when there are
// no gaps. Designed for use from provider lifecycle flows.
func WarnOverlayEgressGaps(ctx context.Context, overlay *runtime.AgentOverlay, allowlist []string, agentName string) {
	gaps := CheckOverlayEgress(overlay, allowlist)
	for _, h := range gaps {
		Warn(ctx, "%s", FormatEgressGapWarning(agentName, h))
	}
}

// CheckCustomConfigEgress returns the MCP-server hostnames declared across the
// fleet + per-agent custom-config layers (feature #31) that are NOT covered by
// the effective egress allowlist. It walks mcp.servers.<name>.url in each source
// — the egress-bearing field operators add when wiring a remote MCP server.
// Hosts are de-duplicated across layers and returned in a deterministic order
// (fleet before per-agent, then by server name). Unparseable sources are skipped
// (the reserved-key/validation paths handle those). Matching mirrors
// CheckOverlayEgress (exact + "*.suffix" wildcard, case-insensitive).
func CheckCustomConfigEgress(srcs CustomConfigSources, allowlist []string) []string {
	seen := make(map[string]bool)
	var missing []string
	for _, data := range [][]byte{srcs.Fleet, srcs.PerAgent} {
		for _, h := range customConfigEgressHosts(data) {
			lower := strings.ToLower(h)
			if seen[lower] {
				continue
			}
			seen[lower] = true
			if !hostMatchesAllowlist(h, allowlist) {
				missing = append(missing, h)
			}
		}
	}
	return missing
}

// WarnCustomConfigEgressGaps emits an egress-gap warning for every MCP endpoint
// declared in the fleet / per-agent custom config that isn't allowlisted
// (feature #31 T6.2). Non-blocking — mirrors WarnOverlayEgressGaps.
func WarnCustomConfigEgressGaps(ctx context.Context, srcs CustomConfigSources, allowlist []string, agentName string) {
	for _, h := range CheckCustomConfigEgress(srcs, allowlist) {
		Warn(ctx, "%s", FormatEgressGapWarning(agentName, h))
	}
}

// customConfigEgressHosts extracts mcp.servers.<name>.url hostnames from one
// custom-config source, in server-name order. Returns nil for empty/unparseable
// input or sources with no MCP servers.
func customConfigEgressHosts(data []byte) []string {
	if len(data) == 0 {
		return nil
	}
	var m struct {
		MCP struct {
			Servers map[string]struct {
				URL string `json:"url"`
			} `json:"servers"`
		} `json:"mcp"`
	}
	if json.Unmarshal(data, &m) != nil {
		return nil
	}
	names := make([]string, 0, len(m.MCP.Servers))
	for n := range m.MCP.Servers {
		names = append(names, n)
	}
	sort.Strings(names)
	var hosts []string
	for _, n := range names {
		if h := extractHost(m.MCP.Servers[n].URL); h != "" {
			hosts = append(hosts, h)
		}
	}
	return hosts
}

// extractHost pulls the hostname out of a base_url. Returns "" for empty
// strings (hosted endpoints) and for unparseable URLs (validation should
// have caught these earlier; we fail silently rather than block the warn
// path on a parse error).
func extractHost(baseURL string) string {
	if baseURL == "" {
		return ""
	}
	u, err := url.Parse(baseURL)
	if err != nil {
		return ""
	}
	return u.Hostname() // strips port, lowercases scheme but preserves host case
}

// hostMatchesAllowlist tests host against each allowlist entry using exact
// + "*.suffix" wildcard semantics. Mirrors policy.MatchDomain.
func hostMatchesAllowlist(host string, allowlist []string) bool {
	hostLower := strings.ToLower(host)
	for _, entry := range allowlist {
		if matchAllowlistEntry(entry, hostLower) {
			return true
		}
	}
	return false
}

// matchAllowlistEntry handles a single pattern → host match. Patterns are
// either exact ("api.anthropic.com") or wildcard ("*.openai.com"). Wildcard
// matches subdomains only — *.openai.com matches api.openai.com but NOT
// openai.com itself, matching the Envoy Lua filter at egress-time.
func matchAllowlistEntry(pattern, hostLower string) bool {
	pattern = strings.ToLower(pattern)
	if !strings.HasPrefix(pattern, "*.") {
		return pattern == hostLower
	}
	suffix := pattern[1:] // ".openai.com"
	return strings.HasSuffix(hostLower, suffix) && hostLower != suffix[1:]
}
