package policy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadEgressPolicyMissingFile(t *testing.T) {
	ep, err := LoadEgressPolicy("/nonexistent", "agent1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ep != nil {
		t.Error("expected nil egress policy for missing file")
	}
}

func TestLoadEgressPolicyWithMerge(t *testing.T) {
	dir := t.TempDir()
	yaml := `
apiVersion: conga.dev/v1alpha1
egress:
  allowed_domains:
    - api.anthropic.com
    - "*.slack.com"
  mode: enforce
agents:
  myagent:
    egress:
      allowed_domains:
        - api.anthropic.com
        - "*.trello.com"
      mode: enforce
`
	os.WriteFile(filepath.Join(dir, "conga-policy.yaml"), []byte(yaml), 0644)

	ep, err := LoadEgressPolicy(dir, "myagent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ep == nil {
		t.Fatal("expected non-nil egress policy")
	}
	if len(ep.AllowedDomains) != 2 {
		t.Fatalf("expected 2 domains, got %d", len(ep.AllowedDomains))
	}
	if ep.AllowedDomains[1] != "*.trello.com" {
		t.Errorf("expected *.trello.com, got %s", ep.AllowedDomains[1])
	}
}

func TestLoadEgressPolicyNoEgressSection(t *testing.T) {
	dir := t.TempDir()
	yaml := `apiVersion: conga.dev/v1alpha1`
	os.WriteFile(filepath.Join(dir, "conga-policy.yaml"), []byte(yaml), 0644)

	ep, err := LoadEgressPolicy(dir, "agent1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ep != nil {
		t.Error("expected nil egress policy when no egress section")
	}
}

func TestEffectiveAllowedDomains(t *testing.T) {
	e := &EgressPolicy{
		AllowedDomains: []string{"api.anthropic.com", "evil.com", "*.slack.com"},
		BlockedDomains: []string{"evil.com"},
	}
	result := EffectiveAllowedDomains(e)
	if len(result) != 2 {
		t.Fatalf("expected 2 domains, got %d", len(result))
	}
	if result[0] != "api.anthropic.com" {
		t.Errorf("expected api.anthropic.com, got %s", result[0])
	}
	if result[1] != "*.slack.com" {
		t.Errorf("expected *.slack.com, got %s", result[1])
	}
}

func TestEffectiveAllowedDomainsNil(t *testing.T) {
	result := EffectiveAllowedDomains(nil)
	if result != nil {
		t.Error("expected nil for nil policy")
	}
}

func TestEffectiveAllowedDomainsEmpty(t *testing.T) {
	e := &EgressPolicy{AllowedDomains: []string{}}
	result := EffectiveAllowedDomains(e)
	if result != nil {
		t.Error("expected nil for empty allowlist")
	}
}

func TestEffectiveAllowedDomainsCaseInsensitive(t *testing.T) {
	e := &EgressPolicy{
		AllowedDomains: []string{"API.Anthropic.Com", "Evil.Com"},
		BlockedDomains: []string{"evil.com"},
	}
	result := EffectiveAllowedDomains(e)
	if len(result) != 1 {
		t.Fatalf("expected 1 domain, got %d", len(result))
	}
}

func TestEgressProxyName(t *testing.T) {
	if EgressProxyName("myagent") != "conga-egress-myagent" {
		t.Errorf("unexpected proxy name: %s", EgressProxyName("myagent"))
	}
}

func TestGenerateProxyConfAllowlist(t *testing.T) {
	domains := []string{"api.anthropic.com", "*.slack.com", "github.com"}
	result := GenerateProxyConf(domains)

	if !strings.Contains(result, "http_port 3128") {
		t.Error("expected squid http_port directive")
	}
	if !strings.Contains(result, "acl allowed_domains dstdomain") {
		t.Error("expected dstdomain ACL in allowlist mode")
	}
	// *.slack.com should become .slack.com for squid
	if !strings.Contains(result, " .slack.com") {
		t.Error("expected .slack.com (squid wildcard format)")
	}
	if !strings.Contains(result, " api.anthropic.com") {
		t.Error("expected exact domain in ACL")
	}
	if !strings.Contains(result, "http_access deny all") {
		t.Error("expected default deny")
	}
	if !strings.Contains(result, "cache_mem 8 MB") {
		t.Error("expected memory constraint")
	}
}

func TestGenerateProxyConfWildcardDedup(t *testing.T) {
	// Squid 6 rejects both "slack.com" and ".slack.com" in the same ACL.
	// When *.slack.com is present, bare slack.com should be omitted.
	domains := []string{"api.anthropic.com", "slack.com", "*.slack.com"}
	result := GenerateProxyConf(domains)

	if !strings.Contains(result, " .slack.com") {
		t.Error("expected .slack.com wildcard entry")
	}
	// Count occurrences — should appear exactly once (as .slack.com, not also as slack.com)
	if strings.Count(result, "slack.com") != 1 {
		t.Errorf("expected slack.com to appear exactly once, got:\n%s", result)
	}
	if !strings.Contains(result, " api.anthropic.com") {
		t.Error("expected non-overlapping domain to remain")
	}
}

func TestGenerateProxyConfPassthrough(t *testing.T) {
	result := GenerateProxyConf(nil)
	if strings.Contains(result, "dstdomain") {
		t.Error("expected no domain ACL in passthrough mode")
	}
	if !strings.Contains(result, "http_port 3128") {
		t.Error("expected port directive in passthrough mode")
	}
	if !strings.Contains(result, "http_access allow all") {
		t.Error("expected allow all in passthrough mode")
	}
}

func TestGenerateProxyConfEmptySlice(t *testing.T) {
	result := GenerateProxyConf([]string{})
	if strings.Contains(result, "dstdomain") {
		t.Error("expected no domain ACL with empty domains")
	}
}

func TestEgressProxyDockerfile(t *testing.T) {
	df := EgressProxyDockerfile()
	if !strings.Contains(df, "FROM alpine:3.21") {
		t.Error("expected alpine base image")
	}
	if !strings.Contains(df, "squid") {
		t.Error("expected squid installation")
	}
}
