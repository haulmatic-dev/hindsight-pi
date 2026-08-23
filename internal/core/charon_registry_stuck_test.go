package core

import (
	"strings"
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

// Budget-as-signal (2026-08-22 correction of option D): a hard kill at the
// ceiling would have killed ux5 (9M) and acx (11.7M) — beads that LANDED —
// and did kill cc0d6 minutes after Pan judged it 'continue'. Soft cap
// (max_bead_tokens) steers the worker to converge and surfaces it to Pan;
// only the hard backstop (2x) kills, and a fresh Pan continue verdict stays
// it. Parked workers burn nothing — never budget-killed.
func TestCharon_TokenBudgetSoftCapSteersNotKills(t *testing.T) {
	c, st := registryCharon(t)
	c.SetMaxBeadTokens(3_000_000)
	rec := pastTimeoutRecord("proj-burn1")

	upsertLive(t, st, "proj-burn1", registry.StateWorking, 0)
	if err := st.Heartbeat("proj-burn1", 5_000_000); err != nil {
		t.Fatalf("Heartbeat: %v", err)
	}
	// between soft (3M) and hard (6M): must NOT kill
	if kill, _ := c.shouldKillAsStuck(rec, time.Now()); kill {
		t.Fatal("soft-cap crossing must not kill — converging work dies (ux5/acx lesson)")
	}
	// must enqueue a converge steer
	c.maybeBudgetSteer(rec)
	pending, err := st.PendingCommands("proj-burn1")
	if err != nil || len(pending) == 0 {
		t.Fatalf("soft cap must enqueue a steer command, pending=%v err=%v", pending, err)
	}
	if pending[0].Kind != registry.CmdSteer {
		t.Errorf("kind = %s, want steer", pending[0].Kind)
	}
}

func TestCharon_TokenBudgetHardBackstopKills(t *testing.T) {
	c, st := registryCharon(t)
	c.SetMaxBeadTokens(3_000_000) // hard = 6M
	rec := pastTimeoutRecord("proj-burn9")
	upsertLive(t, st, "proj-burn9", registry.StateWorking, 0)
	if err := st.Heartbeat("proj-burn9", 9_000_000); err != nil {
		t.Fatalf("Heartbeat: %v", err)
	}
	kill, reason := c.shouldKillAsStuck(rec, time.Now())
	if !kill {
		t.Fatal("worker past the hard backstop (2x) must be killed")
	}
	if !strings.Contains(reason, "token budget") {
		t.Errorf("reason %q must name the token budget", reason)
	}
}

func TestCharon_TokenBudgetPanContinueStaysKill(t *testing.T) {
	c, st := registryCharon(t)
	c.SetMaxBeadTokens(3_000_000)
	c.SetPanArbiter(func(beadID string) bool { return beadID == "proj-conv1" })
	rec := pastTimeoutRecord("proj-conv1")
	upsertLive(t, st, "proj-conv1", registry.StateWorking, 0)
	if err := st.Heartbeat("proj-conv1", 9_000_000); err != nil {
		t.Fatalf("Heartbeat: %v", err)
	}
	if kill, _ := c.shouldKillAsStuck(rec, time.Now()); kill {
		t.Fatal("fresh Pan continue verdict must stay the backstop kill")
	}
}

func TestCharon_TokenBudgetSparesParkedAndDisabled(t *testing.T) {
	c, st := registryCharon(t)
	c.SetMaxBeadTokens(3_000_000)
	rec := pastTimeoutRecord("proj-park1")
	upsertLive(t, st, "proj-park1", registry.StateParked, 0)
	if err := st.Heartbeat("proj-park1", 9_000_000); err != nil {
		t.Fatalf("AddTokens: %v", err)
	}
	if kill, _ := c.shouldKillAsStuck(rec, time.Now()); kill {
		t.Error("parked worker must never be budget-killed (it burns nothing)")
	}

	c2, st2 := registryCharon(t) // budget disabled (0)
	rec2 := pastTimeoutRecord("proj-burn2")
	upsertLive(t, st2, "proj-burn2", registry.StateWorking, 0)
	if err := st2.Heartbeat("proj-burn2", 50_000_000); err != nil {
		t.Fatalf("AddTokens: %v", err)
	}
	if kill, _ := c2.shouldKillAsStuck(rec2, time.Now()); kill {
		t.Error("budget 0 (disabled) must never kill")
	}
}
