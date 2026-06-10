package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/cruxdigital-llc/conga-line/pkg/common"
	"github.com/cruxdigital-llc/conga-line/pkg/provider"
	"github.com/cruxdigital-llc/conga-line/pkg/runtime"
	"github.com/spf13/cobra"
)

var behaviorAsName string

func init() {
	agentBehaviorCmd := &cobra.Command{
		Use:   "agent",
		Short: "Manage per-agent behavior files",
		Long: `Manage per-agent behavior files that override the defaults.

Agent-specific behavior files (SOUL.md, AGENTS.md, USER.md) replace the
shared defaults when present. They are deployed on provision and refresh
but never clobber agent-mutable state like MEMORY.md.

Changes take effect on the next 'conga refresh'.`,
	}

	listCmd := &cobra.Command{
		Use:   "list <agent>",
		Short: "List behavior files for an agent",
		Args:  cobra.ExactArgs(1),
		RunE:  agentBehaviorListRun,
	}

	addCmd := &cobra.Command{
		Use:   "add <agent> <file>",
		Short: "Add a behavior file to an agent",
		Long: `Copy a markdown file into the agent's behavior directory.
The file will be deployed to the agent's workspace on the next refresh,
replacing the default version of that file.`,
		Args: cobra.ExactArgs(2),
		RunE: agentBehaviorAddRun,
	}
	addCmd.Flags().StringVar(&behaviorAsName, "as", "", "Rename the file on copy (e.g. --as SOUL.md)")

	rmCmd := &cobra.Command{
		Use:   "rm <agent> <name>",
		Short: "Remove a behavior file from an agent",
		Long: `Remove an agent-specific behavior file. On the next refresh, the agent
will fall back to the shared default for that file.`,
		Args: cobra.ExactArgs(2),
		RunE: agentBehaviorRmRun,
	}

	showCmd := &cobra.Command{
		Use:   "show <agent> <name>",
		Short: "Display an agent behavior file",
		Args:  cobra.ExactArgs(2),
		RunE:  agentBehaviorShowRun,
	}

	diffCmd := &cobra.Command{
		Use:   "diff <agent>",
		Short: "Compare agent behavior source to deployed workspace",
		Args:  cobra.ExactArgs(1),
		RunE:  agentBehaviorDiffRun,
	}

	rebaselineCmd := &cobra.Command{
		Use:   "rebaseline <agent>",
		Short: "Reset an agent's customization file to the generated baseline",
		Long: `Reset the admin-owned agent-custom.json (the "$include" target) back to the
generated baseline. The current file is backed up to a timestamped .bak, then
emptied to {} and the agent is refreshed so the gateway reloads.

This discards admin config drift (e.g. an added MCP server). Agent data
(memory, workspace, sessions) is never touched.`,
		Args: cobra.ExactArgs(1),
		RunE: agentRebaselineRun,
	}
	rebaselineCmd.Flags().BoolVar(&rebaselineYes, "yes", false, "Skip the confirmation prompt")

	agentBehaviorCmd.AddCommand(listCmd, addCmd, rmCmd, showCmd, diffCmd, rebaselineCmd)
	rootCmd.AddCommand(agentBehaviorCmd)
}

// agentBehaviorDir returns the directory that holds per-agent overlay
// subdirectories. Prefers the live repo (via repo_path) so edits land in
// the source tree; falls back to the data-dir snapshot when repo_path isn't
// configured. Callers append <agent-name>/ to locate a specific agent.
func agentBehaviorDir() string {
	dataDir := provider.DefaultDataDir()
	if flagDataDir != "" {
		dataDir = flagDataDir
	}

	if repoPath := readLocalConfigValue(dataDir, "repo_path"); repoPath != "" {
		return filepath.Join(repoPath, "agents")
	}

	return filepath.Join(dataDir, "agents")
}

// readLocalConfigValue reads a key from local-config.json.
func readLocalConfigValue(dataDir, key string) string {
	data, err := os.ReadFile(filepath.Join(dataDir, "local-config.json"))
	if err != nil {
		return ""
	}
	var m map[string]string
	if err := json.Unmarshal(data, &m); err != nil {
		return ""
	}
	return m[key]
}

