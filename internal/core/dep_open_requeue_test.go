package core

import (
	"strings"
	"testing"
	"time"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// RED-phase tests for "dep-open requeues without a strike, bounded".
//
// Contract pinned here for GREEN (internal/core/bead_closer_hard_gates.go):
//   - A chain member closed while a dependency is still non-terminal is a
//     REQUEUE, not a verification failure: runCloseHardGates returns an error
//     containing "dep-open", the bead is reopened, but NO strike is taken —
//     no verified-close-reject-attempt-N label, no validation-retry count, no
//     round-cap escalation however low validation.max_rounds is.
//   - The requeues are bounded by a durable counter label
//     dep-open-requeue-N (N = consecutive dep-open requeues so far). The
//     fifth consecutive dep-open rejection escalates: labels escalated +
//     needs-human + verified-close-escalated, one bead.escalated event.
//   - A passing dependency gate (dependencies terminal) removes every
//     dep-open-requeue-* label, so the bound counts CONSECUTIVE rejections.
//   - Other rejection reasons keep striking exactly as before.

const (
	depOpenRequeuePrefix = "dep-open-requeue-"
	depOpenEscalateAt    = 5 // the fifth consecutive dep-open rejection escalates
)

// depOpenFixture builds a closer, a non-terminal RED dependency and a GREEN
// chain member depending on it. fresh() re-reads the member from the store so
// durable labels written by earlier rejections are visible to the next call,
// as they are in production.
func depOpenFixture(t *testing.T, maxRounds int, id string) (c *BeadCloser, mock *mockBeadsClient, rm *RetryManager, repo, sinkDir string, fresh func() *types.Bead) {
	t.Helper()
	repo = initGitRepo(t)
	cfg := hardGateConfig(maxRounds)
	rm = NewRetryManager(time.Second, time.Minute)
	vs := NewValidationService(cfg, rm, nil)
	c, mock, sinkDir = hardGateCloser(t, cfg, repo)
	c.validationService = vs

	mock.beads[id+"-dep"] = types.Bead{ID: types.BeadID(id + "-dep"), Title: "RED: Write tests", Project: "test-project", Status: types.StatusInProgress}
	mock.beads[id] = types.Bead{
		ID:           types.BeadID(id),
		Title:        "GREEN: Implement activity log",
		Project:      "test-project",
		Status:       types.StatusInProgress,
		Dependencies: []types.BeadID{types.BeadID(id + "-dep")},
	}
	fresh = func() *types.Bead {
		b := mock.beads[id]
		return &b
	}
	return
}

func labelsWithPrefix(b types.Bead, prefix string) []string {
	var out []string
	for _, l := range b.Labels {
		if strings.HasPrefix(l, prefix) {
			out = append(out, l)
		}
	}
	return out
}

func hasLabelNamed(b types.Bead, name string) bool {
	for _, l := range b.Labels {
		if l == name {
			return true
		}
	}
	return false
}

// Three consecutive dep-open rejections: each returns an error containing
// dep-open, none escalates, and none takes a strike — even with max_rounds=1,
// where a single strike would already have escalated the second call.
func TestDepOpen_ThreeConsecutiveRejectionsRequeueWithoutStrike(t *testing.T) {
	c, mock, rm, repo, _, fresh := depOpenFixture(t, 1, "proj-do1")

	for i := 1; i <= 3; i++ {
		err := c.runCloseHardGates(fresh(), repo, "", repo)
		if err == nil {
			t.Fatalf("rejection %d: chain member with an open dependency must be rejected", i)
		}
		if !strings.Contains(err.Error(), "dep-open") {
			t.Fatalf("rejection %d: error must contain dep-open, got: %v", i, err)
		}
		if strings.Contains(err.Error(), "escalated") {
			t.Fatalf("rejection %d: dep-open requeue must not escalate before the bound, got: %v", i, err)
		}
		b := mock.beads["proj-do1"]
		if b.Status != types.StatusOpen {
			t.Fatalf("rejection %d: requeue must reopen the bead, status = %s", i, b.Status)
		}
	}

	b := mock.beads["proj-do1"]
	if got := labelsWithPrefix(b, closeRejectAttemptPrefix); len(got) != 0 {
		t.Errorf("dep-open requeue must not leave a strike label, got %v", got)
	}
	if n := rm.GetValidationRetryCount("proj-do1"); n != 0 {
		t.Errorf("dep-open requeue must not count a validation retry, got %d", n)
	}
	if hasLabelNamed(b, "needs-human") || hasLabelNamed(b, "verified-close-escalated") {
		t.Errorf("no escalation labels expected after 3 dep-open requeues, got %v", b.Labels)
	}
}

// The requeue is bounded by a durable dep-open-requeue-N label counter, so the
// bound survives a daemon restart (labels live in the bead store).
func TestDepOpen_RequeueCounterLabelIsDurableAndSingle(t *testing.T) {
	c, mock, _, repo, _, fresh := depOpenFixture(t, 3, "proj-do2")

	for i := 1; i <= 2; i++ {
		if err := c.runCloseHardGates(fresh(), repo, "", repo); err == nil {
			t.Fatalf("rejection %d must return a dep-open error", i)
		}
	}
	got := labelsWithPrefix(mock.beads["proj-do2"], depOpenRequeuePrefix)
	if len(got) == 0 {
		t.Fatalf("expected a %sN counter label after dep-open requeues, labels = %v", depOpenRequeuePrefix, mock.beads["proj-do2"].Labels)
	}
	if len(got) != 1 {
		t.Errorf("counter must be a single label (highest N replaces the previous), got %v", got)
	}
	if want := depOpenRequeuePrefix + "2"; got[0] != want {
		t.Errorf("after 2 consecutive dep-open requeues label must be %q, got %q", want, got[0])
	}
}

// Fifth consecutive dep-open rejection escalates: escalated + needs-human +
// verified-close-escalated labels and exactly one bead.escalated event. The
// four before it requeue without escalating (default max_rounds=3 would have
// escalated at the fourth if dep-open still took strikes).
func TestDepOpen_FifthConsecutiveRejectionEscalates(t *testing.T) {
	c, mock, _, repo, sinkDir, fresh := depOpenFixture(t, 3, "proj-do3")

	for i := 1; i < depOpenEscalateAt; i++ {
		err := c.runCloseHardGates(fresh(), repo, "", repo)
		if err == nil || !strings.Contains(err.Error(), "dep-open") {
			t.Fatalf("rejection %d must be a dep-open requeue, got: %v", i, err)
		}
		if strings.Contains(err.Error(), "escalated") {
			t.Fatalf("rejection %d escalated before the fifth, got: %v", i, err)
		}
	}
	if evs := hardGateEvents(t, sinkDir, "bead.escalated"); len(evs) != 0 {
		t.Fatalf("no bead.escalated event expected before the fifth rejection, got %d", len(evs))
	}

	err := c.runCloseHardGates(fresh(), repo, "", repo)
	if err == nil || !strings.Contains(err.Error(), "escalated") {
		t.Fatalf("fifth consecutive dep-open rejection must escalate, got: %v", err)
	}
	b := mock.beads["proj-do3"]
	for _, want := range []string{"escalated", "needs-human", "verified-close-escalated"} {
		if !hasLabelNamed(b, want) {
			t.Errorf("escalated bead must carry label %q, got %v", want, b.Labels)
		}
	}
	if evs := hardGateEvents(t, sinkDir, "bead.escalated"); len(evs) != 1 {
		t.Fatalf("expected exactly one bead.escalated event on the fifth rejection, got %d", len(evs))
	}
}

// A passing dependency gate removes dep-open-requeue-*: the bound counts
// consecutive rejections, so a later blocked close starts from zero again.
func TestDepOpen_PassingGateClearsRequeueLabels(t *testing.T) {
	c, mock, _, repo, _, fresh := depOpenFixture(t, 3, "proj-do4")

	for i := 1; i <= 3; i++ {
		if err := c.runCloseHardGates(fresh(), repo, "", repo); err == nil {
			t.Fatalf("rejection %d must return a dep-open error", i)
		}
	}
	if len(labelsWithPrefix(mock.beads["proj-do4"], depOpenRequeuePrefix)) == 0 {
		t.Fatalf("precondition: requeue counter label must exist, labels = %v", mock.beads["proj-do4"].Labels)
	}

	// The dependency completes; the gate now passes.
	dep := mock.beads["proj-do4-dep"]
	dep.Status = types.StatusCompleted
	mock.beads["proj-do4-dep"] = dep

	if err := c.runCloseHardGates(fresh(), repo, "", repo); err != nil {
		t.Fatalf("terminal dependency must pass the gate: %v", err)
	}
	if got := labelsWithPrefix(mock.beads["proj-do4"], depOpenRequeuePrefix); len(got) != 0 {
		t.Errorf("passing dependency gate must remove %s* labels, still have %v", depOpenRequeuePrefix, got)
	}
}

// After a pass the counter restarts: three more dep-open rejections must not
// escalate (they would at the 5-bound if the earlier four were not cleared).
func TestDepOpen_BoundCountsConsecutiveRejectionsOnly(t *testing.T) {
	c, mock, _, repo, _, fresh := depOpenFixture(t, 3, "proj-do5")

	for i := 1; i <= 4; i++ {
		if err := c.runCloseHardGates(fresh(), repo, "", repo); err == nil || strings.Contains(err.Error(), "escalated") {
			t.Fatalf("rejection %d must be a plain dep-open requeue, got: %v", i, err)
		}
	}
	depID := types.BeadID("proj-do5-dep")
	dep := mock.beads[string(depID)]
	dep.Status = types.StatusCompleted
	mock.beads[string(depID)] = dep
	if err := c.runCloseHardGates(fresh(), repo, "", repo); err != nil {
		t.Fatalf("terminal dependency must pass the gate: %v", err)
	}

	// The dependency regresses (reopened); the chain member is blocked again.
	dep.Status = types.StatusInProgress
	mock.beads[string(depID)] = dep
	for i := 1; i <= 3; i++ {
		err := c.runCloseHardGates(fresh(), repo, "", repo)
		if err == nil || !strings.Contains(err.Error(), "dep-open") {
			t.Fatalf("post-pass rejection %d must be dep-open, got: %v", i, err)
		}
		if strings.Contains(err.Error(), "escalated") {
			t.Fatalf("post-pass rejection %d escalated; the counter must restart after a pass, got: %v", i, err)
		}
	}
}

// dep-open requeues must not consume the strike budget of other rejection
// reasons: after four dep-open requeues a verification failure is still round 1.
func TestDepOpen_RequeuesDoNotConsumeStrikeBudgetOfOtherRejections(t *testing.T) {
	c, mock, rm, repo, _, fresh := depOpenFixture(t, 3, "proj-do6")

	for i := 1; i < depOpenEscalateAt; i++ {
		if err := c.runCloseHardGates(fresh(), repo, "", repo); err == nil {
			t.Fatalf("rejection %d must return a dep-open error", i)
		}
	}

	// Dependency completes, but the bead's own acceptance command fails.
	dep := mock.beads["proj-do6-dep"]
	dep.Status = types.StatusCompleted
	mock.beads["proj-do6-dep"] = dep
	b := mock.beads["proj-do6"]
	b.Description = "**Verification Command:**\n```\nexit 9\n```\n"
	mock.beads["proj-do6"] = b

	err := c.runCloseHardGates(fresh(), repo, "", repo)
	if err == nil || !strings.Contains(err.Error(), "verification-failed") {
		t.Fatalf("verification failure must be a normal rejection, got: %v", err)
	}
	if strings.Contains(err.Error(), "escalated") {
		t.Fatalf("verification failure escalated; dep-open requeues must not have consumed strikes, got: %v", err)
	}
	if n := rm.GetValidationRetryCount("proj-do6"); n != 1 {
		t.Errorf("verification failure must count exactly one strike, got %d", n)
	}
}
