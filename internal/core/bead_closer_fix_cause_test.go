package core

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

func writeTestWorkerLog(t *testing.T, beadID string, lines ...string) {
	t.Helper()

	logDir := workersLogDir()
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		t.Fatalf("mkdir worker log dir: %v", err)
	}

	safe := strings.ReplaceAll(strings.ReplaceAll(beadID, "/", "-"), " ", "-")
	f, err := os.Create(filepath.Join(logDir, fmt.Sprintf("%s-123.log", safe)))
	if err != nil {
		t.Fatalf("create worker log: %v", err)
	}
	defer f.Close()

	for _, line := range lines {
		fmt.Fprintf(f, "2026-07-29T05:00:00Z [stderr] %s\n", line)
	}
}

func setupFixCauseCloser(t *testing.T, projectPath string) (*BeadCloser, *mockBeadsClient) {
	t.Helper()

	mock := newMockBeadsClient()
	resolver := setupTestProjectResolver(t, filepath.Base(projectPath), projectPath)
	cfg := DefaultConfig()
	cfg.Validation.CloseBlockedOnFailed = true

	closer := NewBeadCloser(mock, nil, resolver, nil, cfg)
	return closer, mock
}

func findFixBead(mock *mockBeadsClient) *types.Bead {
	mock.mu.RLock()
	defer mock.mu.RUnlock()

	for _, b := range mock.beads {
		if b.Type == "fix" {
			return &b
		}
	}
	return nil
}

func countFixBeadsForOriginal(mock *mockBeadsClient, origID types.BeadID) int {
	mock.mu.RLock()
	defer mock.mu.RUnlock()

	n := 0
	for _, b := range mock.beads {
		if b.Type != "fix" {
			continue
		}
		for _, dep := range b.Dependencies {
			if dep == origID {
				n++
				break
			}
		}
	}
	return n
}

// AC-1: a fix bead created for a failed original whose worker log names a failing
// test must include a "What failed last time" cause block with the test name.
func TestCreateFixBead_DescriptionIncludesCauseFromLog(t *testing.T) {
	t.Setenv("HYDRA_HOME", t.TempDir())
	projectPath := t.TempDir()
	closer, mock := setupFixCauseCloser(t, projectPath)

	projectName := filepath.Base(projectPath)
	origID := types.BeadID(fmt.Sprintf("%s-1", projectName))
	orig := types.Bead{
		ID:       origID,
		Title:    "GREEN: Implement cause-aware fix beads",
		Project:  projectName,
		Priority: 1,
		Status:   types.StatusInProgress,
	}
	mock.addBead(orig)

	writeTestWorkerLog(t, string(origID), "--- FAIL: TestCauseAwareDedup (0.00s)")

	worker := &types.Worker{BeadID: origID}
	if err := closer.Close(worker, false, 1); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	fixBead := findFixBead(mock)
	if fixBead == nil {
		t.Fatalf("expected a fix bead to be created, got none")
	}
	if !strings.Contains(fixBead.Description, "### What failed last time") {
		t.Errorf("fix bead description missing cause section:\n%s", fixBead.Description)
	}
	if !strings.Contains(fixBead.Description, "TestCauseAwareDedup") {
		t.Errorf("fix bead description missing failing test name:\n%s", fixBead.Description)
	}
}

// AC-2: creating a fix bead twice for the same open original must be a no-op and
// must return a nil error.
func TestCreateFixBead_DeduplicatesOpenFixBead(t *testing.T) {
	projectPath := t.TempDir()
	closer, mock := setupFixCauseCloser(t, projectPath)

	projectName := filepath.Base(projectPath)
	origID := types.BeadID(fmt.Sprintf("%s-2", projectName))
	orig := types.Bead{
		ID:       origID,
		Title:    "GREEN: Implement dedup",
		Project:  projectName,
		Priority: 1,
		Status:   types.StatusInProgress,
	}
	mock.addBead(orig)

	if err := closer.createFixBeadForFailedClose(&orig, projectPath, "", string(origID)); err != nil {
		t.Fatalf("first createFixBeadForFailedClose: %v", err)
	}
	if err := closer.createFixBeadForFailedClose(&orig, projectPath, "", string(origID)); err != nil {
		t.Fatalf("second createFixBeadForFailedClose: %v", err)
	}

	if got := countFixBeadsForOriginal(mock, origID); got != 1 {
		t.Errorf("expected exactly 1 fix bead for original after dedup, got %d", got)
	}
}

