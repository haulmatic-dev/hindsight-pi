package pan

// Signature normalization + repair efficacy (2026-09-15 run analysis):
//
// 1. Candidate reasons embed volatile ages ("Blocked 94h27m on stale human
//    escalation") — the age ticks every minute, so the dedup signature
//    never stabilizes and Pan re-judges the same set every tick anyway
//    (~31 judgments/hour observed after dedup shipped). Normalization
//    strips durations/timestamps from the signature ONLY; the judgment
//    prompt keeps the raw reason.
//
// 2. Repair efficacy: 66 prune-worktree-registration executions in one day
//    on one wedge — the repair never fixed the cause and Pan kept
//    proposing it every 30 minutes. After maxConsecutiveRepairs attempts
//    on the same (action, scope, normalized reason) key, refuse and
//    classify human-only. A changed reason is a new key (natural reset).

import (
	"strings"
	"testing"
	"time"

	"github.com/haulmatic-dev/hydra/internal/registry"
)

// Reasons that differ only in volatile ages/timestamps must produce the
// same signature; genuinely different causes must not.
func TestNormalizeReasonStabilizesSignature(t *testing.T) {
	r1 := "Blocked 94h27m on stale human escalation"
	r2 := "Blocked 95h02m on stale human escalation"
	r3 := "Blocked 2h on store corruption"

	rec := func() registry.WorkerRecord {
		return registry.WorkerRecord{BeadID: "b-1", State: "blocked"}
	}
	c1 := Candidate{Record: rec(), Reason: r1}
	c2 := Candidate{Record: rec(), Reason: r2}
	c3 := Candidate{Record: rec(), Reason: r3}

	if candidateSignature([]Candidate{c1}) != candidateSignature([]Candidate{c2}) {
		t.Errorf("age-only difference changed signature:\n  %q\n  %q", r1, r2)
	}
	if candidateSignature([]Candidate{c1}) == candidateSignature([]Candidate{c3}) {
		t.Errorf("different causes produced the same signature")
	}
	if normalizeReason(r1) == r1 {
		t.Errorf("normalizeReason left volatile age in place: %q", normalizeReason(r1))
	}
}

// After maxConsecutiveRepairs attempts on the same (action, scope, reason)
// key, further repairs are refused as ineffective. A reason change is a new
// key and repairs resume.
func TestRepairEfficacyRefusesRepeatedAttempts(t *testing.T) {
	store := &fakeStore{}
	reason := "PUSH-PENDING rejected (fetch first)"
	executed := 0
	events := []string{}

	s := New(Config{Enabled: true}, store, nil)
	s.SetBlockedBeads(func() []BlockedBead {
		return []BlockedBead{{BeadID: "webapp-push-pending", Reason: reason, Since: time.Now()}}
	})
	s.SetProjectScope(func(beadID string) string { return "webapp" })
	s.SetRepairHandlers(map[string]func(beadID string) error{
		"re-push-integration": func(beadID string) error { executed++; return nil },
	})
	s.SetOnRepairEvent(func(beadID, action, result, detail string) {
		events = append(events, action+":"+result+":"+detail)
	})

	for i := 0; i < maxConsecutiveRepairs+2; i++ {
		s.handleRepair("webapp-push-pending", "re-push-integration")
		// Bypass the 30-minute project rate limiter: efficacy counts
		// ATTEMPTS, and the test makes them back-to-back.
		s.repairMu.Lock()
		s.repairLimits = make(map[string]time.Time)
		s.repairMu.Unlock()
	}

	if executed != maxConsecutiveRepairs {
		t.Errorf("handler executions = %d, want %d (refusals after cap)", executed, maxConsecutiveRepairs)
	}
	var refused bool
	for _, e := range events {
		if strings.Contains(e, "ineffective") {
			refused = true
		}
	}
	if !refused {
		t.Errorf("no ineffective refusal event in %v", events)
	}

	// A changed cause is a new key: repairs resume.
	reason = "PUSH-PENDING: pr merge failed: 403"
	s.repairMu.Lock()
	s.repairLimits = make(map[string]time.Time)
	s.repairMu.Unlock()
	s.handleRepair("webapp-push-pending", "re-push-integration")
	if executed != maxConsecutiveRepairs+1 {
		t.Errorf("after reason change executions = %d, want %d", executed, maxConsecutiveRepairs+1)
	}
}
