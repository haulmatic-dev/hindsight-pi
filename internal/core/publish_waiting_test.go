package core

import "testing"

// The waiting-mark map is the idempotence state: a branch whose tip has a PR
// standing open must be skippable, and must be forgotten when the tip moves.
func TestPRWaitingMarkLifecycle(t *testing.T) {
	clearPRWaiting("p", "b")
	if got := prWaitingMark("p", "b"); got != "" {
		t.Fatalf("expected no mark, got %q", got)
	}
	setPRWaiting("p", "b", "abc123")
	if got := prWaitingMark("p", "b"); got != "abc123" {
		t.Fatalf("expected abc123, got %q", got)
	}
	// Isolation: another branch of the same project is unaffected.
	if got := prWaitingMark("p", "other"); got != "" {
		t.Fatalf("expected no mark for other branch, got %q", got)
	}
	clearPRWaiting("p", "b")
	if got := prWaitingMark("p", "b"); got != "" {
		t.Fatalf("expected mark cleared, got %q", got)
	}
}

// A missing branch must error (fail-open: caller falls through to publish).
func TestLocalBranchTipMissingBranch(t *testing.T) {
	if _, err := localBranchTip(t.TempDir(), "no-such-branch"); err == nil {
		t.Fatal("expected error for missing branch in a non-repo dir")
	}
}
