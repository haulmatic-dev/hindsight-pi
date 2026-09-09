package core

import (
	"errors"
	"testing"
	"time"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// A close that was rejected or escalated must prune the bead from the
// in-memory completed set at the write site. The entry can predate a reopen;
// left in place it silently filters the bead from scheduling until daemon
// restart (gybo4, 2026-09-09). Companion to BeadDiscovery's filter-time
// revalidation (pruneStaleCompleted / stillCompleted).
func TestHandleCompletion_PrunesCompletedOnCloseError(t *testing.T) {
	completed := map[string]bool{"hydra-x1": true}
	wl := &WorkerLifecycle{
		retryManager:    NewRetryManager(time.Second, time.Minute),
		errorLogger:     NewErrorLogger(func(string) int { return 0 }),
		completed:       completed,
		triggerSchedule: func() {},
		getMaxRetries:   func() int { return 3 },
	}

	wl.HandleCompletion(workerCompletion{
		worker: &types.Worker{ID: "hydra-x1"},
		success: true, // worker itself succeeded; the close was rejected
		closeError: errors.New("verified-close: bead hydra-x1 escalated after 3 rejections (empty-diff)"),
	})

	if completed["hydra-x1"] {
		t.Fatalf("close rejected/escalated but bead still lingers in completed set")
	}
}

// Successful close with no error keeps the completed entry (no regression).
func TestHandleCompletion_KeepsCompletedOnSuccess(t *testing.T) {
	completed := map[string]bool{}
	wl := &WorkerLifecycle{
		retryManager:    NewRetryManager(time.Second, time.Minute),
		errorLogger:     NewErrorLogger(func(string) int { return 0 }),
		completed:       completed,
		triggerSchedule: func() {},
		getMaxRetries:   func() int { return 3 },
	}

	wl.HandleCompletion(workerCompletion{
		worker:  &types.Worker{ID: "hydra-x2"},
		success: true,
	})

	if !completed["hydra-x2"] {
		t.Fatalf("successful close did not mark bead completed")
	}
}
