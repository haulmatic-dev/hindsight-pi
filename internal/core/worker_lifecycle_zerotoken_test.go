package core

import (
	"os/exec"
	"sync"
	"testing"
	"time"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// A worker that produced ZERO tokens never attempted the task — no turn ever
// completed. Any exit it makes is by definition infrastructure (spawn env,
// provider capacity in a transport shape we have not classified yet, session
// corruption), never a bead code fault. Burning one of the 3 real retry
// attempts on such a death is what killed hcj3h/nl7r3/iz5z7/ehzq2 on
// 2026-08-30/31: the RPC-turn_end 403 shape slipped past classification, the
// worker exited 1 within a minute of spawn, and three of those manufactured
// faults closed the beads failed. Defense in depth: even when a NEW transport
// shape bypasses worker-side classification, the accounting layer must not
// count a zero-token death against the bead.
func newZeroTokenLifecycle() (*WorkerLifecycle, *RetryManager) {
	rm := NewRetryManager(time.Millisecond, 10*time.Millisecond)
	wl := &WorkerLifecycle{
		retryManager:    rm,
		getMaxRetries:   func() int { return 3 },
		triggerSchedule: func() {},
		removeWorker:    func(string) {},
		dag:             NewDAG(),
		dagMu:           &sync.RWMutex{},
		errorLogger:     NewErrorLogger(func(string) int { return 0 }),
	}
	return wl, rm
}

func TestHandleFailure_ZeroTokenExitIsInfra(t *testing.T) {
	wl, rm := newZeroTokenLifecycle()
	wl.tokensLookup = func(string) int64 { return 0 }

	worker := &types.Worker{ID: "bead-zero", BeadID: "bead-zero", StartTime: time.Now()}
	wl.HandleFailure(worker, 1, time.Minute)

	if got := rm.GetRetryCount("bead-zero"); got != 0 {
		t.Fatalf("zero-token exit 1 counted against fault budget: retries=%d, want 0 (infra must not burn attempts)", got)
	}
	if got := rm.GetInfraRetryCount("bead-zero"); got != 1 {
		t.Fatalf("zero-token exit 1 not tracked as infra: infra=%d, want 1", got)
	}
}

func TestHandleFailure_NonZeroTokenExitCounts(t *testing.T) {
	wl, rm := newZeroTokenLifecycle()
	wl.tokensLookup = func(string) int64 { return 42_000 }

	worker := &types.Worker{ID: "bead-real", BeadID: "bead-real", StartTime: time.Now()}
	wl.HandleFailure(worker, 1, 10*time.Minute)

	if got := rm.GetRetryCount("bead-real"); got != 1 {
		t.Fatalf("real task failure (42k tokens burned) must count: retries=%d, want 1", got)
	}
}

func TestHandleFailure_NilTokensLookupKeepsOldBehavior(t *testing.T) {
	wl, rm := newZeroTokenLifecycle() // tokensLookup deliberately nil

	worker := &types.Worker{ID: "bead-nil", BeadID: "bead-nil", StartTime: time.Now()}
	wl.HandleFailure(worker, 1, time.Minute)

	if got := rm.GetRetryCount("bead-nil"); got != 1 {
		t.Fatalf("nil lookup must preserve pre-change behavior: retries=%d, want 1", got)
	}
}

// Known infra exit codes must stay infra regardless of the lookup.
func TestHandleFailure_InfraExitCodeUnchanged(t *testing.T) {
	wl, rm := newZeroTokenLifecycle()
	wl.tokensLookup = func(string) int64 { return 999_999 } // even WITH tokens

	worker := &types.Worker{ID: "bead-e7", BeadID: "bead-e7", StartTime: time.Now()}
	wl.HandleFailure(worker, 7, 30*time.Second)

	if got := rm.GetRetryCount("bead-e7"); got != 0 {
		t.Fatalf("exit 7 must remain infra: retries=%d, want 0", got)
	}
}

// A zero-token death reaching the close path must be protected by the same
// invariant as the accounting layer (#194): HandleFailure already records it
// as infra, but Wait's close guard consulted only the exit code, so a
// pi-spawn failure (fork/exec ENOENT burst, 2026-09-01: 52q9u/u93jp closed
// failed at 0/3 retries) still closed beads failed. The guard must ask the
// same question.
func TestWait_ZeroTokenFailureLeavesBeadOpen(t *testing.T) {
	rm := NewRetryManager(time.Millisecond, 10*time.Millisecond)
	closed := false
	wl := &WorkerLifecycle{
		retryManager:        rm,
		getMaxRetries:       func() int { return 3 },
		triggerSchedule:     func() {},
		removeWorker:        func(string) {},
		closeBead:           func(*types.Worker, bool, int) error { closed = true; return nil },
		tokensLookup:        func(string) int64 { return 0 },
		dag:                 NewDAG(),
		dagMu:               &sync.RWMutex{},
		errorLogger:         NewErrorLogger(func(string) int { return 0 }),
		concurrencyGovernor: NewConcurrencyGovernor(2, GovernorConfig{}),
		workerTimeout:       30 * time.Second,
	}

	cmd := exec.Command("false") // exits 1 immediately — the pi-spawn-failure shape
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	worker := &types.Worker{ID: "bead-spawnfail", BeadID: "bead-spawnfail", PID: cmd.Process.Pid, StartTime: time.Now()}
	wl.Wait(worker, cmd)

	if closed {
		t.Fatal("zero-token exit-1 closed the bead despite retries remaining — infra deaths must leave the bead open")
	}
	if got := rm.GetRetryCount("bead-spawnfail"); got != 0 {
		t.Fatalf("fault budget burned: retries=%d, want 0", got)
	}
}

// A REAL task failure (tokens burned) must still reach closeBead — the guard
// must not become a blanket amnesty.
func TestWait_RealFailureStillCloses(t *testing.T) {
	rm := NewRetryManager(time.Millisecond, 10*time.Millisecond)
	closed := false
	wl := &WorkerLifecycle{
		retryManager:        rm,
		getMaxRetries:       func() int { return 3 },
		triggerSchedule:     func() {},
		removeWorker:        func(string) {},
		closeBead:           func(*types.Worker, bool, int) error { closed = true; return nil },
		tokensLookup:        func(string) int64 { return 50_000 },
		dag:                 NewDAG(),
		dagMu:               &sync.RWMutex{},
		errorLogger:         NewErrorLogger(func(string) int { return 0 }),
		concurrencyGovernor: NewConcurrencyGovernor(2, GovernorConfig{}),
		workerTimeout:       30 * time.Second,
	}

	cmd := exec.Command("false")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	worker := &types.Worker{ID: "bead-realfail", BeadID: "bead-realfail", PID: cmd.Process.Pid, StartTime: time.Now()}
	wl.Wait(worker, cmd)

	if !closed {
		t.Fatal("real task failure with 50k tokens burned must reach closeBead")
	}
}
