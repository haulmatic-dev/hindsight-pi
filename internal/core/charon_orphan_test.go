package core

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/haulmatic-dev/hydra/internal/beads"
	"github.com/haulmatic-dev/hydra/pkg/types"
)

// recordingExecutor is a beads CLIExecutor that records every command it sees
// and returns canned responses for the br list/update/close calls used by
// orphan recovery tests.
type recordingExecutor struct {
	mu        sync.Mutex
	calls     []string
	listResp  []byte
	getResp   []byte
	getErr    error
	closeErr  error
	updateErr error
}

func (e *recordingExecutor) Execute(name string, arg ...string) ([]byte, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.calls = append(e.calls, strings.Join(append([]string{name}, arg...), " "))

	return e.respond(name, arg)
}

func (e *recordingExecutor) ExecuteInDir(dir string, name string, arg ...string) ([]byte, error) {
	return e.Execute(name, arg...)
}

func (e *recordingExecutor) respond(name string, arg []string) ([]byte, error) {
	if name == "br" && len(arg) > 0 {
		switch arg[0] {
		case "list":
			return e.listResp, nil
		case "show":
			if e.getErr != nil {
				return nil, e.getErr
			}
			return e.getResp, nil
		case "update":
			if e.updateErr != nil {
				return nil, e.updateErr
			}
			return nil, nil
		case "close":
			if e.closeErr != nil {
				return nil, e.closeErr
			}
			// Fail by default so any unexpected close is visible in logs and
			// does not silently succeed.
			return nil, fmt.Errorf("unexpected close call")
		}
	}
	return nil, fmt.Errorf("unexpected command: %s %s", name, strings.Join(arg, " "))
}

func (e *recordingExecutor) Calls() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]string, len(e.calls))
	copy(out, e.calls)
	return out
}

func (e *recordingExecutor) CountCalls(prefix string) int {
	n := 0
	for _, c := range e.Calls() {
		if strings.HasPrefix(c, prefix) {
			n++
		}
	}
	return n
}

// beadListJSON returns a paginated br list response containing a single
// in_progress bead with the supplied updatedAt timestamp.
func inProgressDetailFromList(listResp []byte) []byte {
	if len(listResp) == 0 {
		return nil
	}
	var page struct {
		Issues []map[string]any `json:"issues"`
	}
	if err := json.Unmarshal(listResp, &page); err != nil || len(page.Issues) == 0 {
		return nil
	}
	issue := page.Issues[0]
	// Force the detail status to in_progress so the default GetBead response
	// matches the list response; tests that need a terminal/failed detail
	// override exec.getResp explicitly.
	issue["status"] = "in_progress"
	b, _ := json.Marshal([]map[string]any{issue})
	return b
}

func terminalBeadJSON(beadID string, status string) []byte {
	updatedStr := time.Now().Add(-10 * time.Minute).Format(time.RFC3339)
	createdStr := time.Now().Add(-70 * time.Minute).Format(time.RFC3339)

	m := map[string]any{
		"id":         beadID,
		"title":      "orphan bead",
		"status":     status,
		"priority":   4,
		"issue_type": "implement",
		"labels":     []string{},
		"created_at": createdStr,
		"updated_at": updatedStr,
	}
	b, _ := json.Marshal([]map[string]any{m})
	return b
}

func beadListJSON(beadID string, updatedAt time.Time) []byte {
	updatedStr := updatedAt.Format(time.RFC3339)
	if updatedAt.IsZero() {
		updatedStr = "0001-01-01T00:00:00Z"
	}
	createdStr := updatedAt.Add(-time.Hour).Format(time.RFC3339)
	if updatedAt.IsZero() {
		createdStr = "0001-01-01T00:00:00Z"
	}

	type page struct {
		Issues  []map[string]any `json:"issues"`
		Total   int              `json:"total"`
		Limit   int              `json:"limit"`
		Offset  int              `json:"offset"`
		HasMore bool             `json:"has_more"`
	}
	p := page{
		Issues: []map[string]any{{
			"id":               beadID,
			"title":            "orphan bead",
			"description":      nil,
			"status":           "in_progress",
			"priority":         4,
			"issue_type":       "implement",
			"labels":           []string{},
			"created_at":       createdStr,
			"updated_at":       updatedStr,
			"dependency_count": 0,
			"dependent_count":  0,
		}},
		Total:   1,
		Limit:   500,
		Offset:  0,
		HasMore: false,
	}
	b, _ := json.Marshal(p)
	return b
}

