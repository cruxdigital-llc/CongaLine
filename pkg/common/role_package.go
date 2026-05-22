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
// module marker). Returns the path to the repo's agents/ directory, or
// "" if not found.
//
// This is intentionally tolerant: operators may invoke conga from inside a
// subdirectory of the repo, the repo root, or somewhere unrelated. The
// caller decides what to do with "" (typically: error with a helpful
// message pointing at how to fix it).
//
// Mirrors the pattern from pkg/provider/awsprovider/channels.go
// `resolveAWSBehaviorDir`; eventually that should call into here.
func ResolveOperatorBehaviorDir() string {
	if info, err := os.Stat("agents"); err == nil && info.IsDir() {
		return "agents"
	}

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
				candidate := filepath.Join(dir, "agents")
				if info, statErr := os.Stat(candidate); statErr == nil && info.IsDir() {
					return candidate
				}
				return ""
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
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
