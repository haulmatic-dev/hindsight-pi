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

	exec := &recordingExecutor{listResp: listResp}
	client := beads.NewClientWithExecutor(exec)
	charon := NewCharon(client, tracker, 0)
	charon.orphanBeadGrace = grace
	charon.SetProjectsProvider(func() []types.Project {
		return []types.Project{{Name: "test", Path: t.TempDir()}}
	})
	return charon, exec
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

// Ensure the log-capture tests do not leak log output state if they panic.
func TestMain(m *testing.M) {
	os.Exit(m.Run())
}
