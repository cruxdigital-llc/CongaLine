package scripts

import (
	"strings"
	"testing"
	"text/template"
)

func TestDeployEgressScriptTemplateRender(t *testing.T) {
	tmpl, err := template.New("deploy-egress").Parse(DeployEgressScript)
	if err != nil {
		t.Fatalf("failed to parse deploy-egress template: %v", err)
	}

	data := struct {
		AgentName        string
		Mode             string
		PolicyContent    string
		EnvoyConfig      string
		ProxyBootstrapJS string
		ManifestJSON     string
	}{
		AgentName: "testagent",
		Mode:      "enforce",
		PolicyContent: `apiVersion: conga.dev/v1alpha1
egress:
  allowed_domains:
    - api.anthropic.com
    - "*.slack.com"
  mode: enforce`,
		EnvoyConfig:      "static_resources:\n  listeners:\n    - name: main\n",
		ProxyBootstrapJS: "const http = require('http');\n",
		ManifestJSON:     `{"schema_version":1,"policy_hash":"abc","egress":{"mode":"enforce"}}`,
	}

	var buf strings.Builder
	if err := tmpl.Execute(&buf, data); err != nil {
		t.Fatalf("failed to execute deploy-egress template: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "testagent") {
		t.Error("expected agent name in rendered output")
	}
	if !strings.Contains(output, "enforce") {
		t.Error("expected mode in rendered output")
	}
	if !strings.Contains(output, "api.anthropic.com") {
		t.Error("expected policy content in rendered output")
	}
	if !strings.Contains(output, "static_resources") {
		t.Error("expected envoy config in rendered output")
	}
	if !strings.Contains(output, "set -euo pipefail") {
		t.Error("expected bash strict mode in rendered output")
	}
	if !strings.Contains(output, `"policy_hash":"abc"`) {
		t.Error("expected manifest JSON in rendered output")
	}
	// The script uses bash-level $AGENT_NAME, so the literal path is
	// egress-$AGENT_NAME.manifest.json until bash expands it at runtime.
	if !strings.Contains(output, "egress-$AGENT_NAME.manifest.json") {
		t.Error("expected manifest file path in rendered output")
	}
}

func TestDeployEgressScriptValidateModeAppliesIptables(t *testing.T) {
	tmpl, err := template.New("deploy-egress").Parse(DeployEgressScript)
	if err != nil {
		t.Fatalf("failed to parse deploy-egress template: %v", err)
	}

	data := struct {
		AgentName        string
		Mode             string
		PolicyContent    string
		EnvoyConfig      string
		ProxyBootstrapJS string
		ManifestJSON     string
	}{
		AgentName: "testagent",
		Mode:      "validate",
		PolicyContent: `apiVersion: conga.dev/v1alpha1
egress:
  allowed_domains:
    - api.anthropic.com
  mode: validate`,
		EnvoyConfig:      "static_resources:\n  listeners:\n    - name: main\n",
		ProxyBootstrapJS: "const http = require('http');\n",
		ManifestJSON:     `{"schema_version":1,"egress":{"mode":"validate"}}`,
	}

	var buf strings.Builder
	if err := tmpl.Execute(&buf, data); err != nil {
		t.Fatalf("failed to execute deploy-egress template: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, `EGRESS_MODE="validate"`) {
		t.Error("expected EGRESS_MODE=validate in rendered output")
	}
	// iptables rules are always applied (even in validate mode) to force all traffic
	// through the proxy. The proxy itself handles validate vs enforce behavior.
	if !strings.Contains(output, "iptables -I DOCKER-USER") {
		t.Error("expected iptables rules in validate mode output")
	}
	// Verify cleanup section (iptables -D) is NOT guarded — it should always run
	if !strings.Contains(output, "iptables -D DOCKER-USER") {
		t.Error("expected iptables cleanup rules (iptables -D) in all modes")
	}
}

func TestRefreshUserScriptTemplateRender(t *testing.T) {
	tmpl, err := template.New("refresh-user").Parse(RefreshUserScript)
	if err != nil {
		t.Fatalf("failed to parse refresh-user template: %v", err)
	}

	data := struct {
		AWSRegion string
		AgentName string
	}{
		AWSRegion: "us-east-1",
		AgentName: "testagent",
	}

	var buf strings.Builder
	if err := tmpl.Execute(&buf, data); err != nil {
		t.Fatalf("failed to execute refresh-user template: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "testagent") {
		t.Error("expected agent name in rendered output")
	}
	if !strings.Contains(output, "us-east-1") {
		t.Error("expected AWS region in rendered output")
	}

	// v2026.5.18 compat: the rebuilt systemd unit must seed @openclaw/slack
	// before the persistent container starts (the plugin is no longer
	// bundled in the OpenClaw image starting v2026.5.x).
	want := "openclaw plugins install @openclaw/slack"
	if !strings.Contains(output, want) {
		t.Errorf("missing %q ExecStartPre — fresh refresh on v2026.5.18+ leaves channel WARNing", want)
	}
	// Regression: --yes is not a valid plugins-install flag and causes
	// the systemd ExecStartPre to silently fail.
	if strings.Contains(output, "plugins install @openclaw/slack --yes") {
		t.Error(`plugins install line still passes --yes; v2026.5.18 rejects it as unrecognized`)
	}
}

// assertOpenClawV5Shape exercises the assertions a rendered OpenClaw config
// heredoc must satisfy on v2026.5.18+. Shared across add-user / add-team
// tests because the gateway / streaming / update / plugin-install shape is
// identical between the two agent types. Type-specific assertions
// (allowFrom vs channels) live in the per-test bodies.
func assertOpenClawV5Shape(t *testing.T, rendered string) {
	t.Helper()

	// Positive: every v2026.5.18-mandatory shape must be present.
	positives := map[string]string{
		"GATEWAY_TOKEN generation":    "GATEWAY_TOKEN=$(openssl rand -hex 32)",
		"gateway.auth.token block":    `"auth": { "mode": "token", "token": "$GATEWAY_TOKEN" }`,
		"streaming object form":       `"streaming": { "mode": "partial", "nativeTransport": true }`,
		"update.checkOnStart false":   `"update": { "checkOnStart": false, "auto": { "enabled": false } }`,
		"plugin install ExecStartPre": "openclaw plugins install @openclaw/slack",
	}
	for desc, want := range positives {
		if !strings.Contains(rendered, want) {
			t.Errorf("missing %s: want %q in rendered template", desc, want)
		}
	}

	// Negative: every legacy v2026.3.x shape must be absent. These produced
	// real production agent crashes during the v2026.5.18 rollout and the
	// fixes are what this PR is about — assert they can't regress.
	negatives := map[string]string{
		`legacy streaming string form`: `"streaming": "partial"`,
		`legacy nativeStreaming key`:   `"nativeStreaming"`,
		`legacy gateway.remote block`:  `"remote": { "url"`,
		`legacy --yes install flag`:    "plugins install @openclaw/slack --yes",
		`gateway.mode=remote`:          `"mode": "remote"`,
	}
	for desc, banned := range negatives {
		if strings.Contains(rendered, banned) {
			t.Errorf("legacy shape still present (%s): %q must not appear in rendered template", desc, banned)
		}
	}
}

func TestAddUserScriptTemplateRender(t *testing.T) {
	tmpl, err := template.New("add-user").Parse(AddUserScript)
	if err != nil {
		t.Fatalf("failed to parse add-user template: %v", err)
	}

	data := struct {
		AgentName, SlackMemberID, SlackChannel, AWSRegion, StateBucket string
		GatewayPort                                                    int
		EnvoyConfig, EgressMode, ProxyBootstrapJS                      string
	}{
		AgentName:        "testuser",
		SlackMemberID:    "U1234",
		AWSRegion:        "us-east-1",
		StateBucket:      "my-bucket",
		GatewayPort:      18789,
		EnvoyConfig:      "static_resources:\n  listeners:\n    - port: 3128\n",
		EgressMode:       "enforce",
		ProxyBootstrapJS: "const http = require('http');\n",
	}

	var buf strings.Builder
	if err := tmpl.Execute(&buf, data); err != nil {
		t.Fatalf("failed to execute add-user template: %v", err)
	}

	output := buf.String()
	checks := map[string]string{
		"agent name":            "testuser",
		"egress mode":           `EGRESS_MODE="enforce"`,
		"envoy config":          "static_resources",
		"proxy bootstrap":       "require('http')",
		"HTTPS_PROXY":           "HTTPS_PROXY=http://",
		"proxy bootstrap mount": "$BOOTSTRAP_PATH:/opt/proxy-bootstrap.js",
		"iptables rules":        "iptables -I DOCKER-USER",
		"egress proxy run":      "conga-egress-proxy",
	}
	for desc, want := range checks {
		if !strings.Contains(output, want) {
			t.Errorf("expected %s (%q) in rendered output", desc, want)
		}
	}

	// v2026.5.18-mandatory shape (shared with add-team).
	assertOpenClawV5Shape(t, output)

	// User-agent-specific: DM allowlist tied to the slack member ID.
	if !strings.Contains(output, `"allowFrom": ["$SLACK_MEMBER_ID"]`) {
		t.Error("user agent must emit allowFrom bound to SLACK_MEMBER_ID")
	}
}

func TestAddTeamScriptTemplateRender(t *testing.T) {
	tmpl, err := template.New("add-team").Parse(AddTeamScript)
	if err != nil {
		t.Fatalf("failed to parse add-team template: %v", err)
	}

	data := struct {
		AgentName, SlackMemberID, SlackChannel, AWSRegion, StateBucket string
		GatewayPort                                                    int
		EnvoyConfig, EgressMode, ProxyBootstrapJS                      string
	}{
		AgentName:        "testteam",
		SlackChannel:     "C5678",
		AWSRegion:        "us-west-2",
		StateBucket:      "team-bucket",
		GatewayPort:      18790,
		EnvoyConfig:      "static_resources:\n  listeners:\n    - port: 3128\n",
		EgressMode:       "enforce",
		ProxyBootstrapJS: "const http = require('http');\n",
	}

	var buf strings.Builder
	if err := tmpl.Execute(&buf, data); err != nil {
		t.Fatalf("failed to execute add-team template: %v", err)
	}

	output := buf.String()
	checks := map[string]string{
		"agent name":       "testteam",
		"egress mode":      `EGRESS_MODE="enforce"`,
		"envoy config":     "static_resources",
		"HTTPS_PROXY":      "HTTPS_PROXY=http://",
		"iptables rules":   "iptables -I DOCKER-USER",
		"egress proxy run": "conga-egress-proxy",
		"channel routing":  "channels",
	}
	for desc, want := range checks {
		if !strings.Contains(output, want) {
			t.Errorf("expected %s (%q) in rendered output", desc, want)
		}
	}

	// v2026.5.18-mandatory shape (shared with add-user).
	assertOpenClawV5Shape(t, output)

	// Team-agent-specific: per-channel binding must use the new "enabled"
	// key. v2026.5.x rejects the legacy "allow" key with
	//   channels.slack.channels.<id>: must NOT have additional properties
	if !strings.Contains(output, `"$SLACK_CHANNEL": { "enabled": true, "requireMention": false }`) {
		t.Error(`team agent must emit channels.<id>.{enabled:true,requireMention:false} (the v2026.5.x canonical shape)`)
	}
	if strings.Contains(output, `"$SLACK_CHANNEL": { "allow": true`) {
		t.Error(`team agent still emits legacy channels.<id>.allow:true — v2026.5.x rejects it`)
	}
}