func setupOrphanCharon(t *testing.T, listResp []byte, grace time.Duration) (*Charon, *recordingExecutor) {
	t.Helper()

	tracker, err := NewWorkerTracker(t.TempDir())
	if err != nil {
		t.Fatalf("NewWorkerTracker: %v", err)
	}

	exec := &recordingExecutor{listResp: listResp, getResp: inProgressDetailFromList(listResp)}
	client := beads.NewClientWithExecutor(exec)
	charon := NewCharon(client, tracker, 0)
	charon.orphanBeadGrace = grace
	charon.SetProjectsProvider(func() []types.Project {
		return []types.Project{{Name: "test", Path: t.TempDir()}}
	})
	return charon, exec
}

func TestRecoverOrphanInProgressBeads_SkipsTerminalBeadAndReapsRow(t *testing.T) {
	beadID := "test-orphan-terminal"
	charon, exec := setupOrphanCharon(t, beadListJSON(beadID, time.Now().Add(-10*time.Minute)), 5*time.Minute)
	exec.getResp = terminalBeadJSON(beadID, "closed")

	projectPath := t.TempDir()
	charon.SetProjectsProvider(func() []types.Project {
		return []types.Project{{Name: "test", Path: projectPath}}
	})

	// Stale worker row for a bead that is already terminal (process is gone, only
	// the persisted record remains).
	if err := charon.workerTracker.TrackWorkerWithPGID(12345, 12345, beadID, projectPath); err != nil {
		t.Fatalf("TrackWorkerWithPGID: %v", err)
	}
	if err := charon.workerTracker.MarkWorkerCompleted(12345, 0); err != nil {
		t.Fatalf("MarkWorkerCompleted: %v", err)
	}

	var buf bytes.Buffer
	oldOutput := log.Writer()
	oldFlags := log.Flags()
	log.SetOutput(&buf)
	log.SetFlags(0)
	defer func() {
		log.SetOutput(oldOutput)
		log.SetFlags(oldFlags)
	}()

	got := charon.recoverOrphanInProgressBeads()
	if got != 0 {
		t.Fatalf("recovered = %d, want 0 for terminal bead", got)
	}

	if charon.stats.OrphanBeadsRecovered != 0 {
		t.Errorf("OrphanBeadsRecovered = %d, want 0", charon.stats.OrphanBeadsRecovered)
	}
	if n := exec.CountCalls("br update " + beadID + " --status open"); n != 0 {
		t.Errorf("expected zero UpdateBeadStatus(open) calls for terminal bead, got %d; calls:\n%v", n, exec.Calls())
	}
	if n := exec.CountCalls("br close"); n != 0 {
		t.Errorf("expected zero CloseBead calls, got %d; calls:\n%v", n, exec.Calls())
	}
	if n := exec.CountCalls("br show"); n != 1 {
		t.Errorf("expected one GetBead call, got %d; calls:\n%v", n, exec.Calls())
	}
	if _, ok := charon.workerTracker.GetWorkerByBead(beadID); ok {
		t.Errorf("expected worker row for terminal bead to be reaped, but it still exists")
	}
	if !strings.Contains(buf.String(), "already terminal") {
		t.Errorf("expected log to contain 'already terminal', got:\n%s", buf.String())
	}
	if strings.Contains(buf.String(), "reopened orphan bead") {
		t.Errorf("expected no 'reopened orphan bead' log, got:\n%s", buf.String())
	}
}

