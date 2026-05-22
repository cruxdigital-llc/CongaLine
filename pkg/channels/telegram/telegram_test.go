package telegram

import (
	"strings"
	"testing"

	"github.com/cruxdigital-llc/conga-line/pkg/channels"
)

func TestName(t *testing.T) {
	tg := &Telegram{}
	if got := tg.Name(); got != "telegram" {
		t.Fatalf("Name() = %q, want telegram", got)
	}
}

func TestValidateBinding_User(t *testing.T) {
	tg := &Telegram{}
	tests := []struct {
		id string
		ok bool
	}{
		{"123456789", true},
		{"12345", true},
		{"999999999999999", true},
		{"0123", false},
		{"abc", false},
		{"", false},
		{"-123", false},
		{"1234", false},
	}
	for _, tt := range tests {
		err := tg.ValidateBinding("user", tt.id)
		if tt.ok && err != nil {
			t.Errorf("ValidateBinding(user, %q) unexpected error: %v", tt.id, err)
		}
		if !tt.ok && err == nil {
			t.Errorf("ValidateBinding(user, %q) expected error, got nil", tt.id)
		}
	}
}

func TestValidateBinding_Team(t *testing.T) {
	tg := &Telegram{}
	tests := []struct {
		id string
		ok bool
	}{
		{"-1001234567890", true},
		{"-12345", true},
		{"123456789", false},
		{"-123", false},
		{"", false},
	}
	for _, tt := range tests {
		err := tg.ValidateBinding("team", tt.id)
		if tt.ok && err != nil {
			t.Errorf("ValidateBinding(team, %q) unexpected error: %v", tt.id, err)
		}
		if !tt.ok && err == nil {
			t.Errorf("ValidateBinding(team, %q) expected error, got nil", tt.id)
		}
	}
}

func TestHasCredentials(t *testing.T) {
	tg := &Telegram{}
	if tg.HasCredentials(map[string]string{}) {
		t.Error("HasCredentials should be false with no secrets")
	}
	if !tg.HasCredentials(map[string]string{"telegram-bot-token": "123:ABC"}) {
		t.Error("HasCredentials should be true with bot token")
	}
}

func TestAgentEnvVars_Empty(t *testing.T) {
	tg := &Telegram{}
	vars := tg.AgentEnvVars(map[string]string{"telegram-bot-token": "123:ABC"})
	if len(vars) != 0 {
		t.Errorf("AgentEnvVars should be empty (router owns the token), got %v", vars)
	}
}

func TestRouterEnvVars(t *testing.T) {
	tg := &Telegram{}
	vars := tg.RouterEnvVars(map[string]string{
		"telegram-bot-token":      "123:ABC",
		"telegram-webhook-secret": "mysecret",
	})
	if vars["TELEGRAM_BOT_TOKEN"] != "123:ABC" {
		t.Errorf("TELEGRAM_BOT_TOKEN = %q, want 123:ABC", vars["TELEGRAM_BOT_TOKEN"])
	}
	if vars["TELEGRAM_WEBHOOK_SECRET"] != "mysecret" {
		t.Errorf("TELEGRAM_WEBHOOK_SECRET = %q, want mysecret", vars["TELEGRAM_WEBHOOK_SECRET"])
	}
}

func TestWebhookPath(t *testing.T) {
	tg := &Telegram{}
	if got := tg.WebhookPath(); got != "/telegram/events" {
		t.Fatalf("WebhookPath() = %q, want /telegram/events", got)
	}
}

func TestSharedSecrets(t *testing.T) {
	tg := &Telegram{}
	secrets := tg.SharedSecrets()
	if len(secrets) != 2 {
		t.Fatalf("SharedSecrets() returned %d items, want 2", len(secrets))
	}
	if secrets[0].Name != "telegram-bot-token" || !secrets[0].Required {
		t.Errorf("first secret: name=%q required=%v", secrets[0].Name, secrets[0].Required)
	}
	if secrets[1].Name != "telegram-webhook-secret" || !secrets[1].RouterOnly {
		t.Errorf("second secret: name=%q routerOnly=%v", secrets[1].Name, secrets[1].RouterOnly)
	}
}

func TestRoutingEntries_User(t *testing.T) {
	tg := &Telegram{}
	binding := channels.ChannelBinding{Platform: "telegram", ID: "123456789"}
	entries := tg.RoutingEntries("user", binding, "myagent", 18789)
	if len(entries) != 1 {
		t.Fatalf("RoutingEntries returned %d entries, want 1", len(entries))
	}
	if entries[0].Section != "members" {
		t.Errorf("Section = %q, want members", entries[0].Section)
	}
	if entries[0].Key != "123456789" {
		t.Errorf("Key = %q, want 123456789", entries[0].Key)
	}
}

