package core

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

func TestBeadTargetFiles(t *testing.T) {
	cases := []struct {
		name string
		bead types.Bead
		want int
	}{
		{"metadata", types.Bead{Metadata: map[string]string{"files": "a.go, b.go"}}, 2},
		{"description", types.Bead{Description: "Do X\nFiles: internal/x.go internal/y.go\nmore text"}, 2},
		{"tbd-ignored", types.Bead{Description: "Files: TBD"}, 0},
		{"none", types.Bead{Description: "no files declared here"}, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := beadTargetFiles(&tc.bead); len(got) != tc.want {
				t.Fatalf("want %d files, got %d (%v)", tc.want, len(got), got)
			}
		})
	}
}

func TestRepoHasChanges(t *testing.T) {
	// Non-git directory: fail open (true) so a close is never blocked on
	// inability to check.
	if ch, _ := repoHasChanges(t.TempDir(), nil); !ch {
		t.Fatalf("non-git dir should fail open to true")
	}

	repo := t.TempDir()
	git := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init")

	// Clean working tree -> no changes.
	if ch, err := repoHasChanges(repo, nil); err != nil || ch {
		t.Fatalf("clean repo: want (false,nil), got (%v,%v)", ch, err)
	}

	// Untracked file -> changes.
	if err := os.WriteFile(filepath.Join(repo, "x.go"), []byte("package x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if ch, err := repoHasChanges(repo, nil); err != nil || !ch {
		t.Fatalf("dirty repo: want (true,nil), got (%v,%v)", ch, err)
	}

	// Scoped to an unrelated path -> no changes.
	if ch, _ := repoHasChanges(repo, []string{"other.go"}); ch {
		t.Fatalf("scoped to unrelated file should report no changes")
	}
	// Scoped to the changed path -> changes.
	if ch, _ := repoHasChanges(repo, []string{"x.go"}); !ch {
		t.Fatalf("scoped to changed file should report changes")
	}
}
