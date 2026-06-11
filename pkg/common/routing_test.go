package common

import (
	"encoding/json"
	"testing"

	"github.com/cruxdigital-llc/conga-line/pkg/channels"
	_ "github.com/cruxdigital-llc/conga-line/pkg/channels/slack"
	"github.com/cruxdigital-llc/conga-line/pkg/provider"
	_ "github.com/cruxdigital-llc/conga-line/pkg/runtime/openclaw"
)

func TestGenerateRoutingJSON(t *testing.T) {
	agents := []provider.AgentConfig{
		{Name: "myagent", Type: provider.AgentTypeUser, Channels: []channels.ChannelBinding{{Platform: "slack", ID: "U0123456789"}}, GatewayPort: 18789},
		{Name: "leadership", Type: provider.AgentTypeTeam, Channels: []channels.ChannelBinding{{Platform: "slack", ID: "C9876543210"}}, GatewayPort: 18790},
	}

	data, err := GenerateRoutingJSON(agents, nil)
	if err != nil {
		t.Fatalf("GenerateRoutingJSON() error: %v", err)
	}

	var cfg RoutingConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("failed to parse output: %v", err)
	}

	// The router delivers over the Docker network to the container-internal
	// gateway port (BaseGatewayPort), regardless of each agent's host-side
	// GatewayPort. leadership's host port is 18790, but its route must still
	// target :18789.
	if got := cfg.Members["U0123456789"]; got != "http://conga-myagent:18789/slack/events" {
		t.Errorf("member route = %q, want http://conga-myagent:18789/slack/events", got)
	}
	if got := cfg.Channels["C9876543210"]; got != "http://conga-leadership:18789/slack/events" {
		t.Errorf("channel route = %q, want http://conga-leadership:18789/slack/events", got)
	}
}

func TestGenerateRoutingJSON_PausedExcluded(t *testing.T) {
	agents := []provider.AgentConfig{
		{Name: "myagent", Type: provider.AgentTypeUser, Channels: []channels.ChannelBinding{{Platform: "slack", ID: "U0123456789"}}, GatewayPort: 18789},
		{Name: "paused-user", Type: provider.AgentTypeUser, Channels: []channels.ChannelBinding{{Platform: "slack", ID: "U9999999999"}}, Paused: true, GatewayPort: 18790},
		{Name: "leadership", Type: provider.AgentTypeTeam, Channels: []channels.ChannelBinding{{Platform: "slack", ID: "C9876543210"}}, GatewayPort: 18791},
		{Name: "paused-team", Type: provider.AgentTypeTeam, Channels: []channels.ChannelBinding{{Platform: "slack", ID: "C0000000000"}}, Paused: true, GatewayPort: 18792},
	}

	data, err := GenerateRoutingJSON(agents, nil)
	if err != nil {
		t.Fatalf("GenerateRoutingJSON() error: %v", err)
	}

	var cfg RoutingConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("failed to parse output: %v", err)
	}

	if len(cfg.Members) != 1 {
		t.Errorf("expected 1 member, got %d", len(cfg.Members))
	}
	if len(cfg.Channels) != 1 {
		t.Errorf("expected 1 channel, got %d", len(cfg.Channels))
	}
	if _, ok := cfg.Members["U9999999999"]; ok {
		t.Error("paused user should not be in routing")
	}
	if _, ok := cfg.Channels["C0000000000"]; ok {
		t.Error("paused team should not be in routing")
	}
}

func TestGenerateRoutingJSON_Empty(t *testing.T) {
	data, err := GenerateRoutingJSON(nil, nil)
	if err != nil {
		t.Fatalf("GenerateRoutingJSON(nil) error: %v", err)
	}

	var cfg RoutingConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("failed to parse output: %v", err)
	}

	if len(cfg.Members) != 0 || len(cfg.Channels) != 0 {
		t.Errorf("expected empty routing, got %d members, %d channels", len(cfg.Members), len(cfg.Channels))
	}
}

func TestGenerateRoutingJSON_GatewayOnly(t *testing.T) {
	agents := []provider.AgentConfig{
		{Name: "myagent", Type: provider.AgentTypeUser, GatewayPort: 18789}, // no channels
	}

	data, err := GenerateRoutingJSON(agents, nil)
	if err != nil {
		t.Fatalf("GenerateRoutingJSON() error: %v", err)
	}

	var cfg RoutingConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("failed to parse output: %v", err)
	}

	if len(cfg.Members) != 0 || len(cfg.Channels) != 0 {
		t.Errorf("expected empty routing for gateway-only agent, got %d members, %d channels", len(cfg.Members), len(cfg.Channels))
	}
}

