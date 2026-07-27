package main

import (
	"errors"
	"testing"
)

// The exact payloads observed live on 2026-07-27 when the Kimi quota was
// exhausted mid-run. Both were classified as generic code failures (exit 1),
// which spawned a fix bead per affected bead and retried each one 4x.
const (
	kimiQuota403    = `403 {"error":{"type":"permission_error","message":"You've reached your usage limit for this billing cycle. Your quota will be refreshed in the next cycle. To continue now, purchase extra usage or upgrade your plan: https://www.kimi.com/code/#pricing"}}`
	kimiQuota403Alt = `403: {"message":"You've reached your usage limit for this billing cycle.","type":"access_terminated_error"}`
)

func TestIsQuotaError(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want bool
	}{
		{"live kimi 403 permission_error", kimiQuota403, true},
		{"live kimi 403 access_terminated", kimiQuota403Alt, true},
		{"insufficient_quota", `{"code":"insufficient_quota"}`, true},
		{"upgrade your plan prose", "please upgrade your plan to continue", true},
		{"case insensitive", "USAGE LIMIT reached", true},
		{"build failure is not quota", "./main.go:12:2: undefined: Foo", false},
		{"test failure is not quota", "--- FAIL: TestThing (0.01s)", false},
		{"plain 403 without quota wording", "HTTP 403 forbidden by proxy", false},
		{"empty", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isQuotaError(tc.in); got != tc.want {
				t.Errorf("isQuotaError(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

// Quota exhaustion classifies as exit code 6: infrastructure (so it never
// spawns a fix bead) and distinct from 2/rate-limit, so the daemon can apply a
// long cooldown instead of a pointless provider failover. A 429 clears in
// seconds; a billing limit clears on cycle rollover.
func TestClassifyFailure_QuotaIsItsOwnInfraCode(t *testing.T) {
	for _, payload := range []string{kimiQuota403, kimiQuota403Alt} {
		got := classifyFailure(&ExecutionResult{Success: false, Error: payload})
		if got != 6 {
			t.Errorf("classifyFailure(quota) = %d, want 6 (quota cooldown); payload: %.60s", got, payload)
		}
	}
}

func TestClassifyFailure_CodeFailureStillOne(t *testing.T) {
	got := classifyFailure(&ExecutionResult{Success: false, Error: "./main.go:12:2: undefined: Foo"})
	if got != 1 {
		t.Errorf("classifyFailure(build error) = %d, want 1 (code failure)", got)
	}
}

func TestClassifyFailure_RateLimitUnchanged(t *testing.T) {
	got := classifyFailure(&ExecutionResult{Success: false, Error: "429 rate_limit exceeded"})
	if got != 2 {
		t.Errorf("classifyFailure(429) = %d, want 2", got)
	}
}

// Retrying an exhausted quota only produces more rejected calls against the
// quota that is already gone — the live incident retried 4x per bead.
func TestShouldRetry_QuotaNotRetried(t *testing.T) {
	policy := RetryPolicy{MaxRetries: 3}
	if shouldRetry(errors.New(kimiQuota403), 1, policy) {
		t.Error("quota error must not be retried")
	}
	if shouldRetry(errors.New(kimiQuota403Alt), 1, policy) {
		t.Error("access_terminated quota error must not be retried")
	}
}

func TestShouldRetry_TransientStillRetried(t *testing.T) {
	policy := RetryPolicy{MaxRetries: 3}
	if !shouldRetry(errors.New("connection reset by peer"), 1, policy) {
		t.Error("transient network error should still be retried")
	}
}
