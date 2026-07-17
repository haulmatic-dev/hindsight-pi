package core

import "testing"

func TestProviderBreaker_FailoverAfterThreshold(t *testing.T) {
	b := NewProviderBreaker("minimax", "MiniMax-M3", "kimi-coding-plan", "kimi-k2.7-code")

	if p, m := b.Choose(); p != "minimax" || m != "MiniMax-M3" {
		t.Fatalf("fresh breaker should choose primary, got %s/%s", p, m)
	}

	b.RecordRateLimit()
	b.RecordRateLimit()
	if p, _ := b.Choose(); p != "minimax" {
		t.Fatalf("below threshold should stay on primary, got %s", p)
	}

	b.RecordRateLimit() // 3rd within window -> trip
	if p, m := b.Choose(); p != "kimi-coding-plan" || m != "kimi-k2.7-code" {
		t.Fatalf("after threshold should fail over to fallback, got %s/%s", p, m)
	}
}

func TestProviderBreaker_NoFallbackNeverOpens(t *testing.T) {
	b := NewProviderBreaker("minimax", "MiniMax-M3", "", "")
	for i := 0; i < 5; i++ {
		b.RecordRateLimit()
	}
	if p, _ := b.Choose(); p != "minimax" {
		t.Fatalf("no fallback configured: must stay on primary, got %s", p)
	}
}

func TestProviderBreaker_Override_ChoosesSelectedWhenClosed(t *testing.T) {
	b := NewProviderBreaker("minimax", "MiniMax-M3", "kimi-coding-plan", "kimi-k2.7-code")
	if p, m := b.Override("p1", "m1"); p != "p1" || m != "m1" {
		t.Fatalf("closed circuit should keep selected provider, got %s/%s", p, m)
	}
}

func TestProviderBreaker_Override_FailoverWhenSelectedOpen(t *testing.T) {
	b := NewProviderBreaker("p1", "m1", "kimi-coding-plan", "kimi-k2.7-code")
	// Open the selected provider by recording rate-limits against it.
	for i := 0; i < 3; i++ {
		b.RecordRateLimit()
	}
	if p, m := b.Override("p1", "m1"); p != "kimi-coding-plan" || m != "kimi-k2.7-code" {
		t.Fatalf("open selected provider should fail over, got %s/%s", p, m)
	}
}

func TestProviderBreaker_Override_FallsBackToPrimaryWhenEmpty(t *testing.T) {
	b := NewProviderBreaker("minimax", "MiniMax-M3", "kimi-coding-plan", "kimi-k2.7-code")
	if p, m := b.Override("", ""); p != "minimax" || m != "MiniMax-M3" {
		t.Fatalf("empty selected provider should fall back to primary, got %s/%s", p, m)
	}
}

func TestProviderBreaker_Override_PrimaryOpenStillFallsBack(t *testing.T) {
	b := NewProviderBreaker("minimax", "MiniMax-M3", "kimi-coding-plan", "kimi-k2.7-code")
	for i := 0; i < 3; i++ {
		b.RecordRateLimit()
	}
	if p, m := b.Override("", ""); p != "kimi-coding-plan" || m != "kimi-k2.7-code" {
		t.Fatalf("open primary should fail over, got %s/%s", p, m)
	}
}

func TestProviderBreaker_EmptyPrimary(t *testing.T) {
	b := NewProviderBreaker("", "", "kimi", "k2")
	if p, m := b.Choose(); p != "" || m != "" {
		t.Fatalf("empty primary should return empty, got %s/%s", p, m)
	}
	b.RecordRateLimit() // must not panic
}
