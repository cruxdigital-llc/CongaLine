package common

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// rolePackageFiles is the canonical set of files copied from a role package
// into a per-agent overlay dir. Anything else in the role directory (README,
// role.meta) is intentionally NOT copied — README is operator docs;
// role.meta is metadata consumed by the CLI here.
var rolePackageFiles = []string{"SOUL.md", "AGENTS.md", "USER.md.tmpl", "agent.yaml"}

// ApplyRolePackage copies a role's default files into the agent's overlay
// dir at <behaviorDir>/<agentName>/. Existing files in the destination are
// preserved (operator customizations win — see spec.md § "CLI changes /
// conga admin add-user --role").
//
// Returns the role's declared agent type from role.meta ("user" or "team")
// so the caller can verify it matches the CLI's add-user vs add-team
// intent. Returns the list of files actually copied (subset of
// rolePackageFiles) for the caller's log/trace.
//
// Errors:
//   - role directory does not exist
//   - role.meta is missing or malformed
//   - file copy fails
func ApplyRolePackage(behaviorDir, agentName, roleSlug, runtimeName string) (declaredType string, copied []string, err error) {
	if roleSlug == "" {
		return "", nil, fmt.Errorf("role slug is required")
	}
	if !strings.HasPrefix(roleSlug, "role-") {
		// Allow operators to pass either "role-ops" or "ops" — normalize.
		roleSlug = "role-" + roleSlug
	}

	roleDir := filepath.Join(behaviorDir, defaultsSubdir, runtimeName, roleSlug)
	if _, err := os.Stat(roleDir); err != nil {
		if os.IsNotExist(err) {
			available := availableRoles(behaviorDir, runtimeName)
			return "", nil, fmt.Errorf("role %q not found for runtime %q. Available roles: %s",
				roleSlug, runtimeName, strings.Join(available, ", "))
		}
		return "", nil, fmt.Errorf("stat role dir %s: %w", roleDir, err)
	}

	declaredType, err = readRoleMeta(filepath.Join(roleDir, "role.meta"))
	if err != nil {
		return "", nil, fmt.Errorf("role %q: %w", roleSlug, err)
	}

	agentDir := filepath.Join(behaviorDir, agentName)
	if err := os.MkdirAll(agentDir, 0700); err != nil {
		return "", nil, fmt.Errorf("create agent dir %s: %w", agentDir, err)
	}

	for _, name := range rolePackageFiles {
		src := filepath.Join(roleDir, name)
		dst := filepath.Join(agentDir, name)

		// Preserve existing files — operator customization wins.
		if _, err := os.Stat(dst); err == nil {
			continue
		} else if !os.IsNotExist(err) {
			return declaredType, copied, fmt.Errorf("stat %s: %w", dst, err)
		}

		// Role package might not ship every file (e.g. a role that
		// chooses to inherit USER.md.tmpl from the runtime/type defaults).
		// Skip missing source files silently — only fail on real I/O errors.
		srcData, err := os.ReadFile(src)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return declaredType, copied, fmt.Errorf("read %s: %w", src, err)
		}
		// Preserve source file mode — agent.yaml may be 0644, prompts may
		// be 0644 too. We don't tighten beyond that.
		info, err := os.Stat(src)
		if err != nil {
			return declaredType, copied, fmt.Errorf("stat %s: %w", src, err)
		}
		if err := os.WriteFile(dst, srcData, info.Mode().Perm()); err != nil {
			return declaredType, copied, fmt.Errorf("write %s: %w", dst, err)
		}
		copied = append(copied, name)
	}

	return declaredType, copied, nil
}

