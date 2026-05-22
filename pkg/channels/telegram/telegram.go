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
	// WARNING — this emission is PRE-v2026.5.18 and is rejected by OpenClaw
	// v2026.5.x's strict-additional-properties channel schema. Specifically:
	//
	//   - `mode: "http"` is not a valid telegram key (telegram has long
	//     polling vs webhook, not a generic http/socket toggle)
	//   - team agents emit `channels: {<id>: {allow: true}}`, but the
	//     canonical v2026.5.x shape is `groups: {<id>: {requireMention: ...}}`
	//   - dropping `allow: true` in favour of `requireMention`/`allowFrom`
	//
	// Additionally, the matching router in `router/telegram/src/index.js`
	// was built for Hermes Agent (it POSTs to /v1/chat/completions on port
	// 8642), not for OpenClaw — so even after fixing this config shape, the
	// inbound delivery path wouldn't reach an OpenClaw agent.
	//
	// Production fleet does not currently use Telegram + OpenClaw; the bug
	// is dormant. A full revamp is scoped in:
	//
	//   specs/2026-05-22_feature_telegram-v2026.5-revamp/
	//
	// — read requirements.md and plan.md there before touching this file.
	// The spec covers topology choice (router fanout vs per-agent direct),
	// the channel config shape migration, the router rewrite, and the live
	// validation plan. Do not "fix the allow→enabled key" here in isolation
	// without that broader context — the rest of the path won't deliver
	// events end-to-end.
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

func (t *Telegram) SetupGuide() string {
	return `To set up Telegram, you'll need a bot token from BotFather:

  1. Open Telegram and message @BotFather
  2. Send /newbot and follow the prompts to name your bot
  3. Copy the bot token (looks like 123456789:ABCdefGHI...)
  4. Optionally, generate a random webhook secret for HMAC verification

To find user IDs for binding agents:
  - Message @userinfobot in Telegram to get your numeric ID
  - For group chats, add @RawDataBot to the group and check the chat ID

You'll need these values:`
}
