package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/haulmatic-dev/hydra/internal/registry"
)

// The 00:05 UTC rollup must persist a durable token_usage_daily row and emit
// a typed token_usage_daily event per project.
func TestTokenUsageRollupJob_RunOnce_PersistsAndEmits(t *testing.T) {
	st, err := registry.Open(":memory:")
	if err != nil {
		t.Fatalf("registry.Open: %v", err)
	}
	defer st.Close()
	rec := registry.WorkerRecord{
		BeadID: "p1-abc", PID: 1, PGID: 1, Project: "proj-x",
		StartedAt: time.Now().UTC(), LastHeartbeat: time.Now().UTC(),
		State: registry.StateCompleted, TokensSoFar: 2000, TokensFresh: 800, TokensOutput: 700,
		TokensCacheRead: 300, TokensCacheCreation: 200,
	}
	if err := st.UpsertWorker(rec); err != nil {
		t.Fatalf("UpsertWorker: %v", err)
	}

	dir := t.TempDir()
	sink := NewEventSink(dir)
	job := NewTokenUsageRollupJob(func() *registry.Store { return st }, sink)
	job.runOnce()

	day := time.Now().UTC().Format("2006-01-02")
	row, ok, err := st.GetDailyRollup(day, "proj-x")
	if err != nil || !ok {
		t.Fatalf("durable row missing: ok=%v err=%v", ok, err)
	}
	if row.Completions != 1 || row.TokensFresh != 800 || row.TokensOutput != 700 || row.TokensCache != 500 {
		t.Errorf("row = %+v", row)
	}

	b, err := os.ReadFile(filepath.Join(dir, day+".jsonl"))
	if !strings.Contains(string(b), "token_usage_daily") {
		t.Errorf("event stream missing token_usage_daily:\n%s", b)
	}
}

func TestTokenUsageRollupJob_NilStoreIsNoOp(t *testing.T) {
	sink := NewEventSink(t.TempDir())
	job := NewTokenUsageRollupJob(func() *registry.Store { return nil }, sink)
	job.runOnce() // must not panic
}

func TestNextRollupTime(t *testing.T) {
	cases := []struct {
		now  string
		want string
	}{
		{"2026-10-02T00:04:59Z", "2026-10-02T00:05:00Z"},
		{"2026-10-02T00:05:00Z", "2026-10-03T00:05:00Z"}, // at boundary → next day
		{"2026-10-02T12:00:00Z", "2026-10-03T00:05:00Z"},
	}
	for _, tc := range cases {
		now, _ := time.Parse(time.RFC3339, tc.now)
		got := nextRollupTime(now).Format("2006-01-02T15:04:05Z")
		if got != tc.want {
			t.Errorf("nextRollupTime(%s) = %s, want %s", tc.now, got, tc.want)
		}
	}
}
