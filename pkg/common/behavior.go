package common

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/cruxdigital-llc/conga-line/pkg/channels"
	"github.com/cruxdigital-llc/conga-line/pkg/provider"
	"github.com/cruxdigital-llc/conga-line/pkg/runtime"
)

// Subdirectory names looked up inside the directory that callers pass as
// behaviorDir. Two layouts coexist during the 2026-05-XX rename:
//
//   - **New layout**: behaviorDir contains per-agent directories directly
//     (<behaviorDir>/<name>/) and a "_defaults" subdir for shipped defaults
//     (<behaviorDir>/_defaults/<runtime>/<type>/). Operators reach this state
//     after running scripts/migrate-behavior-to-agents.sh.
//   - **Legacy layout**: behaviorDir contains an "agents" subdir for
//     per-agent (<behaviorDir>/agents/<name>/) and a "default" subdir for
//     shipped defaults (<behaviorDir>/default/<runtime>/<type>/). This is
//     the pre-rename production state.
//
// Provider helpers (e.g. localprovider.behaviorDir) decide which directory
// to return — typically the new dir if it exists on disk, else the legacy
// dir for back-compat. The loader detects the layout INSIDE that directory
// by trying the new subdirs first and falling back to legacy with a one-time
// deprecation warning.
const (
	// New layout subdir names (post-2026-05-XX rename).
	defaultsSubdirNew = "_defaults"

	// Legacy layout subdir names, retained for one release as a back-compat
	// fallback. Drop these and the legacyPathFallbackEnabled branch in the
	// next minor release. Migration script: scripts/migrate-behavior-to-agents.sh.
	legacyAgentsSubdir   = "agents"
	legacyDefaultsSubdir = "default"

	// Single feature gate for the entire fallback codepath. Flip to false (or
	// delete all gated blocks + the legacy constants) to fully retire legacy.
	legacyPathFallbackEnabled = true
)

// behaviorPathWarningOnce dedupes "legacy path read" warnings across both
// loader entry points (resolveBehaviorFiles + LoadAgentOverlay) so the same
// file produces at most one stderr warning per process.
var behaviorPathWarningOnce sync.Map // map[string]struct{}

// warnLegacyBehaviorPath logs the first occurrence (per path) of a legacy
// directory read. Subsequent reads of the same path are silent.
func warnLegacyBehaviorPath(path string) {
	if _, loaded := behaviorPathWarningOnce.LoadOrStore("legacy-path:"+path, struct{}{}); loaded {
		return
	}
	fmt.Fprintf(os.Stderr,
		"warning: reading %s from legacy path. Run scripts/migrate-behavior-to-agents.sh to migrate, or rename behavior/ -> agents/. This fallback will be removed in the next release.\n",
		path)
}

// resolveAgentDir returns the directory containing per-agent files for the
// named agent. Prefers the new layout (<behaviorDir>/<name>/); when that's
// absent and the fallback is enabled, returns the legacy layout
// (<behaviorDir>/agents/<name>/) with isLegacy=true so the caller can emit
// a deprecation warning per file read.
func resolveAgentDir(behaviorDir, agentName string) (path string, isLegacy bool) {
	newPath := filepath.Join(behaviorDir, agentName)
	if _, err := os.Stat(newPath); err == nil {
		return newPath, false
	}
	if legacyPathFallbackEnabled {
		legacyPath := filepath.Join(behaviorDir, legacyAgentsSubdir, agentName)
		if _, err := os.Stat(legacyPath); err == nil {
			return legacyPath, true
		}
	}
	// Miss — caller's per-file os.ReadFile will surface file-not-found.
	return newPath, false
}

// resolveDefaultDir returns the directory containing shipped default files
// for the given runtime + agent type. Same prefer-new-fallback-to-legacy
// semantics as resolveAgentDir.
func resolveDefaultDir(behaviorDir, rtName, agentType string) (path string, isLegacy bool) {
	newPath := filepath.Join(behaviorDir, defaultsSubdirNew, rtName, agentType)
	if _, err := os.Stat(newPath); err == nil {
		return newPath, false
	}
	if legacyPathFallbackEnabled {
		legacyPath := filepath.Join(behaviorDir, legacyDefaultsSubdir, rtName, agentType)
		if _, err := os.Stat(legacyPath); err == nil {
			return legacyPath, true
		}
	}
	return newPath, false
}

// BehaviorFile holds content and metadata for a single behavior file.
type BehaviorFile struct {
	Content []byte
	Source  string // "default" or "agent"
}

// BehaviorFiles maps workspace-relative filename -> file for an agent's behavior directory.
type BehaviorFiles map[string]BehaviorFile

