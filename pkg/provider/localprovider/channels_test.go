package localprovider

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cruxdigital-llc/conga-line/pkg/channels"

	// Channel registry registration happens in package init for the
	// concrete implementations; importing them as side effects makes them
	// available to channels.Get() in this test.
	_ "github.com/cruxdigital-llc/conga-line/pkg/channels/slack"
	_ "github.com/cruxdigital-llc/conga-line/pkg/channels/telegram"
)

// TestBindChannel_RuntimeGate_RejectsTelegramOnOpenClaw verifies the
// channel × runtime compatibility gate added by the telegram-v2026.5-revamp
// spec (Option C). BindChannel must refuse telegram bindings on agents
// whose effective runtime resolves to openclaw, with an actionable error
// message that points to the spec dir.
//
// This is the canonical test for the gate; the same check fires in
// the remote + AWS providers and in the CLI/MCP provisioning paths, but
// they share the same Channel.SupportsRuntime call so coverage here
// guards the seam where it most matters (provider-side).
func TestBindChannel_RuntimeGate_RejectsTelegramOnOpenClaw(t *testing.T) {
	p := testProvider(t)

	// Seed shared telegram secrets so HasCredentials passes — otherwise
	// BindChannel short-circuits on "not configured" before the gate
	// fires, and we'd be testing the wrong rejection path.
	if err := os.MkdirAll(p.sharedSecretsDir(), 0700); err != nil {
		t.Fatalf("mkdir shared secrets dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(p.sharedSecretsDir(), "telegram-bot-token"), []byte("123:fake"), 0400); err != nil {
		t.Fatalf("seed telegram-bot-token: %v", err)
	}

	// Seed an openclaw user agent. Empty Runtime resolves to openclaw via
	// runtime.ResolveRuntime, matching the legacy-agent edge case in the
	// spec; setting it explicitly is more honest about what we're
	// asserting.
	if err := os.MkdirAll(p.agentsDir(), 0700); err != nil {
		t.Fatalf("mkdir agents: %v", err)
	}
	agentJSON := `{"type":"user","gateway_port":18789,"runtime":"openclaw"}`
	if err := os.WriteFile(filepath.Join(p.agentsDir(), "ocagent.json"), []byte(agentJSON), 0644); err != nil {
		t.Fatalf("seed agent: %v", err)
	}

	binding := channels.ChannelBinding{Platform: "telegram", ID: "123456789"}
	err := p.BindChannel(context.Background(), "ocagent", binding)
	if err == nil {
		t.Fatal("BindChannel should refuse telegram on openclaw agent; got nil error")
	}
	if !strings.Contains(err.Error(), "telegram") {
		t.Errorf("error should mention telegram; got: %v", err)
	}
	if !strings.Contains(err.Error(), "openclaw") {
		t.Errorf("error should mention openclaw; got: %v", err)
	}
	if !strings.Contains(err.Error(), "specs/2026-05-22_feature_telegram-v2026.5-revamp/") {
		t.Errorf("error should point operators at the spec dir; got: %v", err)
	}

	// And the agent's persisted channel list must remain empty — the gate
	// fires before any state mutation.
	a, err := p.GetAgent(context.Background(), "ocagent")
	if err != nil {
		t.Fatalf("re-load agent: %v", err)
	}
	if len(a.Channels) != 0 {
		t.Errorf("BindChannel was refused but channels list mutated: %v", a.Channels)
	}
}
