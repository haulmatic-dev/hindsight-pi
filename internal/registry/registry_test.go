package registry

import (
	"os"
	"testing"
	"time"
)

func openTest(t *testing.T) *Store {
	t.Helper()
	st, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func liveRecord(beadID string) WorkerRecord {
	now := time.Now()
	return WorkerRecord{
		BeadID:        beadID,
		PID:           1234,
		PGID:          1234,
		Project:       "test-project",
		StartedAt:     now,
		LastHeartbeat: now,
		State:         StateWorking,
		PiSessionID:   "sess-1",
	}
}

func TestUpsertAndGetWorker(t *testing.T) {
	st := openTest(t)
	rec := liveRecord("bead-1")
	rec.ErrStatus = 403
	rec.ErrType = "permission_error"
	rec.ErrMessage = "concurrent request limit"
	rec.TokensSoFar = 42000

	if err := st.UpsertWorker(rec); err != nil {
		t.Fatalf("UpsertWorker: %v", err)
	}

	got, ok, err := st.GetWorker("bead-1")
	if err != nil || !ok {
		t.Fatalf("GetWorker: ok=%v err=%v", ok, err)
	}
	if got.State != StateWorking || got.ErrStatus != 403 || got.TokensSoFar != 42000 {
		t.Errorf("round-trip mismatch: %+v", got)
	}
	if got.StartedAt.IsZero() {
		t.Errorf("StartedAt lost in round-trip")
	}
}

func TestGetWorkerMissing(t *testing.T) {
	st := openTest(t)
	_, ok, err := st.GetWorker("nope")
	if err != nil || ok {
		t.Fatalf("GetWorker missing: ok=%v err=%v", ok, err)
	}
}

func TestCompletedWorkerNotResurrected(t *testing.T) {
	st := openTest(t)
	if err := st.UpsertWorker(liveRecord("bead-1")); err != nil {
		t.Fatalf("UpsertWorker: %v", err)
	}
	if err := st.MarkCompleted("bead-1", 0); err != nil {
		t.Fatalf("MarkCompleted: %v", err)
	}

	// A late upsert (worker crash report racing the completion) must not
	// bring the worker back to life.
	rec := liveRecord("bead-1")
	rec.State = StateParked
	if err := st.UpsertWorker(rec); err != nil {
		t.Fatalf("UpsertWorker 2: %v", err)
	}
	got, _, _ := st.GetWorker("bead-1")
	if got.State != StateCompleted {
		t.Errorf("completed worker resurrected to %s", got.State)
	}
	if !got.State.Terminal() {
		t.Errorf("completed state should be terminal")
	}
}

func TestHeartbeatAndStateTransitions(t *testing.T) {
	st := openTest(t)
	if err := st.UpsertWorker(liveRecord("bead-1")); err != nil {
		t.Fatalf("UpsertWorker: %v", err)
	}

	if err := st.Heartbeat("bead-1", 99999); err != nil {
		t.Fatalf("Heartbeat: %v", err)
	}
	got, _, _ := st.GetWorker("bead-1")
	if got.TokensSoFar != 99999 {
		t.Errorf("tokens not updated: %d", got.TokensSoFar)
	}

	if err := st.SetState("bead-1", StateParked); err != nil {
		t.Fatalf("SetState parked: %v", err)
	}
	got, _, _ = st.GetWorker("bead-1")
	if got.State != StateParked {
		t.Errorf("state = %s, want parked", got.State)
	}

	// Heartbeat on a completed worker must fail (no phantom liveness).
	if err := st.MarkCompleted("bead-1", 7); err != nil {
		t.Fatalf("MarkCompleted: %v", err)
	}
	if err := st.Heartbeat("bead-1", 1); err == nil {
		t.Errorf("heartbeat on completed worker should error")
	}
}

func TestListWorkersFilters(t *testing.T) {
	st := openTest(t)
	for _, b := range []string{"a", "b", "c"} {
		if err := st.UpsertWorker(liveRecord(b)); err != nil {
			t.Fatalf("UpsertWorker %s: %v", b, err)
		}
	}
	st.SetState("b", StateParked)
	st.MarkCompleted("c", 0)

	all, err := st.ListWorkers()
	if err != nil || len(all) != 2 {
		t.Fatalf("ListWorkers() = %d, err=%v; want 2 live", len(all), err)
	}

	parked, err := st.ListWorkers(StateParked)
	if err != nil || len(parked) != 1 || parked[0].BeadID != "b" {
		t.Fatalf("ListWorkers(parked) = %+v, err=%v; want [b]", parked, err)
	}
}

func TestCommandQueue(t *testing.T) {
	st := openTest(t)
	if err := st.EnqueueCommand("bead-1", CmdSteer, "verdict missing — continue"); err != nil {
		t.Fatalf("EnqueueCommand: %v", err)
	}
	if err := st.EnqueueCommand("bead-1", CmdAbandonWithSummary, "park cap exceeded"); err != nil {
		t.Fatalf("EnqueueCommand 2: %v", err)
	}
	if err := st.EnqueueCommand("bead-2", CmdContinue, ""); err != nil {
		t.Fatalf("EnqueueCommand 3: %v", err)
	}

	pending, err := st.PendingCommands("bead-1")
	if err != nil || len(pending) != 2 {
		t.Fatalf("PendingCommands = %d, err=%v; want 2", len(pending), err)
	}
	if pending[0].Kind != CmdSteer || pending[1].Kind != CmdAbandonWithSummary {
		t.Errorf("order wrong: %+v", pending)
	}
	if !pending[0].Pending() {
		t.Errorf("fresh command should be pending")
	}

	if err := st.ConsumeCommand(pending[0].ID); err != nil {
		t.Fatalf("ConsumeCommand: %v", err)
	}
	rest, _ := st.PendingCommands("bead-1")
	if len(rest) != 1 {
		t.Errorf("after consume: %d pending, want 1", len(rest))
	}

	// Double consume fails.
	if err := st.ConsumeCommand(pending[0].ID); err == nil {
		t.Errorf("double consume should error")
	}
}

func TestNudgeCounter(t *testing.T) {
	st := openTest(t)
	if err := st.UpsertWorker(liveRecord("bead-1")); err != nil {
		t.Fatalf("UpsertWorker: %v", err)
	}
	for i := 0; i < 3; i++ {
		if err := st.IncrementNudges("bead-1"); err != nil {
			t.Fatalf("IncrementNudges: %v", err)
		}
	}
	got, _, _ := st.GetWorker("bead-1")
	if got.NudgeCount != 3 {
		t.Errorf("NudgeCount = %d, want 3", got.NudgeCount)
	}
}

func TestParkFieldsRoundTrip(t *testing.T) {
	st := openTest(t)
	rec := liveRecord("bead-1")
	rec.State = StateParked
	rec.ParkSince = time.Now().Add(-5 * time.Minute)
	rec.ParkWakeAt = time.Now().Add(2 * time.Minute)
	rec.ParkReason = "provider-capacity: 403 concurrent request limit"
	if err := st.UpsertWorker(rec); err != nil {
		t.Fatalf("UpsertWorker: %v", err)
	}
	got, _, _ := st.GetWorker("bead-1")
	if got.ParkSince.IsZero() || got.ParkWakeAt.IsZero() {
		t.Errorf("park times lost: %+v", got)
	}
	if got.ParkReason != rec.ParkReason {
		t.Errorf("park reason = %q", got.ParkReason)
	}
}

func staleRecord(beadID string, heartbeatAge time.Duration) WorkerRecord {
	now := time.Now()
	return WorkerRecord{
		BeadID:        beadID,
		PID:           1234,
		PGID:          1234,
		Project:       "test-project",
		StartedAt:     now.Add(-heartbeatAge),
		LastHeartbeat: now.Add(-heartbeatAge),
		State:         StateWorking,
		PiSessionID:   "sess-1",
	}
}

func TestReapStale_RemovesTerminalAndStaleButNotFresh(t *testing.T) {
	st := openTest(t)

	// Terminal: should be reaped regardless of heartbeat age.
	term := liveRecord("bead-terminal")
	term.State = StateCompleted

	// Stale working: heartbeat older than cutoff.
	stale := staleRecord("bead-stale", 2*time.Hour)

	// Fresh working: heartbeat newer than cutoff.
	fresh := liveRecord("bead-fresh")

	for _, rec := range []WorkerRecord{term, stale, fresh} {
		if err := st.UpsertWorker(rec); err != nil {
			t.Fatalf("UpsertWorker %s: %v", rec.BeadID, err)
		}
	}

	// Queue a command for the stale worker; it should be cleaned up too.
	if err := st.EnqueueCommand("bead-stale", CmdSteer, "go faster"); err != nil {
		t.Fatalf("EnqueueCommand: %v", err)
	}

	cutoff := time.Now().Add(-30 * time.Minute)
	n, err := st.ReapStale(cutoff)
	if err != nil {
		t.Fatalf("ReapStale: %v", err)
	}
	if n != 2 {
		t.Errorf("expected 2 reaped, got %d", n)
	}

	for _, id := range []string{"bead-terminal", "bead-stale"} {
		_, ok, _ := st.GetWorker(id)
		if ok {
			t.Errorf("%s should have been reaped", id)
		}
	}
	_, ok, _ := st.GetWorker("bead-fresh")
	if !ok {
		t.Errorf("bead-fresh should survive reap")
	}

	cmds, err := st.PendingCommands("bead-stale")
	if err != nil {
		t.Fatalf("PendingCommands: %v", err)
	}
	if len(cmds) != 0 {
		t.Errorf("commands for reaped worker should be removed, got %d", len(cmds))
	}
}

func TestReapStale_NoCandidates(t *testing.T) {
	st := openTest(t)
	fresh := liveRecord("bead-fresh")
	if err := st.UpsertWorker(fresh); err != nil {
		t.Fatalf("UpsertWorker: %v", err)
	}
	n, err := st.ReapStale(time.Now().Add(-30 * time.Minute))
	if err != nil {
		t.Fatalf("ReapStale: %v", err)
	}
	if n != 0 {
		t.Errorf("expected 0 reaped, got %d", n)
	}
}

// Daemon restart leaves non-terminal worker rows behind. Rows pointing at
// dead PIDs must be marked completed so the bead can respawn; live rows
// must be left untouched.
func TestReconcileDeadPIDs_MarksDeadAndPreservesLive(t *testing.T) {
	st := openTest(t)

	dead := liveRecord("bead-dead")
	dead.PID = 999999 // non-existent process

	live := liveRecord("bead-live")
	live.PID = os.Getpid()
	live.State = StateParked

	done := liveRecord("bead-done")
	done.State = StateCompleted

	for _, rec := range []WorkerRecord{dead, live, done} {
		if err := st.UpsertWorker(rec); err != nil {
			t.Fatalf("UpsertWorker %s: %v", rec.BeadID, err)
		}
	}

	if err := st.EnqueueCommand("bead-dead", CmdContinue, ""); err != nil {
		t.Fatalf("EnqueueCommand: %v", err)
	}

	n, ids, err := st.ReconcileDeadPIDs()
	if err != nil {
		t.Fatalf("ReconcileDeadPIDs: %v", err)
	}
	if n != 1 || len(ids) != 1 || ids[0] != "bead-dead" {
		t.Fatalf("expected 1 dead row (bead-dead), got n=%d ids=%v", n, ids)
	}

	deadRec, ok, _ := st.GetWorker("bead-dead")
	if !ok || deadRec.State != StateCompleted || deadRec.ExitCode != -1 {
		t.Errorf("dead row not marked orphan-reaped: state=%s exit=%d", deadRec.State, deadRec.ExitCode)
	}
	cmds, _ := st.PendingCommands("bead-dead")
	if len(cmds) != 0 {
		t.Errorf("pending commands for dead worker should be dropped, got %d", len(cmds))
	}

	liveRec, ok, _ := st.GetWorker("bead-live")
	if !ok || liveRec.State != StateParked {
		t.Errorf("live parked row was touched: state=%s", liveRec.State)
	}

	doneRec, ok, _ := st.GetWorker("bead-done")
	if !ok || doneRec.State != StateCompleted {
		t.Errorf("completed row disappeared or changed: state=%s", doneRec.State)
	}
}

func TestReapGhosts(t *testing.T) {
	st, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer st.Close()

	now := time.Now()
	mk := func(id string, state WorkerState, hbAge time.Duration) WorkerRecord {
		return WorkerRecord{BeadID: id, PID: 1, State: state,
			StartedAt: now.Add(-4 * time.Hour), LastHeartbeat: now.Add(-hbAge)}
	}
	for _, rec := range []WorkerRecord{
		mk("g-working", StateWorking, 3*time.Hour),
		mk("g-parked", StateParked, 3*time.Hour),
		mk("g-hung", StateHung, 3*time.Hour),
		mk("k-fresh", StateWorking, 30*time.Second),
		mk("k-done", StateCompleted, 3*time.Hour),
	} {
		if err := st.UpsertWorker(rec); err != nil {
			t.Fatalf("UpsertWorker %s: %v", rec.BeadID, err)
		}
	}
	if err := st.EnqueueCommand("g-working", CmdSteer, "x"); err != nil {
		t.Fatalf("EnqueueCommand: %v", err)
	}

	n, err := st.ReapGhosts(now.Add(-time.Hour))
	if err != nil {
		t.Fatalf("ReapGhosts: %v", err)
	}
	if n != 3 {
		t.Errorf("reaped %d, want 3 ghosts", n)
	}
	for _, id := range []string{"g-working", "g-parked", "g-hung"} {
		if _, ok, _ := st.GetWorker(id); ok {
			t.Errorf("%s must be reaped", id)
		}
	}
	for _, id := range []string{"k-fresh", "k-done"} {
		if _, ok, _ := st.GetWorker(id); !ok {
			t.Errorf("%s must survive", id)
		}
	}
	cmds, _ := st.PendingCommands("g-working")
	if len(cmds) != 0 {
		t.Errorf("ghost's queued commands must be reaped too, got %d", len(cmds))
	}
}
