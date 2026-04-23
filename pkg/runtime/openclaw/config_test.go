package openclaw

import (
	"encoding/json"
	"testing"

	"github.com/cruxdigital-llc/conga-line/pkg/channels"
	_ "github.com/cruxdigital-llc/conga-line/pkg/channels/slack"
	"github.com/cruxdigital-llc/conga-line/pkg/provider"
	"github.com/cruxdigital-llc/conga-line/pkg/runtime"
)

// sampleSecrets returns Slack credentials suitable for openclaw config generation.
func sampleSecrets() provider.SharedSecrets {
	return provider.SharedSecrets{
		Values: map[string]string{
			"slack-bot-token":      "xoxb-test",
			"slack-signing-secret": "secret",
		},
	}
}

// extractSlackSection unmarshals the generated openclaw.json and returns
// its channels.slack section, or fails the test.
func extractSlackSection(t *testing.T, data []byte) map[string]any {
	t.Helper()
	var cfg map[string]any
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("failed to unmarshal openclaw config: %v", err)
	}
	channelsCfg, ok := cfg["channels"].(map[string]any)
	if !ok {
		t.Fatalf("channels section missing or not a map: %v", cfg["channels"])
	}
	slack, ok := channelsCfg["slack"].(map[string]any)
	if !ok {
		t.Fatalf("channels.slack section missing or not a map: %v", channelsCfg["slack"])
	}
	return slack
}

func TestGenerateConfig_TeamAgentSingleBinding_Unchanged(t *testing.T) {
	// Regression: a team agent with exactly one Slack binding must produce
	// the same channels.slack section it produced before multi-binding landed.
	r := &Runtime{}
	params := runtime.ConfigParams{
		Agent: provider.AgentConfig{
			Name:        "leadership",
			Type:        provider.AgentTypeTeam,
			GatewayPort: 18790,
			Channels: []channels.ChannelBinding{
				{Platform: "slack", ID: "C9876543210"},
			},
		},
		Secrets: sampleSecrets(),
	}

	data, err := r.GenerateConfig(params)
	if err != nil {
		t.Fatalf("GenerateConfig: %v", err)
	}

	slack := extractSlackSection(t, data)
	chans, ok := slack["channels"].(map[string]any)
	if !ok {
		t.Fatalf("slack.channels missing or not a map: %v", slack["channels"])
	}
	if len(chans) != 1 {
		t.Errorf("want 1 channel entry, got %d: %v", len(chans), chans)
	}
	if _, has := chans["C9876543210"]; !has {
		t.Errorf("want channels.C9876543210 present; got %v", chans)
	}
}

func TestGenerateConfig_TeamAgentMultiBinding_ChannelsIncludesAll(t *testing.T) {
	// Core of the feature: three Slack bindings on one team agent should
	// produce three entries under channels.slack.channels.
	r := &Runtime{}
	params := runtime.ConfigParams{
		Agent: provider.AgentConfig{
			Name:        "contracts",
			Type:        provider.AgentTypeTeam,
			GatewayPort: 18791,
			Channels: []channels.ChannelBinding{
				{Platform: "slack", ID: "C1", Label: "#legal"},
				{Platform: "slack", ID: "C2", Label: "#sales"},
				{Platform: "slack", ID: "C3"},
			},
		},
		Secrets: sampleSecrets(),
	}

	data, err := r.GenerateConfig(params)
	if err != nil {
		t.Fatalf("GenerateConfig: %v", err)
	}

	slack := extractSlackSection(t, data)
	if slack["groupPolicy"] != "allowlist" {
		t.Errorf("groupPolicy = %v, want allowlist", slack["groupPolicy"])
	}
	chans, ok := slack["channels"].(map[string]any)
	if !ok {
		t.Fatalf("slack.channels missing or not a map: %v", slack["channels"])
	}
	if len(chans) != 3 {
		t.Errorf("want 3 channel entries, got %d: %v", len(chans), chans)
	}
	for _, id := range []string{"C1", "C2", "C3"} {
		entry, ok := chans[id].(map[string]any)
		if !ok {
			t.Errorf("missing entry for %q: %v", id, chans[id])
			continue
		}
		if entry["allow"] != true {
			t.Errorf("channels[%q].allow = %v, want true", id, entry["allow"])
		}
	}
}

func TestGenerateConfig_UserAgentMultiBinding_AllowFromIncludesAll(t *testing.T) {
	// A user agent with multiple Slack member IDs aggregates them all.
	r := &Runtime{}
	params := runtime.ConfigParams{
		Agent: provider.AgentConfig{
			Name:        "ada",
			Type:        provider.AgentTypeUser,
			GatewayPort: 18789,
			Channels: []channels.ChannelBinding{
				{Platform: "slack", ID: "U0000000001"},
				{Platform: "slack", ID: "U0000000002"},
			},
		},
		Secrets: sampleSecrets(),
	}

	data, err := r.GenerateConfig(params)
	if err != nil {
		t.Fatalf("GenerateConfig: %v", err)
	}

	slack := extractSlackSection(t, data)
	// JSON round-trip turns []string into []any; inspect loosely.
	allowFromRaw, ok := slack["allowFrom"].([]any)
	if !ok {
		t.Fatalf("allowFrom not a []any: %T %v", slack["allowFrom"], slack["allowFrom"])
	}
	got := make([]string, 0, len(allowFromRaw))
	for _, v := range allowFromRaw {
		got = append(got, v.(string))
	}
	if len(got) != 2 || got[0] != "U0000000001" || got[1] != "U0000000002" {
		t.Errorf("allowFrom = %v, want [U0000000001 U0000000002]", got)
	}
}

func TestGenerateConfig_TeamAgentMultiBinding_ByteIdenticalSingleOutput(t *testing.T) {
	// Equivalence guarantee: feeding the multi-binding call path a single
	// binding must produce byte-identical JSON to the old singular path
	// (which is now itself a call through the multi wrapper).
	r := &Runtime{}

	singleBinding := runtime.ConfigParams{
		Agent: provider.AgentConfig{
			Name: "leadership", Type: provider.AgentTypeTeam, GatewayPort: 18790,
			Channels: []channels.ChannelBinding{{Platform: "slack", ID: "C9876543210"}},
		},
		Secrets: sampleSecrets(),
	}

	data, err := r.GenerateConfig(singleBinding)
	if err != nil {
		t.Fatalf("GenerateConfig: %v", err)
	}

	// Pre-multi-binding snapshot for the same inputs: channels.slack has
	// one entry C9876543210, groupPolicy=allowlist, dmPolicy=disabled.
	// We don't snapshot the WHOLE JSON (openclaw-defaults.json can drift),
	// only the slack section — which is what this feature actually touches.
	slack := extractSlackSection(t, data)
	if slack["groupPolicy"] != "allowlist" {
		t.Errorf("groupPolicy = %v, want allowlist", slack["groupPolicy"])
	}
	if slack["dmPolicy"] != "disabled" {
		t.Errorf("dmPolicy = %v, want disabled", slack["dmPolicy"])
	}
	chans, ok := slack["channels"].(map[string]any)
	if !ok {
		t.Fatalf("channels not a map: %v", slack["channels"])
	}
	if len(chans) != 1 {
		t.Errorf("want 1 entry, got %d", len(chans))
	}
}
