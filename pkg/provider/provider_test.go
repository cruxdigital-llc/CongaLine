package provider

import (
	"testing"

	"github.com/cruxdigital-llc/conga-line/pkg/channels"
)

func TestChannelBinding_ReturnsFirstMatch(t *testing.T) {
	a := &AgentConfig{
		Channels: []channels.ChannelBinding{
			{Platform: "slack", ID: "C1"},
			{Platform: "slack", ID: "C2"},
		},
	}
	got := a.ChannelBinding("slack")
	if got == nil || got.ID != "C1" {
		t.Fatalf("want first slack binding C1, got %+v", got)
	}
}

func TestChannelBinding_NilWhenNone(t *testing.T) {
	a := &AgentConfig{}
	if got := a.ChannelBinding("slack"); got != nil {
		t.Fatalf("want nil, got %+v", got)
	}
}

func TestChannelBindings_ReturnsAllMatches(t *testing.T) {
	a := &AgentConfig{
		Channels: []channels.ChannelBinding{
			{Platform: "slack", ID: "C1", Label: "#legal"},
			{Platform: "telegram", ID: "123"},
			{Platform: "slack", ID: "C2", Label: "#sales"},
			{Platform: "slack", ID: "C3"},
		},
	}
	got := a.ChannelBindings("slack")
	if len(got) != 3 {
		t.Fatalf("want 3 slack bindings, got %d: %+v", len(got), got)
	}
	wantIDs := []string{"C1", "C2", "C3"}
	for i, b := range got {
		if b.ID != wantIDs[i] {
			t.Errorf("bindings[%d].ID = %q, want %q", i, b.ID, wantIDs[i])
		}
	}
}

func TestChannelBindings_EmptyWhenNone(t *testing.T) {
	a := &AgentConfig{
		Channels: []channels.ChannelBinding{
			{Platform: "telegram", ID: "123"},
		},
	}
	got := a.ChannelBindings("slack")
	if len(got) != 0 {
		t.Fatalf("want empty slice, got %+v", got)
	}
}

func TestChannelBindings_PreservesInsertionOrder(t *testing.T) {
	a := &AgentConfig{
		Channels: []channels.ChannelBinding{
			{Platform: "slack", ID: "Czzz"},
			{Platform: "slack", ID: "Caaa"},
			{Platform: "slack", ID: "Cmmm"},
		},
	}
	got := a.ChannelBindings("slack")
	want := []string{"Czzz", "Caaa", "Cmmm"}
	for i, b := range got {
		if b.ID != want[i] {
			t.Errorf("bindings[%d].ID = %q, want %q (insertion order must be preserved — do not sort)",
				i, b.ID, want[i])
		}
	}
}

func TestChannelBindings_EmptyAgentReturnsEmpty(t *testing.T) {
	a := &AgentConfig{}
	if got := a.ChannelBindings("slack"); len(got) != 0 {
		t.Fatalf("want empty slice, got %+v", got)
	}
}