// syncBehaviorToDeployed copies the repo's agents/ tree to the deployed
// location (~/.conga/agents/) so that the next refresh's prompt loader
// (which reads from the data-dir snapshot) picks up source edits. The
// overlay loader (agent.yaml) already prefers the live repo when repo_path
// is configured, but the prompt resolver still uses the snapshot — see
// pkg/provider/localprovider/provider.go behaviorDir vs overlayBehaviorDir
// for the asymmetry.
//
// Also removes files from the deployed location that no longer exist in
// the repo so the snapshot stays in sync with deletes.
func syncBehaviorToDeployed(dataDir string) {
	repoPath := readLocalConfigValue(dataDir, "repo_path")
	if repoPath == "" {
		return
	}

	src := filepath.Join(repoPath, "agents")
	dst := filepath.Join(dataDir, "agents")
	if _, err := os.Stat(src); err != nil {
		return
	}

	// Copy src -> dst
	filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			os.MkdirAll(target, 0755)
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		os.MkdirAll(filepath.Dir(target), 0755)
		os.WriteFile(target, content, 0644)
		return nil
	})

	// Remove files from dst that no longer exist in src.
	//
	// IMPORTANT: only reconcile files INSIDE per-agent subdirectories
	// (rel contains a path separator). Direct children of dst (e.g.
	// dataDir/agents/<name>.json — the local provider's identity store —
	// or dataDir/agents/README.md) are not part of the overlay surface
	// and must not be deleted. The pre-2026-05-XX layout sidestepped this
	// by living under dataDir/behavior/agents/, but the rename collapsed
	// both file types into a single parent dir. See
	// pkg/provider/localprovider/provider.go behaviorDir() doc comment
	// for the cohabitation rules.
	if _, err := os.Stat(dst); err != nil {
		return
	}
	var emptyDirs []string
	filepath.WalkDir(dst, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			// Collect dirs for post-walk cleanup (deepest first via append order)
			if path != dst {
				emptyDirs = append(emptyDirs, path)
			}
			return nil
		}
		rel, _ := filepath.Rel(dst, path)
		// Skip top-level files (no separator in rel) — they're not overlay
		// content. See comment above the walk for the full rationale.
		if !strings.Contains(rel, string(filepath.Separator)) {
			return nil
		}
		if _, err := os.Stat(filepath.Join(src, rel)); os.IsNotExist(err) {
			os.Remove(path)
		}
		return nil
	})
	// Remove empty directories deepest-first
	for i := len(emptyDirs) - 1; i >= 0; i-- {
		os.Remove(emptyDirs[i]) // fails silently if non-empty
	}
}

// validateBehaviorFileName rejects names containing path separators or
// traversal components. Prevents path traversal via rm, show, or add --as.
func validateBehaviorFileName(name string) error {
	if strings.ContainsAny(name, "/\\") || strings.Contains(name, "..") {
		return fmt.Errorf("invalid behavior file name %q: must not contain path separators or '..'", name)
	}
	return nil
}

func agentBehaviorListRun(cmd *cobra.Command, args []string) error {
	agentName := args[0]
	if err := validateAgentName(agentName); err != nil {
		return err
	}
	dir := filepath.Join(agentBehaviorDir(), agentName)

	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		fmt.Printf("No agent-specific behavior files for %s (using defaults).\n", agentName)
		return nil
	}
	if err != nil {
		return err
	}

	if len(entries) == 0 {
		fmt.Printf("No agent-specific behavior files for %s (using defaults).\n", agentName)
		return nil
	}

	fmt.Printf("agents/%s/\n", agentName)
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, _ := e.Info()
		size := "?"
		if info != nil {
			size = formatSize(info.Size())
		}
		fmt.Printf("  %s  (%s)\n", e.Name(), size)
	}
	return nil
}

func agentBehaviorAddRun(cmd *cobra.Command, args []string) error {
	ctx, cancel := commandContext()
	defer cancel()

	agentName := args[0]
	srcPath := args[1]

	if err := validateAgentName(agentName); err != nil {
		return err
	}
	agentCfg, err := prov.GetAgent(ctx, agentName)
	if err != nil {
		return fmt.Errorf("agent %q not found: %w", agentName, err)
	}

	if _, err := os.Stat(srcPath); err != nil {
		return fmt.Errorf("source file %q: %w", srcPath, err)
	}

	targetName := filepath.Base(srcPath)
	if behaviorAsName != "" {
		targetName = behaviorAsName
	}

	if err := validateBehaviorFileName(targetName); err != nil {
		return err
	}
	if !strings.HasSuffix(strings.ToLower(targetName), ".md") {
		return fmt.Errorf("behavior files must be .md (got %q)", targetName)
	}
	switch targetName {
	case "SOUL.md", "AGENTS.md", "USER.md":
		// ok
	default:
		return fmt.Errorf("unsupported behavior file %q: only SOUL.md, AGENTS.md, USER.md are accepted", targetName)
	}

	rt := runtime.ResolveRuntime(agentCfg.Runtime, "")
	if common.IsProtectedPath(targetName, rt) {
		return fmt.Errorf("%q is a protected path and cannot be used as a behavior file", targetName)
	}

	content, err := os.ReadFile(srcPath)
	if err != nil {
		return err
	}
	if int64(len(content)) > common.MaxBehaviorFileSize {
		return fmt.Errorf("file exceeds size limit (%d bytes > %d)", len(content), common.MaxBehaviorFileSize)
	}

	dir := filepath.Join(agentBehaviorDir(), agentName)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	targetPath := filepath.Join(dir, targetName)
	if err := os.WriteFile(targetPath, content, 0644); err != nil {
		return err
	}

	dataDir := provider.DefaultDataDir()
	if flagDataDir != "" {
		dataDir = flagDataDir
	}
	syncBehaviorToDeployed(dataDir)

	fmt.Printf("Copied %s -> agents/%s/%s\n", filepath.Base(srcPath), agentName, targetName)
	fmt.Printf("Run 'conga refresh --agent %s' to deploy.\n", agentName)
	return nil
}

