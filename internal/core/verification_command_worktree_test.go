package core

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// The verification-command guard must run in the bead's chain worktree when
// one exists: the main checkout pre-integration has neither the bead's code
// nor a fresh dependency install, so gating there is RED no matter what the
// worker did (2026-10-15: ts-jest-missing in an empty main-checkout
// node_modules escalated vv3 3/3 and capped 269 at 5 attempts on a gate
// that could never pass).
func TestVerificationCommandGuard_PrefersChainWorktree(t *testing.T) {
	projectPath := t.TempDir()
	runGitVC(t, projectPath, "init")
	runGitVC(t, projectPath, "commit", "--allow-empty", "-m", "init")

	closer, _ := gateTestCloser(t, gateTestConfig(true, "true"), projectPath)
	marker := filepath.Join(t.TempDir(), "gate-ran-in")
	bead := &types.Bead{
		ID: "proj-vc1", Project: "proj", Status: types.StatusInProgress,
		Metadata: map[string]string{"test_command": "pwd > " + marker},
	}

	t.Run("runs in worktree when one exists", func(t *testing.T) {
		wtPath := filepath.Join(worktreeRoot(projectPath), sanitizeKey("chain-k1"))
		if err := os.MkdirAll(filepath.Dir(wtPath), 0o755); err != nil {
			t.Fatal(err)
		}
		runGitVC(t, projectPath, "worktree", "add", wtPath)
		closer.SetWorktreeManager(NewWorktreeManager(), func(types.Bead) string { return "chain-k1" }, nil)

		if err := closer.verificationCommandGuard(bead, projectPath); err != nil {
			t.Fatalf("guard: %v", err)
		}
		got := strings.TrimSpace(readFile(t, marker))
		if got != wtPath {
			t.Errorf("gate ran in %q, want worktree %q", got, wtPath)
		}
	})

	t.Run("falls back to main checkout without a worktree", func(t *testing.T) {
		os.Remove(marker)
		closer.SetWorktreeManager(NewWorktreeManager(), func(types.Bead) string { return "no-such-chain" }, nil)

		if err := closer.verificationCommandGuard(bead, projectPath); err != nil {
			t.Fatalf("guard: %v", err)
		}
		got := strings.TrimSpace(readFile(t, marker))
		if got != projectPath {
			t.Errorf("gate ran in %q, want main checkout %q", got, projectPath)
		}
	})
}

func runGitVC(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