// resolveBehaviorFiles assembles behavior files for an agent.
//
// Resolution order (all files):
//  1. <agents-root>/<agent_name>/<file> — agent-specific override (full replacement)
//  2. <agents-root>/_defaults/<runtime>/<type>/<file> — runtime+type-specific default
//
// Legacy layouts (<behaviorDir>/behavior/agents/<name>/ and
// <behaviorDir>/behavior/default/<runtime>/<type>/) are tried as a fallback
// when legacyPathFallbackEnabled is true, emitting a one-time deprecation
// warning per file read.
//
// USER.md.tmpl is rendered with agent template variables before deployment.
func resolveBehaviorFiles(behaviorDir string, agent provider.AgentConfig) BehaviorFiles {
	files := make(BehaviorFiles)
	agentType := string(agent.Type)
	rtName := string(runtime.ResolveRuntime(agent.Runtime, ""))

	agentDir, agentDirIsLegacy := resolveAgentDir(behaviorDir, agent.Name)
	defaultDir, defaultDirIsLegacy := resolveDefaultDir(behaviorDir, rtName, agentType)

	// SOUL.md and AGENTS.md: agent-specific > runtime+type default
	for _, name := range []string{"SOUL.md", "AGENTS.md"} {
		if data, err := os.ReadFile(filepath.Join(agentDir, name)); err == nil {
			files[name] = BehaviorFile{Content: data, Source: "agent"}
			if agentDirIsLegacy {
				warnLegacyBehaviorPath(filepath.Join(agentDir, name))
			}
			continue
		}
		if data, err := os.ReadFile(filepath.Join(defaultDir, name)); err == nil {
			files[name] = BehaviorFile{Content: data, Source: "default"}
			if defaultDirIsLegacy {
				warnLegacyBehaviorPath(filepath.Join(defaultDir, name))
			}
		}
	}

	// USER.md: agent-specific > render runtime+type template
	if data, err := os.ReadFile(filepath.Join(agentDir, "USER.md")); err == nil {
		files["USER.md"] = BehaviorFile{Content: data, Source: "agent"}
		if agentDirIsLegacy {
			warnLegacyBehaviorPath(filepath.Join(agentDir, "USER.md"))
		}
	} else {
		tmplPath := filepath.Join(defaultDir, "USER.md.tmpl")
		if data, err := os.ReadFile(tmplPath); err == nil {
			content := string(data)
			content = strings.ReplaceAll(content, "{{.AgentName}}", agent.Name)
			content = strings.ReplaceAll(content, "{{AGENT_NAME}}", agent.Name) // legacy compat — drop once all .tmpl files use {{.AgentName}}

			for _, binding := range agent.Channels {
				ch, ok := channels.Get(binding.Platform)
				if !ok {
					continue
				}
				for k, v := range ch.BehaviorTemplateVars(string(agent.Type), binding) {
					content = strings.ReplaceAll(content, "{{"+k+"}}", v)
				}
			}

			files["USER.md"] = BehaviorFile{Content: []byte(content), Source: "default"}
			if defaultDirIsLegacy {
				warnLegacyBehaviorPath(tmplPath)
			}
		}
	}

	return files
}

// ComposeAgentWorkspaceFiles assembles all behavior files for an agent and
// computes deletion reconciliation against the previous manifest.
//
// hashWorkspaceFile is called to hash existing workspace files for deletion
// reconciliation. Pass nil if not needed (e.g. first provision).
func ComposeAgentWorkspaceFiles(
	behaviorDir string,
	agent provider.AgentConfig,
	prevManifest *OverlayManifest,
	hashWorkspaceFile func(rel string) (string, error),
) (files BehaviorFiles, toDelete []string, next OverlayManifest, err error) {
	files = resolveBehaviorFiles(behaviorDir, agent)

	// Validate agent-specific files against protected paths
	rt := runtime.ResolveRuntime(agent.Runtime, "")
	for name := range files {
		if files[name].Source == "agent" && IsProtectedPath(name, rt) {
			return nil, nil, OverlayManifest{}, fmt.Errorf("agent behavior file %s is on the protected path list", name)
		}
	}

	if len(files) == 0 {
		return nil, nil, OverlayManifest{}, fmt.Errorf("no behavior files found in %s", behaviorDir)
	}

	toDelete = reconcileDeletions(prevManifest, files, hashWorkspaceFile)
	next = buildManifest(files)

	var agentCount int
	for _, f := range next.Files {
		if f.Source == "agent" {
			agentCount++
		}
	}
	defaultCount := len(next.Files) - agentCount
	if agentCount > 0 {
		fmt.Fprintf(os.Stderr, "behavior: %d agent-specific, %d default\n", agentCount, defaultCount)
	}

	return files, toDelete, next, nil
}

// ComposeBehaviorFiles is the legacy entry point.
// Deprecated: use ComposeAgentWorkspaceFiles instead.
//
// NOTE: Return type changed from map[string][]byte to BehaviorFiles
// (map[string]BehaviorFile) in the per-agent-config-overlay feature.
// No external Go callers exist; safe to remove in a future release.
// This wrapper skips manifest generation and deletion reconciliation.
// The AWS deploy path (deploy-agents.sh) handles file resolution
// independently in shell.
func ComposeBehaviorFiles(behaviorDir string, agent provider.AgentConfig) (BehaviorFiles, error) {
	files := resolveBehaviorFiles(behaviorDir, agent)
	if len(files) == 0 {
		return nil, fmt.Errorf("no behavior files found in %s", behaviorDir)
	}
	return files, nil
}
