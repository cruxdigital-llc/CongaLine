package mcpserver_test

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/cruxdigital-llc/conga-line/internal/mcpserver"
	"github.com/cruxdigital-llc/conga-line/pkg/common"
	"github.com/cruxdigital-llc/conga-line/pkg/provider"
	"github.com/mark3labs/mcp-go/mcptest"
)

// findProvisionTool returns the conga_provision_agent tool definition
// out of the registered tools list. Used by schema-shape tests.
func findProvisionTool(t *testing.T, srv *mcpserver.Server) map[string]any {
	t.Helper()
	for _, tool := range srv.Tools() {
		if tool.Tool.Name == "conga_provision_agent" {
			return tool.Tool.InputSchema.Properties
		}
	}
	t.Fatal("conga_provision_agent tool not registered")
	return nil
}

// TestToolProvisionAgent_SchemaIncludesRoleAndRuntime guards the MCP
// surface contract for PR #53. The `role` and `runtime` params are how
// MCP callers (Claude / other agents) opt into the role-package flow
// the CLI also exposes via --role. Dropping either field from the
// schema would silently de-list the capability from MCP discovery.
func TestToolProvisionAgent_SchemaIncludesRoleAndRuntime(t *testing.T) {
	mock := &mockProvider{name: "mock"}
	srv := mcpserver.NewServer(mock, "test")

	props := findProvisionTool(t, srv)

	role, ok := props["role"].(map[string]any)
	if !ok {
		t.Fatal("conga_provision_agent input schema missing `role` property")
	}
	if typ, _ := role["type"].(string); typ != "string" {
		t.Errorf("role.type = %v, want \"string\"", role["type"])
	}
	if desc, _ := role["description"].(string); !strings.Contains(desc, "role-") {
		t.Errorf("role description should reference role-* slugs, got %q", desc)
	}

	rt, ok := props["runtime"].(map[string]any)
	if !ok {
		t.Fatal("conga_provision_agent input schema missing `runtime` property")
	}
	enum, _ := rt["enum"].([]string)
	wantRuntimes := map[string]bool{"openclaw": false, "hermes": false}
	for _, v := range enum {
		if _, ok := wantRuntimes[v]; ok {
			wantRuntimes[v] = true
		}
	}
	for name, found := range wantRuntimes {
		if !found {
			t.Errorf("runtime enum missing %q (got %v)", name, enum)
		}
	}
}

// TestToolProvisionAgent_RoleWithoutBehaviorDir_ReturnsError verifies
// that when the MCP server cannot locate the agents/ directory (no
// go.mod marker upstream of cwd), a `role` parameter produces an
// actionable error rather than silently dropping the role flag.
// Regression guard for the worktree-cwd silent-wrong fix (followups #1).
func TestToolProvisionAgent_RoleWithoutBehaviorDir_ReturnsError(t *testing.T) {
	// Chdir to a fresh tmpdir so ResolveOperatorBehaviorDir's walk-up
	// finds no conga-line go.mod marker. We must restore cwd before
	// the test ends or sibling tests will fail.
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	tmp := t.TempDir()
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("chdir tmp: %v", err)
	}

	mock := &mockProvider{name: "mock"}
	srv := mcpserver.NewServer(mock, "test")
	testSrv, err := mcptest.NewServer(t, srv.Tools()...)
	if err != nil {
		t.Fatal(err)
	}
	defer testSrv.Close()

	result := callTool(t, testSrv.Client(), "conga_provision_agent", map[string]any{
		"agent_name": "newagent",
		"type":       "user",
		"role":       "role-ops",
	})
	if !result.IsError {
		t.Fatal("expected error when role is set but behavior dir cannot be located")
	}
	msg := textContent(t, result)
	if !strings.Contains(msg, "role-ops") {
		t.Errorf("error should name the requested role, got %q", msg)
	}
	if !strings.Contains(msg, "agents/") {
		t.Errorf("error should mention the missing agents/ directory, got %q", msg)
	}
}

// warningProvider embeds mockProvider and emits configurable warnings
// via common.Warn on lifecycle methods. Used to verify the MCP server
// drains its context sink and surfaces warnings in tool result text.
type warningProvider struct {
	*mockProvider
	refreshWarn   []string
	unpauseWarn   []string
	provisionWarn []string
}

func (m *warningProvider) RefreshAgent(ctx context.Context, name string) error {
	for _, w := range m.refreshWarn {
		common.Warn(ctx, "%s", w)
	}
	return m.mockProvider.RefreshAgent(ctx, name)
}

