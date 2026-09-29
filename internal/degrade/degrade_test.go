package degrade

import (
	"testing"
	"time"
)

func resetGlobal() {
	global = NewRegistry(time.Hour)
}

func TestRecordAttemptAndSuccess(t *testing.T) {
	resetGlobal()
	RecordAttempt("s1")
	RecordAttempt("s1")
	RecordAttempt("s1")
	snap := Snapshot()
	if len(snap) != 1 || snap[0].Name != "s1" || snap[0].Attempts != 3 || snap[0].Successes != 0 {
		t.Fatalf("unexpected snapshot: %+v", snap)
	}
	if !snap[0].Degraded() {
		t.Fatal("3 attempts, 0 successes should be degraded")
	}
	RecordSuccess("s1")
	snap = Snapshot()
	if snap[0].Successes != 1 || snap[0].Degraded() {
		t.Fatalf("success should clear degradation: %+v", snap[0])
	}
}

func TestDegradedNeedsMinAttempts(t *testing.T) {
	resetGlobal()
	RecordAttempt("s1")
	RecordAttempt("s1")
	snap := Snapshot()
	if snap[0].Degraded() {
		t.Fatal("2 attempts must not be degraded")
	}
}

func TestUnknownSubsystemNotDegraded(t *testing.T) {
	resetGlobal()
	RecordSuccess("s1")
	snap := Snapshot()
	if snap[0].Degraded() || snap[0].Attempts != 0 {
		t.Fatalf("success-only entry must not degrade: %+v", snap[0])
	}
}

func TestWindowRollsOverAfterSuccess(t *testing.T) {
	r := NewRegistry(time.Hour)
	r.RecordAttempt("s")
	r.RecordSuccess("s")
	// Backdate the entry past the window.
	r.mu.Lock()
	r.entries["s"].first = time.Now().Add(-2 * time.Hour)
	r.mu.Unlock()
	r.RecordAttempt("s")
	r.RecordAttempt("s")
	r.RecordAttempt("s")
	snap := r.Snapshot()
	if snap[0].Attempts != 3 || snap[0].Successes != 0 {
		t.Fatalf("window should have rolled over: %+v", snap[0])
	}
	if !snap[0].Degraded() {
		t.Fatalf("fresh post-rollover attempt count should still be counted: %+v", snap[0])
	}
}

func TestWindowKeepsZeroSuccessAccumulating(t *testing.T) {
	r := NewRegistry(time.Hour)
	for i := 0; i < 3; i++ {
		r.RecordAttempt("s")
	}
	snap := r.Snapshot()
	if snap[0].Attempts != 3 {
		t.Fatalf("failing attempts accumulate inside the window: %+v", snap[0])
	}
}

func TestFailureStreakAfterSuccessDegrades(t *testing.T) {
	r := NewRegistry(time.Hour)
	r.RecordAttempt("s")
	r.RecordSuccess("s")
	// Tumble the window forward, then fail: the new window must judge
	// successes vs attempts on its own, not inherit the old success.
	r.mu.Lock()
	r.entries["s"].first = time.Now().Add(-2 * time.Hour)
	r.mu.Unlock()
	for i := 0; i < 3; i++ {
		r.RecordAttempt("s")
	}
	snap := r.Snapshot()
	if !snap[0].Degraded() {
		t.Fatalf("failure streak in a fresh window must degrade: %+v", snap[0])
	}
}

func TestDefaultWindow(t *testing.T) {
	if DefaultWindow != 24*time.Hour {
		t.Fatalf("PRD FR-032 default window must be 24h, got %s", DefaultWindow)
	}
	if NewRegistry(0).window != DefaultWindow {
		t.Fatal("zero window must fall back to DefaultWindow")
	}
}

func TestStringFormat(t *testing.T) {
	s := Status{Name: "x", Attempts: 12, Successes: 0}
	if s.String() != "x 0/12" {
		t.Fatalf("unexpected render: %s", s.String())
	}
}
