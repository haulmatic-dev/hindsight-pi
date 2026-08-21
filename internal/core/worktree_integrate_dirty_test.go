package core

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Dirty main checkout must never block integration (2026-08-19: 9 legacy-era
// uncommitted files in srv-smart-engine made every `git merge` die with
// "Your local changes would be overwritten", so gate-verified work piled up
// in quarantine while beads closed as completed). IntegrateAndRemove
// auto-commits a dirty main checkout as a janitorial wip commit first.

func TestIntegrateAndRemove_DirtyMainAutoCommits(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	proj := gitInitRepo(t)
	m := NewWorktreeManager()

	// give seed.txt enough lines that two edits can touch different hunks
	seedLines := "l1\nl2\nl3\nl4\nl5\nl6\n"
	if err := os.WriteFile(filepath.Join(proj, "seed.txt"), []byte(seedLines), 0o644); err != nil {
		t.Fatal(err)
	}
	commit := exec.Command("git", "-C", proj, "commit", "-am", "expand seed")
	if out, err := commit.CombinedOutput(); err != nil {
		t.Fatalf("commit seed: %v\n%s", err, out)
	}

	// worktree with a committed change: edits the LAST line of seed.txt
	wt, err := m.EnsureWorktree(proj, "proj-dirty1")
	if err != nil {
		t.Fatalf("EnsureWorktree: %v", err)
	}
	if err := os.WriteFile(filepath.Join(wt, "feature.go"), []byte("package x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wt, "seed.txt"), []byte(seedLines+"worktree line\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Commit(wt, "bead(b1): add feature"); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	// dirty the MAIN checkout with an uncommitted edit to the SAME file but
	// a DIFFERENT hunk — the srv-smart-engine failure mode: git merge dies
	// with "Your local changes would be overwritten"; auto-commit turns it
	// into a clean two-hunk merge.
	if err := os.WriteFile(filepath.Join(proj, "seed.txt"), []byte("loose worker edit\n"+seedLines), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := m.IntegrateAndRemove(proj, "proj-dirty1"); err != nil {
		t.Fatalf("dirty main must be auto-committed, not block integration: %v", err)
	}

	// main is clean and contains both the janitorial commit and the work
	out, _ := exec.Command("git", "-C", proj, "status", "--porcelain").Output()
	if strings.TrimSpace(string(out)) != "" {
		t.Errorf("main checkout must be clean after integration, got %q", out)
	}
	if _, err := os.Stat(filepath.Join(proj, "feature.go")); err != nil {
		t.Errorf("worktree change must land in main: %v", err)
	}
	log, _ := exec.Command("git", "-C", proj, "log", "--oneline").Output()
	if !strings.Contains(string(log), "wip: hydra auto-commit") {
		t.Errorf("janitorial wip commit must be visible, log:\n%s", log)
	}
	if !strings.Contains(string(log), "hydra: integrate") {
		t.Errorf("integrate commit must be visible, log:\n%s", log)
	}
}

// .beads is daemon machinery state (status flips, DB mirror) — it must never
// travel from a worktree back into main. Union-merge on id-keyed JSONL
// produced duplicate rows and broke br entirely (srv soe53, 2026-08-20).
func TestCommit_ExcludesBeadsDir(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	proj := gitInitRepo(t)
	m := NewWorktreeManager()
	wt, err := m.EnsureWorktree(proj, "proj-b1")
	if err != nil {
		t.Fatalf("EnsureWorktree: %v", err)
	}
	// product change + machinery change
	if err := os.WriteFile(filepath.Join(wt, "feature.go"), []byte("package x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(wt, ".beads"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wt, ".beads", "issues.jsonl"), []byte(`{"id":"x"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Commit(wt, "bead(b1): work"); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	out, _ := exec.Command("git", "-C", wt, "show", "--stat", "--format=", "HEAD").Output()
	if !strings.Contains(string(out), "feature.go") {
		t.Errorf("product change must be committed: %s", out)
	}
	if strings.Contains(string(out), "issues.jsonl") {
		t.Errorf(".beads machinery must not be committed: %s", out)
	}
}