func TestRecoverOrphanInProgressBeads_ReopensOldOrphan(t *testing.T) {
	beadID := "test-orphan-old"
	charon, exec := setupOrphanCharon(t, beadListJSON(beadID, time.Now().Add(-10*time.Minute)), 5*time.Minute)

	got := charon.recoverOrphanInProgressBeads()
	if got != 1 {
		t.Fatalf("recovered = %d, want 1", got)
	}

	if charon.stats.OrphanBeadsRecovered != 1 {
		t.Errorf("OrphanBeadsRecovered = %d, want 1", charon.stats.OrphanBeadsRecovered)
	}

	if n := exec.CountCalls("br update " + beadID + " --status open"); n != 1 {
		t.Errorf("expected one UpdateBeadStatus(open) call, got %d; calls:\n%v", n, exec.Calls())
	}
	if n := exec.CountCalls("br close"); n != 0 {
		t.Errorf("expected zero CloseBead calls, got %d; calls:\n%v", n, exec.Calls())
	}
}

func TestRecoverOrphanInProgressBeads_RespectsGrace(t *testing.T) {
	beadID := "test-orphan-young"
	charon, exec := setupOrphanCharon(t, beadListJSON(beadID, time.Now().Add(-1*time.Minute)), 5*time.Minute)

	got := charon.recoverOrphanInProgressBeads()
	if got != 0 {
		t.Fatalf("recovered = %d, want 0", got)
	}

	if n := exec.CountCalls("br update " + beadID + " --status open"); n != 0 {
		t.Errorf("expected zero UpdateBeadStatus calls for young bead, got %d; calls:\n%v", n, exec.Calls())
	}
	if n := exec.CountCalls("br close"); n != 0 {
		t.Errorf("expected zero CloseBead calls, got %d; calls:\n%v", n, exec.Calls())
	}
}

func TestRecoverOrphanInProgressBeads_SkipsZeroUpdatedAt(t *testing.T) {
	beadID := "test-orphan-no-time"
	charon, exec := setupOrphanCharon(t, beadListJSON(beadID, time.Time{}), 5*time.Minute)

	got := charon.recoverOrphanInProgressBeads()
	if got != 0 {
		t.Fatalf("recovered = %d, want 0", got)
	}

	if n := exec.CountCalls("br update " + beadID + " --status open"); n != 0 {
		t.Errorf("expected zero UpdateBeadStatus calls for zero UpdatedAt, got %d; calls:\n%v", n, exec.Calls())
	}
	if n := exec.CountCalls("br close"); n != 0 {
		t.Errorf("expected zero CloseBead calls, got %d; calls:\n%v", n, exec.Calls())
	}
}

func TestRecoverOrphanInProgressBeads_SkipsLiveWorker(t *testing.T) {
	beadID := "test-orphan-live"
	charon, exec := setupOrphanCharon(t, beadListJSON(beadID, time.Now().Add(-10*time.Minute)), 5*time.Minute)

	if err := charon.workerTracker.TrackWorkerWithPGID(12345, 12345, beadID, t.TempDir()); err != nil {
		t.Fatalf("TrackWorkerWithPGID: %v", err)
	}

	got := charon.recoverOrphanInProgressBeads()
	if got != 0 {
		t.Fatalf("recovered = %d, want 0", got)
	}

	if n := exec.CountCalls("br update " + beadID + " --status open"); n != 0 {
		t.Errorf("expected zero UpdateBeadStatus calls when worker is live, got %d; calls:\n%v", n, exec.Calls())
	}
	if n := exec.CountCalls("br close"); n != 0 {
		t.Errorf("expected zero CloseBead calls, got %d; calls:\n%v", n, exec.Calls())
	}
}

func TestRecoverOrphanInProgressBeads_DisabledWhenGraceZero(t *testing.T) {
	beadID := "test-orphan-disabled"
	charon, exec := setupOrphanCharon(t, beadListJSON(beadID, time.Now().Add(-10*time.Minute)), 0)

	got := charon.recoverOrphanInProgressBeads()
	if got != 0 {
		t.Fatalf("recovered = %d, want 0", got)
	}

	if n := exec.CountCalls("br update " + beadID + " --status open"); n != 0 {
		t.Errorf("expected zero UpdateBeadStatus calls when grace is zero, got %d; calls:\n%v", n, exec.Calls())
	}
	if n := exec.CountCalls("br close"); n != 0 {
		t.Errorf("expected zero CloseBead calls, got %d; calls:\n%v", n, exec.Calls())
	}
}

