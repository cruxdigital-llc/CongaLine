package telegram

import (
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
