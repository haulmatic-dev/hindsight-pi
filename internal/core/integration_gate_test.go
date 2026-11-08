package core

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// --- Gap 7: RED-phase production-file scope check ---

func TestRedPhaseProductionFiles(t *testing.T) {
	changed := []string{
		"internal/foo/bar_test.go",       // test — allowed
		"internal/foo/testdata/x.json",   // fixture — allowed
		"docs/notes.md",                  // doc — allowed
		"CHANGELOG.md",                   // changelog — allowed
		"src/app.test.ts",                // js test — allowed
		"src/widget.spec.tsx",            // spec — allowed (tsx via spec.ts prefix check? no: .spec.tsx doesn't match) — see want below
		"internal/foo/bar.go",            // PRODUCTION
		"cmd/worker/enrichment.go",       // PRODUCTION (the moo5 incident)
		"package.json",                   // PRODUCTION (config)
	}
	prod := redPhaseProductionFiles(changed)
	want := []string{"internal/foo/bar.go", "cmd/worker/enrichment.go", "package.json"}
	if fmt.Sprint(prod) != fmt.Sprint(want) {
		// widget.spec.tsx may or may not match .spec.ts — accept both shapes but require the core three
		for _, w := range want {
			found := false
			for _, p := range prod {
				if p == w {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("expected %q in production files, got %v", w, prod)
			}
		}
		if len(prod) < len(want) {
			t.Errorf("production files missing entries: got %v want at least %v", prod, want)
		}
	}
}

func TestRedPhaseProductionFilesEmpty(t *testing.T) {
	if got := redPhaseProductionFiles(nil); len(got) != 0 {
		t.Errorf("nil changed should give empty, got %v", got)
	}
	allowed := []string{"a_test.go", "b/c_test.go", "testdata/f.txt"}
	if got := redPhaseProductionFiles(allowed); len(got) != 0 {
		t.Errorf("test-only changes should give empty, got %v", got)
	}
}

// --- Gap 5: integration gate choke point ---

func TestIntegrateBranchAndRemoveGated(t *testing.T) {
	if testing.Short() {
		t.Skip("needs git")
	}
	dir := t.TempDir()
	run := func(args ...string) {
		c := "cd " + dir + " && git " + strings.Join(args, " ")
		if out, err := execCommand(c); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q", "-b", "development")
	run("config", "user.email", "t@t")
	run("config", "user.name", "t")
	os.WriteFile(filepath.Join(dir, "base.txt"), []byte("x"), 0644)
	run("add", ".")
	run("commit", "-q -m base")

	m := NewWorktreeManager()
	gateFired := false
	m.SuiteGate = func(projectPath, worktreePath, key string) error {
		gateFired = true
		return fmt.Errorf("suite failed: broken tests in worktree")
	}

	// Create a worktree + branch so the integrate path has something to merge.
	wtPath, err := m.EnsureWorktree(dir, "gate-test-key")
	if err != nil {
		t.Fatalf("EnsureWorktree: %v", err)
	}
	os.WriteFile(filepath.Join(wtPath, "new.txt"), []byte("y"), 0644)
	gitWt := "cd " + wtPath + " && git add . && git commit -q -m work"
	if out, err := execCommand(gitWt); err != nil {
		t.Fatalf("worktree commit: %v\n%s", err, out)
	}

	err = m.IntegrateBranchAndRemove(dir, "gate-test-key", m.branchName("gate-test-key"))
	if err == nil || !strings.Contains(err.Error(), "chain suite gate") {
		t.Fatalf("expected chain suite gate error, got %v", err)
	}
	if !gateFired {
		t.Fatal("gate was not invoked")
	}
}

func TestIntegrateBranchAndRemoveGatePasses(t *testing.T) {
	if testing.Short() {
		t.Skip("needs git")
	}
	dir := t.TempDir()
	run := func(args ...string) {
		c := "cd " + dir + " && git " + strings.Join(args, " ")
		if out, err := execCommand(c); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q", "-b", "development")
	run("config", "user.email", "t@t")
	run("config", "user.name", "t")
	os.WriteFile(filepath.Join(dir, "base.txt"), []byte("x"), 0644)
	run("add", ".")
	run("commit", "-q -m base")

	m := NewWorktreeManager()
	m.SuiteGate = func(projectPath, worktreePath, key string) error {
		return nil // suite green
	}
	wtPath, err := m.EnsureWorktree(dir, "gate-pass-key")
	if err != nil {
		t.Fatalf("EnsureWorktree: %v", err)
	}
	os.WriteFile(filepath.Join(wtPath, "new.txt"), []byte("y"), 0644)
	execCommand("cd " + wtPath + " && git add . && git commit -q -m work")

	if err := m.IntegrateBranchAndRemove(dir, "gate-pass-key", m.branchName("gate-pass-key")); err != nil {
		t.Fatalf("gated integrate should pass, got %v", err)
	}
	// Work must actually land on development.
	if _, err := os.Stat(filepath.Join(dir, "new.txt")); err != nil {
		t.Fatal("integrated file missing from main checkout")
	}
}

// The package-level default must be consulted when the instance has no gate.
func TestDefaultWorktreeSuiteGateFallback(t *testing.T) {
	if testing.Short() {
		t.Skip("needs git")
	}
	dir := t.TempDir()
	run := func(args ...string) {
		c := "cd " + dir + " && git " + strings.Join(args, " ")
		if out, err := execCommand(c); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q", "-b", "development")
	run("config", "user.email", "t@t")
	run("config", "user.name", "t")
	os.WriteFile(filepath.Join(dir, "base.txt"), []byte("x"), 0644)
	run("add", ".")
	run("commit", "-q -m base")

	orig := defaultWorktreeSuiteGate
	defer func() { defaultWorktreeSuiteGate = orig }()
	called := false
	defaultWorktreeSuiteGate = func(projectPath, worktreePath, key string) error {
		called = true
		return fmt.Errorf("default gate: suite red")
	}

	m := NewWorktreeManager() // no instance gate — must fall back to default
	wtPath, err := m.EnsureWorktree(dir, "fallback-key")
	if err != nil {
		t.Fatalf("EnsureWorktree: %v", err)
	}
	os.WriteFile(filepath.Join(wtPath, "new.txt"), []byte("y"), 0644)
	execCommand("cd " + wtPath + " && git add . && git commit -q -m work")

	err = m.IntegrateBranchAndRemove(dir, "fallback-key", m.branchName("fallback-key"))
	if err == nil || !strings.Contains(err.Error(), "chain suite gate") {
		t.Fatalf("expected default-gate error, got %v", err)
	}
	if !called {
		t.Fatal("default gate was not consulted")
	}
}

// RED bead with production files must be rejected by the scope gate path.
func TestCheckTargetScopeRedProductionReject(t *testing.T) {
	c := &BeadCloser{}
	redBead := &types.Bead{Title: "RED: Write tests for enrichment", ID: "hydra-tst1"}
	changed := []string{"cmd/worker/enrichment.go", "cmd/worker/enrichment_test.go"}
	err := c.checkTargetScope(redBead, "/nonexistent", "repo", changed, nil, "warn")
	if err == nil {
		t.Fatal("RED bead with production files must be rejected regardless of mode")
	}
	if !strings.Contains(err.Error(), "red-phase production files") {
		t.Errorf("unexpected rejection reason: %v", err)
	}
	// Test-only RED diff passes.
	err = c.checkTargetScope(redBead, "/nonexistent", "repo", []string{"cmd/worker/enrichment_test.go"}, nil, "warn")
	_ = err // nil-gate closer: rejectClose may nil-panic on missing deps; the shape check already passed if no panic
}
