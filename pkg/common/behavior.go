package common

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/cruxdigital-llc/conga-line/pkg/channels"
	"github.com/cruxdigital-llc/conga-line/pkg/provider"
	"github.com/cruxdigital-llc/conga-line/pkg/runtime"
)

// Subdirectory names looked up inside the directory that callers pass as
// behaviorDir:
//
//   - <behaviorDir>/<name>/         — per-agent overlay
//   - <behaviorDir>/_defaults/<runtime>/<type>/  — shipped defaults
//
// The leading underscore on _defaults disambiguates it from any real agent
// name (which validateAgentName forbids starting with `_`).
const defaultsSubdir = "_defaults"

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
//  1. <behaviorDir>/<agent_name>/<file> — agent-specific override (full replacement)
//  2. <behaviorDir>/_defaults/<runtime>/<type>/<file> — runtime+type-specific default
//
// USER.md.tmpl is rendered with agent template variables before deployment.
func resolveBehaviorFiles(behaviorDir string, agent provider.AgentConfig) BehaviorFiles {
	files := make(BehaviorFiles)
	agentType := string(agent.Type)
	rtName := string(runtime.ResolveRuntime(agent.Runtime, ""))

	agentDir := filepath.Join(behaviorDir, agent.Name)
	defaultDir := filepath.Join(behaviorDir, defaultsSubdir, rtName, agentType)

	// SOUL.md and AGENTS.md: agent-specific > runtime+type default
	for _, name := range []string{"SOUL.md", "AGENTS.md"} {
		if data, err := os.ReadFile(filepath.Join(agentDir, name)); err == nil {
			files[name] = BehaviorFile{Content: data, Source: "agent"}
			continue
		}
		if data, err := os.ReadFile(filepath.Join(defaultDir, name)); err == nil {
			files[name] = BehaviorFile{Content: data, Source: "default"}
		}
	}

	// USER.md resolution order:
	//   1. <agentDir>/USER.md         — pre-rendered, agent-authored (highest priority)
	//   2. <agentDir>/USER.md.tmpl    — per-agent template (e.g. dropped in by `--role` copy)
	//   3. <defaultDir>/USER.md.tmpl  — runtime+type default template
	//
	// (2) was added in spec.md § "CLI changes" so role packages can ship a
	// .tmpl that gets templated with the agent's name + channel vars at
	// deploy time, the same as the (3) fallback path.
	if data, err := os.ReadFile(filepath.Join(agentDir, "USER.md")); err == nil {
		files["USER.md"] = BehaviorFile{Content: data, Source: "agent"}
	} else {
		var tmplData []byte
		source := "default"
		if data, err := os.ReadFile(filepath.Join(agentDir, "USER.md.tmpl")); err == nil {
			tmplData = data
			source = "agent"
		} else if data, err := os.ReadFile(filepath.Join(defaultDir, "USER.md.tmpl")); err == nil {
			tmplData = data
		}
		if tmplData != nil {
			content := string(tmplData)
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

			files["USER.md"] = BehaviorFile{Content: []byte(content), Source: source}
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
