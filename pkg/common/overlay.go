package common

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cruxdigital-llc/conga-line/pkg/runtime"
)

// ManifestVersion is the current behavior manifest schema version.
const ManifestVersion = 1

// MaxBehaviorFileSize is the maximum size of a single behavior file (1 MiB).
const MaxBehaviorFileSize = 1 << 20

// OverlayManifest tracks which files were placed in the agent's workspace.
type OverlayManifest struct {
	Version   int            `json:"version"`
	Files     []OverlayEntry `json:"files"`
	WrittenAt string         `json:"written_at,omitempty"`
}

// OverlayEntry records a single file placed in the workspace.
type OverlayEntry struct {
	Path   string `json:"path"`   // workspace-relative, forward slashes
	SHA256 string `json:"sha256"` // hex-encoded
	Source string `json:"source"` // "agent" or "default"
}

const manifestFileName = ".conga-overlay-manifest.json"

// Universal protected paths — apply to every runtime.
var protectedWorkspacePaths = []string{
	"MEMORY.md",
	"memory/",
	"logs/",
	manifestFileName,
}

// Per-runtime protected paths.
var runtimeProtectedPaths = map[runtime.RuntimeName][]string{
	runtime.RuntimeOpenClaw: {"agents/", "canvas/", "cron/", "devices/", "identity/", "media/"},
	runtime.RuntimeHermes:   {"skills/"},
}

// IsProtectedPath returns true if rel matches the protected path list for the
// given runtime. Paths ending in "/" are treated as directory prefixes.
func IsProtectedPath(rel string, rt runtime.RuntimeName) bool {
	rel = filepath.ToSlash(rel)
	for _, p := range protectedWorkspacePaths {
		if rel == p {
			return true
		}
		if strings.HasSuffix(p, "/") && strings.HasPrefix(rel, p) {
			return true
		}
	}
	for _, p := range runtimeProtectedPaths[rt] {
		if rel == p {
			return true
		}
		if strings.HasSuffix(p, "/") && strings.HasPrefix(rel, p) {
			return true
		}
	}
	return false
}

// ParseOverlayManifest parses manifest JSON bytes.
// Returns nil if the data can't be parsed or has the wrong version.
func ParseOverlayManifest(data []byte) *OverlayManifest {
	var m OverlayManifest
	if err := json.Unmarshal(data, &m); err != nil {
		fmt.Fprintf(os.Stderr, "WARNING: behavior manifest corrupt, treating as empty: %v\n", err)
		return nil
	}
	if m.Version != ManifestVersion {
		fmt.Fprintf(os.Stderr, "WARNING: behavior manifest version %d != %d, treating as empty\n", m.Version, ManifestVersion)
		return nil
	}
	return &m
}

// ReadOverlayManifest reads the manifest from a workspace directory.
// Returns nil if the file doesn't exist or can't be parsed.
func ReadOverlayManifest(workspaceDir string) *OverlayManifest {
	data, err := os.ReadFile(filepath.Join(workspaceDir, manifestFileName))
	if err != nil {
		return nil
	}
	return ParseOverlayManifest(data)
}

// MarshalOverlayManifest serializes a manifest to JSON bytes.
func MarshalOverlayManifest(m OverlayManifest) ([]byte, error) {
	m.WrittenAt = time.Now().UTC().Format(time.RFC3339)
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal behavior manifest: %w", err)
	}
	return data, nil
}

// WriteOverlayManifest writes the manifest to a workspace directory.
func WriteOverlayManifest(workspaceDir string, m OverlayManifest) error {
	data, err := MarshalOverlayManifest(m)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(workspaceDir, manifestFileName), data, 0644)
}

// HashFileContent returns the hex-encoded SHA-256 of data.
func HashFileContent(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

// reconcileDeletions computes which files from the previous manifest should be
// deleted. A file is deleted only if it was agent-sourced, is no longer in the
// new file set, and its workspace content still matches the manifest hash.
func reconcileDeletions(prev *OverlayManifest, newFiles BehaviorFiles, hashFile func(rel string) (string, error)) (toDelete []string) {
	if prev == nil {
		return nil
	}
	for _, entry := range prev.Files {
		if entry.Source != "agent" && entry.Source != "overlay" {
			// "overlay" accepted for backwards compat with existing manifests
			continue
		}
		if _, stillPresent := newFiles[entry.Path]; stillPresent {
			continue
		}
		currentHash, err := hashFile(entry.Path)
		if err != nil {
			continue
		}
		if currentHash == entry.SHA256 {
			toDelete = append(toDelete, entry.Path)
		} else {
			fmt.Fprintf(os.Stderr, "WARNING: behavior file %s was modified in workspace; not deleting — remove it manually if desired\n", entry.Path)
		}
	}
	return toDelete
}

// buildManifest constructs a new manifest from the given file set.
func buildManifest(files BehaviorFiles) OverlayManifest {
	m := OverlayManifest{Version: ManifestVersion}
	for path, f := range files {
		m.Files = append(m.Files, OverlayEntry{
			Path:   path,
			SHA256: HashFileContent(f.Content),
			Source: f.Source,
		})
	}
	return m
}
