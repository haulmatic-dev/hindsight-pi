package core

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func testSink(t *testing.T) (*EventSink, string) {
	t.Helper()
	dir := t.TempDir()
	s := NewEventSink(dir)
	// Deterministic clock so day-file naming and Time stamps are assertable.
	fixed := time.Date(2026, 7, 29, 10, 30, 0, 0, time.UTC)
	s.now = func() time.Time { return fixed }
	return s, dir
}

func TestEmit_WritesTypedJSONLToDayFile(t *testing.T) {
	s, dir := testSink(t)
	s.Emit(Event{Type: "bead_closed", Project: "p1", Bead: "p1-abc", Reason: "failed",
		Fields: map[string]any{"exit_code": 7}})

	events, err := ReadEvents(filepath.Join(dir, "2026-07-29.jsonl"))
	if err != nil || len(events) != 1 {
		t.Fatalf("ReadEvents: %v, n=%d, want 1", err, len(events))
	}
	ev := events[0]
	if ev.Type != "bead_closed" || ev.Bead != "p1-abc" || ev.Reason != "failed" {
		t.Errorf("round-trip mismatch: %+v", ev)
	}
	if !strings.HasPrefix(ev.Time, "2026-07-29T10:30:00") {
		t.Errorf("Time = %q, want stamped from the sink clock", ev.Time)
	}
	if ev.Fields["exit_code"] != float64(7) {
		t.Errorf("Fields[exit_code] = %v, want 7", ev.Fields["exit_code"])
	}
}

// TestEmitRepo_MirrorsIntoRepoAndGitignores pins the two-tier design: the repo
// copy is the user's window into what hydra did to their repo; it must exist,
// match the central record, and never become committable.
func TestEmitRepo_MirrorsIntoRepoAndGitignores(t *testing.T) {
	s, dir := testSink(t)
	repo := t.TempDir()

	s.EmitRepo(repo, Event{Type: "fix_created", Project: "p1", Bead: "p1-abc"})

	repoEvents, err := ReadEvents(filepath.Join(repo, ".hydra", "events.jsonl"))
	if err != nil || len(repoEvents) != 1 {
		t.Fatalf("repo mirror: %v, n=%d, want 1", err, len(repoEvents))
	}
	central, _ := ReadEvents(filepath.Join(dir, "2026-07-29.jsonl"))
	if len(central) != 1 || central[0].Time != repoEvents[0].Time {
		t.Errorf("central and repo copies must be the same stamped record")
	}
	gi, err := os.ReadFile(filepath.Join(repo, ".gitignore"))
	if err != nil || !strings.Contains(string(gi), ".hydra/") {
		t.Errorf(".hydra/ must be gitignored in the user's repo; got %q, err %v", gi, err)
	}
}

// TestEventSink_NilAndDisabledAreNoOps: the stream observes the system and must
// never be able to break it — nil sinks and failed writes are silent.
func TestEventSink_NilAndDisabledAreNoOps(t *testing.T) {
	var nilSink *EventSink
	nilSink.Emit(Event{Type: "x"})                  // must not panic
	nilSink.EmitRepo(t.TempDir(), Event{Type: "x"}) // must not panic

	disabled := NewEventSink("") // no central dir
	repo := t.TempDir()
	disabled.EmitRepo(repo, Event{Type: "escalation", Detail: "still mirrors to the repo"})
	events, _ := ReadEvents(filepath.Join(repo, ".hydra", "events.jsonl"))
	if len(events) != 1 {
		t.Errorf("repo mirroring must work even with the central stream disabled, n=%d", len(events))
	}
}

// TestReadEvents_TornLastLine: a crash mid-append leaves a torn final line; the
// reader must keep every intact record rather than failing the whole file.
func TestReadEvents_TornLastLine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "events.jsonl")
	content := `{"time":"2026-07-29T10:00:00.000Z","type":"bead_closed","bead":"a"}` + "\n" +
		`{"time":"2026-07-29T10:01:00.000Z","type":"fix_created","bead":"b"}` + "\n" +
		`{"time":"2026-07-29T10:02:00.000Z","type":"fix_cap` // torn
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	events, err := ReadEvents(path)
	if err != nil {
		t.Fatalf("ReadEvents: %v", err)
	}
	if len(events) != 2 {
		t.Errorf("n=%d, want 2 intact records despite the torn tail", len(events))
	}
	if _, err := ReadEvents(filepath.Join(dir, "absent.jsonl")); err != nil {
		t.Errorf("a missing file is an empty stream, not an error: %v", err)
	}
}

// TestEmit_ConcurrentWritersProduceIntactLines: many goroutines, one file, and
// every line must still parse — the mutex is doing its job or records tear.
func TestEmit_ConcurrentWritersProduceIntactLines(t *testing.T) {
	s, dir := testSink(t)
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			s.Emit(Event{Type: "worker_timeout", Bead: strings.Repeat("x", n%7+1),
				Fields: map[string]any{"n": n}})
		}(i)
	}
	wg.Wait()
	events, err := ReadEvents(filepath.Join(dir, "2026-07-29.jsonl"))
	if err != nil || len(events) != 50 {
		t.Fatalf("n=%d err=%v, want 50 intact records", len(events), err)
	}
}

func TestEmit_TypelessBecomesVisible(t *testing.T) {
	s, dir := testSink(t)
	s.Emit(Event{Detail: "programming error somewhere"})
	events, _ := ReadEvents(filepath.Join(dir, "2026-07-29.jsonl"))
	if len(events) != 1 || events[0].Type != "unknown" {
		t.Errorf("a typeless event must surface as type=unknown, not vanish: %+v", events)
	}
}
