package registry

import (
	"testing"
	"time"
)

// ReapStale must persist doomed completed rows' token usage into the durable
// daily table before deleting them — pruning must not erase spend trends.
func TestReapStale_PersistsTokensBeforeDelete(t *testing.T) {
	st := openTest(t)
	day := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	rec := liveRecord("p1-abc")
	rec.StartedAt = day
	rec.State = StateCompleted
	rec.TokensSoFar = 5000
	rec.TokensFresh = 2000
	rec.TokensOutput = 1000
	rec.TokensCacheRead = 1500
	rec.TokensCacheCreation = 500
	if err := st.UpsertWorker(rec); err != nil {
		t.Fatalf("UpsertWorker: %v", err)
	}

	n, err := st.ReapStale(time.Now().UTC().Add(-24 * time.Hour))
	if err != nil || n != 1 {
		t.Fatalf("ReapStale = %d, %v; want 1, nil", n, err)
	}
	row, ok, err := st.GetDailyRollup("2026-10-01", "test-project")
	if err != nil || !ok {
		t.Fatalf("GetDailyRollup ok=%v err=%v; want true, nil", ok, err)
	}
	if row.Completions != 1 || row.TokensFresh != 2000 || row.TokensOutput != 1000 || row.TokensCache != 2000 {
		t.Errorf("row = %+v; want completions=1 fresh=2000 output=1000 cache=2000", row)
	}
	if row.MaxPerBead != 5000 || row.MedianPerBead != 5000 || row.ZeroTokenCount != 0 {
		t.Errorf("max=%v median=%v zero=%v; want 5000/5000/0", row.MaxPerBead, row.MedianPerBead, row.ZeroTokenCount)
	}
	// Workers row really gone.
	if _, ok, _ := st.GetWorker("p1-abc"); ok {
		t.Error("worker row should be deleted")
	}
}

// ReapGhosts persists partial token usage of dead non-terminal workers.
func TestReapGhosts_PersistsPartialTokens(t *testing.T) {
	st := openTest(t)
	rec := liveRecord("p1-ghost")
	rec.LastHeartbeat = time.Now().UTC().Add(-2 * time.Hour)
	rec.TokensSoFar = 300
	if err := st.UpsertWorker(rec); err != nil {
		t.Fatalf("UpsertWorker: %v", err)
	}
	n, err := st.ReapGhosts(time.Now().UTC().Add(-time.Hour))
	if err != nil || n != 1 {
		t.Fatalf("ReapGhosts = %d, %v", n, err)
	}
	day := time.Now().UTC().Format("2006-01-02")
	row, ok, err := st.GetDailyRollup(day, "test-project")
	if err != nil || !ok {
		t.Fatalf("rollup missing after ghost reap: ok=%v err=%v", ok, err)
	}
	if row.TokensFresh != 0 || row.Completions != 0 || row.MaxPerBead != 300 {
		t.Errorf("row = %+v; want completions=0 max=300", row)
	}
}

// SnapshotAndRollup aggregates live rows: completions, token sums, median,
// max and zero-token counts per (day, project).
func TestSnapshotAndRollup_Aggregates(t *testing.T) {
	st := openTest(t)
	for _, tc := range []struct {
		id     string
		state  WorkerState
		soFar  int64
		fresh  int64
		output int64
		cache  int64
	}{
		{"a", StateCompleted, 1000, 400, 300, 300},
		{"b", StateCompleted, 3000, 1000, 1000, 1000},
		{"c", StateCompleted, 0, 0, 0, 0},
		{"d", StateWorking, 500, 500, 0, 0},
	} {
		rec := liveRecord(tc.id)
		rec.State = tc.state
		rec.TokensSoFar = tc.soFar
		rec.TokensFresh = tc.fresh
		rec.TokensOutput = tc.output
		rec.TokensCacheRead = tc.cache
		rec.TokensCacheCreation = 0
		if err := st.UpsertWorker(rec); err != nil {
			t.Fatalf("UpsertWorker %s: %v", tc.id, err)
		}
	}
	rows, err := st.SnapshotAndRollup()
	if err != nil {
		t.Fatalf("SnapshotAndRollup: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	r := rows[0]
	if r.Completions != 3 {
		t.Errorf("completions = %d, want 3", r.Completions)
	}
	if r.TokensFresh != 1900 || r.TokensOutput != 1300 || r.TokensCache != 1300 {
		t.Errorf("fresh=%d output=%d cache=%d; want 1900/1300/1300", r.TokensFresh, r.TokensOutput, r.TokensCache)
	}
	if r.MaxPerBead != 3000 || r.MedianPerBead != 1000 || r.ZeroTokenCount != 1 {
		t.Errorf("max=%v median=%v zero=%v; want 3000/1000/1", r.MaxPerBead, r.MedianPerBead, r.ZeroTokenCount)
	}
}

// A worker re-registered over a completed row is a rerun and must be
// recorded durably in the daily rollup.
func TestUpsertWorker_RerunRecorded(t *testing.T) {
	st := openTest(t)
	rec := liveRecord("p1-abc")
	rec.State = StateCompleted
	rec.TokensSoFar = 100
	if err := st.UpsertWorker(rec); err != nil {
		t.Fatalf("UpsertWorker: %v", err)
	}
	rec2 := liveRecord("p1-abc") // same bead, new PID, working again
	rec2.PID = 9999
	if err := st.UpsertWorker(rec2); err != nil {
		t.Fatalf("UpsertWorker rerun: %v", err)
	}
	row, ok, err := st.GetDailyRollup(time.Now().UTC().Format("2006-01-02"), "test-project")
	if err != nil || !ok {
		t.Fatalf("rollup missing: ok=%v err=%v", ok, err)
	}
	if row.RerunCount != 1 {
		t.Errorf("rerun_count = %d, want 1", row.RerunCount)
	}
}