func TestPerformCleanup_RecoversOrphanWithZeroTrackedWorkers(t *testing.T) {
	beadID := "test-orphan-cleanup"
	charon, exec := setupOrphanCharon(t, beadListJSON(beadID, time.Now().Add(-10*time.Minute)), 5*time.Minute)

	// Suppress the hourly worktree sweep: performCleanup launches it in a
	// goroutine that outlives the call and keeps writing under the project
	// path, which races t.TempDir()'s cleanup ("directory not empty" — this
	// test failed ~25% of runs before this line). Pretending the sweep just
	// ran keeps the test deterministic without weakening what it asserts.
	charon.lastWorktreeGC = time.Now()

	var buf bytes.Buffer
	oldOutput := log.Writer()
	oldFlags := log.Flags()
	log.SetOutput(&buf)
	log.SetFlags(0)
	defer func() {
		log.SetOutput(oldOutput)
		log.SetFlags(oldFlags)
	}()

	charon.performCleanup()

	if charon.stats.OrphanBeadsRecovered != 1 {
		t.Errorf("OrphanBeadsRecovered = %d, want 1", charon.stats.OrphanBeadsRecovered)
	}
	if n := exec.CountCalls("br update " + beadID + " --status open"); n != 1 {
		t.Errorf("expected one UpdateBeadStatus(open) call from cleanup, got %d; calls:\n%v", n, exec.Calls())
	}
	if n := exec.CountCalls("br close"); n != 0 {
		t.Errorf("expected zero CloseBead calls from cleanup, got %d; calls:\n%v", n, exec.Calls())
	}
	if !strings.Contains(buf.String(), "reopened orphan bead") {
		t.Errorf("expected cleanup log to contain literal 'reopened orphan bead', got:\n%s", buf.String())
	}
}

func TestPerformCleanup_OldOrphanUpdateFailureIsLogged(t *testing.T) {
	beadID := "test-orphan-update-fail"
	charon, exec := setupOrphanCharon(t, beadListJSON(beadID, time.Now().Add(-10*time.Minute)), 5*time.Minute)
	exec.updateErr = fmt.Errorf("br update failed")

	got := charon.recoverOrphanInProgressBeads()
	if got != 0 {
		t.Fatalf("recovered = %d, want 0 when update fails", got)
	}

	if n := exec.CountCalls("br update " + beadID + " --status open"); n != 1 {
		t.Errorf("expected one UpdateBeadStatus attempt, got %d; calls:\n%v", n, exec.Calls())
	}
	if n := exec.CountCalls("br close"); n != 0 {
		t.Errorf("expected zero CloseBead calls, got %d; calls:\n%v", n, exec.Calls())
	}
}

// TestMain isolates this package's tests from the production state directory,
// and ensures the log-capture tests do not leak log output state if they panic.
//
// worktreeRoot() roots worktrees under $HYDRA_HOME, which is exactly right in
// production and catastrophic under test: the daemon sets HYDRA_HOME=/opt/hydra
// and workers inherit it, so when a worker ran this repo's own test suite (hydra
// dogfooding hydra), every test that created a worktree registered it in the
// live state directory. That accumulated 10,251 dead entries under
// /opt/hydra/.hydra-worktrees by 2026-07-31 — none with a .git file, all named
// after Go test functions — and Charon then walked all of them on every orphan
// sweep.
//
// Forcing HYDRA_HOME to a per-run temp directory here fixes it for every test in
// the package, including ones not yet written. Tests that assert on a specific
// HYDRA_HOME still use t.Setenv, which overrides this and restores it after.
func TestMain(m *testing.M) {
	tmp, err := os.MkdirTemp("", "hydra-core-test-home-")
	if err != nil {
		panic("could not create an isolated HYDRA_HOME for tests: " + err.Error())
	}
	if err := os.Setenv("HYDRA_HOME", tmp); err != nil {
		panic("could not isolate HYDRA_HOME: " + err.Error())
	}

	code := m.Run()

	os.RemoveAll(tmp)
	os.Exit(code)
}
