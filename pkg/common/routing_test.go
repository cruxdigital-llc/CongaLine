package common

import (
	"encoding/json"
	"testing"

	"github.com/cruxdigital-llc/conga-line/pkg/channels"
	_ "github.com/cruxdigital-llc/conga-line/pkg/channels/slack"
	"github.com/cruxdigital-llc/conga-line/pkg/provider"
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

	if got := cfg.Members["U0123456789"]; got != "http://conga-myagent:18789/slack/events" {
		t.Errorf("member route = %q, want http://conga-myagent:18789/slack/events", got)
	}
	if got := cfg.Channels["C9876543210"]; got != "http://conga-leadership:18790/slack/events" {
		t.Errorf("channel route = %q, want http://conga-leadership:18790/slack/events", got)
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

func TestGenerateRoutingJSON_TeamAgentSingleChannel_ByteIdentical(t *testing.T) {
	// Regression: single-binding team agent must produce byte-identical
	// output to the pre-multi-binding implementation. This is the blast-radius
	// guarantee for operators upgrading without opting into multi-binding.
	agents := []provider.AgentConfig{
		{
			Name: "leadership", Type: provider.AgentTypeTeam, GatewayPort: 18790,
			Channels: []channels.ChannelBinding{{Platform: "slack", ID: "C9876543210"}},
		},
	}

	got, err := GenerateRoutingJSON(agents, nil)
	if err != nil {
		t.Fatalf("GenerateRoutingJSON() error: %v", err)
	}

	want := `{
  "channels": {
    "C9876543210": "http://conga-leadership:18790/slack/events"
  },
  "members": {}
}`
	if string(got) != want {
		t.Errorf("single-binding output drifted.\ngot:\n%s\nwant:\n%s", got, want)
	}
}

func TestGenerateRoutingJSON_TeamAgentMultipleChannels(t *testing.T) {
	// Core of the feature: one team agent bound to three channels should
	// produce three entries in cfg.Channels, all pointing at the same URL.
	agents := []provider.AgentConfig{
		{
			Name: "contracts", Type: provider.AgentTypeTeam, GatewayPort: 18791,
			Channels: []channels.ChannelBinding{
				{Platform: "slack", ID: "C1", Label: "#legal"},
				{Platform: "slack", ID: "C2", Label: "#sales"},
				{Platform: "slack", ID: "C3"},
			},
		},
	}

	data, err := GenerateRoutingJSON(agents, nil)
	if err != nil {
		t.Fatalf("GenerateRoutingJSON() error: %v", err)
	}

	var cfg RoutingConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("failed to parse output: %v", err)
	}

	if len(cfg.Channels) != 3 {
		t.Fatalf("want 3 channel routes, got %d: %+v", len(cfg.Channels), cfg.Channels)
	}
	wantURL := "http://conga-contracts:18791/slack/events"
	for _, id := range []string{"C1", "C2", "C3"} {
		if got := cfg.Channels[id]; got != wantURL {
			t.Errorf("channels[%q] = %q, want %q", id, got, wantURL)
		}
	}
}

func TestGenerateRoutingJSON_TeamAgentMixedWithOtherAgents(t *testing.T) {
	// Multi-binding team agent alongside a user agent and a single-binding
	// team agent — ensure no cross-contamination.
	agents := []provider.AgentConfig{
		{
			Name: "ada", Type: provider.AgentTypeUser, GatewayPort: 18789,
			Channels: []channels.ChannelBinding{{Platform: "slack", ID: "U0000000001"}},
		},
		{
			Name: "contracts", Type: provider.AgentTypeTeam, GatewayPort: 18791,
			Channels: []channels.ChannelBinding{
				{Platform: "slack", ID: "C1"},
				{Platform: "slack", ID: "C2"},
			},
		},
		{
			Name: "leadership", Type: provider.AgentTypeTeam, GatewayPort: 18790,
			Channels: []channels.ChannelBinding{{Platform: "slack", ID: "C99"}},
		},
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
		t.Errorf("want 1 member, got %d", len(cfg.Members))
	}
	if len(cfg.Channels) != 3 {
		t.Errorf("want 3 channel routes, got %d", len(cfg.Channels))
	}
	if cfg.Members["U0000000001"] != "http://conga-ada:18789/slack/events" {
		t.Errorf("ada user route wrong: %q", cfg.Members["U0000000001"])
	}
	if cfg.Channels["C1"] != "http://conga-contracts:18791/slack/events" {
		t.Errorf("contracts C1 route wrong: %q", cfg.Channels["C1"])
	}
	if cfg.Channels["C2"] != "http://conga-contracts:18791/slack/events" {
		t.Errorf("contracts C2 route wrong: %q", cfg.Channels["C2"])
	}
	if cfg.Channels["C99"] != "http://conga-leadership:18790/slack/events" {
		t.Errorf("leadership route wrong: %q", cfg.Channels["C99"])
	}
}

func TestFindMultiBindingAgents_None(t *testing.T) {
	agents := []provider.AgentConfig{
		{Name: "ada", Channels: []channels.ChannelBinding{{Platform: "slack", ID: "U1"}}},
		{Name: "leadership", Channels: []channels.ChannelBinding{{Platform: "slack", ID: "C1"}}},
	}
	if got := FindMultiBindingAgents(agents); len(got) != 0 {
		t.Errorf("want empty reports, got %+v", got)
	}
}

func TestFindMultiBindingAgents_FindsTeam(t *testing.T) {
	agents := []provider.AgentConfig{
		{Name: "ada", Channels: []channels.ChannelBinding{{Platform: "slack", ID: "U1"}}},
		{
			Name: "contracts",
			Channels: []channels.ChannelBinding{
				{Platform: "slack", ID: "C1"},
				{Platform: "slack", ID: "C2"},
				{Platform: "slack", ID: "C3"},
			},
		},
	}
	reports := FindMultiBindingAgents(agents)
	if len(reports) != 1 {
		t.Fatalf("want 1 report, got %d: %+v", len(reports), reports)
	}
	r := reports[0]
	if r.AgentName != "contracts" || r.Platform != "slack" {
		t.Errorf("report identity = (%q, %q), want (contracts, slack)", r.AgentName, r.Platform)
	}
	if len(r.ChannelIDs) != 3 {
		t.Errorf("want 3 channel ids, got %d", len(r.ChannelIDs))
	}
	wantIDs := []string{"C1", "C2", "C3"}
	for i, id := range r.ChannelIDs {
		if id != wantIDs[i] {
			t.Errorf("ChannelIDs[%d] = %q, want %q (preserve insertion order)", i, id, wantIDs[i])
		}
	}
}

func TestFindMultiBindingAgents_ExcludesPaused(t *testing.T) {
	agents := []provider.AgentConfig{
		{
			Name: "contracts", Paused: true,
			Channels: []channels.ChannelBinding{
				{Platform: "slack", ID: "C1"},
				{Platform: "slack", ID: "C2"},
			},
		},
	}
	if got := FindMultiBindingAgents(agents); len(got) != 0 {
		t.Errorf("paused agent should not appear in reports, got %+v", got)
	}
}

func TestFindMultiBindingAgents_StableOrder(t *testing.T) {
	// Two multi-binding agents — reports should be ordered by agent name ascending.
	agents := []provider.AgentConfig{
		{
			Name: "zebra",
			Channels: []channels.ChannelBinding{
				{Platform: "slack", ID: "Cz1"},
				{Platform: "slack", ID: "Cz2"},
			},
		},
		{
			Name: "alpha",
			Channels: []channels.ChannelBinding{
				{Platform: "slack", ID: "Ca1"},
				{Platform: "slack", ID: "Ca2"},
			},
		},
	}
	reports := FindMultiBindingAgents(agents)
	if len(reports) != 2 || reports[0].AgentName != "alpha" || reports[1].AgentName != "zebra" {
		t.Errorf("want [alpha, zebra], got %+v", reports)
	}
}

func TestMultiBindingReport_LogLine(t *testing.T) {
	r := MultiBindingReport{
		AgentName:  "contracts",
		Platform:   "slack",
		ChannelIDs: []string{"C1", "C2", "C3"},
	}
	want := "[router-config] multi-binding agent: name=contracts platform=slack bindings=3 channel_ids=[C1,C2,C3]"
	if got := r.LogLine(); got != want {
		t.Errorf("LogLine()\ngot:  %s\nwant: %s", got, want)
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
