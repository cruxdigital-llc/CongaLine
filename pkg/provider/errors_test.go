package provider

import (
	"errors"
	"fmt"
	"testing"
)

func TestErrNotFound_Wrapping(t *testing.T) {
	// Simulate how providers wrap ErrNotFound
	wrapped := fmt.Errorf("agent %q not found: %w", "myagent", ErrNotFound)

	if !errors.Is(wrapped, ErrNotFound) {
		t.Error("errors.Is should match ErrNotFound through wrapping")
	}

	// Double-wrapped (e.g., caller wraps again)
	doubleWrapped := fmt.Errorf("operation failed: %w", wrapped)
	if !errors.Is(doubleWrapped, ErrNotFound) {
		t.Error("errors.Is should match ErrNotFound through double wrapping")
	}
}
