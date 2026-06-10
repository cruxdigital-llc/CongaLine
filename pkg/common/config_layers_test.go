package common

import (
	"testing"

	"github.com/cruxdigital-llc/conga-line/pkg/runtime"

	// Register runtimes so runtime.Get resolves.
	_ "github.com/cruxdigital-llc/conga-line/pkg/runtime/hermes"
	_ "github.com/cruxdigital-llc/conga-line/pkg/runtime/openclaw"
)

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
