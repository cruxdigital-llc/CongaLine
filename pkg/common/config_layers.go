package common

import (
	"bytes"
	"encoding/json"

	"github.com/cruxdigital-llc/conga-line/pkg/runtime"
)

// ConfigLayer is one $include layer of an agent's effective OpenClaw config,
// for the `conga agent show-config` view (feature #31). Layers are returned in
// precedence order — Precedence 1 is highest and wins on a conflicting key.
// The managed root (openclaw.json) is highest; among the $include array the
// admin-drift file wins, then the per-agent layer, then the fleet baseline.
type ConfigLayer struct {
	File       string          `json:"file"`              // deployed filename in the agent data dir
	Role       string          `json:"role"`              // human label (managed root / admin drift / per-agent / fleet baseline)
	Owner      string          `json:"owner"`             // who edits it (conga / admin / operator)
	Precedence int             `json:"precedence"`        // 1 = highest (wins on conflict)
	Present    bool            `json:"present"`           // file existed on the agent
	Content    json.RawMessage `json:"content,omitempty"` // parsed JSON content (omitted if absent or unparseable)
}

// ConfigLayerSpec describes a layer's identity before its content is read.
type ConfigLayerSpec struct {
	File  string
	Role  string
	Owner string
}

// EffectiveConfigSpecs returns the precedence-ordered (highest first) layer
// specs for a runtime's effective config. For runtimes without $include layering
// (Hermes) it is just the managed root. The order encodes the verified merge
// model: root > admin-drift > per-agent > fleet.
func EffectiveConfigSpecs(rt runtime.Runtime) []ConfigLayerSpec {
	specs := []ConfigLayerSpec{{File: rt.ConfigFileName(), Role: "managed root", Owner: "conga"}}
	if rt.CustomConfigFileName() == "" {
		return specs // no $include layering
	}
	// Admin-drift is the highest-precedence include (sacrosanct, never clobbered).
	specs = append(specs, ConfigLayerSpec{File: rt.CustomConfigFileName(), Role: "admin drift", Owner: "admin"})
	// ManagedCustomConfigFiles is lowest-precedence-first; show highest first.
	managed := rt.ManagedCustomConfigFiles()
	for i := len(managed) - 1; i >= 0; i-- {
		specs = append(specs, ConfigLayerSpec{File: managed[i], Role: managedLayerRole(managed[i]), Owner: "operator"})
	}
	return specs
}

// managedLayerRole maps a deployed managed-include filename to a human role.
// String-keyed (not via the openclaw consts) to keep common decoupled from the
// runtime implementation; the deployed names are stable.
func managedLayerRole(file string) string {
	switch file {
	case "agent-managed-custom.json":
		return "per-agent"
	case "fleet-custom.json":
		return "fleet baseline"
	default:
		return "managed include"
	}
}

// BuildConfigLayers reads each spec's content via read (which returns the bytes
// and whether the file was present) and assembles the precedence-ordered layer
// view. Content is included only when present and valid JSON; an absent or
// unparseable layer still appears (with Present reflecting reality) so operators
// see the full picture.
func BuildConfigLayers(specs []ConfigLayerSpec, read func(file string) ([]byte, bool)) []ConfigLayer {
	out := make([]ConfigLayer, 0, len(specs))
	for i, s := range specs {
		content, present := read(s.File)
		layer := ConfigLayer{File: s.File, Role: s.Role, Owner: s.Owner, Precedence: i + 1, Present: present}
		if present {
			if trimmed := bytes.TrimSpace(content); json.Valid(trimmed) {
				layer.Content = json.RawMessage(trimmed)
			}
		}
		out = append(out, layer)
	}
	return out
}
