//go:build integration

package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestLocalMultiChannelBinding verifies that a single team agent can be
// bound to multiple Slack channels, that routing.json and openclaw.json
// reflect every binding, and that unbind correctly handles the specific-id
// and single-remaining cases.
//
// Covers the end-to-end Phases 1-6 surface on the local provider:
//   - BindChannel idempotency + cross-agent uniqueness guard
//   - GenerateRoutingJSON fan-out across N channel IDs → same agent URL
//   - openclaw.json channels.slack.channels map includes all IDs
//   - UnbindChannel with specific id vs. empty-id-single-remaining legacy
//   - conga channels list --agent rendering
func TestLocalMultiChannelBinding(t *testing.T) {
	dataDir, agentName := setupTestEnv(t)
	base := baseArgs(dataDir)
	parent := t

	const (
		chanA = "C0111111111"
		chanB = "C0222222222"
		chanC = "C0333333333"
	)
	otherAgent := agentName + "b"

	t.Run("setup", func(t *testing.T) {
		cfg := fmt.Sprintf(`{"image":%q}`, testImage)
		mustRunCLI(t, append(base, "admin", "setup", "--json", cfg)...)
	})

	t.Run("add-team-agent", func(t *testing.T) {
		skipIfPriorFailed(t, parent)
		mustRunCLI(t, append(base, "admin", "add-team", agentName)...)
		assertContainerRunning(t, agentName)
	})

	t.Run("channels-add-slack", func(t *testing.T) {
		skipIfPriorFailed(t, parent)
		cfg := `{"slack-bot-token":"xoxb-fake","slack-signing-secret":"fakesigning","slack-app-token":"xapp-fake"}`
		mustRunCLI(t, append(base, "channels", "add", "slack", "--json", cfg)...)
	})

	t.Run("bind-first-channel", func(t *testing.T) {
		skipIfPriorFailed(t, parent)
		mustRunCLI(t, append(base, "channels", "bind", agentName, "slack:"+chanA, "--label", "#legal")...)
	})

	t.Run("bind-second-channel", func(t *testing.T) {
		skipIfPriorFailed(t, parent)
		mustRunCLI(t, append(base, "channels", "bind", agentName, "slack:"+chanB, "--label", "#sales")...)
	})

	t.Run("bind-third-channel", func(t *testing.T) {
		skipIfPriorFailed(t, parent)
		mustRunCLI(t, append(base, "channels", "bind", agentName, "slack:"+chanC)...)
	})

	t.Run("idempotent-rebind-same-id-same-label", func(t *testing.T) {
		skipIfPriorFailed(t, parent)
		mustRunCLI(t, append(base, "channels", "bind", agentName, "slack:"+chanA, "--label", "#legal")...)
		// Expect success with no duplicate binding — verified by routing count below.
	})

	t.Run("label-mismatch-rebind-errors", func(t *testing.T) {
		skipIfPriorFailed(t, parent)
		_, _, err := runCLI(t, append(base, "channels", "bind", agentName, "slack:"+chanA, "--label", "#DIFFERENT")...)
		if err == nil {
			t.Fatal("expected bind to error on label mismatch; got nil")
		}
	})

	t.Run("verify-routing-contains-all-three", func(t *testing.T) {
		skipIfPriorFailed(t, parent)
		routing := readRoutingJSON(t, dataDir)
		chans, ok := routing["channels"].(map[string]any)
		if !ok {
			t.Fatalf("routing.json channels section malformed: %+v", routing["channels"])
		}
		for _, id := range []string{chanA, chanB, chanC} {
			url, has := chans[id].(string)
			if !has {
				t.Errorf("routing.json missing channel %q; got: %+v", id, chans)
				continue
			}
			if !strings.Contains(url, "conga-"+agentName) {
				t.Errorf("routing.json[%q] = %q; want to route to agent %q", id, url, agentName)
			}
		}
	})

	t.Run("verify-openclaw-allowlist-has-all-three", func(t *testing.T) {
		skipIfPriorFailed(t, parent)
		cfg := readOpenClawJSON(t, dataDir, agentName)
		slack, ok := extractSlackChannels(cfg)
		if !ok {
			t.Fatalf("openclaw.json channels.slack.channels missing or malformed: %+v", cfg["channels"])
		}
		for _, id := range []string{chanA, chanB, chanC} {
			if _, has := slack[id]; !has {
				t.Errorf("openclaw.json channels.slack.channels missing %q; got keys: %v", id, keysOf(slack))
			}
		}
	})

	t.Run("channels-list-per-agent", func(t *testing.T) {
		skipIfPriorFailed(t, parent)
		out := mustRunCLI(t, append(base, "channels", "list", "--agent", agentName, "--output", "json")...)
		for _, id := range []string{chanA, chanB, chanC} {
			if !strings.Contains(out, id) {
				t.Errorf("channels list --agent output missing %q:\n%s", id, out)
			}
		}
		// Labels should be present for the bindings that have them.
		if !strings.Contains(out, "#legal") || !strings.Contains(out, "#sales") {
			t.Errorf("channels list --agent output missing labels:\n%s", out)
		}
	})

	t.Run("cross-agent-collision-blocked", func(t *testing.T) {
		skipIfPriorFailed(t, parent)
		mustRunCLI(t, append(base, "admin", "add-team", otherAgent)...)
		assertContainerRunning(t, otherAgent)

		_, _, err := runCLI(t, append(base, "channels", "bind", otherAgent, "slack:"+chanA)...)
		if err == nil {
			t.Fatalf("expected cross-agent collision error; got nil")
		}
	})

	t.Run("unbind-one-specific-id", func(t *testing.T) {
		skipIfPriorFailed(t, parent)
		mustRunCLI(t, append(base, "channels", "unbind", agentName, "slack:"+chanB, "--force")...)

		routing := readRoutingJSON(t, dataDir)
		chans, _ := routing["channels"].(map[string]any)
		if _, has := chans[chanB]; has {
			t.Errorf("routing.json still contains %q after unbind", chanB)
		}
		if _, has := chans[chanA]; !has {
			t.Errorf("routing.json should still contain %q", chanA)
		}
		if _, has := chans[chanC]; !has {
			t.Errorf("routing.json should still contain %q", chanC)
		}
	})

	t.Run("unbind-another-specific-id", func(t *testing.T) {
		skipIfPriorFailed(t, parent)
		mustRunCLI(t, append(base, "channels", "unbind", agentName, "slack:"+chanA, "--force")...)

		routing := readRoutingJSON(t, dataDir)
		chans, _ := routing["channels"].(map[string]any)
		if len(chans) != 1 {
			t.Errorf("expected 1 channel remaining, got %d: %+v", len(chans), chans)
		}
	})

	t.Run("unbind-last-legacy-empty-id", func(t *testing.T) {
		skipIfPriorFailed(t, parent)
		// Legacy form: platform-only when exactly one binding remains.
		mustRunCLI(t, append(base, "channels", "unbind", agentName, "slack", "--force")...)

		routing := readRoutingJSON(t, dataDir)
		chans, _ := routing["channels"].(map[string]any)
		if len(chans) != 0 {
			t.Errorf("expected 0 channels after final unbind, got: %+v", chans)
		}
	})

	t.Run("verify-openclaw-slack-section-removed", func(t *testing.T) {
		skipIfPriorFailed(t, parent)
		cfg := readOpenClawJSON(t, dataDir, agentName)
		// The channels section may still exist for other platforms but
		// channels.slack should be gone (or empty with no IDs).
		if chanCfg, ok := cfg["channels"].(map[string]any); ok {
			if slack, hasSlack := chanCfg["slack"].(map[string]any); hasSlack {
				if _, hasChannels := slack["channels"]; hasChannels {
					t.Errorf("openclaw.json channels.slack.channels should be absent after unbinding all; got: %+v", slack)
				}
			}
		}
	})

	t.Run("teardown", func(t *testing.T) {
		mustRunCLI(t, append(base, "admin", "teardown", "--force")...)
	})
}