func TestGenerateRoutingJSON_MixedRuntimes(t *testing.T) {
	agents := []provider.AgentConfig{
		{Name: "ocagent", Type: provider.AgentTypeUser, Runtime: "openclaw",
			Channels: []channels.ChannelBinding{{Platform: "slack", ID: "U0001111111"}}, GatewayPort: 18789},
		{Name: "hermes1", Type: provider.AgentTypeUser, Runtime: "hermes",
			Channels: []channels.ChannelBinding{{Platform: "slack", ID: "U0002222222"}}, GatewayPort: 18790},
	}

	resolver := func(agentRuntime, platform string) WebhookTarget {
		if agentRuntime == "hermes" {
			return WebhookTarget{Port: 8644, Path: "/webhooks/" + platform}
		}
		return WebhookTarget{Path: "/slack/events"}
	}

	data, err := GenerateRoutingJSON(agents, resolver)
	if err != nil {
		t.Fatalf("GenerateRoutingJSON() error: %v", err)
	}

	var cfg RoutingConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("failed to parse output: %v", err)
	}

	// OpenClaw agent should get /slack/events
	if got := cfg.Members["U0001111111"]; got != "http://conga-ocagent:18789/slack/events" {
		t.Errorf("OpenClaw route = %q, want /slack/events path", got)
	}
	// Hermes agent should get port 8644 + /webhooks/slack
	if got := cfg.Members["U0002222222"]; got != "http://conga-hermes1:8644/webhooks/slack" {
		t.Errorf("Hermes route = %q, want http://conga-hermes1:8644/webhooks/slack", got)
	}
}

// TestGenerateRoutingJSON_ZeroPortFallback locks in the contract that a
// resolver returning Port == 0 (e.g. the local provider's openclaw branch,
// where rt.WebhookPort() is 0) falls through to BaseGatewayPort — NOT the
// agent's host-side GatewayPort. The host port (18790 here) must never reach
// the inter-container URL.
func TestGenerateRoutingJSON_ZeroPortFallback(t *testing.T) {
	agents := []provider.AgentConfig{
		{Name: "ocagent", Type: provider.AgentTypeUser, Runtime: "openclaw",
			Channels: []channels.ChannelBinding{{Platform: "slack", ID: "U0003333333"}}, GatewayPort: 18790},
	}

	// Resolver mirrors the local provider: openclaw returns Port 0 (use default).
	resolver := func(agentRuntime, platform string) WebhookTarget {
		return WebhookTarget{Port: 0, Path: "/slack/events"}
	}

	data, err := GenerateRoutingJSON(agents, resolver)
	if err != nil {
		t.Fatalf("GenerateRoutingJSON() error: %v", err)
	}

	var cfg RoutingConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("failed to parse output: %v", err)
	}

	if got := cfg.Members["U0003333333"]; got != "http://conga-ocagent:18789/slack/events" {
		t.Errorf("zero-port route = %q, want http://conga-ocagent:18789/slack/events (BaseGatewayPort, not host port 18790)", got)
	}
}

// TestGenerateRoutingJSON_Loopback locks in the host-networking router topology
// (specs/2026-06-11_bugfix_router-host-networking): a resolver returning
// Loopback delivers to 127.0.0.1:<agent host GatewayPort>, NOT to the
// Docker-network name conga-<agent>:BaseGatewayPort. Each agent's distinct host
// port must appear in its URL.
func TestGenerateRoutingJSON_Loopback(t *testing.T) {
	agents := []provider.AgentConfig{
		{Name: "myagent", Type: provider.AgentTypeUser, Channels: []channels.ChannelBinding{{Platform: "slack", ID: "U0123456789"}}, GatewayPort: 18789},
		{Name: "leadership", Type: provider.AgentTypeTeam, Channels: []channels.ChannelBinding{{Platform: "slack", ID: "C9876543210"}}, GatewayPort: 18791},
	}

	resolver := func(agentRuntime, platform string) WebhookTarget {
		return WebhookTarget{Path: "/slack/events", Loopback: true}
	}

	data, err := GenerateRoutingJSON(agents, resolver)
	if err != nil {
		t.Fatalf("GenerateRoutingJSON() error: %v", err)
	}

	var cfg RoutingConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("failed to parse output: %v", err)
	}

	if got := cfg.Members["U0123456789"]; got != "http://127.0.0.1:18789/slack/events" {
		t.Errorf("member loopback route = %q, want http://127.0.0.1:18789/slack/events", got)
	}
	// leadership's host port (18791) must be used — not BaseGatewayPort.
	if got := cfg.Channels["C9876543210"]; got != "http://127.0.0.1:18791/slack/events" {
		t.Errorf("channel loopback route = %q, want http://127.0.0.1:18791/slack/events (host GatewayPort)", got)
	}
}

// TestLoopbackWebhookResolver verifies the shared resolver used by all providers
// emits loopback targets with the runtime-aware path (OpenClaw → the channel's
// default /slack/events).
func TestLoopbackWebhookResolver(t *testing.T) {
	agents := []provider.AgentConfig{
		{Name: "ocagent", Type: provider.AgentTypeUser, Runtime: "openclaw",
			Channels: []channels.ChannelBinding{{Platform: "slack", ID: "U0004444444"}}, GatewayPort: 18790},
	}

	data, err := GenerateRoutingJSON(agents, LoopbackWebhookResolver(""))
	if err != nil {
		t.Fatalf("GenerateRoutingJSON() error: %v", err)
	}

	var cfg RoutingConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("failed to parse output: %v", err)
	}

	if got := cfg.Members["U0004444444"]; got != "http://127.0.0.1:18790/slack/events" {
		t.Errorf("loopback resolver route = %q, want http://127.0.0.1:18790/slack/events", got)
	}
}
