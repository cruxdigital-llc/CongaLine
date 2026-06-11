package common

import (
	"encoding/json"
	"testing"

	"github.com/cruxdigital-llc/conga-line/pkg/provider"
	"github.com/cruxdigital-llc/conga-line/pkg/runtime"

	// Register runtimes so runtime.Get resolves.
	_ "github.com/cruxdigital-llc/conga-line/pkg/runtime/hermes"
	_ "github.com/cruxdigital-llc/conga-line/pkg/runtime/openclaw"
)

// TestCompositionPrecedenceContract pins the layer-composition contract that
// OpenClaw's deep-merge relies on, across the two code paths that represent it:
//   - the generator's deployed "$include" array — DEPLOY order, lowest precedence
//     first (OpenClaw merges later-in-array wins);
//   - EffectiveConfigSpecs — the `show-config` view, highest precedence first.
//
// The two must be exact reverses of each other (for the include layers) and must
// match the documented model root > admin-drift > per-agent > fleet. If anyone
// reorders ManagedCustomConfigFiles, the generator array, or EffectiveConfigSpecs,
// this fails — so the operator-facing precedence can never silently drift from
// what OpenClaw actually merges. (The merge RESULT — union + later-wins — is
// verified live against real OpenClaw in the integration suite.)
func TestCompositionPrecedenceContract(t *testing.T) {
	rt, err := runtime.Get(runtime.RuntimeOpenClaw)
	if err != nil {
		t.Fatal(err)
	}

	// Deployed $include array (lowest precedence first).
	out, err := rt.GenerateConfig(runtime.ConfigParams{
		Agent:        provider.AgentConfig{Name: "t", Type: provider.AgentTypeUser, GatewayPort: 18789},
		Secrets:      provider.SharedSecrets{Values: map[string]string{}},
		GatewayToken: "fixed-token",
	})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	var cfg map[string]any
	if err := json.Unmarshal(out, &cfg); err != nil {
		t.Fatalf("decode: %v", err)
	}
	rawInc, ok := cfg["$include"].([]any)
	if !ok {
		t.Fatalf("missing $include array: %T", cfg["$include"])
	}
	includeOrder := make([]string, len(rawInc))
	for i, v := range rawInc {
		includeOrder[i] = v.(string)
	}

	// show-config view (highest precedence first).
	specs := EffectiveConfigSpecs(rt)

	// (a) Highest-precedence layer is the managed root.
	if specs[0].File != rt.ConfigFileName() {
		t.Fatalf("rank 1 must be the managed root %q, got %q", rt.ConfigFileName(), specs[0].File)
	}

	// (b) The include layers (specs[1:], high→low) reversed == deployed $include
	//     array (low→high). This is the contract both paths must honor.
	includeSpecs := specs[1:]
	if len(includeSpecs) != len(includeOrder) {
		t.Fatalf("include count mismatch: show-config has %d, $include has %d", len(includeSpecs), len(includeOrder))
	}
	for i, s := range includeSpecs {
		want := includeOrder[len(includeOrder)-1-i]
		if s.File != want {
			t.Errorf("precedence drift at include rank %d: show-config=%q but $include(reversed)=%q", i+2, s.File, want)
		}
	}

	// (c) Pin the documented model explicitly so the intent is self-evident.
	// (Precedence is positional in EffectiveConfigSpecs — BuildConfigLayers stamps
	// it as index+1, covered by TestBuildConfigLayers.)
	wantOrder := []string{"openclaw.json", "agent-custom.json", "agent-managed-custom.json", "fleet-custom.json"}
	if len(specs) != len(wantOrder) {
		t.Fatalf("expected %d layers, got %d", len(wantOrder), len(specs))
	}
	for i, want := range wantOrder {
		if specs[i].File != want {
			t.Errorf("rank %d: got %q, want %q", i+1, specs[i].File, want)
		}
	}
}

func TestEffectiveConfigSpecs_OpenClaw(t *testing.T) {
	rt, err := runtime.Get(runtime.RuntimeOpenClaw)
	if err != nil {
		t.Fatal(err)
	}
	specs := EffectiveConfigSpecs(rt)
	// Precedence high → low: root, admin drift, per-agent, fleet.
	want := []ConfigLayerSpec{
		{File: "openclaw.json", Role: "managed root", Owner: "conga"},
		{File: "agent-custom.json", Role: "admin drift", Owner: "admin"},
		{File: "agent-managed-custom.json", Role: "per-agent", Owner: "operator"},
		{File: "fleet-custom.json", Role: "fleet baseline", Owner: "operator"},
	}
	if len(specs) != len(want) {
		t.Fatalf("got %d specs, want %d: %+v", len(specs), len(want), specs)
	}
	for i := range want {
		if specs[i] != want[i] {
			t.Errorf("spec[%d] = %+v, want %+v", i, specs[i], want[i])
		}
	}
}

func TestEffectiveConfigSpecs_Hermes_RootOnly(t *testing.T) {
	rt, err := runtime.Get(runtime.RuntimeHermes)
	if err != nil {
		t.Fatal(err)
	}
	specs := EffectiveConfigSpecs(rt)
	if len(specs) != 1 || specs[0].Role != "managed root" {
		t.Fatalf("hermes should have only the managed root layer, got %+v", specs)
	}
}

func TestBuildConfigLayers(t *testing.T) {
	specs := []ConfigLayerSpec{
		{File: "openclaw.json", Role: "managed root", Owner: "conga"},
		{File: "agent-custom.json", Role: "admin drift", Owner: "admin"},
		{File: "fleet-custom.json", Role: "fleet baseline", Owner: "operator"},
	}
	read := func(file string) ([]byte, bool) {
		switch file {
		case "openclaw.json":
			return []byte(`{"gateway":{"port":18789}}`), true
		case "agent-custom.json":
			return nil, false // absent on this agent
		case "fleet-custom.json":
			return []byte("  not json  "), true // present but unparseable
		}
		return nil, false
	}
	layers := BuildConfigLayers(specs, read)
	if len(layers) != 3 {
		t.Fatalf("got %d layers", len(layers))
	}
	// Precedence assigned by order, 1 = highest.
	if layers[0].Precedence != 1 || layers[2].Precedence != 3 {
		t.Fatalf("precedence not assigned by order: %+v", layers)
	}
	// Root: present + parseable → content set.
	if !layers[0].Present || len(layers[0].Content) == 0 {
		t.Errorf("root layer should be present with content: %+v", layers[0])
	}
	// Admin: absent.
	if layers[1].Present {
		t.Errorf("admin layer should be absent")
	}
	// Fleet: present but unparseable → no content, but still listed.
	if !layers[2].Present || len(layers[2].Content) != 0 {
		t.Errorf("unparseable layer should be present with no content: %+v", layers[2])
	}
}
