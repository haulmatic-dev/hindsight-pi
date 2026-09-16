package registry

import (
	"testing"
	"time"
)

// 2026-09-16 webapp-zpf husk incident: the reconciler deleted a live worker's
// sandbox because worktree protection depended on in-memory chain
// registration. The registry now carries the worker-reported worktree key so
// protection is derivable from durable state.

func TestWorktreeKey_RoundtripAndPreserve(t *testing.T) {
	st := openTest(t)

	rec := liveRecord("bead-wt")
	rec.WorktreeKey = "webapp-zpf"
	if err := st.UpsertWorker(rec); err != nil {
		t.Fatalf("UpsertWorker: %v", err)
	}

	got, ok, err := st.GetWorker("bead-wt")
	if err != nil || !ok {
		t.Fatalf("GetWorker: ok=%v err=%v", ok, err)
	}
	if got.WorktreeKey != "webapp-zpf" {
		t.Fatalf("WorktreeKey = %q, want webapp-zpf", got.WorktreeKey)
	}

	// A heartbeat that carries no key (older worker binary) must NOT blank
	// the stored key.
	hb := liveRecord("bead-wt")
	hb.LastHeartbeat = time.Now()
	if err := st.UpsertWorker(hb); err != nil {
		t.Fatalf("UpsertWorker heartbeat: %v", err)
	}
	got, _, err = st.GetWorker("bead-wt")
	if err != nil {
		t.Fatalf("GetWorker after heartbeat: %v", err)
	}
	if got.WorktreeKey != "webapp-zpf" {
		t.Fatalf("WorktreeKey blanked by keyless heartbeat: %q", got.WorktreeKey)
	}

	rows, err := st.ListWorkers(StateWorking)
	if err != nil || len(rows) != 1 {
		t.Fatalf("ListWorkers: %v rows=%d", err, len(rows))
	}
	if rows[0].WorktreeKey != "webapp-zpf" {
		t.Fatalf("ListWorkers WorktreeKey = %q", rows[0].WorktreeKey)
	}
}