func (m *warningProvider) UnpauseAgent(ctx context.Context, name string) error {
	for _, w := range m.unpauseWarn {
		common.Warn(ctx, "%s", w)
	}
	return m.mockProvider.UnpauseAgent(ctx, name)
}

func (m *warningProvider) ProvisionAgent(ctx context.Context, cfg provider.AgentConfig) error {
	for _, w := range m.provisionWarn {
		common.Warn(ctx, "%s", w)
	}
	return m.mockProvider.ProvisionAgent(ctx, cfg)
}

// TestRefreshAgent_PropagatesWarningsToToolResult covers CRIT-5 — the
// reason the WarningSink exists. Provider warnings emitted via
// common.Warn during a refresh must appear in the tool result so MCP
// operators (where stderr is invisible) see them. Without the sink
// wiring, these warnings vanish.
func TestRefreshAgent_PropagatesWarningsToToolResult(t *testing.T) {
	base := &mockProvider{name: "mock"}
	mock := &warningProvider{
		mockProvider: base,
		refreshWarn:  []string{"routing.json regeneration failed: timeout", "egress redeploy: deny-all fallback"},
	}

	srv := mcpserver.NewServer(mock, "test")
	testSrv, err := mcptest.NewServer(t, srv.Tools()...)
	if err != nil {
		t.Fatal(err)
	}
	defer testSrv.Close()

	result := callTool(t, testSrv.Client(), "conga_refresh_agent", map[string]any{
		"agent_name": "agent1",
	})
	if result.IsError {
		t.Fatalf("unexpected error: %s", textContent(t, result))
	}

	text := textContent(t, result)
	if !strings.Contains(text, "refreshed") {
		t.Errorf("result should still report success, got %q", text)
	}
	if !strings.Contains(text, "Warnings:") {
		t.Errorf("result should include a Warnings: block when sink has entries, got %q", text)
	}
	if !strings.Contains(text, "routing.json regeneration failed: timeout") {
		t.Errorf("result should include the first warning verbatim, got %q", text)
	}
	if !strings.Contains(text, "egress redeploy: deny-all fallback") {
		t.Errorf("result should include the second warning verbatim, got %q", text)
	}
}

// TestRefreshAgent_NoWarnings_PlainResult is the negative twin — when
// nothing emits warnings, the result must NOT include an empty
// Warnings: block (would look broken to operators).
func TestRefreshAgent_NoWarnings_PlainResult(t *testing.T) {
	mock := &mockProvider{name: "mock"}
	srv := mcpserver.NewServer(mock, "test")
	testSrv, err := mcptest.NewServer(t, srv.Tools()...)
	if err != nil {
		t.Fatal(err)
	}
	defer testSrv.Close()

	result := callTool(t, testSrv.Client(), "conga_refresh_agent", map[string]any{
		"agent_name": "agent1",
	})
	if result.IsError {
		t.Fatalf("unexpected error: %s", textContent(t, result))
	}

	text := textContent(t, result)
	if strings.Contains(text, "Warnings:") {
		t.Errorf("empty sink should not produce a Warnings: block, got %q", text)
	}
}

// TestRefreshAgent_WarningsSurfaceOnErrorPath is the regression guard
// for CONV-1 — the warnings accumulated in the sink before a lifecycle
// method errors MUST appear in the error result, because that's
// precisely when the warnings have the highest diagnostic value (e.g.
// a step-0 egress-gap warning preceding a step-1 config-regen failure
// tells the operator the misconfiguration is likely the root cause).
// Dropping warnings on the error path silently defeats CRIT-5's intent.
func TestRefreshAgent_WarningsSurfaceOnErrorPath(t *testing.T) {
	base := &mockProvider{name: "mock", err: errors.New("simulated step-1 failure")}
	mock := &warningProvider{
		mockProvider: base,
		refreshWarn:  []string{"egress-gap: example.com not in allowlist"},
	}

	srv := mcpserver.NewServer(mock, "test")
	testSrv, err := mcptest.NewServer(t, srv.Tools()...)
	if err != nil {
		t.Fatal(err)
	}
	defer testSrv.Close()

	result := callTool(t, testSrv.Client(), "conga_refresh_agent", map[string]any{
		"agent_name": "agent1",
	})
	if !result.IsError {
		t.Fatal("expected IsError=true when provider returns an error")
	}

	text := textContent(t, result)
	if !strings.Contains(text, "simulated step-1 failure") {
		t.Errorf("error result must include the original error, got %q", text)
	}
	if !strings.Contains(text, "Warnings:") {
		t.Errorf("error result must include Warnings: block when sink has entries, got %q", text)
	}
	if !strings.Contains(text, "egress-gap: example.com not in allowlist") {
		t.Errorf("error result must include the accumulated warning verbatim, got %q", text)
	}
}