// AC-3: an original whose only fix bead is already closed must be allowed to get
// a new fix bead.
func TestCreateFixBead_NewFixWhenExistingClosed(t *testing.T) {
	projectPath := t.TempDir()
	closer, mock := setupFixCauseCloser(t, projectPath)

	projectName := filepath.Base(projectPath)
	origID := types.BeadID(fmt.Sprintf("%s-3", projectName))
	orig := types.Bead{
		ID:       origID,
		Title:    "GREEN: Implement retry",
		Project:  projectName,
		Priority: 1,
		Status:   types.StatusInProgress,
	}
	mock.addBead(orig)

	closedFix := types.Bead{
		ID:           types.BeadID(fmt.Sprintf("%s-fix-closed", projectName)),
		Title:        "Fix: GREEN: Implement retry",
		Type:         "fix",
		Project:      projectName,
		Status:       types.StatusClosed,
		Dependencies: []types.BeadID{origID},
		Labels:       []string{"auto-fix", "failed-closure"},
	}
	mock.addBead(closedFix)

	if err := closer.createFixBeadForFailedClose(&orig, projectPath, "", string(origID)); err != nil {
		t.Fatalf("createFixBeadForFailedClose: %v", err)
	}

	if got := countFixBeadsForOriginal(mock, origID); got != 2 {
		t.Errorf("expected 2 fix beads for original (closed + new), got %d", got)
	}
}

// AC-4: even when there is no worker log on disk, a fix bead is still created
// and its description names the exit code.
func TestCreateFixBead_DescriptionIncludesExitCodeWithNoLog(t *testing.T) {
	projectPath := t.TempDir()
	closer, mock := setupFixCauseCloser(t, projectPath)

	projectName := filepath.Base(projectPath)
	origID := types.BeadID(fmt.Sprintf("%s-4", projectName))
	orig := types.Bead{
		ID:       origID,
		Title:    "GREEN: Implement no-log path",
		Project:  projectName,
		Priority: 1,
		Status:   types.StatusInProgress,
	}
	mock.addBead(orig)

	worker := &types.Worker{BeadID: origID}
	if err := closer.Close(worker, false, 5); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	fixBead := findFixBead(mock)
	if fixBead == nil {
		t.Fatalf("expected a fix bead to be created, got none")
	}
	if !strings.Contains(fixBead.Description, "Exit code: 5") {
		t.Errorf("fix bead description missing exit code:\n%s", fixBead.Description)
	}
}

// AC-5 regression guard: the recursion cap and its tests must keep working
// after the cause/dedup changes.
func TestCreateFixBead_CauseAndDedupDoNotAffectRecursionCap(t *testing.T) {
	projectPath := t.TempDir()
	closer, mock := setupFixCauseCloser(t, projectPath)

	projectName := filepath.Base(projectPath)
	orig := types.Bead{
		ID:       types.BeadID(fmt.Sprintf("%s-recursion-1", projectName)),
		Title:    "Fix: GREEN: Implement the reader",
		Project:  filepath.Base(projectPath),
		Priority: 1,
	}
	mock.addBead(orig)

	if err := closer.createFixBeadForFailedClose(&orig, projectPath, "", string(orig.ID)); err != nil {
		t.Fatalf("createFixBeadForFailedClose: %v", err)
	}
	if got := countFixBeadsForOriginal(mock, orig.ID); got != 0 {
		t.Errorf("expected 0 nested fix beads, got %d", got)
	}
}