func agentBehaviorRmRun(cmd *cobra.Command, args []string) error {
	agentName := args[0]
	name := args[1]

	if err := validateAgentName(agentName); err != nil {
		return err
	}
	if err := validateBehaviorFileName(name); err != nil {
		return err
	}

	path := filepath.Join(agentBehaviorDir(), agentName, name)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return fmt.Errorf("behavior file %s/%s not found", agentName, name)
	}

	if err := os.Remove(path); err != nil {
		return err
	}

	// Also remove from deployed location
	dataDir := provider.DefaultDataDir()
	if flagDataDir != "" {
		dataDir = flagDataDir
	}
	deployedPath := filepath.Join(dataDir, "agents", agentName, name)
	os.Remove(deployedPath)

	fmt.Printf("Removed agents/%s/%s\n", agentName, name)
	fmt.Printf("Run 'conga refresh --agent %s' to apply (will fall back to default).\n", agentName)
	return nil
}

func agentBehaviorShowRun(cmd *cobra.Command, args []string) error {
	agentName := args[0]
	name := args[1]

	if err := validateAgentName(agentName); err != nil {
		return err
	}
	if err := validateBehaviorFileName(name); err != nil {
		return err
	}

	path := filepath.Join(agentBehaviorDir(), agentName, name)
	content, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("behavior file %s/%s: %w", agentName, name, err)
	}

	fmt.Print(string(content))
	return nil
}

func agentBehaviorDiffRun(cmd *cobra.Command, args []string) error {
	ctx, cancel := commandContext()
	defer cancel()

	agentName := args[0]

	if err := validateAgentName(agentName); err != nil {
		return err
	}
	agentCfg, err := prov.GetAgent(ctx, agentName)
	if err != nil {
		return fmt.Errorf("agent %q not found: %w", agentName, err)
	}

	// List agent-specific source files
	srcDir := filepath.Join(agentBehaviorDir(), agentName)
	srcFiles := map[string][]byte{}
	if entries, err := os.ReadDir(srcDir); err == nil {
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".md") {
				continue
			}
			data, _ := os.ReadFile(filepath.Join(srcDir, e.Name()))
			srcFiles[e.Name()] = data
		}
	}

	// Build runtime-aware manifest path inside the container
	rtName := runtime.ResolveRuntime(agentCfg.Runtime, "")
	rt, err := runtime.Get(rtName)
	if err != nil {
		return fmt.Errorf("unknown runtime %q: %w", rtName, err)
	}
	manifestPath := filepath.Join(rt.ContainerDataPath(), rt.WorkspacePath(), ".conga-overlay-manifest.json")

	// Read workspace manifest via container exec
	manifestJSON, err := prov.ContainerExec(ctx, agentName, []string{"cat", manifestPath})
	var manifest *common.OverlayManifest
	if err == nil && manifestJSON != "" {
		manifest = common.ParseOverlayManifest([]byte(manifestJSON))
	}

	if len(srcFiles) == 0 && manifest == nil {
		fmt.Printf("No agent-specific behavior files for %s (using defaults).\n", agentName)
		return nil
	}

	// Build workspace hash map from manifest (agent-sourced files only)
	wsHashes := map[string]string{}
	if manifest != nil {
		for _, entry := range manifest.Files {
			if entry.Source == "agent" || entry.Source == "overlay" {
				wsHashes[entry.Path] = entry.SHA256
			}
		}
	}

	for name, srcContent := range srcFiles {
		srcHash := common.HashFileContent(srcContent)
		if wsHash, ok := wsHashes[name]; ok {
			if srcHash == wsHash {
				fmt.Printf("  %-30s in-sync\n", name)
			} else {
				fmt.Printf("  %-30s DIFFERS (refresh to update)\n", name)
			}
			delete(wsHashes, name)
		} else {
			fmt.Printf("  %-30s NEW (not yet deployed)\n", name)
		}
	}
	for name := range wsHashes {
		fmt.Printf("  %-30s REMOVED FROM SOURCE (will revert to default on refresh)\n", name)
	}

	return nil
}

func formatSize(bytes int64) string {
	if bytes < 1024 {
		return fmt.Sprintf("%d B", bytes)
	}
	return fmt.Sprintf("%.1f KB", float64(bytes)/1024)
}
