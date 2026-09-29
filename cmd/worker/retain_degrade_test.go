package main

import (
	"testing"

	"github.com/haulmatic-dev/hydra/internal/degrade"
)

// TestRetainDegradationCounters (FR-032): retention is fail-open, so its
// attempt/success counters must be recorded for the Pan digest.
func TestRetainDegradationCounters(t *testing.T) {
	degrade.Reset()
	recordRetainAttempt()
	recordRetainAttempt()
	recordRetainSuccess()
	found := false
	for _, s := range degrade.Snapshot() {
		if s.Name == "hindsight-retain" {
			found = true
			if s.Attempts != 2 || s.Successes != 1 {
				t.Fatalf("unexpected counters: %+v", s)
			}
		}
	}
	if !found {
		t.Fatal("hindsight-retain counters missing")
	}
}
