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

// TestBeadCloser_IntegrateErrorFailsClose: an integrate failure must fail the
// close — the bead is left open, an integration-failed event is emitted, and
// the worktree is preserved. (Previous behavior logged "integrate failed" and
// closed the bead anyway, which is how gate-verified work dangled
// unreferenced — hydra-xnx 2026-09-06.)
func TestBeadCloser_IntegrateErrorFailsClose(t *testing.T) {
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
	closer.SetEventSink(NewEventSink(""))

	err = closer.Close(&types.Worker{BeadID: beadID}, true, 0)
	if err == nil {
		t.Fatal("Close() should fail closed on integrate error, got nil")
	}
	if !strings.Contains(err.Error(), "integration-failed") {
		t.Fatalf("Close() error = %v, want integration-failed", err)
	}

	if !strings.Contains(buf.String(), "FAILED conflict-def at integrate") {
		t.Fatalf("expected integrate failure in log, got:\n%s", buf.String())
	}
	if strings.Contains(buf.String(), "integrated singleton") {
		t.Fatalf("'integrated singleton' logged over a failed merge:\n%s", buf.String())
	}

	// Bead left open, not closed.
	fresh, gErr := mockBeads.GetBead(projectPath, "conflict", beadID)
	if gErr != nil || fresh == nil {
		t.Fatalf("GetBead: %v", gErr)
	}
	if fresh.Status.IsTerminal() {
		t.Fatalf("bead %s closed despite integrate failure (status %s)", beadID, fresh.Status)
	}
	if fresh.Status != types.StatusOpen {
		t.Fatalf("bead %s status = %s, want %s (reopened)", beadID, fresh.Status, types.StatusOpen)
	}

	// Worktree preserved for inspection.
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

	// Typed event with the exact error.
	events, rErr := ReadEvents(filepath.Join(projectPath, ".hydra", "events.jsonl"))
	if rErr != nil {
		t.Fatalf("ReadEvents: %v", rErr)
	}
	found := false
	for _, ev := range events {
		if ev.Type == "integration-failed" {
			found = true
			if ev.Reason != "integrate_failed" {
				t.Errorf("integration-failed reason = %q, want integrate_failed", ev.Reason)
			}
			if !strings.Contains(ev.Detail, "integrate") {
				t.Errorf("integration-failed detail missing stage: %q", ev.Detail)
			}
		}
	}
	if !found {
		t.Fatal("expected integration-failed event in repo mirror")
	}
}

// TestBeadCloser_PushRejectionFailsClosed: a GH006-style branch-protection
// rejection must not be swallowed — the bead stays open, the worktree is
// preserved, the local merge stays, and the event carries the exact remote
// error. This is the hydra-xnx 2026-09-06 incident: "pushed" was logged, the
// bead closed, and the gate-verified commits dangled unreferenced.
func TestBeadCloser_PushRejectionFailsClosed(t *testing.T) {
	projectPath := initGitRepoWithCommit(t)
	t.Setenv("HYDRA_HOME", t.TempDir())

	remote := filepath.Join(t.TempDir(), "remote.git")
	initBareRemote(t, remote)
	git(t, projectPath, "remote", "add", "origin", remote)
	git(t, projectPath, "push", "-u", "origin", "main")
	baseSHA := headCommit(t, projectPath)
	// Branch protection rejects every subsequent push, GH006-style.
	writeHook(t, remote, "pre-receive", "#!/bin/sh\necho \"remote: error: GH006: Protected branch update failed. Changes must be made through a pull request.\" >&2\nexit 1\n")

	m := NewWorktreeManager()
	beadID := types.BeadID("pushrej-abc")
	key := beadID.String()

	wt, err := m.EnsureWorktree(projectPath, key)
	if err != nil {
		t.Fatalf("EnsureWorktree: %v", err)
	}
	writeFile(t, wt, "feature.txt", "feature work\n")

	mockBeads := newMockBeadsClient()
	mockBeads.addBead(types.Bead{
		ID:      beadID,
		Title:   "push rejection bead",
		Project: "pushrej",
		Status:  types.StatusInProgress,
	})

	var buf bytes.Buffer
	oldOutput := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(oldOutput)

	closer := newTestBeadCloser(t, "pushrej", projectPath, m,
		func(b types.Bead) string { return b.ID.String() },
		func(types.BeadID) bool { return false },
		mockBeads,
	)
	closer.config.Output.Push = true
	closer.config.Output.Remote = "origin"
	closer.SetEventSink(NewEventSink(""))

	err = closer.Close(&types.Worker{BeadID: beadID}, true, 0)
	if err == nil {
		t.Fatal("Close() should fail closed on push rejection, got nil")
	}
	if !strings.Contains(err.Error(), "integration-failed") {
		t.Fatalf("Close() error = %v, want integration-failed", err)
	}

	if !strings.Contains(buf.String(), "push verification FAILED") && !strings.Contains(buf.String(), "push skipped") {
		t.Fatalf("expected push failure in log, got:\n%s", buf.String())
	}
	if strings.Contains(buf.String(), "integrated singleton") {
		t.Fatalf("'integrated singleton' logged over a rejected push:\n%s", buf.String())
	}

	// Bead left open.
	fresh, gErr := mockBeads.GetBead(projectPath, "pushrej", beadID)
	if gErr != nil || fresh == nil {
		t.Fatalf("GetBead: %v", gErr)
	}
	if fresh.Status.IsTerminal() {
		t.Fatalf("bead %s closed despite push rejection (status %s)", beadID, fresh.Status)
	}

	// Worktree preserved.
	if !branchExists(projectPath, "hydra/wt-"+key) {
		t.Fatal("expected worktree branch to remain after rejected push")
	}

	// Local merge landed (integrate succeeded; only push failed).
	got, rErr := os.ReadFile(filepath.Join(projectPath, "feature.txt"))
	if rErr != nil {
		t.Fatalf("expected local merge to land feature.txt: %v", rErr)
	}
	if string(got) != "feature work\n" {
		t.Fatalf("feature.txt = %q, want %q", got, "feature work\n")
	}

	// Remote ref did NOT advance.
	if out := git(t, projectPath, "ls-remote", remote, "refs/heads/main"); !strings.Contains(out, baseSHA) {
		t.Fatalf("remote main moved: ls-remote = %q, want base %s", out, baseSHA)
	}

	// Typed event carries the exact remote error.
	events, rErr := ReadEvents(filepath.Join(projectPath, ".hydra", "events.jsonl"))
	if rErr != nil {
		t.Fatalf("ReadEvents: %v", rErr)
	}
	found := false
	for _, ev := range events {
		if ev.Type == "integration-failed" {
			found = true
			if ev.Reason != "push_failed" {
				t.Errorf("integration-failed reason = %q, want push_failed", ev.Reason)
			}
			if !strings.Contains(ev.Detail, "GH006") {
				t.Errorf("integration-failed detail missing exact remote error: %q", ev.Detail)
			}
		}
	}
	if !found {
		t.Fatal("expected integration-failed event in repo mirror")
	}
}

