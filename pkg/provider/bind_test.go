package provider

import (
	"errors"
	"strings"
	"testing"

	"github.com/cruxdigital-llc/conga-line/pkg/channels"
)

func TestCheckBindPreconditions_NewBinding_Proceed(t *testing.T) {
	agent := &AgentConfig{Name: "acme"}
	skip, err := CheckBindPreconditions(agent,
		channels.ChannelBinding{Platform: "slack", ID: "C1"},
		nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if skip {
		t.Fatalf("want skip=false for a new binding")
	}
}

func TestCheckBindPreconditions_ExactDuplicate_Idempotent(t *testing.T) {
	agent := &AgentConfig{
		Name: "acme",
		Channels: []channels.ChannelBinding{
			{Platform: "slack", ID: "C1", Label: "#legal"},
		},
	}
	skip, err := CheckBindPreconditions(agent,
		channels.ChannelBinding{Platform: "slack", ID: "C1", Label: "#legal"},
		nil)
	if err != nil {
		t.Fatalf("idempotent rebind should not error, got: %v", err)
	}
	if !skip {
		t.Fatalf("want skip=true for exact duplicate with matching label")
	}
}

func TestCheckBindPreconditions_ExactID_EmptyLabel_Idempotent(t *testing.T) {
	agent := &AgentConfig{
		Name: "acme",
		Channels: []channels.ChannelBinding{
			{Platform: "slack", ID: "C1", Label: "#legal"},
		},
	}
	// Caller supplies no label — treat as no-op, keep existing label.
	skip, err := CheckBindPreconditions(agent,
		channels.ChannelBinding{Platform: "slack", ID: "C1"},
		nil)
	if err != nil {
		t.Fatalf("empty-label rebind should not error, got: %v", err)
	}
	if !skip {
		t.Fatalf("want skip=true when new label is empty")
	}
}

func TestCheckBindPreconditions_ExactID_DifferentLabel_Errors(t *testing.T) {
	agent := &AgentConfig{
		Name: "acme",
		Channels: []channels.ChannelBinding{
			{Platform: "slack", ID: "C1", Label: "#legal"},
		},
	}
	skip, err := CheckBindPreconditions(agent,
		channels.ChannelBinding{Platform: "slack", ID: "C1", Label: "#legal-vendor"},
		nil)
	if err == nil {
		t.Fatalf("want error for label mismatch, got nil")
	}
	if skip {
		t.Fatalf("want skip=false when error is returned")
	}
	if !strings.Contains(err.Error(), "different label") {
		t.Errorf("error message should mention label mismatch, got: %v", err)
	}
	if !strings.Contains(err.Error(), "#legal") {
		t.Errorf("error should name the existing label, got: %v", err)
	}
}

func TestCheckBindPreconditions_CrossAgentCollision_Errors(t *testing.T) {
	target := &AgentConfig{Name: "acme"}
	others := []AgentConfig{
		{Name: "acme"}, // same agent — should be skipped
		{
			Name: "payroll",
			Channels: []channels.ChannelBinding{
				{Platform: "slack", ID: "C1", Label: "#finance"},
			},
		},
	}
	skip, err := CheckBindPreconditions(target,
		channels.ChannelBinding{Platform: "slack", ID: "C1"},
		others)
	if err == nil {
		t.Fatalf("want error for cross-agent collision, got nil")
	}
	if skip {
		t.Fatalf("want skip=false when error is returned")
	}
	if !strings.Contains(err.Error(), "payroll") {
		t.Errorf("error should name the colliding agent, got: %v", err)
	}
}

func TestCheckBindPreconditions_DifferentPlatforms_Proceed(t *testing.T) {
	// Same ID but different platform is not a collision.
	agent := &AgentConfig{
		Name: "acme",
		Channels: []channels.ChannelBinding{
			{Platform: "slack", ID: "123"},
		},
	}
	others := []AgentConfig{
		{
			Name: "payroll",
			Channels: []channels.ChannelBinding{
				{Platform: "telegram", ID: "123"},
			},
		},
	}
	skip, err := CheckBindPreconditions(agent,
		channels.ChannelBinding{Platform: "telegram", ID: "999"},
		others)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if skip {
		t.Fatalf("want skip=false for a new binding")
	}
}

func TestCheckBindPreconditions_MultipleBindingsSamePlatform_Proceed(t *testing.T) {
	// Core of the feature: N bindings per platform per agent.
	agent := &AgentConfig{
		Name: "contracts",
		Channels: []channels.ChannelBinding{
			{Platform: "slack", ID: "C1"},
			{Platform: "slack", ID: "C2"},
		},
	}
	skip, err := CheckBindPreconditions(agent,
		channels.ChannelBinding{Platform: "slack", ID: "C3"},
		nil)
	if err != nil {
		t.Fatalf("want no error adding a third Slack binding, got: %v", err)
	}
	if skip {
		t.Fatalf("want skip=false for a new binding")
	}
}

func TestCheckBindPreconditions_IdempotentBeatsCrossAgent(t *testing.T) {
	// Safety: if (impossibly) a binding appears on both this agent AND another,
	// the idempotent path must fire first so repeat bind-to-self doesn't
	// falsely claim cross-agent collision against itself via a different path.
	agent := &AgentConfig{
		Name: "acme",
		Channels: []channels.ChannelBinding{
			{Platform: "slack", ID: "C1"},
		},
	}
	others := []AgentConfig{
		{Name: "acme"},    // same — skipped by name filter
		{Name: "payroll"}, // no bindings
	}
	skip, err := CheckBindPreconditions(agent,
		channels.ChannelBinding{Platform: "slack", ID: "C1"},
		others)
	if err != nil {
		t.Fatalf("idempotent rebind to self should not error, got: %v", err)
	}
	if !skip {
		t.Fatalf("want skip=true for idempotent rebind to self")
	}
}

// --- CheckUnbindRequest ---

func TestCheckUnbindRequest_SingleBinding_EmptyID_ReturnsIt(t *testing.T) {
	agent := &AgentConfig{
		Name: "ada",
		Channels: []channels.ChannelBinding{
			{Platform: "slack", ID: "U1"},
		},
	}
	id, err := CheckUnbindRequest(agent, "slack", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "U1" {
		t.Errorf("targetID = %q, want U1", id)
	}
}

func TestCheckUnbindRequest_SpecificID_Matches(t *testing.T) {
	agent := &AgentConfig{
		Name: "contracts",
		Channels: []channels.ChannelBinding{
			{Platform: "slack", ID: "C1"},
			{Platform: "slack", ID: "C2"},
		},
	}
	id, err := CheckUnbindRequest(agent, "slack", "C2")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "C2" {
		t.Errorf("targetID = %q, want C2", id)
	}
}

func TestCheckUnbindRequest_MultipleBindings_EmptyID_ErrAmbiguous(t *testing.T) {
	agent := &AgentConfig{
		Name: "contracts",
		Channels: []channels.ChannelBinding{
			{Platform: "slack", ID: "C1"},
			{Platform: "slack", ID: "C2"},
			{Platform: "slack", ID: "C3"},
		},
	}
	_, err := CheckUnbindRequest(agent, "slack", "")
	if err == nil {
		t.Fatal("want error, got nil")
	}
	if !errors.Is(err, ErrAmbiguousUnbind) {
		t.Errorf("error should wrap ErrAmbiguousUnbind, got: %v", err)
	}
	if !strings.Contains(err.Error(), "3") {
		t.Errorf("error should mention the count, got: %v", err)
	}
}

func TestCheckUnbindRequest_NoBindings_Errors(t *testing.T) {
	agent := &AgentConfig{Name: "naked"}
	_, err := CheckUnbindRequest(agent, "slack", "")
	if err == nil {
		t.Fatal("want error for no-bindings, got nil")
	}
	if errors.Is(err, ErrAmbiguousUnbind) {
		t.Errorf("no-bindings error should not match ErrAmbiguousUnbind")
	}
}

func TestCheckUnbindRequest_SpecificID_NotFound_Errors(t *testing.T) {
	agent := &AgentConfig{
		Name: "contracts",
		Channels: []channels.ChannelBinding{
			{Platform: "slack", ID: "C1"},
		},
	}
	_, err := CheckUnbindRequest(agent, "slack", "C999")
	if err == nil {
		t.Fatal("want error for unknown id, got nil")
	}
	if !strings.Contains(err.Error(), "C999") {
		t.Errorf("error should mention the missing id, got: %v", err)
	}
}

func TestCheckUnbindRequest_OtherPlatformBindings_Ignored(t *testing.T) {
	// An agent with many telegram bindings and exactly one slack binding
	// should have the slack binding resolvable without id.
	agent := &AgentConfig{
		Name: "polyglot",
		Channels: []channels.ChannelBinding{
			{Platform: "telegram", ID: "100"},
			{Platform: "telegram", ID: "200"},
			{Platform: "slack", ID: "C1"},
		},
	}
	id, err := CheckUnbindRequest(agent, "slack", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "C1" {
		t.Errorf("targetID = %q, want C1", id)
	}
}

// Sentinel error check — if a future caller type-asserts on a specific error
// type, make sure we don't accidentally wrap the sentinel ErrNotFound here.
func TestCheckBindPreconditions_ErrorsAreNotSentinels(t *testing.T) {
	agent := &AgentConfig{
		Name: "acme",
		Channels: []channels.ChannelBinding{
			{Platform: "slack", ID: "C1", Label: "#legal"},
		},
	}
	_, err := CheckBindPreconditions(agent,
		channels.ChannelBinding{Platform: "slack", ID: "C1", Label: "different"},
		nil)
	if errors.Is(err, ErrNotFound) {
		t.Error("label-mismatch error must not match ErrNotFound")
	}
}
