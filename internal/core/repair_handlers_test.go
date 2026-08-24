package core

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

func TestRetryManager_ResetRetryCount(t *testing.T) {
	rm := NewRetryManager(10*time.Second, 5*time.Minute)

	// Record a genuine fault.
	rm.RecordRetry("bead-1", fmt.Errorf("worker failed"))
	if got := rm.GetRetryCount("bead-1"); got != 1 {
		t.Fatalf("retry count before reset = %d, want 1", got)
	}

	prev := rm.ResetRetryCount("bead-1")
	if prev != 1 {
		t.Fatalf("ResetRetryCount previous = %d, want 1", prev)
	}
	if got := rm.GetRetryCount("bead-1"); got != 0 {
		t.Fatalf("retry count after reset = %d, want 0", got)
	}

	// Reset for an unknown bead is a no-op that returns 0.
	if got := rm.ResetRetryCount("bead-unknown"); got != 0 {
		t.Fatalf("ResetRetryCount unknown = %d, want 0", got)
	}
}

func TestWorkerSpawner_ClearSpawningPlaceholder(t *testing.T) {
	mu := &sync.RWMutex{}
	active := make(map[string]*types.Worker)
	active["bead-spawning"] = &types.Worker{ID: "bead-spawning", Status: "spawning"}
	active["bead-working"] = &types.Worker{ID: "bead-working", Status: "working"}

	s := NewWorkerSpawner(
		active,
		mu,
		nil, // workerTracker
		nil, // spawnRateLimiter
		nil, // errorLogger
		nil, // retryManager
		&Config{},
		nil, // beadsClient
		nil, // beadAnalyzer
		nil, // workerEnvBuilder
		nil, // workerLifecycle
		nil, // userContexts
	)

	if !s.ClearSpawningPlaceholder("bead-spawning") {
		t.Fatal("ClearSpawningPlaceholder returned false for a spawning placeholder")
	}
	if _, ok := active["bead-spawning"]; ok {
		t.Fatal("spawning placeholder was not removed")
	}

	if s.ClearSpawningPlaceholder("bead-working") {
		t.Fatal("ClearSpawningPlaceholder returned true for a non-spawning worker")
	}
	if _, ok := active["bead-working"]; !ok {
		t.Fatal("non-spawning worker was removed")
	}

	if s.ClearSpawningPlaceholder("bead-unknown") {
		t.Fatal("ClearSpawningPlaceholder returned true for an unknown bead")
	}
}