// TestBeadCloser_IntegratePushVerifiedSuccess: on the happy path the success
// logs happen only after verification — the merge is confirmed to contain the
// worktree branch tip and the remote ref is confirmed (ls-remote) to match the
// local head before "integrated"/"pushed" are logged and the worktree is
// removed.
func TestBeadCloser_IntegratePushVerifiedSuccess(t *testing.T) {
	projectPath := initGitRepoWithCommit(t)
	t.Setenv("HYDRA_HOME", t.TempDir())

	remote := filepath.Join(t.TempDir(), "remote.git")
	initBareRemote(t, remote)
	git(t, projectPath, "remote", "add", "origin", remote)
	git(t, projectPath, "push", "-u", "origin", "main")

	m := NewWorktreeManager()
	beadID := types.BeadID("pushok-abc")
	key := beadID.String()

	wt, err := m.EnsureWorktree(projectPath, key)
	if err != nil {
		t.Fatalf("EnsureWorktree: %v", err)
	}
	writeFile(t, wt, "feature.txt", "feature work\n")

	mockBeads := newMockBeadsClient()
	mockBeads.addBead(types.Bead{
		ID:      beadID,
		Title:   "push ok bead",
		Project: "pushok",
		Status:  types.StatusInProgress,
	})

	var buf bytes.Buffer
	oldOutput := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(oldOutput)

	closer := newTestBeadCloser(t, "pushok", projectPath, m,
		func(b types.Bead) string { return b.ID.String() },
		func(types.BeadID) bool { return false },
		mockBeads,
	)
	closer.config.Output.Push = true
	closer.config.Output.Remote = "origin"
	closer.SetEventSink(NewEventSink(""))

	if err := closer.Close(&types.Worker{BeadID: beadID}, true, 0); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	logs := buf.String()
	if !strings.Contains(logs, "integrated singleton") {
		t.Fatalf("expected 'integrated singleton' in log:\n%s", logs)
	}
	if !strings.Contains(logs, "pushed "+string(beadID)+" to origin/main") {
		t.Fatalf("expected verified push log line:\n%s", logs)
	}

	// Worktree removed only after verified integrate+push.
	if branchExists(projectPath, "hydra/wt-"+key) {
		t.Fatal("expected worktree branch removed after verified integrate+push")
	}

	// Remote ref advanced to the local merge commit.
	local := headCommit(t, projectPath)
	if !remoteHas(t, remote, "main", local) {
		t.Fatalf("remote main missing verified merge commit %s", local)
	}

	// Bead closed, no integration-failed event.
	fresh, gErr := mockBeads.GetBead(projectPath, "pushok", beadID)
	if gErr != nil || fresh == nil {
		t.Fatalf("GetBead: %v", gErr)
	}
	if !fresh.Status.IsTerminal() {
		t.Fatalf("bead %s status = %s, want terminal", beadID, fresh.Status)
	}
	events, rErr := ReadEvents(filepath.Join(projectPath, ".hydra", "events.jsonl"))
	if rErr != nil {
		t.Fatalf("ReadEvents: %v", rErr)
	}
	for _, ev := range events {
		if ev.Type == "integration-failed" {
			t.Fatalf("unexpected integration-failed event on success: %+v", ev)
		}
	}
}