// TestProvisionAgent_PropagatesWarningsToToolResult mirrors the refresh
// test for the provision tool. Without this, a future change that drops
// the `ctx, sink := withSink(ctx)` line from toolProvisionAgent would
// silently lose the WarnOverlayEgressGaps output emitted by every
// provider's ProvisionAgent path.
func TestProvisionAgent_PropagatesWarningsToToolResult(t *testing.T) {
	// chdir to tmpdir so the role-package path is skipped; the test
	// focuses on the post-role provision call.
	origDir, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(origDir) })
	_ = os.Chdir(t.TempDir())

	base := &mockProvider{name: "mock"}
	mock := &warningProvider{
		mockProvider:  base,
		provisionWarn: []string{"overlay endpoint litellm.lan not in egress allowlist"},
	}

	srv := mcpserver.NewServer(mock, "test")
	testSrv, err := mcptest.NewServer(t, srv.Tools()...)
	if err != nil {
		t.Fatal(err)
	}
	defer testSrv.Close()

	result := callTool(t, testSrv.Client(), "conga_provision_agent", map[string]any{
		"agent_name":   "newagent",
		"type":         "user",
		"gateway_port": 18800,
	})
	if result.IsError {
		t.Fatalf("unexpected error: %s", textContent(t, result))
	}
	text := textContent(t, result)
	if !strings.Contains(text, "Warnings:") || !strings.Contains(text, "overlay endpoint litellm.lan not in egress allowlist") {
		t.Errorf("provision result must surface the warning, got %q", text)
	}
}

// TestUnpauseAgent_PropagatesWarningsToToolResult covers the unpause
// path. Important because UnpauseAgent on AWS internally calls
// RefreshAgent (followups #5 self-heal), so warnings emitted during
// the refresh inside an unpause are exactly the kind operators need to
// see in MCP.
func TestUnpauseAgent_PropagatesWarningsToToolResult(t *testing.T) {
	base := &mockProvider{name: "mock"}
	mock := &warningProvider{
		mockProvider: base,
		unpauseWarn:  []string{"systemd unit was missing; recreated"},
	}

	srv := mcpserver.NewServer(mock, "test")
	testSrv, err := mcptest.NewServer(t, srv.Tools()...)
	if err != nil {
		t.Fatal(err)
	}
	defer testSrv.Close()

	result := callTool(t, testSrv.Client(), "conga_unpause_agent", map[string]any{
		"agent_name": "agent1",
	})
	if result.IsError {
		t.Fatalf("unexpected error: %s", textContent(t, result))
	}
	text := textContent(t, result)
	if !strings.Contains(text, "Warnings:") || !strings.Contains(text, "systemd unit was missing; recreated") {
		t.Errorf("unpause result must surface the warning, got %q", text)
	}
}

// TestRefreshAll_PropagatesWarningsToToolResult covers the bulk-refresh
// tool. Without this, per-agent warnings from a fleet refresh
// (N agents × multiple steps each) would silently vanish under MCP.
func TestRefreshAll_PropagatesWarningsToToolResult(t *testing.T) {
	base := &mockProvider{name: "mock"}
	mock := &refreshAllWarningProvider{
		mockProvider:    base,
		refreshAllWarns: []string{"agent1: routing.json regen failed", "agent2: egress redeploy partial"},
	}

	srv := mcpserver.NewServer(mock, "test")
	testSrv, err := mcptest.NewServer(t, srv.Tools()...)
	if err != nil {
		t.Fatal(err)
	}
	defer testSrv.Close()

	result := callTool(t, testSrv.Client(), "conga_refresh_all", nil)
	if result.IsError {
		t.Fatalf("unexpected error: %s", textContent(t, result))
	}
	text := textContent(t, result)
	if !strings.Contains(text, "Warnings:") {
		t.Errorf("refresh-all result must include Warnings: block when sink has entries, got %q", text)
	}
	if !strings.Contains(text, "agent1: routing.json regen failed") || !strings.Contains(text, "agent2: egress redeploy partial") {
		t.Errorf("refresh-all must surface per-agent warnings verbatim, got %q", text)
	}
}

// refreshAllWarningProvider is a tiny specialization of warningProvider
// for the RefreshAll tool — emits warnings on the bulk call.
type refreshAllWarningProvider struct {
	*mockProvider
	refreshAllWarns []string
}

func (m *refreshAllWarningProvider) RefreshAll(ctx context.Context) error {
	for _, w := range m.refreshAllWarns {
		common.Warn(ctx, "%s", w)
	}
	return m.mockProvider.RefreshAll(ctx)
}
