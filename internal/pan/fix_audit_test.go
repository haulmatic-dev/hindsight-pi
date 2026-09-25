package pan

import (
	"strings"
	"testing"

	"github.com/haulmatic-dev/hydra/internal/registry"
)

// Closure-audit duty (operator 2026-09-25): open auto-fix beads become
// judgment candidates so Pan can close redundant fixes before a worker redoes
// finished work, and flag hollow verdicts instead of trusting them (jx78).

func TestFixBeadsBecomeCandidates(t *testing.T) {
	s := New(Config{Enabled: true}, &fakeStore{}, nil)
	calls := 0
	s.SetFixBeads(func() []FixBead {
		calls++
		return []FixBead{{
			BeadID:          "fx-1",
			OriginalID:      "orig-9",
			CloseReason:     "failed (worker exited non-zero)",
			RecordedVerdict: `HYDRA_VERDICT: {"done": true, ...}`,
		}}
	})
	s.SetFixVerifier(func(fixBeadID, originalID string) FixVerification {
		return FixVerification{Status: "verified", Detail: "test gate green"}
	})

	cands, err := s.Candidates()
	if err != nil {
		t.Fatalf("Candidates: %v", err)
	}
	if len(cands) != 1 {
		t.Fatalf("candidates = %d, want 1", len(cands))
	}
	r := cands[0].Reason
	for _, want := range []string{
		"FIX-AUDIT: open auto-fix bead for original orig-9",
		"failed (worker exited non-zero)",
		`HYDRA_VERDICT: {"done": true`,
		"re-verification: verified",
	} {
		if !strings.Contains(r, want) {
			t.Errorf("reason %q missing %q", r, want)
		}
	}
	if calls != 1 {
		t.Errorf("FixBeads called %d times, want 1", calls)
	}
}

// Verified result cached: verifier must not run per tick (it executes real
// gate commands). Unknown results are also rate-limited.
func TestFixVerificationRateLimited(t *testing.T) {
	s := New(Config{Enabled: true}, &fakeStore{}, nil)
	verifierCalls := 0
	s.SetFixBeads(func() []FixBead {
		return []FixBead{{BeadID: "fx-2", OriginalID: "orig-8"}}
	})
	s.SetFixVerifier(func(fixBeadID, originalID string) FixVerification {
		verifierCalls++
		return FixVerification{Status: "unknown", Detail: "no scoped test gate"}
	})

	for i := 0; i < 3; i++ {
		if _, err := s.Candidates(); err != nil {
			t.Fatalf("Candidates: %v", err)
		}
	}
	if verifierCalls != 1 {
		t.Errorf("verifier called %d times across 3 scans, want 1 (cache TTL)", verifierCalls)
	}
}

// A fix bead holding a live worker row is suppressed: the worker is already
// redoing the work; judgment must not race it.
func TestFixBeadWithLiveWorkerSkipped(t *testing.T) {
	store := &fakeStore{workers: []registry.WorkerRecord{{BeadID: "fx-3", State: registry.StateWorking}}}
	s := New(Config{Enabled: true}, store, nil)
	s.SetFixBeads(func() []FixBead {
		return []FixBead{{BeadID: "fx-3", OriginalID: "orig-7"}}
	})
	cands, err := s.Candidates()
	if err != nil {
		t.Fatalf("Candidates: %v", err)
	}
	if len(cands) != 0 {
		t.Errorf("candidates = %d, want 0 (live worker row suppresses fix-audit)", len(cands))
	}
}

// Prompt carries the closure-audit judgment rules, including the jx78
// hollow-verdict lesson: a recorded verdict alone never closes a fix bead.
func TestPromptContainsFixAuditRules(t *testing.T) {
	prompt := BuildPrompt(nil)
	for _, want := range []string{
		"FIX-AUDIT candidates are OPEN auto-fix beads",
		`Never close on a verdict alone`,
		`"contradicted"`,
		"original verdict is hollow",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt missing %q", want)
		}
	}
}