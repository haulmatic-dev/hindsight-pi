package core

import (
	"os/exec"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// The Overseer's per-worker timeout must exclude parked time. The blind 30m
// wall clock killed hydra-code-9p7 on 2026-08-07: 20m burned parked on
// provider congestion, then killed 5m into real work with a tool call 14s
// before death. The worker's internal maxTotalRuntime already excludes parked
// time; the daemon-side clock must too. It must also honor
// config.Overseer.WorkerTimeout rather than a hardcoded 30m.
func TestWaitTimeoutExcludesParkedTime(t *testing.T) {
	var parked atomic.Bool
	parked.Store(true)

	wl := &WorkerLifecycle{
		workerTimeout: 150 * time.Millisecond,
		parkedLookup:  func(string) bool { return parked.Load() },
		// Deps exercised by Wait's post-completion path; no-ops suffice.
		retryManager:    NewRetryManager(time.Second, time.Minute),
		getMaxRetries:   func() int { return 3 },
		triggerSchedule: func() {},
		removeWorker:    func(string) {},
		dag:             NewDAG(),
		dagMu:           &sync.RWMutex{},
		errorLogger:     NewErrorLogger(func(string) int { return 0 }),
	}

	cmd := exec.Command("sleep", "10")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start sleeper: %v", err)
	}
	worker := &types.Worker{ID: "bead-park", PID: cmd.Process.Pid, StartTime: time.Now()}

	type outcome struct {
		c workerCompletion
	}
	resCh := make(chan outcome, 1)
	go func() {
		resCh <- outcome{c: wl.Wait(worker, cmd)}
	}()

	// While parked, the timeout must not fire even after 3× the budget.
	select {
	case <-resCh:
		t.Fatal("Wait killed a parked worker — parked time must not count against the timeout")
	case <-time.After(450 * time.Millisecond):
	}

	// Unpark: the active clock now runs and must kill within ~the budget.
	parked.Store(false)
	select {
	case out := <-resCh:
		if out.c.success || out.c.exitCode == 0 {
			t.Fatalf("expected timeout kill after unpark, got success=%v exit=%d", out.c.success, out.c.exitCode)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Wait did not kill within 3s of unpark (timeout not honored)")
	}
}
