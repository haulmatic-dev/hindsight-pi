package agents

import (
	"context"
	"testing"
	"time"

	"github.com/haulmatic-dev/hydra/internal/degrade"
	"github.com/haulmatic-dev/hydra/internal/hindsight"
	"github.com/haulmatic-dev/hydra/pkg/logger"
	"github.com/haulmatic-dev/hydra/pkg/types"
)

// TestQueryHindsightDegradationCounters (FR-032): the fail-open recall path
// must record attempt/success counters so a 0%-success subsystem (the 500ms
// timeout bug, hydra-bye7) is visible instead of silently swallowed.
func TestQueryHindsightDegradationCounters(t *testing.T) {
	// Unreachable endpoint: every Search fails, exactly the degradation case.
	client := hindsight.NewClient(
		hindsight.WithBaseURL("http://127.0.0.1:1"),
		hindsight.WithTimeout(500*time.Millisecond),
		hindsight.WithEnabled(true),
	)
	p := NewContextProcessor(client, DefaultContextProcessorConfig(), logger.NewSimpleLogger("test"))
	if p == nil || !p.IsEnabled() {
		t.Fatalf("processor must be enabled for the counter path")
	}

	ctx := context.Background()
	bead := types.Bead{ID: types.BeadID("dg-test-1"), Title: "counter test", Description: "probe"}
	if _, err := p.QueryHindsight(ctx, bead); err != nil {
		t.Fatalf("QueryHindsight is fail-open, must not error: %v", err)
	}

	var st *degrade.Status
	for _, s := range degrade.Snapshot() {
		if s.Name == "hindsight-recall" {
			c := s
			st = &c
		}
	}
	if st == nil {
		t.Fatal("hindsight-recall counters missing after failed query")
	}
	if st.Attempts != 1 || st.Successes != 0 {
		t.Fatalf("expected attempts=1 successes=0, got %+v", st)
	}

	if _, err := p.QueryHindsightByID(ctx, types.BeadID("dg-test-1")); err != nil {
		t.Fatalf("QueryHindsightByID is fail-open: %v", err)
	}
	if _, err := p.QueryHindsightWithQuery(ctx, "probe query", 5); err != nil {
		t.Fatalf("QueryHindsightWithQuery is fail-open: %v", err)
	}
	for _, s := range degrade.Snapshot() {
		if s.Name == "hindsight-recall" && s.Attempts != 3 {
			t.Fatalf("expected 3 attempts across all query paths, got %d", s.Attempts)
		}
	}

	// Failed queries are not cached, so a retry counts as a new attempt.
	if _, err := p.QueryHindsight(ctx, bead); err != nil {
		t.Fatalf("fail-open: %v", err)
	}
	for _, s := range degrade.Snapshot() {
		if s.Name == "hindsight-recall" && s.Attempts != 4 {
			t.Fatalf("retry after failure should record an attempt, got %d", s.Attempts)
		}
	}
}
