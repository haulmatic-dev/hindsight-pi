package main

import "testing"

// The exact payload observed live on 2026-07-30. Kimi reports a concurrency
// ceiling with the SAME HTTP status and error type as genuine quota exhaustion
// (403 / access_terminated_error), so the quota markers matched it and the
// daemon applied a 30-minute fleet-wide spawn pause — 37 times in one day,
// starving a dogfood run to roughly a 10% duty cycle while the account's quota
// was never touched. Only the message distinguishes the two.
const kimiConcurrency403 = `403 {"error":{"message":"You've reached your concurrent request limit. Please wait for your ongoing requests to finish and try again.","type":"access_terminated_error"}}`

func TestIsConcurrencyLimit(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want bool
	}{
		{"kimi live payload", kimiConcurrency403, true},
		{"lowercase variant", "error: concurrency limit reached", true},
		{"too many concurrent", "429 too many concurrent requests", true},
		{"max concurrent", "max concurrent sessions exceeded", true},
		// Must NOT swallow real quota exhaustion — that still needs the long
		// cooldown, because it clears when the billing cycle rolls over.
		{"real quota", kimiQuota403, false},
		{"real quota alt", kimiQuota403Alt, false},
		{"transient overload", "the engine is currently overloaded", false},
		{"code failure", "undefined: fooBar", false},
		{"empty", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isConcurrencyLimit(tc.in); got != tc.want {
				t.Errorf("isConcurrencyLimit(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

// TestClassifyFailure_ConcurrencyBeatsQuota is the regression that matters: the
// concurrency payload must classify as 7 (retryable capacity pressure that
// feeds the AIMD governor) and not 6 (quota, which pauses every spawn for 30
// minutes). Ordering is the whole fix — both predicates match this string, so
// whichever runs first decides the fleet's behaviour.
func TestClassifyFailure_ConcurrencyBeatsQuota(t *testing.T) {
	if got := classifyFailure(&ExecutionResult{Error: kimiConcurrency403}); got != 7 {
		t.Errorf("concurrency limit classified as exit %d, want 7 (governor, not breaker)", got)
	}
	// The quota path must be untouched by the reordering.
	if got := classifyFailure(&ExecutionResult{Error: kimiQuota403}); got != 6 {
		t.Errorf("real quota exhaustion classified as exit %d, want 6", got)
	}
	if got := classifyFailure(&ExecutionResult{Error: kimiQuota403Alt}); got != 6 {
		t.Errorf("real quota (alt payload) classified as exit %d, want 6", got)
	}
}

// Exit 7's downstream contract — infrastructure, so no fix bead, and never fed
// to the circuit breaker — is asserted daemon-side in
// internal/core/transient_overload_test.go. It lives there because
// isInfrastructureExitCode is in internal/core; this file only pins that the
// concurrency payload reaches that code path in the first place.
