package main

import (
	"os"
	"path/filepath"
	"testing"
)

// 2026-09-16 webapp-zpf husk incident: the worker kept turning for 25 minutes
// inside a reconciler-deleted worktree. The canary must detect the missing
// .git and fail fast.

func TestWorktreeKeyFromDir(t *testing.T) {
	cases := []struct {
		dir  string
		want string
	}{
		{"/opt/dev-tools/var/hydra/.hydra-worktrees/-home-u-projects-r/webapp-zpf", "webapp-zpf"},
		{"/opt/dev-tools/var/hydra/.hydra-worktrees/-home-u-projects-r/webapp-zpf/", "webapp-zpf"},
		{"/home/nipuna.j/projects/webapp", ""}, // main checkout: no key
		{"/opt/dev-tools/var/hydra", ""},       // daemon home: no key
		{"", ""},                               // unset
	}
	for _, tc := range cases {
		if got := worktreeKeyFromDir(tc.dir); got != tc.want {
			t.Errorf("worktreeKeyFromDir(%q) = %q, want %q", tc.dir, got, tc.want)
		}
	}
}

func TestWorkspaceDestroyed(t *testing.T) {
	root := t.TempDir()
	wt := filepath.Join(root, ".hydra-worktrees", "-proj", "bead-1")
	if err := os.MkdirAll(wt, 0o755); err != nil {
		t.Fatal(err)
	}

	// Worktree without .git: destroyed.
	if !workspaceDestroyed(wt) {
		t.Fatal("missing .git in a worktree path must report destroyed")
	}

	// Real worktree (.git file present): intact.
	if err := os.WriteFile(filepath.Join(wt, ".git"), []byte("gitdir: /x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if workspaceDestroyed(wt) {
		t.Fatal(".git present: must not report destroyed")
	}

	// Main-checkout run: canary disarmed entirely.
	if workspaceDestroyed(filepath.Join(root, "some-checkout")) {
		t.Fatal("non-worktree runDir must never report destroyed")
	}
}
