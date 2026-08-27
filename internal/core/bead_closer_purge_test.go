package core

import (
	"sync"
	"testing"
	"time"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

func TestBeadCloser_PurgesRetryAndSpawnStateOnVerifiedClose(t *testing.T) {
	mock := newMockBeadsClient()
	mock.addBead(types.Bead{
		ID:     types.BeadID("demo/bead-1"),
		Status: types.StatusOpen,
		Type:   "task",
	})

	resolver := setupTestProjectResolver(t, "demo", t.TempDir())
	rm := NewRetryManager(1*time.Second, 30*time.Second)
	spawner := &WorkerSpawner{
		activeWorkers: make(map[string]*types.Worker),
		workerMutex:   &sync.RWMutex{},
		spawnFailures: make(map[string]*spawnFailureInfo),
	}

	closer := NewBeadCloser(mock, NewIntelligenceHub(IntelligenceConfig{}), resolver, nil, &Config{})
	closer.SetRetryManager(rm)
	closer.SetWorkerSpawner(spawner)
	closer.SetEventSink(NewEventSink(t.TempDir()))

	rm.RecordRetry("demo/bead-1", errExample)
	rm.IncrementValidationRetry("demo/bead-1")
	spawner.spawnFailures["demo/bead-1"] = &spawnFailureInfo{err: "spawn failed", timestamp: time.Now()}

	worker := &types.Worker{BeadID: types.BeadID("demo/bead-1")}
	if err := closer.Close(worker, true, 0); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}

	if _, exists := rm.retries["demo/bead-1"]; exists {
		t.Errorf("retry state for demo/bead-1 survived close")
	}
	if c := rm.GetValidationRetryCount("demo/bead-1"); c != 0 {
		t.Errorf("validation retry count = %d, want 0", c)
	}
	if _, _, ok := spawner.LastSpawnFailure("demo/bead-1"); ok {
		t.Errorf("spawn failure for demo/bead-1 survived close")
	}
}

func TestBeadCloser_DoesNotPurgeStateOnFailedClose(t *testing.T) {
	mock := newMockBeadsClient()
	mock.addBead(types.Bead{
		ID:     types.BeadID("demo/bead-1"),
		Status: types.StatusOpen,
		Type:   "task",
	})

	resolver := setupTestProjectResolver(t, "demo", t.TempDir())
	rm := NewRetryManager(1*time.Second, 30*time.Second)
	spawner := &WorkerSpawner{
		activeWorkers: make(map[string]*types.Worker),
		workerMutex:   &sync.RWMutex{},
		spawnFailures: make(map[string]*spawnFailureInfo),
	}

	closer := NewBeadCloser(mock, NewIntelligenceHub(IntelligenceConfig{}), resolver, nil, &Config{})
	closer.SetRetryManager(rm)
	closer.SetWorkerSpawner(spawner)
	closer.SetEventSink(NewEventSink(t.TempDir()))

	rm.RecordRetry("demo/bead-1", errExample)
	spawner.spawnFailures["demo/bead-1"] = &spawnFailureInfo{err: "spawn failed", timestamp: time.Now()}

	worker := &types.Worker{BeadID: types.BeadID("demo/bead-1")}
	if err := closer.Close(worker, false, 1); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}

	if _, exists := rm.retries["demo/bead-1"]; !exists {
		t.Errorf("retry state for failed close was purged; should survive")
	}
	if _, _, ok := spawner.LastSpawnFailure("demo/bead-1"); !ok {
		t.Errorf("spawn failure for failed close was purged; should survive")
	}
}

var errExample = &customError{msg: "example"}

type customError struct{ msg string }

func (e *customError) Error() string { return e.msg }
