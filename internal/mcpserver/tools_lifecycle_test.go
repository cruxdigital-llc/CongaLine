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

// TestRefreshAgent_PropagatesWarningsToToolResult guards the refresh
// tool's WarningSink wiring (toolRefreshAgent → withSink → okWithWarnings).
// The mock provider emits via common.Warn(ctx, ...); the test asserts
// those warnings appear in the MCP tool result text. It does NOT verify
// that real providers route through common.Warn (vs fmt.Fprintf to
// stderr) — followups #13 tracks the broader migration of remaining
// stderr sites. This is a CRIT-5 wiring guard, not a provider-level
// coverage assertion.
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
// guard for the provision tool: verifies the `withSink` + okWithWarnings
// wiring inside toolProvisionAgent surfaces common.Warn output in the
// MCP result text. A future change that drops the `ctx, sink := withSink(ctx)`
// line — or that swaps okWithWarnings back to a plain success result —
// would fail this test. Like the refresh variant, it doesn't assert
// real providers actually route through common.Warn (see followups #13).
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

// TestUnpauseAgent_PropagatesWarningsToToolResult guards the unpause
// tool's WarningSink wiring. Verifies that if UnpauseAgent emits via
// common.Warn, those warnings reach the MCP result text. The test
// uses a mock that emits directly from UnpauseAgent — it does not
// exercise AWS's internal unpause→refresh self-heal path (followups #5),
// just the MCP wrapping around whatever Unpause emits.
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

// TestRefreshAll_PropagatesWarningsToToolResult guards the bulk-refresh
// tool's WarningSink wiring. A future change to toolRefreshAll that
// dropped the `withSink` / okWithWarnings pattern would fail this test.
// Per-agent warnings (N agents × multiple steps) all flow through the
// same sink, so a single-mock emission is sufficient to exercise the
// wrapping. Like the per-agent variants, this guards MCP wiring, not
// real-provider Warn usage (see followups #13).
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
