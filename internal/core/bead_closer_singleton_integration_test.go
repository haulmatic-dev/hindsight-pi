package core

import (
	"bytes"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

func initGitRepoWithCommit(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runGit(t, dir, "init", "--initial-branch=main")
	runGit(t, dir, "config", "user.email", "test@example.com")
	runGit(t, dir, "config", "user.name", "Test")
	writeFile(t, dir, "README.md", "# repo\n")
	runGit(t, dir, "add", "README.md")
	runGit(t, dir, "commit", "-m", "initial")
	return dir
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v in %s: %v\n%s", args, dir, err, out)
	}
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

func newTestBeadCloser(t *testing.T, projectName, projectPath string, m *WorktreeManager, chainKeyFn func(types.Bead) string, isChainMember func(types.BeadID) bool, beadsClient *mockBeadsClient) *BeadCloser {
	t.Helper()
	cfg := DefaultConfig()
	cfg.Validation.Enabled = false
	cfg.Validation.CloseBlockedOnFailed = false

	resolver := NewProjectResolver([]types.Project{
		{Name: projectName, Path: projectPath, Type: types.RepoTypeSingle},
	})
	closer := NewBeadCloser(
		beadsClient,
		NewIntelligenceHub(cfg.Intelligence),
		resolver,
		NewValidationService(cfg, NewRetryManager(5*time.Second, 5*time.Minute), beadsClient),
		cfg,
	)
	closer.SetWorktreeManager(m, chainKeyFn, isChainMember)
	return closer
}

func TestBeadCloser_IntegratesSingletonOnClose(t *testing.T) {
	projectPath := initGitRepoWithCommit(t)
	t.Setenv("HYDRA_HOME", t.TempDir())

	m := NewWorktreeManager()
	beadID := types.BeadID("singleton-abc")
	key := beadID.String()

	wt, err := m.EnsureWorktree(projectPath, key)
	if err != nil {
		t.Fatalf("EnsureWorktree: %v", err)
	}
	writeFile(t, wt, "feature.txt", "feature work\n")

	mockBeads := newMockBeadsClient()
	mockBeads.addBead(types.Bead{
		ID:      beadID,
		Title:   "singleton bead",
		Project: "singleton",
		Status:  types.StatusInProgress,
	})

	closer := newTestBeadCloser(t, "singleton", projectPath, m,
		func(b types.Bead) string { return b.ID.String() },
		func(types.BeadID) bool { return false },
		mockBeads,
	)

	if err := closer.Close(&types.Worker{BeadID: beadID}, true, 0); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	if branchExists(projectPath, "hydra/wt-"+key) {
		t.Fatal("expected singleton worktree branch to be removed")
	}

	got, err := os.ReadFile(filepath.Join(projectPath, "feature.txt"))
	if err != nil {
		t.Fatalf("expected feature.txt on working branch: %v", err)
	}
	if string(got) != "feature work\n" {
		t.Fatalf("feature.txt = %q, want %q", got, "feature work\n")
	}
}

func TestBeadCloser_DoesNotIntegrateChainMember(t *testing.T) {
	projectPath := initGitRepoWithCommit(t)
	t.Setenv("HYDRA_HOME", t.TempDir())

	m := NewWorktreeManager()
	beadID := types.BeadID("member-xyz")
	chainKey := "root-chain-123"

	wt, err := m.EnsureWorktree(projectPath, chainKey)
	if err != nil {
		t.Fatalf("EnsureWorktree: %v", err)
	}
	writeFile(t, wt, "chain-feature.txt", "chain work\n")

	mockBeads := newMockBeadsClient()
	mockBeads.addBead(types.Bead{
		ID:      beadID,
		Title:   "chain member",
		Project: "member",
		Status:  types.StatusInProgress,
	})

	closer := newTestBeadCloser(t, "member", projectPath, m,
		func(types.Bead) string { return chainKey },
		func(types.BeadID) bool { return true },
		mockBeads,
	)

	if err := closer.Close(&types.Worker{BeadID: beadID}, true, 0); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	if !branchExists(projectPath, "hydra/wt-"+chainKey) {
		t.Fatal("expected chain worktree branch to remain")
	}

	if _, err := os.ReadFile(filepath.Join(projectPath, "chain-feature.txt")); err == nil {
		t.Fatal("did not expect chain feature on working branch yet")
	}
}

func TestBeadCloser_IntegrateErrorDoesNotFailClose(t *testing.T) {
	projectPath := initGitRepoWithCommit(t)
	t.Setenv("HYDRA_HOME", t.TempDir())

	m := NewWorktreeManager()
	beadID := types.BeadID("conflict-def")
	key := beadID.String()

	// Create the worktree before the conflicting commit on main, so the two
	// branches diverge and the same file is added independently.
	wt, err := m.EnsureWorktree(projectPath, key)
	if err != nil {
		t.Fatalf("EnsureWorktree: %v", err)
	}

	writeFile(t, projectPath, "conflict.txt", "main content\n")
	runGit(t, projectPath, "add", "conflict.txt")
	runGit(t, projectPath, "commit", "-m", "add conflict on main")

	writeFile(t, wt, "conflict.txt", "worktree content\n")

	mockBeads := newMockBeadsClient()
	mockBeads.addBead(types.Bead{
		ID:      beadID,
		Title:   "conflicting singleton",
		Project: "conflict",
		Status:  types.StatusInProgress,
	})

	var buf bytes.Buffer
	oldOutput := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(oldOutput)

	closer := newTestBeadCloser(t, "conflict", projectPath, m,
		func(b types.Bead) string { return b.ID.String() },
		func(types.BeadID) bool { return false },
		mockBeads,
	)

	if err := closer.Close(&types.Worker{BeadID: beadID}, true, 0); err != nil {
		t.Fatalf("Close() should not fail on integrate error, got %v", err)
	}

	if !strings.Contains(buf.String(), "integrate failed") {
		t.Fatalf("expected log to contain 'integrate failed', got:\n%s", buf.String())
	}

	if !branchExists(projectPath, "hydra/wt-"+key) {
		t.Fatal("expected conflicting worktree branch to remain for inspection")
	}

	// Working branch should be unchanged (merge aborted).
	got, err := os.ReadFile(filepath.Join(projectPath, "conflict.txt"))
	if err != nil {
		t.Fatalf("read conflict.txt: %v", err)
	}
	if string(got) != "main content\n" {
		t.Fatalf("working branch conflict.txt = %q, want %q", got, "main content\n")
	}
}
