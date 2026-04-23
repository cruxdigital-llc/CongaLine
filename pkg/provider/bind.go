package provider

import (
	"fmt"

	"github.com/cruxdigital-llc/conga-line/pkg/channels"
)

// CheckBindPreconditions validates that a new channel binding can be applied
// to the given agent and reports whether the caller should persist it.
//
// Returns (skip=true, nil) when the exact (platform, id) binding already
// exists on the agent with a matching label (or the caller supplied no new
// label): the caller should treat the operation as an idempotent no-op and
// skip persistence.
//
// Returns (false, err) when the binding conflicts:
//   - Same (platform, id) on this agent with a different non-empty label.
//   - Same (platform, id) on a different agent (would silently overwrite
//     routing.json).
//
// Returns (false, nil) when the binding is new and the caller should
// proceed to validate and persist it.
func CheckBindPreconditions(agent *AgentConfig, binding channels.ChannelBinding, allAgents []AgentConfig) (skip bool, err error) {
	for _, existing := range agent.Channels {
		if existing.Platform == binding.Platform && existing.ID == binding.ID {
			if binding.Label != "" && existing.Label != binding.Label {
				return false, fmt.Errorf(
					"binding %s:%s already exists on agent %q with a different label (%q); unbind first to relabel",
					binding.Platform, binding.ID, agent.Name, existing.Label)
			}
			return true, nil
		}
	}

	for _, other := range allAgents {
		if other.Name == agent.Name {
			continue
		}
		for _, b := range other.Channels {
			if b.Platform == binding.Platform && b.ID == binding.ID {
				return false, fmt.Errorf(
					"channel %s:%s is already bound to agent %q; unbind it there first",
					binding.Platform, binding.ID, other.Name)
			}
		}
	}

	return false, nil
}

// CheckUnbindRequest resolves an unbind request against the agent's current
// bindings and returns the target channel ID the caller should remove, or an
// error describing why the request cannot be satisfied.
//
// Behavior:
//   - No bindings for the platform → error "no %s binding".
//   - id == "" and exactly one binding for the platform → returns that
//     binding's ID (legacy single-binding unbind).
//   - id == "" and 2+ bindings for the platform → returns ErrAmbiguousUnbind
//     (wrapped with an informative message listing counts and platform).
//   - id set and not present on the agent → error "no %s:%s binding".
//   - id set and present → returns id unchanged.
//
// The caller then removes the returned targetID via RemoveBinding.
func CheckUnbindRequest(agent *AgentConfig, platform, id string) (targetID string, err error) {
	bindings := agent.ChannelBindings(platform)
	if len(bindings) == 0 {
		return "", fmt.Errorf("agent %q has no %s binding", agent.Name, platform)
	}

	if id == "" {
		if len(bindings) > 1 {
			return "", fmt.Errorf("agent %q has %d %s bindings; specify which to remove: %w",
				agent.Name, len(bindings), platform, ErrAmbiguousUnbind)
		}
		return bindings[0].ID, nil
	}

	for _, b := range bindings {
		if b.ID == id {
			return id, nil
		}
	}
	return "", fmt.Errorf("agent %q has no %s:%s binding", agent.Name, platform, id)
}
