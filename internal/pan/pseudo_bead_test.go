package pan

import (
	"context"
	"strings"
	"testing"
	"time"
)

// Pseudo-beads ("<project>-push-pending") are project-level signals, not
// beads: abandon/close decisions against them can never succeed and, left
// unguarded, repeat every tick forever (observed live 2026-09-14:
// webapp-push-pending abandoned ~every 2 minutes, each attempt failing).
func TestPseudoBeadAbandonIgnored(t *testing.T) {
	store := &fakeStore{}
	abandonCalled := false

	runner := func(ctx context.Context, cfg Config, prompt string) (string, error) {
		return `[{"bead_id": "webapp-push-pending", "action": "abandon-with-summary", "message": "human-only divergence"}]`, nil
	}
	s := New(Config{Enabled: true}, store, runner)
	s.SetBlockedBeads(func() []BlockedBead {
		return []BlockedBead{{BeadID: "webapp-push-pending", Reason: "PUSH-PENDING: rejected fetch-first", Since: time.Now()}}
	})
	s.SetOnAbandonWithSummary(func(beadID string) error {
		abandonCalled = true
		return nil
	})

	s.Tick(context.Background())

	if abandonCalled {
		t.Error("abandon handler called for pseudo-bead; want ignored")
	}
	cmds, _ := store.PendingCommands("webapp-push-pending")
	if len(cmds) != 0 {
		t.Errorf("pending commands for pseudo-bead = %d, want 0", len(cmds))
	}
}

// An unchanged pseudo-bead reason is re-presented at most once per 30
// minutes; a changed reason (new failure mode) is presented immediately.
func TestPseudoBeadCandidacyCooldown(t *testing.T) {
	reason := "PUSH-PENDING: rejected fetch-first"
	s := New(Config{Enabled: true}, &fakeStore{}, nil)
	s.SetBlockedBeads(func() []BlockedBead {
		return []BlockedBead{{BeadID: "webapp-push-pending", Reason: reason, Since: time.Now()}}
	})

	first, err := s.Candidates()
	if err != nil {
		t.Fatalf("Candidates: %v", err)
	}
	if len(first) != 1 {
		t.Fatalf("first Candidates = %d, want 1", len(first))
	}

	second, _ := s.Candidates()
	if len(second) != 0 {
		t.Errorf("second Candidates = %d, want 0 (unchanged reason within cooldown)", len(second))
	}

	reason = "PUSH-PENDING: pr merge failed: 403"
	third, _ := s.Candidates()
	if len(third) != 1 {
		t.Errorf("third Candidates = %d, want 1 (changed reason bypasses cooldown)", len(third))
	}
	if len(third) == 1 && !strings.Contains(third[0].Reason, "403") {
		t.Errorf("re-presented reason = %q, want the new 403 reason", third[0].Reason)
	}
}
