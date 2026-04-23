package provider

import "errors"

// ErrNotFound indicates a resource (agent, secret, etc.) was not found.
// Provider implementations should wrap this error so callers can check with errors.Is().
var ErrNotFound = errors.New("resource not found")

// ErrAmbiguousUnbind indicates UnbindChannel was called with an empty id
// on an agent that has multiple bindings for the given platform. Callers
// should surface the available bindings and ask the user to specify which
// one to remove. Wrap this error so callers can check with errors.Is().
var ErrAmbiguousUnbind = errors.New("ambiguous unbind: agent has multiple bindings for this platform")
