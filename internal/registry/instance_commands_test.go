package registry

import (
	"testing"
	"time"
)

// Shepherd commands are judgments against a specific worker INSTANCE. When a
// new worker registers for the same bead (retry after a death), the registry
// row is keyed by bead_id, so commands queued against the dead worker — e.g.
// Pan's abandon-with-summary issued on the old row's stale heartbeat — would
// be drained and obeyed by the innocent new worker. Live incident 2026-08-07
// (srv-smart-engine-pes): a healthy worker 2m20s into its turn drained an
// "abandoned by shepherd" judgment made against its predecessor and
// self-aborted. A PID change on upsert means a new instance: pending commands
// are void.
func TestUpsertWorker_NewInstanceClearsPendingCommands(t *testing.T) {
	st := openTest(t)

	rec := liveRecord("bead-1")
	if err := st.UpsertWorker(rec); err != nil {
		t.Fatalf("UpsertWorker: %v", err)
	}
	if err := st.EnqueueCommand("bead-1", "abandon-with-summary", "judged against old worker"); err != nil {
		t.Fatalf("EnqueueCommand: %v", err)
	}

	// Same instance heartbeats again: commands must survive (park-resume and
	// command drainage depend on this).
	if err := st.UpsertWorker(rec); err != nil {
		t.Fatalf("UpsertWorker heartbeat: %v", err)
	}
	if cmds, _ := st.PendingCommands("bead-1"); len(cmds) != 1 {
		t.Fatalf("same-instance upsert cleared commands; pending = %d, want 1", len(cmds))
	}

	// New instance (different PID): pending commands are void.
	next := liveRecord("bead-1")
	next.PID = 9876
	next.PGID = 9876
	next.StartedAt = time.Now()
	if err := st.UpsertWorker(next); err != nil {
		t.Fatalf("UpsertWorker new instance: %v", err)
	}
	if cmds, _ := st.PendingCommands("bead-1"); len(cmds) != 0 {
		t.Fatalf("new-instance upsert kept stale commands; pending = %d, want 0", len(cmds))
	}
}
