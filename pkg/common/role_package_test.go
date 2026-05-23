package common

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// makeRoleDir creates a minimal role package under
// <root>/_defaults/<runtime>/<roleSlug>/. The files map keys are filenames
// inside the role dir; values are contents. role.meta and the rolePackageFiles
// set are the canonical set; anything else is ignored by ApplyRolePackage but
// fine to put in the fixture.
func makeRoleDir(t *testing.T, root, runtimeName, roleSlug string, files map[string]string) string {
	t.Helper()
	roleDir := filepath.Join(root, defaultsSubdir, runtimeName, roleSlug)
	if err := os.MkdirAll(roleDir, 0755); err != nil {
		t.Fatalf("mkdir role dir: %v", err)
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(roleDir, name), []byte(content), 0644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	return roleDir
}

func TestApplyRolePackage_HappyPath(t *testing.T) {
	root := t.TempDir()
	makeRoleDir(t, root, "openclaw", "role-ops", map[string]string{
		"role.meta":    "type: user\n",
		"SOUL.md":      "# soul",
		"AGENTS.md":    "# agents",
		"USER.md.tmpl": "# user {{.AgentName}}",
		"agent.yaml":   "version: 2\nmodel:\n  provider: openai\n  name: x\n  base_url: https://h/v1\n",
		"README.md":    "ignored",
	})

	gotType, copied, err := ApplyRolePackage(root, "myagent", "role-ops", "openclaw")
	if err != nil {
		t.Fatalf("ApplyRolePackage: %v", err)
	}
	if gotType != "user" {
		t.Fatalf("declaredType: want user, got %q", gotType)
	}

	sort.Strings(copied)
	wantCopied := []string{"AGENTS.md", "SOUL.md", "USER.md.tmpl", "agent.yaml"}
	if !reflect.DeepEqual(copied, wantCopied) {
		t.Fatalf("copied: want %v, got %v", wantCopied, copied)
	}

	// README.md and role.meta must NOT be copied.
	for _, name := range []string{"README.md", "role.meta"} {
		if _, err := os.Stat(filepath.Join(root, "myagent", name)); err == nil {
			t.Fatalf("%s should not be copied to agent dir", name)
		}
	}

	// agent.yaml content actually copied.
	data, err := os.ReadFile(filepath.Join(root, "myagent", "agent.yaml"))
	if err != nil {
		t.Fatalf("read copied agent.yaml: %v", err)
	}
	if !strings.Contains(string(data), "version: 2") {
		t.Fatalf("agent.yaml did not survive the copy: %q", data)
	}
}

func TestApplyRolePackage_RoleSlugNormalization(t *testing.T) {
	// Operators can pass either "role-ops" or just "ops" — both work.
	root := t.TempDir()
	makeRoleDir(t, root, "openclaw", "role-ops", map[string]string{
		"role.meta":  "type: user\n",
		"agent.yaml": "version: 2\n",
	})

	gotType, copied, err := ApplyRolePackage(root, "myagent", "ops", "openclaw")
	if err != nil {
		t.Fatalf("ApplyRolePackage with un-prefixed slug: %v", err)
	}
	if gotType != "user" {
		t.Fatalf("declaredType: want user, got %q", gotType)
	}
	if len(copied) != 1 || copied[0] != "agent.yaml" {
		t.Fatalf("copied: want [agent.yaml], got %v", copied)
	}
}

func TestApplyRolePackage_IdempotencyPreservesExistingFiles(t *testing.T) {
	// QA persona note from spec.md Phase 6: running `--role X` on an existing
	// agent with a customized agent.yaml must NOT overwrite the customization.
	root := t.TempDir()
	makeRoleDir(t, root, "openclaw", "role-ops", map[string]string{
		"role.meta":    "type: user\n",
		"SOUL.md":      "# default soul",
		"AGENTS.md":    "# default agents",
		"USER.md.tmpl": "# default user",
		"agent.yaml":   "version: 2\nmodel:\n  provider: openai\n  name: default-qwen\n  base_url: https://default/v1\n",
	})

	// Operator pre-customizes agent.yaml; provisioning was started long ago.
	agentDir := filepath.Join(root, "myagent")
	if err := os.MkdirAll(agentDir, 0700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	customAgentYaml := "version: 2\nmodel:\n  provider: openai\n  name: my-custom-qwen\n  base_url: https://my.lan/v1\n"
	if err := os.WriteFile(filepath.Join(agentDir, "agent.yaml"), []byte(customAgentYaml), 0644); err != nil {
		t.Fatalf("write custom agent.yaml: %v", err)
	}

	// Re-running `--role role-ops` must preserve the customization.
	_, copied, err := ApplyRolePackage(root, "myagent", "role-ops", "openclaw")
	if err != nil {
		t.Fatalf("ApplyRolePackage: %v", err)
	}

	// agent.yaml was preserved → not in the copied list.
	for _, name := range copied {
		if name == "agent.yaml" {
			t.Fatalf("agent.yaml should have been preserved, but was reported as copied: %v", copied)
		}
	}
	// SOUL.md / AGENTS.md / USER.md.tmpl ARE copied since they didn't exist before.
	sort.Strings(copied)
	wantCopied := []string{"AGENTS.md", "SOUL.md", "USER.md.tmpl"}
	if !reflect.DeepEqual(copied, wantCopied) {
		t.Fatalf("copied: want %v, got %v", wantCopied, copied)
	}

	// Customization survives.
	got, err := os.ReadFile(filepath.Join(agentDir, "agent.yaml"))
	if err != nil {
		t.Fatalf("read agent.yaml: %v", err)
	}
	if string(got) != customAgentYaml {
		t.Fatalf("agent.yaml was overwritten\nwant: %q\ngot:  %q", customAgentYaml, got)
	}
}

func TestApplyRolePackage_RoleNotFound(t *testing.T) {
	root := t.TempDir()
	// Set up two roles so we can verify the error lists what's available.
	makeRoleDir(t, root, "openclaw", "role-ops", map[string]string{"role.meta": "type: user\n"})
	makeRoleDir(t, root, "openclaw", "role-code-dev", map[string]string{"role.meta": "type: team\n"})

	_, _, err := ApplyRolePackage(root, "myagent", "role-nonexistent", "openclaw")
	if err == nil {
		t.Fatal("expected error for missing role, got nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, "not found") {
		t.Fatalf("error should mention \"not found\": %v", err)
	}
	if !strings.Contains(msg, "role-ops") || !strings.Contains(msg, "role-code-dev") {
		t.Fatalf("error should list available roles: %v", err)
	}
}

func TestApplyRolePackage_RoleMetaMissing(t *testing.T) {
	root := t.TempDir()
	makeRoleDir(t, root, "openclaw", "role-broken", map[string]string{
		"agent.yaml": "version: 2\n",
		// role.meta missing
	})
	_, _, err := ApplyRolePackage(root, "myagent", "role-broken", "openclaw")
	if err == nil {
		t.Fatal("expected error for missing role.meta, got nil")
	}
	if !strings.Contains(err.Error(), "role.meta") {
		t.Fatalf("error should mention role.meta: %v", err)
	}
}

func TestApplyRolePackage_RoleMetaMalformed(t *testing.T) {
	tests := []struct {
		name     string
		meta     string
		wantSubs string
	}{
		{
			name:     "missing type key",
			meta:     "# just a comment\n",
			wantSubs: "missing required key: type",
		},
		{
			name:     "bad type value",
			meta:     "type: superadmin\n",
			wantSubs: "must be \"user\" or \"team\"",
		},
		{
			name:     "unknown key",
			meta:     "type: user\nweird: thing\n",
			wantSubs: "unknown key",
		},
		{
			name:     "malformed line",
			meta:     "this is not a key-value line\n",
			wantSubs: "malformed role.meta line",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			makeRoleDir(t, root, "openclaw", "role-x", map[string]string{
				"role.meta":  tc.meta,
				"agent.yaml": "version: 2\n",
			})
			_, _, err := ApplyRolePackage(root, "myagent", "role-x", "openclaw")
			if err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tc.wantSubs) {
				t.Fatalf("want substring %q in error, got %v", tc.wantSubs, err)
			}
		})
	}
}

func TestApplyRolePackage_RuntimeIsolation(t *testing.T) {
	// A role defined for openclaw must NOT be findable when the requested
	// runtime is hermes.
	root := t.TempDir()
	makeRoleDir(t, root, "openclaw", "role-onlyopenclaw", map[string]string{
		"role.meta":  "type: user\n",
		"agent.yaml": "version: 2\n",
	})
	_, _, err := ApplyRolePackage(root, "myagent", "role-onlyopenclaw", "hermes")
	if err == nil {
		t.Fatal("expected error when role exists for openclaw but requested runtime is hermes")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("want not-found error, got %v", err)
	}
}

func TestApplyRolePackage_MissingFilesInRoleSilentlySkipped(t *testing.T) {
	// A role that ships only agent.yaml + role.meta (no SOUL/AGENTS/USER) is
	// valid — they'll fall back to the runtime+type defaults at resolveBehaviorFiles
	// time. ApplyRolePackage should copy what's there and not error.
	root := t.TempDir()
	makeRoleDir(t, root, "openclaw", "role-minimal", map[string]string{
		"role.meta":  "type: team\n",
		"agent.yaml": "version: 2\n",
	})
	gotType, copied, err := ApplyRolePackage(root, "myagent", "role-minimal", "openclaw")
	if err != nil {
		t.Fatalf("ApplyRolePackage: %v", err)
	}
	if gotType != "team" {
		t.Fatalf("declaredType: want team, got %q", gotType)
	}
	if len(copied) != 1 || copied[0] != "agent.yaml" {
		t.Fatalf("copied: want [agent.yaml], got %v", copied)
	}
}

func TestResolveOperatorBehaviorDir_FromCWDWithAgentsDir(t *testing.T) {
	tmp := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmp, "agents"), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// go.mod with the conga-line module marker.
	goModContent := "module github.com/cruxdigital-llc/conga-line\n\ngo 1.21\n"
	if err := os.WriteFile(filepath.Join(tmp, "go.mod"), []byte(goModContent), 0644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}

	// chdir into a subdir of the fake repo to exercise the walk-up branch.
	subDir := filepath.Join(tmp, "subdir")
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatalf("mkdir subdir: %v", err)
	}

	origCWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(origCWD) })
	if err := os.Chdir(subDir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	got := ResolveOperatorBehaviorDir()
	want := filepath.Join(tmp, "agents")
	// On macOS, t.TempDir uses /private/var/folders/... — resolve symlinks
	// for the comparison.
	gotResolved, _ := filepath.EvalSymlinks(got)
	wantResolved, _ := filepath.EvalSymlinks(want)
	if gotResolved != wantResolved {
		t.Fatalf("ResolveOperatorBehaviorDir: want %s (resolved %s), got %s (resolved %s)", want, wantResolved, got, gotResolved)
	}
}

