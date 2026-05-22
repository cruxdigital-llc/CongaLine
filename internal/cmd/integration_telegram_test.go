//go:build integration

package cmd

import (
	"fmt"
	"strings"
	"testing"
)

// TestAddUser_TelegramOnOpenClawRejected verifies the channel × runtime
// compatibility gate from the telegram-v2026.5-revamp spec (Option C):
// `conga admin add-user --runtime openclaw --channel telegram:<id>`
// must refuse with an operator-actionable error before any provisioning
// side effects.
//
// Promoted from a manual smoke (spec V1) per QA persona review so future
// refactors that accidentally bypass the gate fail in CI rather than in
// a one-time hand-test. Lives behind the `integration` build tag because
// it shares the test harness with the Docker-based tests; the gate itself
// fires before any Docker call, but `admin setup` needs Docker to
// validate the configured image.
func TestAddUser_TelegramOnOpenClawRejected(t *testing.T) {
	dataDir, agentName := setupTestEnv(t)
	base := baseArgs(dataDir)

	// Minimal setup so add-user reaches the channel resolution path.
	cfg := fmt.Sprintf(`{"image":%q}`, testImage)
	mustRunCLI(t, append(base, "admin", "setup", "--json", cfg)...)

	// Add telegram so HasCredentials passes (otherwise add-user errors on
	// "telegram is not configured" before the runtime gate has a chance to
	// fire — would be a false negative in CI).
	mustRunCLI(t, append(base, "channels", "add", "telegram", "--json",
		`{"telegram-bot-token":"123:fake-for-test"}`)...)

	// Now attempt the unsupported combination. Use runCLI (not
	// mustRunCLI) — we EXPECT this to fail with a specific message.
	stdout, stderr, err := runCLI(t, append(base, "admin", "add-user", agentName,
		"--runtime", "openclaw",
		"--channel", "telegram:123456789",
	)...)
	if err == nil {
		t.Fatalf("add-user with telegram+openclaw should fail; got nil error\nstdout: %s\nstderr: %s", stdout, stderr)
	}

	// The error surface for cobra-driven CLIs can be the returned err OR
	// captured stderr depending on the error type. Check both so the
	// assertion is robust to error-formatting drift.
	combined := err.Error() + "\n" + stderr
	requireContains := []string{"telegram", "openclaw", "specs/2026-05-22_feature_telegram-v2026.5-revamp/"}
	for _, want := range requireContains {
		if !strings.Contains(combined, want) {
			t.Errorf("expected rejection message to contain %q; got err=%q stderr=%q", want, err.Error(), stderr)
		}
	}
}