// --- Helpers ---

// readRoutingJSON reads and parses the routing.json the local provider
// writes under $dataDir/config/ (see pkg/provider/localprovider/provider.go
// where routingPath is filepath.Join(p.configDir(), "routing.json") and
// configDir() is filepath.Join(p.dataDir, "config")).
func readRoutingJSON(t *testing.T, dataDir string) map[string]any {
	t.Helper()
	path := filepath.Join(dataDir, "config", "routing.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read routing.json: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("parse routing.json: %v\n%s", err, raw)
	}
	return out
}

// readOpenClawJSON reads and parses the agent's openclaw.json.
func readOpenClawJSON(t *testing.T, dataDir, agentName string) map[string]any {
	t.Helper()
	path := filepath.Join(dataDir, "data", agentName, "openclaw.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read openclaw.json for %s: %v", agentName, err)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("parse openclaw.json: %v\n%s", err, raw)
	}
	return out
}

// extractSlackChannels digs into openclaw.json to reach channels.slack.channels.
// Returns (chans, true) on success, (nil, false) if any intermediate key
// is missing or the wrong shape.
func extractSlackChannels(cfg map[string]any) (map[string]any, bool) {
	ch, ok := cfg["channels"].(map[string]any)
	if !ok {
		return nil, false
	}
	slack, ok := ch["slack"].(map[string]any)
	if !ok {
		return nil, false
	}
	chans, ok := slack["channels"].(map[string]any)
	return chans, ok
}

func keysOf(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
