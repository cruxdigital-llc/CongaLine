package common

import (
	"bytes"
	"context"
	"os"
	"strings"
	"sync"
	"testing"
)

func TestWarn_WithSink_AccumulatesIntoSink(t *testing.T) {
	sink := &WarningSink{}
	ctx := WithWarningSink(context.Background(), sink)

	Warn(ctx, "first %s", "warning")
	Warn(ctx, "second")

	got := sink.Drain()
	if len(got) != 2 {
		t.Fatalf("expected 2 warnings, got %d: %v", len(got), got)
	}
	if got[0] != "first warning" {
		t.Errorf("got[0] = %q, want %q", got[0], "first warning")
	}
	if got[1] != "second" {
		t.Errorf("got[1] = %q, want %q", got[1], "second")
	}
}

func TestWarn_NoSink_FallsThroughToStderr(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	origStderr := os.Stderr
	os.Stderr = w
	defer func() { os.Stderr = origStderr }()

	Warn(context.Background(), "fell through %s", "ok")

	w.Close()
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(r); err != nil {
		t.Fatal(err)
	}

	out := buf.String()
	if !strings.Contains(out, "Warning: fell through ok") {
		t.Errorf("expected stderr to contain warning, got %q", out)
	}
}

func TestWarningSink_Drain_ClearsState(t *testing.T) {
	sink := &WarningSink{}
	sink.Add("one")
	sink.Add("two")

	first := sink.Drain()
	if len(first) != 2 {
		t.Fatalf("first drain: got %d, want 2", len(first))
	}

	second := sink.Drain()
	if len(second) != 0 {
		t.Errorf("second drain: got %d, want 0", len(second))
	}
}

func TestWarningSink_Concurrent_NoRace(t *testing.T) {
	sink := &WarningSink{}
	ctx := WithWarningSink(context.Background(), sink)

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			Warn(ctx, "concurrent")
		}()
	}
	wg.Wait()

	got := sink.Drain()
	if len(got) != 50 {
		t.Errorf("expected 50 warnings, got %d", len(got))
	}
}
