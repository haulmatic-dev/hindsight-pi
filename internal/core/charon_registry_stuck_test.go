package core

import (
	"testing"
	"time"

	"github.com/haulmatic-dev/hydra/internal/registry"
)

// registryCharon builds a Charon with an in-memory registry and a 15m legacy
// timeout. bead records are StartedAt 20m ago — past the legacy kill line.
func registryCharon(t *testing.T) (*Charon, *registry.Store) {
	t.Helper()
	st, err := registry.Open(":memory:")
	if err != nil {
		t.Fatalf("registry.Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	c := NewCharon(nil, nil, 15*time.Minute)
	c.SetRegistry(st)
	return c, st
}

func pastTimeoutRecord(beadID string) *WorkerRecord {
	return &WorkerRecord{PID: 1, PGID: 1, BeadID: beadID, StartedAt: time.Now().Add(-20 * time.Minute)}
}

func upsertLive(t *testing.T, st *registry.Store, beadID string, state registry.WorkerState, heartbeatAge time.Duration) {
	t.Helper()
	rec := registry.WorkerRecord{
		BeadID:        beadID,
		PID:           1,
		State:         state,
		StartedAt:     time.Now().Add(-20 * time.Minute),
		LastHeartbeat: time.Now().Add(-heartbeatAge),
	}
	if err := st.UpsertWorker(rec); err != nil {
		t.Fatalf("UpsertWorker: %v", err)
	}
}

func TestStuckDecision_ParkedFreshHeartbeatExempt(t *testing.T) {
	c, st := registryCharon(t)
	upsertLive(t, st, "b1", registry.StateParked, 30*time.Second)

	kill, reason := c.shouldKillAsStuck(pastTimeoutRecord("b1"), time.Now())
	if kill {
		t.Errorf("parked worker with fresh heartbeat must not be killed (reason: %s)", reason)
	}
}

func TestStuckDecision_WorkingFreshHeartbeatExempt(t *testing.T) {
	c, st := registryCharon(t)
	upsertLive(t, st, "b1", registry.StateWorking, 45*time.Second)

	kill, _ := c.shouldKillAsStuck(pastTimeoutRecord("b1"), time.Now())
	if kill {
		t.Errorf("working worker with fresh heartbeat must not be killed — liveness is the heartbeat, not the wall clock")
	}
}

func TestStuckDecision_StaleHeartbeatKills(t *testing.T) {
	c, st := registryCharon(t)
	upsertLive(t, st, "b1", registry.StateWorking, 10*time.Minute)

	kill, reason := c.shouldKillAsStuck(pastTimeoutRecord("b1"), time.Now())
	if !kill {
		t.Errorf("stale heartbeat = hung = kill")
	}
	if reason == "" {
		t.Errorf("kill decision must carry a reason")
	}
}

func TestStuckDecision_ParkedStaleHeartbeatKills(t *testing.T) {
	c, st := registryCharon(t)
	upsertLive(t, st, "b1", registry.StateParked, 10*time.Minute)

	kill, _ := c.shouldKillAsStuck(pastTimeoutRecord("b1"), time.Now())
	if !kill {
		t.Errorf("abandoned park (stale heartbeat) must be killed")
	}
}

func TestStuckDecision_NoRegistryRowFallsBackToLegacy(t *testing.T) {
	c, _ := registryCharon(t)

	kill, _ := c.shouldKillAsStuck(pastTimeoutRecord("b-absent"), time.Now())
	if !kill {
		t.Errorf("no registry row + past legacy timeout = legacy kill")
	}

	young := &WorkerRecord{PID: 1, BeadID: "b-young", StartedAt: time.Now().Add(-5 * time.Minute)}
	kill, _ = c.shouldKillAsStuck(young, time.Now())
	if kill {
		t.Errorf("no registry row + inside legacy timeout = no kill")
	}
}

func TestStuckDecision_NilRegistryIsLegacy(t *testing.T) {
	c := NewCharon(nil, nil, 15*time.Minute)
	kill, _ := c.shouldKillAsStuck(pastTimeoutRecord("b1"), time.Now())
	if !kill {
		t.Errorf("nil registry = legacy wall-clock behavior")
	}
}
