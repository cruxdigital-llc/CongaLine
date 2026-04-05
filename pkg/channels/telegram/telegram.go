// Package telegram implements the Channel interface for Telegram integration.
// Uses the same proxy pattern as Slack: one bot token, one webhook URL owned
// by the Telegram router, fan-out to per-agent containers via HTTP POST.
package telegram

import (
	"fmt"
	"regexp"

	"github.com/cruxdigital-llc/conga-line/pkg/channels"
)

func init() {
	channels.Register(&Telegram{})
}

// userIDPattern matches Telegram numeric user IDs (positive integers).
var userIDPattern = regexp.MustCompile(`^[1-9]\d{4,14}$`)

// chatIDPattern matches Telegram group chat IDs (negative integers).
var chatIDPattern = regexp.MustCompile(`^-\d{5,15}$`)

// Telegram implements the channels.Channel interface.
type Telegram struct{}

func (t *Telegram) Name() string { return "telegram" }

func (t *Telegram) ValidateBinding(agentType, id string) error {
	switch agentType {
	case "user":
		if !userIDPattern.MatchString(id) {
			return fmt.Errorf("invalid Telegram user ID %q: must be a positive numeric ID (e.g., 123456789)", id)
		}
	case "team":
		if !chatIDPattern.MatchString(id) {
			return fmt.Errorf("invalid Telegram chat ID %q: must be a negative numeric ID (e.g., -1001234567890)", id)
		}
	default:
		return fmt.Errorf("unknown agent type %q for telegram binding validation", agentType)
	}
	return nil
}

func (t *Telegram) SharedSecrets() []channels.SecretDef {
	return []channels.SecretDef{
		{Name: "telegram-bot-token", EnvVar: "TELEGRAM_BOT_TOKEN", Prompt: "Telegram bot token (from @BotFather)", Required: true},
		{Name: "telegram-webhook-secret", EnvVar: "TELEGRAM_WEBHOOK_SECRET", Prompt: "Telegram webhook secret (random string for HMAC verification)", Required: false, RouterOnly: true},
	}
}

func (t *Telegram) HasCredentials(sv map[string]string) bool {
	return sv["telegram-bot-token"] != ""
}

func (t *Telegram) OpenClawChannelConfig(agentType string, binding channels.ChannelBinding, sv map[string]string) (map[string]any, error) {
	// OpenClaw has a Telegram plugin. Configure it in HTTP webhook mode
	// so it receives events from the Conga router (not direct long polling).
	cfg := map[string]any{
		"mode":    "http",
		"enabled": true,
	}

	switch agentType {
	case "user":
		cfg["dmPolicy"] = "allowlist"
		if binding.ID != "" {
			cfg["allowFrom"] = []string{binding.ID}
		}
	case "team":
		cfg["groupPolicy"] = "allowlist"
		if binding.ID != "" {
			cfg["channels"] = map[string]any{
				binding.ID: map[string]any{"allow": true},
			}
		}
	}

	return cfg, nil
}

func (t *Telegram) OpenClawPluginConfig(enabled bool) map[string]any {
	return map[string]any{"enabled": enabled}
}

func (t *Telegram) RoutingEntries(agentType string, binding channels.ChannelBinding, agentName string, port int) []channels.RoutingEntry {
	if binding.ID == "" {
		return nil
	}
	// URL is a placeholder — GenerateRoutingJSON overrides it with the
	// runtime-resolved webhook target (path + port).
	url := fmt.Sprintf("http://conga-%s:%d/telegram/events", agentName, port)
	switch agentType {
	case "user":
		return []channels.RoutingEntry{{Section: "members", Key: binding.ID, URL: url}}
	case "team":
		return []channels.RoutingEntry{{Section: "channels", Key: binding.ID, URL: url}}
	}
	return nil
}

func (t *Telegram) AgentEnvVars(sv map[string]string) map[string]string {
	// Do NOT pass TELEGRAM_BOT_TOKEN to agent containers — the router owns
	// the bot connection. Passing it would cause Hermes to start its own
	// long-polling connection, conflicting with the router's webhook.
	return map[string]string{}
}

func (t *Telegram) RouterEnvVars(sv map[string]string) map[string]string {
	vars := map[string]string{}
	if v := sv["telegram-bot-token"]; v != "" {
		vars["TELEGRAM_BOT_TOKEN"] = v
	}
	if v := sv["telegram-webhook-secret"]; v != "" {
		vars["TELEGRAM_WEBHOOK_SECRET"] = v
	}
	return vars
}

func (t *Telegram) WebhookPath() string { return "/telegram/events" }

func (t *Telegram) BehaviorTemplateVars(agentType string, binding channels.ChannelBinding) map[string]string {
	return map[string]string{"TELEGRAM_ID": binding.ID}
}
