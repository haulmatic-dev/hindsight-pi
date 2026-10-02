package core

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// initRepo creates a git repo at dir with an initial commit.
func initRepo(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"init"},
		{"config", "user.email", "t@t"},
		{"config", "user.name", "t"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
}

// readEvents (see context_pack_scout_test.go) is the package helper.

// TestCommitBead_StripsEmbeddedGitRepo pins the fmsfe-b8h wedge fix: an
// embedded git repo scaffolded inside a worktree subdir (agent drift) must
// not kill the close-time `git add -A` with exit 128 "does not have a commit
// checked out". The .git metadata is removed, the files survive as plain
// content and are committed, and a typed embedded_repo_detected event is
// emitted.
func TestCommitBead_StripsEmbeddedGitRepo(t *testing.T) {
	proj := t.TempDir()
	initRepo(t, proj)
	if out, err := exec.Command("git", "-C", proj, "commit", "--allow-empty", "-m", "init").CombinedOutput(); err != nil {
		t.Fatalf("init commit: %v: %s", err, out)
	}

	// Agent-drift scaffold: a nested repo with content inside.
	nested := filepath.Join(proj, "internal", "core")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	initRepo(t, nested)
	if err := os.WriteFile(filepath.Join(nested, "drift.txt"), []byte("scaffolded work"), 0o644); err != nil {
		t.Fatal(err)
	}

	central := filepath.Join(t.TempDir(), "events")
	sink := NewEventSink(central)
	m := NewWorktreeManager()
	m.SetEventSink(sink)

	committed, err := m.CommitBead(proj, "bead(t): close work", "fmsfe-b8h")
	if err != nil {
		t.Fatalf("CommitBead: %v", err)
	}
	if !committed {
		t.Fatal("expected commit")
	}

	// .git metadata gone; file content survived and is committed.
	if _, err := os.Stat(filepath.Join(nested, ".git")); !os.IsNotExist(err) {
		t.Fatalf("embedded .git still present (err=%v)", err)
	}
	data, err := os.ReadFile(filepath.Join(nested, "drift.txt"))
	if err != nil || string(data) != "scaffolded work" {
		t.Fatalf("drift file lost: %v %q", err, data)
	}
	out, err := exec.Command("git", "-C", proj, "show", "--name-only", "--pretty=format:", "HEAD").CombinedOutput()
	if err != nil || !strings.Contains(string(out), "internal/core/drift.txt") {
		t.Fatalf("drift file not committed: %v %s", err, out)
	}

	// Typed event emitted with bead + path.
	evs := readEventsOfType(t, sink, "embedded_repo_detected")
	if len(evs) != 1 {
		t.Fatalf("want 1 embedded_repo_detected event, got %d", len(evs))
	}
	hit := evs[0]
	if hit.Bead != "fmsfe-b8h" || hit.Reason != "removed" {
		t.Fatalf("bad event: %+v", hit)
	}
	if p, _ := hit.Fields["path"].(string); p != filepath.Clean(nested) {
		t.Fatalf("event path %q, want %q", p, filepath.Clean(nested))
	}
}

func TestEmbeddedGitRepos_FindsNestedSkipsVendorAndRoot(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, "pkg", "sub")
	initRepo(t, nested)
	initRepo(t, filepath.Join(root, "vendor", "lib"))
	if err := os.MkdirAll(filepath.Join(root, ".beads"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".beads", "x.jsonl"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := embeddedGitRepos(root)
	if len(got) != 1 || !strings.HasSuffix(got[0], filepath.Join("pkg", "sub")) {
		t.Fatalf("got %v", got)
	}
}