// ResolveOperatorBehaviorDir walks up from the current working directory
// looking for the conga-line repo (identified by go.mod containing the
// module marker). Returns the absolute path to the repo's agents/
// directory, or "" if not found.
//
// **Worktree-aware**: if the resolved repo root is a git worktree (i.e.
// `<root>/.git` is a regular file rather than a directory), this function
// follows the worktree's `gitdir:` pointer to locate the MAIN worktree and
// returns its `agents/` instead. This is essential: per-agent overlay
// directories (gitignored) only exist on the operator's main checkout, so a
// conga binary running with cwd inside an isolated worktree would otherwise
// see the worktree's `agents/` (containing only the committed `_defaults/`)
// and silently produce defaults-only config for every agent.
//
// The early `./agents` cwd-relative check that earlier versions had has
// been removed precisely because it caused the silent-wrong behavior
// described above: when the operator's cwd happened to be inside the
// worktree, the local `agents/` (with role packages + _example) was a
// valid-looking-but-incomplete answer.
//
// This is intentionally tolerant: operators may invoke conga from inside a
// subdirectory of the repo, the repo root, or somewhere unrelated. The
// caller decides what to do with "" (typically: error with a helpful
// message pointing at how to fix it).
//
// Used by both `pkg/common/role_package.go` (--role flow) and
// `pkg/provider/awsprovider/channels.go::resolveAWSBehaviorDir` (overlay
// load during AWS provision/refresh).
func ResolveOperatorBehaviorDir() string {
	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}
	const moduleMarker = "module github.com/cruxdigital-llc/conga-line"
	dir := cwd
	for {
		goMod := filepath.Join(dir, "go.mod")
		if data, readErr := os.ReadFile(goMod); readErr == nil {
			if bytes.Contains(data, []byte(moduleMarker)) {
				return resolveAgentsDirForRepoRoot(dir)
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// resolveAgentsDirForRepoRoot returns the path to the agents/ directory for
// a confirmed conga-line repo root, handling the git-worktree case.
//
// If `<repoRoot>/.git` is a directory, this IS the main checkout: return
// `<repoRoot>/agents`. If `<repoRoot>/.git` is a regular file, this is a
// git worktree; parse the `gitdir:` pointer and return the main worktree's
// agents/ instead. Falls back to the worktree's own `agents/` if the main
// worktree can't be located or doesn't have an `agents/` dir.
func resolveAgentsDirForRepoRoot(repoRoot string) string {
	worktreeAgents := filepath.Join(repoRoot, "agents")
	if info, err := os.Stat(worktreeAgents); err != nil || !info.IsDir() {
		return ""
	}

	gitMarker := filepath.Join(repoRoot, ".git")
	info, err := os.Stat(gitMarker)
	if err != nil {
		// No .git at the repo root — caller is in a detached layout we
		// don't understand. Return the agents/ we found anyway; the
		// loader will fail gracefully if per-agent dirs are missing.
		return worktreeAgents
	}
	if info.IsDir() {
		// Main checkout. agents/ here IS the canonical source.
		return worktreeAgents
	}

	// .git is a regular file → worktree. Parse `gitdir: <path>` and walk
	// up to the main worktree.
	data, err := os.ReadFile(gitMarker)
	if err != nil {
		return worktreeAgents
	}
	mainAgents := mainWorktreeAgentsFromGitdir(string(data), repoRoot)
	if mainAgents == "" {
		return worktreeAgents
	}
	if info, err := os.Stat(mainAgents); err != nil || !info.IsDir() {
		return worktreeAgents
	}
	return mainAgents
}

// mainWorktreeAgentsFromGitdir parses the `.git` file of a worktree and
// returns the path to the main worktree's agents/ directory.
//
// Worktree `.git` files look like:
//
//	gitdir: /Users/aaron/Development/crux/congaline/.git/worktrees/explore-agent-routing
//
// The main worktree is the directory that contains the `.git` directory
// referenced by that gitdir line (walk up three levels: worktrees/<name> →
// worktrees → .git → main worktree).
//
// Relative gitdir paths (uncommon, but allowed by git) are resolved
// relative to the worktree's own root.
func mainWorktreeAgentsFromGitdir(gitFileContent, worktreeRoot string) string {
	for _, line := range strings.Split(gitFileContent, "\n") {
		line = strings.TrimSpace(line)
		key, value, ok := strings.Cut(line, ":")
		if !ok || strings.TrimSpace(key) != "gitdir" {
			continue
		}
		gitdir := strings.TrimSpace(value)
		if !filepath.IsAbs(gitdir) {
			gitdir = filepath.Join(worktreeRoot, gitdir)
		}
		// gitdir = .../<main>/.git/worktrees/<name>
		// Main worktree = parent of the .git directory.
		mainGitDir := filepath.Dir(filepath.Dir(gitdir)) // strip worktrees/<name>
		mainWorktree := filepath.Dir(mainGitDir)         // strip .git
		return filepath.Join(mainWorktree, "agents")
	}
	return ""
}

// readRoleMeta parses a role.meta file. Format: a single line
// "type: user" or "type: team". Comments and blank lines are tolerated;
// any other content is rejected so misconfigured role packages fail
// loudly.
func readRoleMeta(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read role.meta: %w", err)
	}
	var declaredType string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			return "", fmt.Errorf("malformed role.meta line %q (want key: value)", line)
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		switch key {
		case "type":
			if value != "user" && value != "team" {
				return "", fmt.Errorf("role.meta type %q: must be \"user\" or \"team\"", value)
			}
			declaredType = value
		default:
			return "", fmt.Errorf("role.meta: unknown key %q", key)
		}
	}
	if declaredType == "" {
		return "", fmt.Errorf("role.meta is missing required key: type")
	}
	return declaredType, nil
}

// availableRoles returns the slugs (e.g. "role-ops", "role-code-dev")
// of role packages defined for the given runtime under
// <behaviorDir>/_defaults/<runtime>/. Returns an empty slice if the
// directory doesn't exist. Sorted for stable error messages.
func availableRoles(behaviorDir, runtimeName string) []string {
	runtimeDir := filepath.Join(behaviorDir, defaultsSubdir, runtimeName)
	entries, err := os.ReadDir(runtimeDir)
	if err != nil {
		return nil
	}
	var roles []string
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(e.Name(), "role-") {
			roles = append(roles, e.Name())
		}
	}
	sort.Strings(roles)
	return roles
}
