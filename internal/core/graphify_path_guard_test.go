package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func newGuardTestIndexer(bin string, sink *EventSink) *GraphifyIndexer {
	idx := NewGraphifyIndexer(GraphifyConfig{Enabled: true, Binary: bin})
	idx.SetEventSink(sink)
	return idx
}

func readSinkEvents(t *testing.T, dir string) []Event {
	t.Helper()
	files, _ := filepath.Glob(filepath.Join(dir, "*.jsonl"))
	var raw string
	for _, f := range files {
		if b, err := os.ReadFile(f); err == nil {
			raw += string(b)
		}
	}
	var evs []Event
	for _, line := range splitNonEmptyLines(raw) {
		var ev Event
		if err := json.Unmarshal([]byte(line), &ev); err == nil {
			evs = append(evs, ev)
		}
	}
	return evs
}

func splitNonEmptyLines(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			if i > start {
				out = append(out, s[start:i])
			}
			start = i + 1
		}
	}
	if start < len(s) {
		out = append(out, s[start:])
	}
	return out
}

func countMissingEvents(evs []Event) []Event {
	var out []Event
	for _, ev := range evs {
		if ev.Type == projectPathMissingEvent {
			out = append(out, ev)
		}
	}
	return out
}

// TestGraphifyPathGuard_NonexistentPathRefused: Refresh on a path that does not
// exist returns false and never spawns a build goroutine (no claim, no build).
func TestGraphifyPathGuard_NonexistentPathRefused(t *testing.T) {
	bin, calls := fakeGraphify(t, "exit 0\n")
	missing := filepath.Join(t.TempDir(), "gone")

	idx := newGuardTestIndexer(bin, nil)
	if idx.Refresh(missing) {
		t.Fatal("Refresh of nonexistent path must return false")
	}
	waitIdle(t, idx, 500*time.Millisecond)
	if _, ok := idx.LastBuilt(missing); ok {
		t.Error("nonexistent path must not be recorded as built/claimed")
	}
	if got := waitForFile(t, calls, 300*time.Millisecond); got != "" {
		t.Errorf("graphify must not be invoked for a nonexistent path, got %q", got)
	}
}

// TestGraphifyPathGuard_EventEmittedOncePerTTL: first miss emits exactly one
// project_path_missing event carrying the path; a second miss within 24h emits
// none.
func TestGraphifyPathGuard_EventEmittedOncePerTTL(t *testing.T) {
	bin, _ := fakeGraphify(t, "exit 0\n")
	dir := t.TempDir()
	sink := NewEventSink(dir)
	missing := filepath.Join(t.TempDir(), "gone")

	idx := newGuardTestIndexer(bin, sink)
	idx.Refresh(missing)
	idx.Refresh(missing) // within TTL: must not emit again

	evs := countMissingEvents(readSinkEvents(t, dir))
	if len(evs) != 1 {
		t.Fatalf("got %d project_path_missing events, want 1", len(evs))
	}
	if evs[0].Fields["path"] != missing {
		t.Errorf("event path field = %v, want %q", evs[0].Fields["path"], missing)
	}

	// After the TTL window, a further miss emits again.
	idx.mu.Lock()
	idx.missingPaths[missing] = time.Now().Add(-25 * time.Hour)
	idx.mu.Unlock()
	idx.Refresh(missing)
	if evs = countMissingEvents(readSinkEvents(t, dir)); len(evs) != 2 {
		t.Fatalf("after TTL expiry got %d events, want 2", len(evs))
	}
}

// TestGraphifyPathGuard_ReappearingPathRefreshes: once the path comes back,
// Refresh proceeds normally and the missing marker is cleared.
func TestGraphifyPathGuard_ReappearingPathRefreshes(t *testing.T) {
	bin, calls := fakeGraphify(t, "exit 0\n")
	dir := t.TempDir()
	sink := NewEventSink(dir)

	idx := newGuardTestIndexer(bin, sink)
	idx.Refresh(filepath.Join(dir, "gone"))

	// Path "reappears".
	existing := filepath.Join(dir, "back")
	if err := os.MkdirAll(existing, 0o755); err != nil {
		t.Fatal(err)
	}
	if !idx.Refresh(existing) {
		t.Fatal("reappeared path must refresh normally")
	}
	waitIdle(t, idx, 3*time.Second)
	if got := waitForFile(t, calls, 3*time.Second); got == "" {
		t.Error("graphify build must run for a path that exists")
	}
	if evs := countMissingEvents(readSinkEvents(t, dir)); len(evs) != 1 {
		t.Errorf("expected exactly the pre-reappearance miss event, got %d", len(evs))
	}
	idx.mu.Lock()
	_, stillMarked := idx.missingPaths[existing]
	idx.mu.Unlock()
	if stillMarked {
		t.Error("existing path must clear the missing marker")
	}
}

// TestGraphifyPathGuard_ExistingPathRefreshes: a plain existing path refreshes
// normally with the guard in place.
func TestGraphifyPathGuard_ExistingPathRefreshes(t *testing.T) {
	bin, calls := fakeGraphify(t, "exit 0\n")
	idx := newGuardTestIndexer(bin, nil)
	project := t.TempDir()

	if !idx.Refresh(project) {
		t.Fatal("existing project must refresh")
	}
	waitIdle(t, idx, 3*time.Second)
	if got := waitForFile(t, calls, 3*time.Second); got == "" {
		t.Error("graphify must be invoked for an existing path")
	}
}
