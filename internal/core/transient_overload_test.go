package core

import (
	"bytes"
	"log"
	"strings"
	"testing"
)

// TestIsInfrastructureExitCode_IncludesTransientOverload: exit 7 must be treated
// as infrastructure so the bead is retried rather than blamed. A provider being
// momentarily overloaded is not a code defect and must never spawn a fix bead —
// that mistake previously produced 36 useless fix beads from quota errors.
func TestIsInfrastructureExitCode_IncludesTransientOverload(t *testing.T) {
	for _, code := range []int{2, 3, 4, 6, 7} {
		if !isInfrastructureExitCode(code) {
			t.Errorf("isInfrastructureExitCode(%d) = false, want true", code)
		}
	}
	// A genuine failure must stay a genuine failure.
	for _, code := range []int{1, 5} {
		if isInfrastructureExitCode(code) {
			t.Errorf("isInfrastructureExitCode(%d) = true, want false", code)
		}
	}
}

// TestProviderBreaker_SelfReferentialFallbackPausesRatherThanFailsOver pins the
// behaviour the deployed config actually produced.
//
// With fallback_provider equal to the primary there is nothing to fail over to,
// so an open circuit must translate into a spawn pause. The log used to claim
// "failing over to kimi-coding" while every worker was in fact frozen; the
// behaviour was right and the message was false.
func TestProviderBreaker_SelfReferentialFallbackPausesRatherThanFailsOver(t *testing.T) {
	b := NewProviderBreaker("kimi-coding", "m", "kimi-coding", "m")

	// Below threshold: nothing should be paused yet.
	b.RecordRateLimit()
	if paused, _ := b.ShouldPause(); paused {
		t.Fatal("one rate limit must not pause the fleet")
	}

	b.RecordRateLimit()
	b.RecordRateLimit()

	paused, remaining := b.ShouldPause()
	if !paused {
		t.Fatal("with no distinct fallback, an open circuit must pause spawning")
	}
	if remaining <= 0 {
		t.Errorf("remaining cooldown = %s, want positive", remaining)
	}

	// Override cannot rescue it: the fallback is the same provider.
	prov, _ := b.Override("kimi-coding", "m")
	if prov != "kimi-coding" {
		t.Errorf("Override returned %q; with a self-referential fallback it can only return the primary", prov)
	}
}

// TestProviderBreaker_DistinctFallbackDoesNotPause: when a real alternative
// exists, work continues on it instead of freezing.
func TestProviderBreaker_DistinctFallbackDoesNotPause(t *testing.T) {
	b := NewProviderBreaker("primary", "m1", "secondary", "m2")
	for i := 0; i < 3; i++ {
		b.RecordRateLimit()
	}
	if paused, _ := b.ShouldPause(); paused {
		t.Error("a distinct usable fallback means spawning must continue, not pause")
	}
	if prov, model := b.Override("primary", "m1"); prov != "secondary" || model != "m2" {
		t.Errorf("Override = %q/%q, want secondary/m2", prov, model)
	}
}

// TestProviderBreaker_LogsHonestlyWhenFallbackIsSelf pins the message, not just
// the behaviour.
//
// This assertion exists because the fix it guards silently failed to apply the
// first time: a string replacement with the wrong indentation was a no-op, the
// commit went out without it, and the claim survived into a PR description. A
// test would have caught that; a grep afterwards did.
func TestProviderBreaker_LogsHonestlyWhenFallbackIsSelf(t *testing.T) {
	var buf bytes.Buffer
	old := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(old)

	b := NewProviderBreaker("kimi-coding", "m", "kimi-coding", "m")
	for i := 0; i < 3; i++ {
		b.RecordRateLimit()
	}

	out := buf.String()
	if strings.Contains(out, "failing over to") {
		t.Errorf("must not announce a failover it cannot perform; got: %s", out)
	}
	if !strings.Contains(out, "nothing to fail over to") {
		t.Errorf("expected the log to name the real consequence; got: %s", out)
	}
}

// TestProviderBreaker_LogsFailoverWhenFallbackIsDistinct: the honest-log change
// must not mute the genuine failover message.
func TestProviderBreaker_LogsFailoverWhenFallbackIsDistinct(t *testing.T) {
	var buf bytes.Buffer
	old := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(old)

	b := NewProviderBreaker("primary", "m1", "secondary", "m2")
	for i := 0; i < 3; i++ {
		b.RecordRateLimit()
	}
	if out := buf.String(); !strings.Contains(out, "failing over to secondary") {
		t.Errorf("expected a real failover to be announced; got: %s", out)
	}
}
