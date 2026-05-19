// Package slack implements the Channel interface for Slack integration.
package slack

import (
	"fmt"
	"regexp"

	"github.com/cruxdigital-llc/conga-line/pkg/channels"
)

func init() {
	channels.Register(&Slack{})
}

// Compile-time assertion: Slack supports multi-binding.
var _ channels.MultiBindingChannel = (*Slack)(nil)

var (
	memberIDPattern  = regexp.MustCompile(`^U[A-Z0-9]{8,12}$`)
	channelIDPattern = regexp.MustCompile(`^C[A-Z0-9]{8,12}$`)
)

// Slack implements the channels.Channel interface.
type Slack struct{}

func (s *Slack) Name() string { return "slack" }

func (s *Slack) ValidateBinding(agentType, id string) error {
	switch agentType {
	case "user":
		if !memberIDPattern.MatchString(id) {
			return fmt.Errorf("invalid Slack member ID %q: must match U + 8-12 alphanumeric chars (e.g., U0123456789)", id)
		}
	case "team":
		if !channelIDPattern.MatchString(id) {
			return fmt.Errorf("invalid Slack channel ID %q: must match C + 8-12 alphanumeric chars (e.g., C0123456789)", id)
		}
	default:
		return fmt.Errorf("unknown agent type %q for slack binding validation", agentType)
	}
	return nil
}

func (s *Slack) SharedSecrets() []channels.SecretDef {
	return []channels.SecretDef{
		{Name: "slack-bot-token", EnvVar: "SLACK_BOT_TOKEN", Prompt: "Slack bot token (xoxb-...)", Required: true},
		{Name: "slack-signing-secret", EnvVar: "SLACK_SIGNING_SECRET", Prompt: "Slack signing secret", Required: true},
		{Name: "slack-app-token", EnvVar: "SLACK_APP_TOKEN", Prompt: "Slack app-level token (xapp-...)", Required: false, RouterOnly: true},
	}
}

func (s *Slack) HasCredentials(sv map[string]string) bool {
	return sv["slack-bot-token"] != "" && sv["slack-signing-secret"] != ""
}

// OpenClawChannelConfig returns the channels.slack config for a single binding.
// Implemented as a thin wrapper around OpenClawChannelConfigMulti so the
// single-binding output is guaranteed byte-identical.
func (s *Slack) OpenClawChannelConfig(agentType string, binding channels.ChannelBinding, sv map[string]string) (map[string]any, error) {
	return s.OpenClawChannelConfigMulti(agentType, []channels.ChannelBinding{binding}, sv)
}

// OpenClawChannelConfigMulti returns the channels.slack config for any number
// of bindings attached to the same agent. For user agents, `allowFrom`
// aggregates every member ID across bindings. For team agents, `channels`
// aggregates every channel ID. An empty bindings slice returns (nil, nil)
// — the caller should omit the channels.slack section entirely.
func (s *Slack) OpenClawChannelConfigMulti(agentType string, bindings []channels.ChannelBinding, sv map[string]string) (map[string]any, error) {
	if len(bindings) == 0 {
		return nil, nil
	}

	cfg := map[string]any{
		"mode":              "http",
		"enabled":           true,
		"botToken":          sv["slack-bot-token"],
		"signingSecret":     sv["slack-signing-secret"],
		"webhookPath":       "/slack/events",
		"userTokenReadOnly": true,
		"streaming":         "partial",
		"nativeStreaming":   true,
	}

	switch agentType {
	case "user":
		cfg["groupPolicy"] = "disabled"
		cfg["dmPolicy"] = "allowlist"
		ids := make([]string, 0, len(bindings))
		for _, b := range bindings {
			if b.ID != "" {
				ids = append(ids, b.ID)
			}
		}
		if len(ids) > 0 {
			cfg["allowFrom"] = ids
		}
		cfg["dm"] = map[string]any{"enabled": true}
	case "team":
		cfg["groupPolicy"] = "allowlist"
		cfg["dmPolicy"] = "disabled"
		channelsMap := map[string]any{}
		for _, b := range bindings {
			if b.ID != "" {
				channelsMap[b.ID] = map[string]any{"allow": true, "requireMention": false}
			}
		}
		if len(channelsMap) > 0 {
			cfg["channels"] = channelsMap
		}
	}

	return cfg, nil
}

func (s *Slack) OpenClawPluginConfig(enabled bool) map[string]any {
	return map[string]any{"enabled": enabled}
}

func (s *Slack) RoutingEntries(agentType string, binding channels.ChannelBinding, agentName string, port int) []channels.RoutingEntry {
	if binding.ID == "" {
		return nil
	}
	url := fmt.Sprintf("http://conga-%s:%d/slack/events", agentName, port)
	switch agentType {
	case "user":
		return []channels.RoutingEntry{{Section: "members", Key: binding.ID, URL: url}}
	case "team":
		return []channels.RoutingEntry{{Section: "channels", Key: binding.ID, URL: url}}
	}
	return nil
}

func (s *Slack) AgentEnvVars(sv map[string]string) map[string]string {
	vars := map[string]string{}
	if v := sv["slack-bot-token"]; v != "" {
		vars["SLACK_BOT_TOKEN"] = v
	}
	if v := sv["slack-signing-secret"]; v != "" {
		vars["SLACK_SIGNING_SECRET"] = v
	}
	return vars
}

func (s *Slack) RouterEnvVars(sv map[string]string) map[string]string {
	vars := map[string]string{}
	if v := sv["slack-app-token"]; v != "" {
		vars["SLACK_APP_TOKEN"] = v
	}
	if v := sv["slack-signing-secret"]; v != "" {
		vars["SLACK_SIGNING_SECRET"] = v
	}
	return vars
}

func (s *Slack) WebhookPath() string { return "/slack/events" }

func (s *Slack) BehaviorTemplateVars(agentType string, binding channels.ChannelBinding) map[string]string {
	return map[string]string{"SLACK_ID": binding.ID}
}

func (s *Slack) SetupGuide() string {
	return `To set up Slack, you'll need to create a Slack app:

  1. Go to https://api.slack.com/apps and click "Create New App"
  2. Choose "From scratch", pick a name and workspace
  3. Under "OAuth & Permissions", add these Bot Token Scopes:
     app_mentions:read, channels:history, channels:read,
     chat:write, groups:history, groups:read, im:history,
     im:read, im:write, users:read
  4. Install the app to your workspace
  5. Copy the "Bot User OAuth Token" (starts with xoxb-)
  6. Under "Basic Information", copy the "Signing Secret"
  7. Under "Socket Mode", enable it and create an App-Level Token
     with the "connections:write" scope (starts with xapp-)

You'll need these three values:`
}