// TestResolveOperatorBehaviorDir_WorktreeRedirectsToMain exercises the
// regression that bit Phase 8: when the operator's cwd is inside a git
// worktree, the function must resolve to the MAIN worktree's agents/
// (where per-agent overlay dirs live), not the worktree's own agents/
// (which contains only the committed _defaults/ and _example/).
func TestResolveOperatorBehaviorDir_WorktreeRedirectsToMain(t *testing.T) {
	tmp := t.TempDir()
	// Set up: tmp/main is the main worktree; tmp/main/.claude/worktrees/wt1 is a git worktree.
	main := filepath.Join(tmp, "main")
	wt := filepath.Join(main, ".claude", "worktrees", "wt1")
	if err := os.MkdirAll(filepath.Join(main, ".git"), 0755); err != nil {
		t.Fatalf("mkdir main: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(main, "agents", "aaron"), 0755); err != nil {
		t.Fatalf("mkdir main agents: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(wt, "agents", "_defaults"), 0755); err != nil {
		t.Fatalf("mkdir wt agents/_defaults: %v", err)
	}
	goMod := "module github.com/cruxdigital-llc/conga-line\n\ngo 1.21\n"
	if err := os.WriteFile(filepath.Join(main, "go.mod"), []byte(goMod), 0644); err != nil {
		t.Fatalf("write main go.mod: %v", err)
	}
	if err := os.WriteFile(filepath.Join(wt, "go.mod"), []byte(goMod), 0644); err != nil {
		t.Fatalf("write wt go.mod: %v", err)
	}
	// Worktree's .git is a regular file pointing back to main/.git/worktrees/wt1
	gitFileContent := fmt.Sprintf("gitdir: %s\n", filepath.Join(main, ".git", "worktrees", "wt1"))
	if err := os.WriteFile(filepath.Join(wt, ".git"), []byte(gitFileContent), 0644); err != nil {
		t.Fatalf("write wt .git: %v", err)
	}

	// Chdir into the worktree.
	origCWD, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(origCWD) })
	if err := os.Chdir(wt); err != nil {
		t.Fatalf("chdir worktree: %v", err)
	}

	got := ResolveOperatorBehaviorDir()
	// On macOS, t.TempDir lives under /private/var; resolve symlinks before comparing.
	gotResolved, _ := filepath.EvalSymlinks(got)
	wantResolved, _ := filepath.EvalSymlinks(filepath.Join(main, "agents"))
	if gotResolved != wantResolved {
		t.Fatalf("worktree resolution failed:\n  want main's agents: %s\n  got:               %s\n  (wt agents would have been: %s)", wantResolved, gotResolved, filepath.Join(wt, "agents"))
	}
}

