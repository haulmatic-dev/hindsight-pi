package core

import (
	"strings"
	"testing"
	"time"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// TestRejectClose_DurableEscalationLadderAcrossRestart pins the fix for the
// ee5r/lkbo/ejsk 2026-10-04 silent gate deaths: the escalation ladder must be
// durable, so a daemon restart (which resets the in-memory retry manager)
// cannot restart the rejection count at zero and let gate failures cycle
// forever without ever reaching max rounds.
func TestRejectClose_DurableEscalationLadderAcrossRestart(t *testing.T) {
	projectPath := t.TempDir()
	closer, client, _ := setupEscalationCloser(t, projectPath)

	bead := types.Bead{
		ID:       types.BeadID("proj-gate1"),
		Project:  "proj",
		Title:    "gate death bead",
		Type:     "implement",
		Status:   types.StatusInProgress,
		Priority: 3,
	}
	client.addBead(bead)
	mark := func() *types.Bead {
		b, err := client.GetBead(projectPath, "repo", bead.ID)
		if err != nil || b == nil {
			t.Fatalf("GetBead: %v / %v", b, err)
		}
		return b
	}

	// Round 1: gate fails, bead reopened, attempt-1 label written.
	if err := closer.rejectClose(mark(), projectPath, "repo", "gate-test-failed"); err == nil || !strings.Contains(err.Error(), "rejected") {
		t.Fatalf("round 1: want rejection, got %v", err)
	}
	if got := durableRejectAttempts(mark().Labels); got != 1 {
		t.Fatalf("after round 1 durable attempts = %d, want 1 (labels %v)", got, mark().Labels)
	}

	// Round 2.
	if err := closer.rejectClose(mark(), projectPath, "repo", "gate-test-failed"); err == nil || !strings.Contains(err.Error(), "rejected") {
		t.Fatalf("round 2: want rejection, got %v", err)
	}
	if got := durableRejectAttempts(mark().Labels); got != 2 {
		t.Fatalf("after round 2 durable attempts = %d, want 2 (labels %v)", got, mark().Labels)
	}

	// Simulate a daemon restart: the in-memory retry manager resets.
	closer.validationService.retryManager = NewRetryManager(time.Millisecond, time.Millisecond)
	if got := closer.validationRetryCount(mark()); got != 2 {
		t.Fatalf("post-restart retry count = %d, want 2 (durable floor must survive)", got)
	}

	// Round 3 post-restart: this was the failure mode — with only the
	// in-memory count the bead would loop rounds 1..3 forever and never
	// escalate. With the durable floor the ladder continues to max.
	if err := closer.rejectClose(mark(), projectPath, "repo", "gate-test-failed"); err == nil || !strings.Contains(err.Error(), "rejected") {
		t.Fatalf("round 3: want rejection, got %v", err)
	}

	// The next close attempt now sees attempts >= max and escalates.
	escalated := mark()
	if err := closer.rejectClose(escalated, projectPath, "repo", "gate-test-failed"); err == nil || !strings.Contains(err.Error(), "escalated after 3 rejections") {
		t.Fatalf("want escalation after durable ladder reached 3, got %v", err)
	}
	if !hasBeadLabel(mark().Labels, escalatedLabel) {
		t.Fatalf("escalated label missing; labels: %v", mark().Labels)
	}
}

// TestDurableRejectAttempts covers the label parser directly.
func TestDurableRejectAttempts(t *testing.T) {
	cases := []struct {
		labels []string
		want   int
	}{
		{nil, 0},
		{[]string{"closed-failed"}, 0},
		{[]string{"verified-close-reject-attempt-1"}, 1},
		{[]string{"verified-close-reject-attempt-2", "verified-close-reject-attempt-1"}, 2},
		{[]string{"verified-close-reject-empty-diff", "verified-close-reject-attempt-3"}, 3},
	}
	for _, c := range cases {
		if got := durableRejectAttempts(c.labels); got != c.want {
			t.Errorf("durableRejectAttempts(%v) = %d, want %d", c.labels, got, c.want)
		}
	}
}
