package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/haulmatic-dev/hydra/internal/core"
)

func initGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runGit(t, dir, "init", "-b", "main")
	runGit(t, dir, "config", "user.email", "hydra-test@example.com")
	runGit(t, dir, "config", "user.name", "Hydra Test")
	commitFile(t, dir, 0, "README.md", "# repo\n")
	return dir
}

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v in %s: %v\n%s", args, dir, err, string(out))
	}
	return string(out)
}

func runGitEnv(t *testing.T, dir string, env []string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v in %s: %v\n%s", args, dir, err, string(out))
	}
	return string(out)
}

func commitFile(t *testing.T, dir string, daysAgo int, name, content string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	when := time.Now().Add(-time.Duration(daysAgo) * 24 * time.Hour).Format(time.RFC3339)
	env := []string{
		"GIT_AUTHOR_DATE=" + when,
		"GIT_COMMITTER_DATE=" + when,
	}
	runGitEnv(t, dir, env, "add", name)
	runGitEnv(t, dir, env, "commit", "-m", "add "+name)
}

func createQuarantineBranch(t *testing.T, dir, name string, daysAgo int, files map[string]string) {
	t.Helper()
	runGit(t, dir, "checkout", "-b", "hydra/quarantine/"+name)
	for name, content := range files {
		commitFile(t, dir, daysAgo, name, content)
	}
	runGit(t, dir, "checkout", "main")
}

func branchExists(t *testing.T, dir, name string) bool {
	t.Helper()
	cmd := exec.Command("git", "rev-parse", "--verify", "refs/heads/"+name)
	cmd.Dir = dir
	err := cmd.Run()
	return err == nil
}

func TestDrainQuarantineBranches(t *testing.T) {
	dir := initGitRepo(t)

	// Class 1: fully merged branch (10 days old, merged into main).
	createQuarantineBranch(t, dir, "merged", 10, map[string]string{"merged.txt": "merged\n"})
	runGit(t, dir, "merge", "--no-ff", "-m", "merge quarantine merged", "hydra/quarantine/merged")

	// Class 2: identical content branch (10 days old, file later landed on main).
	createQuarantineBranch(t, dir, "identical", 10, map[string]string{"shared.txt": "same content\n"})
	commitFile(t, dir, 5, "shared.txt", "same content\n")

	// Class 3: superseded tests branch (20 days old, adds only a spec that exists on main).
	createQuarantineBranch(t, dir, "superseded", 20, map[string]string{"feature.spec.ts": "test body\n"})
	commitFile(t, dir, 5, "feature.spec.ts", "test body main\n")

	// Class 4: in-flight young branch (2 days old, adds new file).
	createQuarantineBranch(t, dir, "young", 2, map[string]string{"inflight.go": "package x\n"})

	// Class 5: old branch with non-test changes (20 days old).
	createQuarantineBranch(t, dir, "nontest", 20, map[string]string{"main.go": "package main\n"})

	// Dry-run should report the three deletable branches without deleting.
	deleted, kept, err := core.DrainQuarantineBranches(dir, true)
	if err != nil {
		t.Fatalf("drain dry-run: %v", err)
	}
	wantDeleted := map[string]bool{
		"hydra/quarantine/merged":     true,
		"hydra/quarantine/identical":  true,
		"hydra/quarantine/superseded": true,
	}
	if len(deleted) != len(wantDeleted) {
		t.Fatalf("dry-run deleted=%v, want %v", deleted, wantDeleted)
	}
	for _, b := range deleted {
		if !wantDeleted[b] {
			t.Fatalf("unexpected dry-run deleted branch %s", b)
		}
	}
	if kept != 2 {
		t.Fatalf("dry-run kept=%d, want 2", kept)
	}
	for _, b := range deleted {
		if !branchExists(t, dir, b) {
			t.Fatalf("dry-run deleted branch %s should still exist", b)
		}
	}

	// Apply should remove exactly those three branches.
	deleted, kept, err = core.DrainQuarantineBranches(dir, false)
	if err != nil {
		t.Fatalf("drain apply: %v", err)
	}
	if len(deleted) != 3 {
		t.Fatalf("apply deleted=%v, want 3", deleted)
	}
	if kept != 2 {
		t.Fatalf("apply kept=%d, want 2", kept)
	}
	for _, b := range deleted {
		if branchExists(t, dir, b) {
			t.Fatalf("branch %s should have been deleted", b)
		}
	}
	if !branchExists(t, dir, "hydra/quarantine/young") {
		t.Fatal("young branch should be kept")
	}
	if !branchExists(t, dir, "hydra/quarantine/nontest") {
		t.Fatal("nontest branch should be kept")
	}
}
