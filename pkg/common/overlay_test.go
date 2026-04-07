package common

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cruxdigital-llc/conga-line/pkg/runtime"
)

func TestIsProtectedPath(t *testing.T) {
	tests := []struct {
		name      string
		rel       string
		rt        runtime.RuntimeName
		protected bool
	}{
		{"MEMORY.md universal", "MEMORY.md", runtime.RuntimeOpenClaw, true},
		{"MEMORY.md hermes", "MEMORY.md", runtime.RuntimeHermes, true},
		{"memory/ subdir", "memory/foo.md", runtime.RuntimeOpenClaw, true},
		{"logs/ subdir", "logs/something.md", runtime.RuntimeOpenClaw, true},
		{"manifest file", ".conga-overlay-manifest.json", runtime.RuntimeOpenClaw, true},
		{"openclaw agents/", "agents/sub.md", runtime.RuntimeOpenClaw, true},
		{"openclaw canvas/", "canvas/note.md", runtime.RuntimeOpenClaw, true},
		{"hermes skills/", "skills/foo.md", runtime.RuntimeHermes, true},
		{"hermes agents/ allowed", "agents/foo.md", runtime.RuntimeHermes, false},
		{"openclaw skills/ allowed", "skills/foo.md", runtime.RuntimeOpenClaw, false},
		{"normal file", "CLIENT.md", runtime.RuntimeOpenClaw, false},
		{"nested normal", "docs/PROJECT.md", runtime.RuntimeOpenClaw, false},
		{"SOUL.md not protected", "SOUL.md", runtime.RuntimeOpenClaw, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsProtectedPath(tt.rel, tt.rt)
			if got != tt.protected {
				t.Errorf("IsProtectedPath(%q, %q) = %v, want %v", tt.rel, tt.rt, got, tt.protected)
			}
		})
	}
}

func TestManifestRoundTrip(t *testing.T) {
	dir := t.TempDir()
	m := OverlayManifest{
		Version: ManifestVersion,
		Files: []OverlayEntry{
			{Path: "CLIENT.md", SHA256: "abc123", Source: "overlay"},
			{Path: "SOUL.md", SHA256: "def456", Source: "composed"},
		},
	}
	if err := WriteOverlayManifest(dir, m); err != nil {
		t.Fatalf("WriteOverlayManifest: %v", err)
	}

	got := ReadOverlayManifest(dir)
	if got == nil {
		t.Fatal("ReadOverlayManifest returned nil")
	}
	if got.Version != ManifestVersion {
		t.Errorf("Version = %d, want %d", got.Version, ManifestVersion)
	}
	if len(got.Files) != 2 {
		t.Fatalf("Files len = %d, want 2", len(got.Files))
	}
	if got.WrittenAt == "" {
		t.Error("WrittenAt should be populated")
	}
}

func TestReadOverlayManifest_Missing(t *testing.T) {
	if got := ReadOverlayManifest(t.TempDir()); got != nil {
		t.Errorf("expected nil for missing manifest, got %+v", got)
	}
}

func TestReadOverlayManifest_Corrupt(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".conga-overlay-manifest.json"), []byte("not json"), 0644)
	if got := ReadOverlayManifest(dir); got != nil {
		t.Errorf("expected nil for corrupt manifest, got %+v", got)
	}
}

func TestReadOverlayManifest_WrongVersion(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".conga-overlay-manifest.json"), []byte(`{"version":99}`), 0644)
	if got := ReadOverlayManifest(dir); got != nil {
		t.Errorf("expected nil for wrong version, got %+v", got)
	}
}

func TestReconcileDeletions(t *testing.T) {
	prev := &OverlayManifest{
		Version: ManifestVersion,
		Files: []OverlayEntry{
			{Path: "OLD.md", SHA256: HashFileContent([]byte("old content")), Source: "agent"},
			{Path: "MODIFIED.md", SHA256: HashFileContent([]byte("original")), Source: "agent"},
			{Path: "SOUL.md", SHA256: HashFileContent([]byte("soul")), Source: "default"},
		},
	}

	newFiles := BehaviorFiles{
		"SOUL.md": BehaviorFile{Content: []byte("soul"), Source: "default"},
		// OLD.md removed, MODIFIED.md removed from overlay source
	}

	hashFile := func(rel string) (string, error) {
		switch rel {
		case "OLD.md":
			return HashFileContent([]byte("old content")), nil // unmodified
		case "MODIFIED.md":
			return HashFileContent([]byte("agent changed this")), nil // modified
		default:
			return "", os.ErrNotExist
		}
	}

	toDelete := reconcileDeletions(prev, newFiles, hashFile)

	if len(toDelete) != 1 || toDelete[0] != "OLD.md" {
		t.Errorf("toDelete = %v, want [OLD.md]", toDelete)
	}
}

func TestReconcileDeletions_NilPrev(t *testing.T) {
	toDelete := reconcileDeletions(nil, BehaviorFiles{}, nil)
	if len(toDelete) != 0 {
		t.Errorf("expected empty toDelete for nil prev, got %v", toDelete)
	}
}
