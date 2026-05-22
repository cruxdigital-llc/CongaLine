// Package telegram implements the Channel interface for Telegram integration.
//
// Telegram is Hermes-only. OpenClaw + Telegram is explicitly unsupported —
// the v2026.5.18+ OpenClaw telegram plugin has no "receive-forwarded-events"
// mode (every instance either long-polls Telegram or registers its own
// webhook directly with api.telegram.org), which makes the Slack-style
// router-fanout pattern incompatible. See
// specs/2026-05-22_feature_telegram-v2026.5-revamp/ for the full analysis.
//
// SupportsRuntime returns (false, …) for openclaw so the channel-abstraction
// gates in CLI, MCP, and provider BindChannel paths refuse the combination
// with an operator-actionable error. OpenClawChannelConfig errors out too,
// as defense in depth.
//
// The Hermes path remains supported via router/telegram/src/index.js,
// which forwards Telegram updates to Hermes's OpenAI-compatible API server.
package telegram

import (
	"errors"
	"fmt"
	"regexp"

	"github.com/cruxdigital-llc/conga-line/pkg/channels"
)

// unsupportedOpenClawMsg is the operator-facing explanation when telegram
// is attempted against the openclaw runtime. SupportsRuntime callers wrap
// this with their own "channel X is not supported for Y runtime:" prefix
// (CLI / MCP / provider BindChannel), so the message itself focuses on
// the actionable fix rather than restating the problem. The defense-in-
// depth OpenClawChannelConfig error path uses the same string as-is.
const unsupportedOpenClawMsg = "use the hermes runtime instead — see specs/2026-05-22_feature_telegram-v2026.5-revamp/ for context"

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

// SupportsRuntime — Telegram is hermes-only (see package doc).
func (t *Telegram) SupportsRuntime(runtimeName string) (bool, string) {
	if runtimeName == "hermes" {
		return true, ""
	}
	return false, unsupportedOpenClawMsg
}

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

// OpenClawChannelConfig is intentionally a hard error path. Telegram is
// hermes-only (see package doc and SupportsRuntime). Any code path that
// reaches this method is calling the wrong abstraction for telegram —
// the CLI / MCP / provider BindChannel gates should have caught it
// first via SupportsRuntime. Failing closed here is defense in depth.
func (t *Telegram) OpenClawChannelConfig(agentType string, binding channels.ChannelBinding, sv map[string]string) (map[string]any, error) {
	return nil, errors.New(unsupportedOpenClawMsg)
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
