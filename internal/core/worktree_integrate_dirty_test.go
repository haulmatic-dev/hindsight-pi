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

// Janitorial auto-commit must exclude .beads/** from its scope — it is
// daemon machinery state (br-owned mirror), same as worktree commits.
func TestAutoCommitDirtyMain_ExcludesBeadsDir(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	proj := gitInitRepo(t)
	m := NewWorktreeManager()
	if _, err := m.EnsureWorktree(proj, "proj-dirtyb"); err != nil {
		t.Fatalf("EnsureWorktree: %v", err)
	}
	// dirty main with a product file AND a beads write
	if err := os.WriteFile(filepath.Join(proj, "loose.go"), []byte("package x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(proj, ".beads"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(proj, ".beads", "issues.jsonl"), []byte(`{"id":"main-loose"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	autoCommitDirtyMain(proj)

	log, _ := exec.Command("git", "-C", proj, "show", "--stat", "--format=", "HEAD").Output()
	if !strings.Contains(string(log), "loose.go") {
		t.Errorf("janitorial commit must include the product file: %s", log)
	}
	if strings.Contains(string(log), "issues.jsonl") {
		t.Errorf("janitorial commit must exclude .beads machinery: %s", log)
	}
}

// Janitorial auto-commit must refuse (skip + alarm) when the dirty main
// checkout carries conflict markers — it has committed marker states into
// git 3+ times across repos (846bdd1 mid-merge capture, 9f74f940 unparseable
// package.json), poisoning every downstream gate.
func TestAutoCommitDirtyMain_RefusesConflictMarkers(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	proj := gitInitRepo(t)
	if err := os.WriteFile(filepath.Join(proj, "clean.go"), []byte("package x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(proj, "conflicted.go"),
		[]byte("package x\n\n<<<<<<< HEAD\na := 1\n=======\na := 2\n>>>>>>> branch\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	autoCommitDirtyMain(proj)

	// no janitorial commit must exist
	log, _ := exec.Command("git", "-C", proj, "log", "--oneline").Output()
	if strings.Contains(string(log), "wip: hydra auto-commit") {
		t.Errorf("janitorial commit must be refused on conflict markers, log:\n%s", log)
	}
	// markers must remain unstaged (reset), working tree intact
	content, _ := os.ReadFile(filepath.Join(proj, "conflicted.go"))
	if !strings.Contains(string(content), "<<<<<<<") {
		t.Errorf("working tree file must be untouched, got %q", content)
	}
	staged, _ := exec.Command("git", "-C", proj, "diff", "--cached", "--name-only").Output()
	if strings.TrimSpace(string(staged)) != "" {
		t.Errorf("nothing must remain staged after refusal, got %q", staged)
	}
}

// WorktreeManager.Commit must refuse a commit whose staged content carries
// conflict markers, so the shepherd quarantines the bead instead of baking
// marker states into history.
func TestCommit_RefusesConflictMarkers(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	proj := gitInitRepo(t)
	m := NewWorktreeManager()
	wt, err := m.EnsureWorktree(proj, "proj-marker1")
	if err != nil {
		t.Fatalf("EnsureWorktree: %v", err)
	}
	if err := os.WriteFile(filepath.Join(wt, "main.go"),
		[]byte("package x\n\n<<<<<<< HEAD\nf()\n=======\ng()\n>>>>>>> other\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = m.Commit(wt, "bead(b1): work")
	if err == nil {
		t.Fatal("Commit must refuse files containing conflict markers")
	}
	if !strings.Contains(err.Error(), "conflict markers") {
		t.Errorf("error must name the cause, got: %v", err)
	}
	log, _ := exec.Command("git", "-C", wt, "log", "--oneline").Output()
	if strings.Contains(string(log), "bead(b1)") {
		t.Errorf("marker-bearing commit must not exist, log:\n%s", log)
	}
}

// .beads/issues.jsonl is a br-owned mirror: both main and the chain worktree
// write it, so it conflicts on EVERY integration that carries beads writes
// (65wa0's gate-proven suite fix quarantined on a .beads-only conflict,
// 2026-08-21). br state on main is the source of truth; worktree-side beads
// writes are redundant. IntegrateAndRemove must auto-resolve conflicts that
// are confined to .beads/ with --ours and proceed. Real-code conflicts still
// quarantine.
func TestIntegrateAndRemove_BeadsOnlyConflictAutoResolves(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	proj := gitInitRepo(t)
	m := NewWorktreeManager()

	beads := filepath.Join(proj, ".beads")
	if err := os.MkdirAll(beads, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(beads, "issues.jsonl"), []byte("{\"id\":\"main-1\"}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run := func(dir string, args ...string) {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run(proj, "add", ".beads/issues.jsonl")
	run(proj, "commit", "-m", "add beads mirror")

	wt, err := m.EnsureWorktree(proj, "proj-beads1")
	if err != nil {
		t.Fatalf("EnsureWorktree: %v", err)
	}
	// worktree: real change + a beads write
	if err := os.WriteFile(filepath.Join(wt, "fix.go"), []byte("package x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wt, ".beads", "issues.jsonl"), []byte("{\"id\":\"wt-1\"}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run(wt, "add", "-A")
	run(wt, "commit", "-m", "work + beads write")

	// main: conflicting beads write
	if err := os.WriteFile(filepath.Join(beads, "issues.jsonl"), []byte("{\"id\":\"main-2\"}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run(proj, "commit", "-am", "main beads write")

	if err := m.IntegrateAndRemove(proj, "proj-beads1"); err != nil {
		t.Fatalf("beads-only conflict must auto-resolve with --ours, got: %v", err)
	}
	// real change landed
	if _, err := os.Stat(filepath.Join(proj, "fix.go")); err != nil {
		t.Error("fix.go must be integrated")
	}
	// main's beads mirror kept
	content, _ := os.ReadFile(filepath.Join(beads, "issues.jsonl"))
	if !strings.Contains(string(content), "main-2") {
		t.Errorf(".beads must resolve to ours, got %q", content)
	}
}