func TestRoutingEntries_Team(t *testing.T) {
	tg := &Telegram{}
	binding := channels.ChannelBinding{Platform: "telegram", ID: "-1001234567890"}
	entries := tg.RoutingEntries("team", binding, "leadership", 18790)
	if len(entries) != 1 {
		t.Fatalf("RoutingEntries returned %d entries, want 1", len(entries))
	}
	if entries[0].Section != "channels" {
		t.Errorf("Section = %q, want channels", entries[0].Section)
	}
}

func TestRoutingEntries_EmptyID(t *testing.T) {
	tg := &Telegram{}
	binding := channels.ChannelBinding{Platform: "telegram", ID: ""}
	entries := tg.RoutingEntries("user", binding, "myagent", 18789)
	if len(entries) != 0 {
		t.Errorf("RoutingEntries with empty ID should return nil, got %d entries", len(entries))
	}
}

// TestBehaviorTemplateVars — parity with slack's same-named test. Confirms
// behavior file templates receive the canonical TELEGRAM_ID substitution
// variable. Without this, agent.yaml templates that reference {{TELEGRAM_ID}}
// would silently render the empty string.
func TestBehaviorTemplateVars(t *testing.T) {
	tg := &Telegram{}
	binding := channels.ChannelBinding{Platform: "telegram", ID: "123456789"}
	vars := tg.BehaviorTemplateVars("user", binding)
	if vars["TELEGRAM_ID"] != "123456789" {
		t.Errorf("TELEGRAM_ID = %q, want 123456789", vars["TELEGRAM_ID"])
	}
}

// TestSupportsRuntime is the central guard for the Hermes-only policy.
// Reasoning is in pkg/channels/telegram/telegram.go (package doc) and
// specs/2026-05-22_feature_telegram-v2026.5-revamp/spec.md.
func TestSupportsRuntime(t *testing.T) {
	tg := &Telegram{}
	cases := []struct {
		name        string
		runtime     string
		wantOK      bool
		msgContains string // substring of reason; only checked when !wantOK
	}{
		{"hermes supported", "hermes", true, ""},
		// The reason string focuses on the actionable fix ("use hermes")
		// rather than restating which runtime is unsupported. Callers
		// (CLI / MCP / provider BindChannel) add a "channel X is not
		// supported for Y runtime:" prefix, so the unsupported-runtime
		// name surfaces in the operator-visible error even though it's
		// not in the per-channel reason itself.
		{"openclaw rejected", "openclaw", false, "hermes"},
		{"empty rejected (defaults to openclaw upstream)", "", false, "hermes"},
		{"unknown runtime rejected", "future-runtime-name", false, "hermes"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ok, reason := tg.SupportsRuntime(tc.runtime)
			if ok != tc.wantOK {
				t.Errorf("SupportsRuntime(%q) ok = %v, want %v", tc.runtime, ok, tc.wantOK)
			}
			if !tc.wantOK {
				if reason == "" {
					t.Errorf("SupportsRuntime(%q) returned ok=false with empty reason; operator gets no actionable message", tc.runtime)
				}
				if tc.msgContains != "" && !strings.Contains(reason, tc.msgContains) {
					t.Errorf("SupportsRuntime(%q) reason %q does not mention %q", tc.runtime, reason, tc.msgContains)
				}
				if !strings.Contains(reason, "specs/2026-05-22_feature_telegram-v2026.5-revamp/") {
					t.Errorf("SupportsRuntime(%q) reason should point operators at the spec dir; got %q", tc.runtime, reason)
				}
			}
		})
	}
}

// TestOpenClawChannelConfig_Errors confirms the defense-in-depth path:
// if any caller bypasses the SupportsRuntime gate and asks for the
// channel config, the channel implementation refuses rather than emit
// the pre-v2026.5.18 shape that the new image would reject anyway.
func TestOpenClawChannelConfig_Errors(t *testing.T) {
	tg := &Telegram{}
	for _, agentType := range []string{"user", "team"} {
		t.Run(agentType, func(t *testing.T) {
			cfg, err := tg.OpenClawChannelConfig(agentType, channels.ChannelBinding{ID: "123"}, nil)
			if err == nil {
				t.Fatalf("expected error, got cfg=%v", cfg)
			}
			if cfg != nil {
				t.Errorf("expected nil cfg on error, got %v", cfg)
			}
			// The error string is the shared unsupportedOpenClawMsg constant —
			// it focuses on the actionable fix ("use the hermes runtime")
			// rather than restating the failure. The wrapper at every gate
			// call site adds the "channel X is not supported for Y runtime"
			// prefix; here (defense-in-depth path) the wrapper isn't applied,
			// so we assert only on the actionable-fix substring.
			if !strings.Contains(err.Error(), "hermes runtime") {
				t.Errorf("error message should suggest the hermes runtime fix, got: %v", err)
			}
			if !strings.Contains(err.Error(), "specs/2026-05-22_feature_telegram-v2026.5-revamp/") {
				t.Errorf("error message should point at the spec dir, got: %v", err)
			}
		})
	}
}
