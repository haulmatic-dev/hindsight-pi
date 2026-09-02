package core

import (
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// mockBeadsClientForResurrection wraps the shared mock so tests can set per-bead
// status and observe UpdateBeadStatus calls.
//
// Note: this test deliberately uses the existing mockBeadsClient; the helper is
// here only to make the test body readable.

// terminalBeadStatus returns a bead with the given status.
func terminalBeadStatus(id types.BeadID, project string, status types.BeadStatus) types.Bead {
	return types.Bead{
		ID:       id,
		Project:  project,
		Type:     "bug",
		Status:   status,
		Priority: 1,
	}
}

// setupResurrectionOverseer builds an overseer wired to a mock beads client and
// a temporary event sink. The returned overseer is otherwise unstarted.
func setupResurrectionOverseer(t *testing.T) (*Overseer, *mockBeadsClient, string) {
	t.Helper()

	cfg := DefaultConfig()
	cfg.Overseer.MaxWorkers = 4
	cfg.Overseer.DefaultRetries = 3
	cfg.Overseer.ResourceMonitoring.Enabled = false
	cfg.Overseer.ChainDetection.Enabled = false
	cfg.Intelligence.Graphify.Enabled = false
	cfg.Intelligence.GPTCache.Enabled = false
	cfg.Intelligence.Hindsight.Enabled = false
	cfg.Intelligence.Patterns.Enabled = false
	cfg.Intelligence.SettledState.Enabled = false
	cfg.Intelligence.CollectiveUnconscious.Enabled = false
	cfg.MCP.Enabled = false
	cfg.API.Enabled = false
	cfg.Agents.Enabled = false

	o := NewOverseer(cfg)

	mock := newMockBeadsClient()
	o.beadsClient = mock
	o.workerSpawner.beadsClient = mock

	projectPath := t.TempDir()
	o.beadAnalyzer.SetProjects([]types.Project{{Name: "proj", Path: projectPath, Type: types.RepoTypeSingle}})

	eventsDir := t.TempDir()
	o.beadCloser.SetEventSink(NewEventSink(eventsDir))

	return o, mock, eventsDir
}

// readEventsToday reads the central event stream for the current UTC day.
func readEventsToday(t *testing.T, dir string) []Event {
	t.Helper()
	events, err := ReadEvents(filepath.Join(dir, time.Now().UTC().Format("2006-01-02")+".jsonl"))
	if err != nil {
		t.Fatalf("ReadEvents: %v", err)
	}
	return events
}

// TestSpawnWorker_SuppressesTerminalBead verifies the root cause fix: a bead
// that is closed/completed in the store must not be spawned, must stay
// terminal, must be dropped from the retry queue, and must emit a typed
// event.
func TestSpawnWorker_SuppressesTerminalBead(t *testing.T) {
	o, mock, eventsDir := setupResurrectionOverseer(t)

	closedBead := terminalBeadStatus("proj-closed", "proj", types.StatusClosed)
	mock.addBead(closedBead)

	// Simulate the bead having failed and landed in the retry queue while it
	// was still open. Later it was closed by Pan / a human / supersession.
	o.retryManager.RecordRetry(string(closedBead.ID), fmt.Errorf("boom"))

	err := o.spawnWorker(closedBead)
	var suppressed *SpawnSuppressedError
	if !errors.As(err, &suppressed) {
		t.Fatalf("expected SpawnSuppressedError, got %v", err)
	}
	if suppressed.Status != types.StatusClosed {
		t.Errorf("suppressed status = %q, want %q", suppressed.Status, types.StatusClosed)
	}

	// Retry state must be purged so the scheduler does not keep trying.
	if o.retryManager.GetRetryCount(string(closedBead.ID)) != 0 {
		t.Errorf("retry count = %d, want 0 after suppression", o.retryManager.GetRetryCount(string(closedBead.ID)))
	}

	// The bead must remain closed, not flipped to in_progress.
	got, _ := mock.GetBead("", "", closedBead.ID)
	if got == nil || got.Status != types.StatusClosed {
		t.Fatalf("bead status = %v, want closed", got)
	}

	events := readEventsToday(t, eventsDir)
	var found bool
	for _, ev := range events {
		if ev.Type == "spawn-suppressed-closed" && ev.Bead == string(closedBead.ID) {
			found = true
			if ev.Reason != "already-terminal" {
				t.Errorf("event reason = %q, want already-terminal", ev.Reason)
			}
			break
		}
	}
	if !found {
		t.Fatalf("expected spawn-suppressed-closed event, got events: %+v", events)
	}
}

// TestSpawnWorker_OpenBeadRetriesNormally verifies that the suppression gate
// does not interfere with an open bead. The spawn itself will fail because there
// is no worker binary in the test environment, but the failure must NOT be a
// SpawnSuppressedError.
func TestSpawnWorker_OpenBeadRetriesNormally(t *testing.T) {
	o, mock, _ := setupResurrectionOverseer(t)

	openBead := terminalBeadStatus("proj-open", "proj", types.StatusOpen)
	mock.addBead(openBead)

	// Simulate a previous failure so this is a retry attempt.
	o.retryManager.RecordRetry(string(openBead.ID), fmt.Errorf("boom"))

	// Force a quick process start so the test exercises the normal spawn path
	// without running the real worker logic.
	t.Setenv("HYDRA_WORKER_BINARY", "/bin/false")

	err := o.spawnWorker(openBead)
	var suppressed *SpawnSuppressedError
	if errors.As(err, &suppressed) {
		t.Fatalf("open bead must not be suppressed, got %v", err)
	}
	// The spawn is allowed to proceed; a failure from /bin/false is still a
	// normal spawn attempt, not a suppression.
	if err != nil {
		t.Logf("open bead spawn returned non-suppression error (acceptable): %v", err)
	}
}

// TestSpawnInternal_RefusesMarkInProgressOnTerminalBead is the defense-in-depth
// test: even if a caller reaches SpawnInternal directly with a bead whose store
// status is terminal, the in_progress write must be refused.
func TestSpawnInternal_RefusesMarkInProgressOnTerminalBead(t *testing.T) {
	o, mock, _ := setupResurrectionOverseer(t)

	completedBead := terminalBeadStatus("proj-completed", "proj", types.StatusCompleted)
	mock.addBead(completedBead)

	_, err := o.workerSpawner.SpawnInternal(completedBead)
	var suppressed *SpawnSuppressedError
	if !errors.As(err, &suppressed) {
		t.Fatalf("expected SpawnSuppressedError from SpawnInternal, got %v", err)
	}

	got, _ := mock.GetBead("", "", completedBead.ID)
	if got == nil || got.Status != types.StatusCompleted {
		t.Fatalf("bead status = %v, want completed", got)
	}
}