// TestResolveOperatorBehaviorDir_MainCheckoutUnchanged confirms that when
// .git is a directory (normal checkout, not a worktree), the function
// returns the local agents/ — same as before.
func TestResolveOperatorBehaviorDir_MainCheckoutUnchanged(t *testing.T) {
	tmp := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmp, ".git"), 0755); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(tmp, "agents"), 0755); err != nil {
		t.Fatalf("mkdir agents: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmp, "go.mod"),
		[]byte("module github.com/cruxdigital-llc/conga-line\n\ngo 1.21\n"), 0644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}

	origCWD, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(origCWD) })
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	got := ResolveOperatorBehaviorDir()
	gotResolved, _ := filepath.EvalSymlinks(got)
	wantResolved, _ := filepath.EvalSymlinks(filepath.Join(tmp, "agents"))
	if gotResolved != wantResolved {
		t.Fatalf("main checkout: want %s, got %s", wantResolved, gotResolved)
	}
}

// TestMainWorktreeAgentsFromGitdir exercises the gitdir-pointer parsing
// directly with both absolute and relative gitdir paths.
func TestMainWorktreeAgentsFromGitdir(t *testing.T) {
	tests := []struct {
		name         string
		gitContent   string
		worktreeRoot string
		want         string
	}{
		{
			name:         "absolute gitdir",
			gitContent:   "gitdir: /Users/aaron/repo/.git/worktrees/feature\n",
			worktreeRoot: "/Users/aaron/repo/.claude/worktrees/feature",
			want:         "/Users/aaron/repo/agents",
		},
		{
			name:         "relative gitdir resolved against worktree root",
			gitContent:   "gitdir: ../../../.git/worktrees/feature\n",
			worktreeRoot: "/Users/aaron/repo/.claude/worktrees/feature",
			want:         filepath.Join("/Users/aaron/repo/.claude/worktrees/feature", "../../../.git/worktrees/feature", "..", "..", "..", "agents"),
		},
		{
			name:         "extra whitespace tolerated",
			gitContent:   "  gitdir:    /Users/aaron/repo/.git/worktrees/feature   \n",
			worktreeRoot: "/Users/aaron/repo/.claude/worktrees/feature",
			want:         "/Users/aaron/repo/agents",
		},
		{
			name:         "non-gitdir lines ignored",
			gitContent:   "# comment\nworktree-purpose: feature\ngitdir: /Users/aaron/repo/.git/worktrees/feature\n",
			worktreeRoot: "/Users/aaron/repo/.claude/worktrees/feature",
			want:         "/Users/aaron/repo/agents",
		},
		{
			name:         "no gitdir found",
			gitContent:   "# nothing useful here\n",
			worktreeRoot: "/Users/aaron/repo/.claude/worktrees/feature",
			want:         "",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := mainWorktreeAgentsFromGitdir(tc.gitContent, tc.worktreeRoot)
			// Normalize both via filepath.Clean to handle the relative-path test case.
			if filepath.Clean(got) != filepath.Clean(tc.want) {
				t.Fatalf("want %q, got %q", tc.want, got)
			}
		})
	}
}

func TestResolveOperatorBehaviorDir_NoRepo(t *testing.T) {
	// Chdir into a tmpdir that has no go.mod and no agents/. Should return "".
	tmp := t.TempDir()
	origCWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(origCWD) })
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	got := ResolveOperatorBehaviorDir()
	// On systems where the tmpdir is nested inside a repo (unlikely but
	// possible if /var/folders/... is somehow inside one), this could
	// surface an unexpected result. Tolerate "" only.
	if got != "" {
		// If non-empty, must be a real agents/ dir (defensive — the walk
		// could find SOME repo above /tmp).
		if info, err := os.Stat(got); err != nil || !info.IsDir() {
			t.Fatalf("got non-empty path that isn't a directory: %q", got)
		}
		t.Logf("ResolveOperatorBehaviorDir found %q despite no local repo — likely walking into a real conga-line checkout above the tmp tree; skipping strict assertion", got)
	}
}
