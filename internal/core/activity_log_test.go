package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAppendActivity_WritesLineAndGitignore(t *testing.T) {
	repo := t.TempDir()

	AppendActivity(repo, "p-1", "closed accepted")

	log, err := os.ReadFile(filepath.Join(repo, ".hydra", "activity.log"))
	if err != nil {
		t.Fatalf("activity.log not created: %v", err)
	}
	if !strings.Contains(string(log), "p-1 closed accepted") {
		t.Errorf("log missing entry, got: %q", log)
	}

	gi, err := os.ReadFile(filepath.Join(repo, ".gitignore"))
	if err != nil {
		t.Fatalf(".gitignore not created: %v", err)
	}
	found := false
	for _, l := range strings.Split(string(gi), "\n") {
		if strings.TrimSpace(l) == ".hydra/" {
			found = true
		}
	}
	if !found {
		t.Errorf(".gitignore missing '.hydra/' line, got: %q", gi)
	}
}

func TestAppendActivity_GitignoreIdempotent(t *testing.T) {
	repo := t.TempDir()
	AppendActivity(repo, "p-1", "a")
	AppendActivity(repo, "p-2", "b")

	gi, _ := os.ReadFile(filepath.Join(repo, ".gitignore"))
	n := strings.Count(string(gi), ".hydra/")
	if n != 1 {
		t.Errorf("expected exactly one '.hydra/' line, got %d in: %q", n, gi)
	}
}

func TestAppendActivity_TwoSequentialAppendsOrdered(t *testing.T) {
	repo := t.TempDir()
	AppendActivity(repo, "p-1", "first")
	AppendActivity(repo, "p-1", "second")

	log, _ := os.ReadFile(filepath.Join(repo, ".hydra", "activity.log"))
	lines := strings.Split(strings.TrimRight(string(log), "\n"), "\n")
	if len(lines) != 2 || !strings.HasSuffix(lines[0], "first") || !strings.HasSuffix(lines[1], "second") {
		t.Errorf("expected 'first' then 'second', got: %v", lines)
	}
}

func TestAppendActivity_PreservesExistingGitignore(t *testing.T) {
	repo := t.TempDir()
	gp := filepath.Join(repo, ".gitignore")
	if err := os.WriteFile(gp, []byte("node_modules\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	AppendActivity(repo, "p-1", "x")

	gi, _ := os.ReadFile(gp)
	if !strings.Contains(string(gi), "node_modules") || !strings.Contains(string(gi), ".hydra/") {
		t.Errorf("must preserve existing entries and add .hydra/, got: %q", gi)
	}
}

func TestAppendActivity_NoPanicOnBadInputs(t *testing.T) {
	// Empty args and an uncreatable path must return normally (fail-open).
	AppendActivity("", "p-1", "x")
	AppendActivity(t.TempDir(), "", "x")
	AppendActivity(filepath.Join(os.DevNull, "cannot", "mkdir", "here"), "p-1", "x")
}
