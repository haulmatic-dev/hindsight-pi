package main

import "testing"

// TestClassifyFailure_TransientOverloadIsNotARateLimit pins the distinction that
// froze the fleet on 2026-07-28.
//
// Kimi returns "The engine is currently overloaded, please try again later" as a
// 429 with type rate_limit_error. Classified as a rate limit (exit 2) it fed the
// circuit breaker, three of them inside ten minutes opened the circuit for
// fifteen, and since fallback_provider equalled the primary there was nothing to
// fail over to — so the spawn gate paused every worker for 11m32s over an error
// whose own message says to retry shortly.
//
// Exit 7 keeps it retryable infrastructure while keeping it away from the breaker.
func TestClassifyFailure_TransientOverloadIsNotARateLimit(t *testing.T) {
	overload := &ExecutionResult{
		Success: false,
		Error:   `429 {"error":{"type":"rate_limit_error","message":"The engine is currently overloaded, please try again later"},"type":"error"}`,
	}
	if got := classifyFailure(overload); got != 7 {
		t.Errorf("transient overload classified as %d, want 7 (not 2 — it must not trip the provider circuit)", got)
	}
}

// TestClassifyFailure_RealRateLimitStillTripsTheBreaker: a genuine throughput
// rate limit — no capacity wording — must still classify as 2 so the breaker can
// back the fleet off. The fix must not blunt that.
func TestClassifyFailure_RealRateLimitStillTripsTheBreaker(t *testing.T) {
	cases := []string{
		`429 {"error":{"type":"rate_limit_error","message":"Too many requests"}}`,
		`rate_limit exceeded for this token plan`,
	}
	for _, msg := range cases {
		if got := classifyFailure(&ExecutionResult{Success: false, Error: msg}); got != 2 {
			t.Errorf("classifyFailure(%q) = %d, want 2", msg, got)
		}
	}
}

// TestClassifyFailure_QuotaStillWinsOverOverload: quota exhaustion is checked
// first and must keep exit 6 (a 30-minute cooldown), because a billing limit
// clears on cycle rollover, not on retry — even if the payload also mentions
// retrying.
func TestClassifyFailure_QuotaStillWinsOverOverload(t *testing.T) {
	msg := `403 {"error":{"type":"permission_error","message":"You have hit your usage limit for this billing cycle, try again later"}}`
	if got := classifyFailure(&ExecutionResult{Success: false, Error: msg}); got != 6 {
		t.Errorf("quota exhaustion classified as %d, want 6 — it must not be downgraded to a transient retry", got)
	}
}

func TestIsTransientOverload(t *testing.T) {
	yes := []string{
		"The engine is currently overloaded, please try again later",
		"503 Service Unavailable",
		"upstream overloaded",
	}
	for _, s := range yes {
		if !isTransientOverload(s) {
			t.Errorf("isTransientOverload(%q) = false, want true", s)
		}
	}
	no := []string{
		"Too many requests",
		"rate_limit exceeded",
		"compile error: undefined: foo",
		"",
	}
	for _, s := range no {
		if isTransientOverload(s) {
			t.Errorf("isTransientOverload(%q) = true, want false", s)
		}
	}
}
