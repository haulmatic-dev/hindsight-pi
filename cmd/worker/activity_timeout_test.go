package main

import (
	"testing"
	"time"
)

// TestDefaultActivityTimeout_ShorterForRPC pins the asymmetry and the reason for
// it.
//
// rpc emits an event for every step, so a working session is never quiet for
// long; silence means the provider request has hung. Measured on 2026-07-28: a
// worker logged "prompt sent over rpc" at 07:50:09 and emitted nothing until the
// 10-minute timeout fired at 08:00:29, while the next attempt received a 429 in
// five seconds. Pi's own retry backoff is delayMs: 2000, so a ten-minute gap is
// never Pi waiting — it is a dead request holding a worker slot. With five slots
// the run stopped closing beads entirely.
//
// Text mode streams prose and a long tool run can legitimately go quiet, so it
// keeps the more patient backstop.
func TestDefaultActivityTimeout_ShorterForRPC(t *testing.T) {
	rpc := defaultActivityTimeout(PiConfig{Mode: "rpc"})
	text := defaultActivityTimeout(PiConfig{})

	if rpc >= text {
		t.Errorf("rpc timeout (%s) must be shorter than text (%s): rpc silence means a hung request, not slow work", rpc, text)
	}
	if text != 10*time.Minute {
		t.Errorf("text-mode timeout = %s, want 10m (unchanged)", text)
	}
	if rpc <= 0 {
		t.Fatalf("rpc timeout = %s, want positive", rpc)
	}
	// Long enough that a genuine multi-step turn is not cut off. Observed
	// healthy rpc turns emit events every few seconds and complete well inside
	// this window.
	if rpc < 2*time.Minute {
		t.Errorf("rpc timeout = %s, too aggressive — a slow but healthy turn would be killed", rpc)
	}
}

// TestDefaultActivityTimeout_ModeVariants: the timeout follows the same
// exact-match rule as the rest of the mode handling, so a typo gets the patient
// text-mode value rather than the aggressive one.
func TestDefaultActivityTimeout_ModeVariants(t *testing.T) {
	short := defaultActivityTimeout(PiConfig{Mode: "rpc"})
	long := defaultActivityTimeout(PiConfig{Mode: "text"})

	for _, mode := range []string{"rpc", "RPC", " rpc "} {
		if got := defaultActivityTimeout(PiConfig{Mode: mode}); got != short {
			t.Errorf("mode %q = %s, want the rpc value %s", mode, got, short)
		}
	}
	for _, mode := range []string{"", "text", "rcp", "banana"} {
		if got := defaultActivityTimeout(PiConfig{Mode: mode}); got != long {
			t.Errorf("mode %q = %s, want the text value %s", mode, got, long)
		}
	}
}
