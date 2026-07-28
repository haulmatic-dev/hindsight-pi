package core

import (
	"strings"
	"testing"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// newFixRecursionCloser builds a BeadCloser with a mock beads client, seeded with
// one bead so label updates have something to attach to.
func newFixRecursionCloser(t *testing.T, seed types.Bead) (*BeadCloser, *mockBeadsClient) {
	t.Helper()
	mock := newMockBeadsClient()
	mock.beads[string(seed.ID)] = seed
	return &BeadCloser{beadsClient: mock}, mock
}

func countFixBeads(m *mockBeadsClient) []types.Bead {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []types.Bead
	for _, b := range m.beads {
		if b.Type == "fix" {
			out = append(out, b)
		}
	}
	return out
}

// TestCreateFixBead_FirstLevelIsAllowed: one fix bead per failed original is the
// whole point — it redoes the work and keeps dependents blocked.
func TestCreateFixBead_FirstLevelIsAllowed(t *testing.T) {
	orig := types.Bead{ID: "proj-001", Title: "GREEN: Implement the reader", Priority: 2}
	closer, mock := newFixRecursionCloser(t, orig)

	if err := closer.createFixBeadForFailedClose(&orig, "/tmp/proj", "proj", string(orig.ID)); err != nil {
		t.Fatalf("createFixBeadForFailedClose: %v", err)
	}

	fixes := countFixBeads(mock)
	if len(fixes) != 1 {
		t.Fatalf("expected exactly 1 fix bead, got %d", len(fixes))
	}
	if !strings.HasPrefix(fixes[0].Title, "Fix: ") {
		t.Errorf("fix bead title = %q, want a %q prefix", fixes[0].Title, "Fix: ")
	}
}

// TestCreateFixBead_NestingIsRefused is the gap-churn fix.
//
// Run H produced titles nested three deep — "Fix: Fix: Fix: NFR-001: The impact
// path stays cheap and offline" — and 158 fix beads against 18 real tasks, because
// the cap was 3. Each extra level re-ran identical instructions through the
// mechanism that had just failed, burning a worker slot and provider quota per
// attempt while burying the original gap.
func TestCreateFixBead_NestingIsRefused(t *testing.T) {
	for _, title := range []string{
		"Fix: GREEN: Implement the reader",
		"Fix: Fix: GREEN: Implement the reader",
		"Fix: Fix: Fix: NFR-001: The impact path stays cheap and offline",
	} {
		orig := types.Bead{ID: types.BeadID("proj-" + title[:6]), Title: title, Priority: 1}
		closer, mock := newFixRecursionCloser(t, orig)

		if err := closer.createFixBeadForFailedClose(&orig, "/tmp/proj", "proj", string(orig.ID)); err != nil {
			t.Fatalf("%q: unexpected error: %v", title, err)
		}

		if fixes := countFixBeads(mock); len(fixes) != 0 {
			t.Errorf("%q: created %d nested fix bead(s), want 0", title, len(fixes))
		}
	}
}

// TestCreateFixBead_RefusalIsLabelled: refusing must not silently close the bead
// and unblock its dependents. That is the regression close_blocked_on_failed
// exists to prevent, so the outcome has to be recorded on the bead.
func TestCreateFixBead_RefusalIsLabelled(t *testing.T) {
	orig := types.Bead{ID: "proj-002", Title: "Fix: GREEN: Implement the reader", Priority: 1}
	closer, mock := newFixRecursionCloser(t, orig)

	if err := closer.createFixBeadForFailedClose(&orig, "/tmp/proj", "proj", string(orig.ID)); err != nil {
		t.Fatalf("createFixBeadForFailedClose: %v", err)
	}

	mock.mu.RLock()
	labels := mock.beads[string(orig.ID)].Labels
	mock.mu.RUnlock()

	var found bool
	for _, l := range labels {
		if l == fixRecursionCappedLabel {
			found = true
		}
	}
	if !found {
		t.Errorf("labels = %v, want %q so the capped recursion is visible rather than silent", labels, fixRecursionCappedLabel)
	}
}

// TestCreateFixBead_FallbackPathWhenBeadLookupFailed: with no bead there is no
// title to inspect, so the fix bead is still created — losing it would drop the
// only record of a failed closure.
func TestCreateFixBead_FallbackPathWhenBeadLookupFailed(t *testing.T) {
	mock := newMockBeadsClient()
	closer := &BeadCloser{beadsClient: mock}

	if err := closer.createFixBeadForFailedClose(nil, "/tmp/proj", "proj", "proj-999"); err != nil {
		t.Fatalf("createFixBeadForFailedClose: %v", err)
	}
	if fixes := countFixBeads(mock); len(fixes) != 1 {
		t.Errorf("expected 1 fix bead on the fallback path, got %d", len(fixes))
	}
}

// TestMaxFixNesting_IsOne pins the cap itself, so raising it back is a deliberate
// decision with a test to change rather than a quiet edit.
func TestMaxFixNesting_IsOne(t *testing.T) {
	if maxFixNesting != 1 {
		t.Errorf("maxFixNesting = %d, want 1 — a fix bead whose fix failed should escalate, not spawn another copy", maxFixNesting)
	}
}
