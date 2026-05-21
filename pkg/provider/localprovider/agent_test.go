package localprovider

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cruxdigital-llc/conga-line/pkg/provider"
)

func TestGetAgent_Success(t *testing.T) {
	p := testProvider(t)
	if err := os.MkdirAll(p.agentsDir(), 0700); err != nil {
		t.Fatalf("mkdir agents: %v", err)
	}
	path := filepath.Join(p.agentsDir(), "myagent.json")
	if err := os.WriteFile(path, []byte(`{"type":"user","gateway_port":18789}`), 0644); err != nil {
		t.Fatalf("write agent file: %v", err)
	}

	cfg, err := p.GetAgent(context.Background(), "myagent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Name != "myagent" {
		t.Errorf("expected name myagent, got %s", cfg.Name)
	}
}

func TestGetAgent_NotFound_ReturnsErrNotFound(t *testing.T) {
	p := testProvider(t)
	if err := os.MkdirAll(p.agentsDir(), 0700); err != nil {
		t.Fatalf("mkdir agents: %v", err)
	}

	_, err := p.GetAgent(context.Background(), "ghost")
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, provider.ErrNotFound) {
		t.Errorf("expected provider.ErrNotFound, got: %v", err)
	}
}

// Regression: non-IsNotExist read failures must NOT be reported as
// "agent not found". Mirrors the AWS/remote fixes — verifies the local
// provider's existing classification doesn't drift back into the bug.
func TestGetAgent_GenericReadErrorIsSurfaced(t *testing.T) {
	p := testProvider(t)
	if err := os.MkdirAll(p.agentsDir(), 0700); err != nil {
		t.Fatalf("mkdir agents: %v", err)
	}
	// Make the "agent file" a directory — os.ReadFile then returns an error
	// that is NOT os.IsNotExist, so the not-found path must be skipped.
	dirPath := filepath.Join(p.agentsDir(), "broken.json")
	if err := os.MkdirAll(dirPath, 0700); err != nil {
		t.Fatalf("mkdir broken: %v", err)
	}

	_, err := p.GetAgent(context.Background(), "broken")
	if err == nil {
		t.Fatal("expected error")
	}
	if errors.Is(err, provider.ErrNotFound) {
		t.Errorf("expected error NOT to be ErrNotFound for a non-IsNotExist read failure, got: %v", err)
	}
	if strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' framing to be absent for a generic read failure, got: %v", err)
	}
}
