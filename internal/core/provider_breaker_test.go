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

func TestProviderBreaker_EmptyPrimary(t *testing.T) {
	b := NewProviderBreaker("", "", "kimi", "k2")
	if p, m := b.Choose(); p != "" || m != "" {
		t.Fatalf("empty primary should return empty, got %s/%s", p, m)
	}
	b.RecordRateLimit() // must not panic
}
