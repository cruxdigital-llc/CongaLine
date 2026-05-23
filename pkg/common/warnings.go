package common

import (
	"context"
	"fmt"
	"os"
	"sync"
)

// WarningSink collects non-fatal warnings from provider lifecycle methods
// (provision, refresh, unpause) so they can be surfaced to operators
// running through MCP, where stderr is invisible. CLI callers do not
// attach a sink; warnings fall through to stderr as before.
type WarningSink struct {
	mu       sync.Mutex
	warnings []string
}

func (s *WarningSink) Add(msg string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.warnings = append(s.warnings, msg)
}

// Drain returns the accumulated warnings and clears the sink. Callers
// typically drain once after the provider call completes.
func (s *WarningSink) Drain() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := s.warnings
	s.warnings = nil
	return out
}

type warningSinkKey struct{}

// WithWarningSink attaches a sink to ctx. The MCP server attaches one
// per tool call so warnings emitted by provider methods can be drained
// and surfaced in the tool result string.
func WithWarningSink(ctx context.Context, sink *WarningSink) context.Context {
	return context.WithValue(ctx, warningSinkKey{}, sink)
}

func warningSinkFromContext(ctx context.Context) *WarningSink {
	if v := ctx.Value(warningSinkKey{}); v != nil {
		if sink, ok := v.(*WarningSink); ok {
			return sink
		}
	}
	return nil
}

// Warn appends a warning to the context's sink if one is attached,
// otherwise writes to stderr with a "Warning: " prefix. Use this for
// non-fatal operational warnings emitted from provider lifecycle paths
// that the MCP server should be able to surface — stderr is invisible
// under MCP, so a warn-and-continue without a sink hides the message
// from the operator.
func Warn(ctx context.Context, format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	if sink := warningSinkFromContext(ctx); sink != nil {
		sink.Add(msg)
		return
	}
	fmt.Fprintln(os.Stderr, "Warning: "+msg)
}
