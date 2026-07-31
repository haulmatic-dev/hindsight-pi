package core

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/haulmatic-dev/hydra/internal/beads"
	"github.com/haulmatic-dev/hydra/pkg/types"
)

// projectAwareExecutor returns canned br list responses per project directory
// and fails for projects marked broken, mimicking production projects whose
// .beads store is unreadable (br list exits non-zero).
type projectAwareExecutor struct {
	mu           sync.Mutex
	calls        []string
	responses    map[string][]byte
	failProjects map[string]bool
}

func (e *projectAwareExecutor) Execute(name string, arg ...string) ([]byte, error) {
	return e.ExecuteInDir("", name, arg...)
}

func (e *projectAwareExecutor) ExecuteInDir(dir string, name string, arg ...string) ([]byte, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.calls = append(e.calls, strings.Join(append([]string{name}, arg...), " "))

	if name == "br" && len(arg) > 0 && arg[0] == "list" {
		if e.failProjects[dir] {
			return nil, fmt.Errorf("exit status 2")
		}
		if resp, ok := e.responses[dir]; ok {
			return resp, nil
		}
		return []byte(`{"issues":[],"total":0,"limit":500,"offset":0,"has_more":false}`), nil
	}
	return []byte("{}"), nil
}

func (e *projectAwareExecutor) countCalls(substr string) int {
	e.mu.Lock()
	defer e.mu.Unlock()
	n := 0
	for _, c := range e.calls {
		if strings.Contains(c, substr) {
			n++
		}
	}
	return n
}

// setupDeadlockCharon builds a Charon with two projects: one healthy with a
// stuck in_progress bead, one whose br list always fails.
func setupDeadlockCharon(t *testing.T, beadID string, beadAge time.Duration) (*Charon, *projectAwareExecutor, *WorkerTracker, string, string) {
	t.Helper()

	tracker, err := NewWorkerTracker(t.TempDir())
	if err != nil {
		t.Fatalf("NewWorkerTracker: %v", err)
	}

	healthyDir := t.TempDir()
	brokenDir := t.TempDir()

	exec := &projectAwareExecutor{
		responses:    map[string][]byte{healthyDir: beadListJSON(beadID, time.Now().Add(-beadAge))},
		failProjects: map[string]bool{brokenDir: true},
	}
	client := beads.NewClientWithExecutor(exec)
	charon := NewCharon(client, tracker, 0)
	charon.orphanBeadGrace = 5 * time.Minute
	charon.SetProjectsProvider(func() []types.Project {
		return []types.Project{
			{Name: "healthy", Path: healthyDir},
			{Name: "broken", Path: brokenDir},
		}
	})
	return charon, exec, tracker, healthyDir, brokenDir
}

// deadPID returns a PID that does not exist on this machine.
func deadPID(t *testing.T) int {
	t.Helper()
	for pid := 4194000; pid < 4194300; pid++ {
		if _, err := os.Stat(fmt.Sprintf("/proc/%d", pid)); os.IsNotExist(err) {
			return pid
		}
	}
	t.Skip("no free PID slot found")
	return 0
}

// TestPerformCleanup_SweepsPhantomAndRecoversOrphan reproduces the 2026-07-31
// fleet deadlock: a worker died without cleanup leaving a phantom "running"
// record, while another project's br list fails. The dead-PID sweep must run
// even though getOpenBeads fails, and the orphan in_progress bead must be
// reopened in the same pass.
func TestPerformCleanup_SweepsPhantomAndRecoversOrphan(t *testing.T) {
	beadID := "test-stuck-phantom"
	charon, exec, tracker, healthyDir, _ := setupDeadlockCharon(t, beadID, 10*time.Minute)

	pid := deadPID(t)
	if err := tracker.TrackWorkerWithPGID(pid, pid, beadID, healthyDir); err != nil {
		t.Fatalf("TrackWorkerWithPGID: %v", err)
	}

	charon.performCleanup()

	// Phantom record must be marked completed even though the broken project
	// makes getOpenBeads fail.
	if recs := tracker.GetRunningWorkerRecords(); len(recs) != 0 {
		t.Fatalf("phantom record still running after cleanup: %+v", recs)
	}

	// The stuck in_progress bead must be reopened for retry.
	if n := exec.countCalls("br update " + beadID + " --status open"); n != 1 {
		t.Errorf("expected one reopen call, got %d; calls:\n%v", n, exec.calls)
	}
}

// TestPerformCleanup_DoesNotKillLiveWorkerInFailedProject guards the
// fail-closed kill decision: when a project's bead list cannot be read,
// Charon must not conclude a live worker there is an orphan (its bead is
// absent from the open map only because the list failed).
func TestPerformCleanup_DoesNotKillLiveWorkerInFailedProject(t *testing.T) {
	beadID := "test-live-broken-project"
	charon, _, tracker, _, brokenDir := setupDeadlockCharon(t, beadID, 10*time.Minute)

	proc := exec.Command("sleep", "120")
	proc.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := proc.Start(); err != nil {
		t.Fatalf("start sleep: %v", err)
	}
	defer proc.Process.Kill()

	pid := proc.Process.Pid
	if err := tracker.TrackWorkerWithPGID(pid, pid, beadID, brokenDir); err != nil {
		t.Fatalf("TrackWorkerWithPGID: %v", err)
	}

	charon.performCleanup()

	// Zombie-safe: a wrongly-killed child would linger as a zombie, which
	// kill(pid, 0) cannot distinguish from alive.
	if !processAliveNonZombie(pid) {
		t.Errorf("live worker in failed-list project was killed")
	}
	if recs := tracker.GetRunningWorkerRecords(); len(recs) != 1 {
		t.Errorf("live worker record should remain running, got %+v", recs)
	}
}

// TestPerformCleanup_StillKillsOrphanInHealthyProject guards against
// over-correction: a live process whose bead is genuinely closed (healthy
// project, bead absent from the open list) must still be reaped.
func TestPerformCleanup_StillKillsOrphanInHealthyProject(t *testing.T) {
	// Healthy project lists no beads at all, so the tracked bead is "closed".
	charon, _, tracker, healthyDir, _ := setupDeadlockCharon(t, "test-unrelated", 10*time.Minute)

	proc := exec.Command("sleep", "120")
	proc.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := proc.Start(); err != nil {
		t.Fatalf("start sleep: %v", err)
	}
	defer proc.Process.Kill()

	pid := proc.Process.Pid
	if err := tracker.TrackWorkerWithPGID(pid, pid, "test-closed-bead", healthyDir); err != nil {
		t.Fatalf("TrackWorkerWithPGID: %v", err)
	}

	charon.performCleanup()

	// The orphan must be reaped. The graceful-kill path escalates through
	// SIGTERM-wait-SIGKILL with multi-second sleeps, so allow generous time.
	// A killed child lingers as a zombie until Wait(), so probe the /proc
	// state field rather than kill(pid, 0), which succeeds for zombies.
	deadline := time.Now().Add(40 * time.Second)
	for time.Now().Before(deadline) {
		if !processAliveNonZombie(pid) {
			return // reaped: pass
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Errorf("orphan process %d in healthy project was not reaped", pid)
}

// processAliveNonZombie reports whether pid exists and is not a zombie.
func processAliveNonZombie(pid int) bool {
	stat, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return false
	}
	// Field 3 (after the comm-in-parens) is the single-letter state.
	if i := strings.LastIndexByte(string(stat), ')'); i >= 0 && i+2 < len(stat) {
		return stat[i+2] != 'Z' && stat[i+2] != 'X'
	}
	return true
}
