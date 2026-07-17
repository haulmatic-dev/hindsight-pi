package core

import (
	"log"
	"sync"
	"time"
)

// ProviderBreaker fails worker LLM calls over to a configured fallback
// provider/model when the primary provider trips a rate-limit circuit
// breaker (>= threshold rate-limit failures within window -> open for
// openFor). Safe for concurrent use.
type ProviderBreaker struct {
	mu sync.Mutex

	primaryProvider  string
	primaryModel     string
	fallbackProvider string
	fallbackModel    string

	fails     map[string][]time.Time
	openUntil map[string]time.Time

	threshold int
	window    time.Duration
	openFor   time.Duration
}

// NewProviderBreaker builds a breaker. Defaults: 3 rate-limit failures
// within 10 min -> open 15 min. Fallback fields may be empty, which
// disables failover (the circuit is then never opened).
func NewProviderBreaker(primaryProvider, primaryModel, fallbackProvider, fallbackModel string) *ProviderBreaker {
	return &ProviderBreaker{
		primaryProvider:  primaryProvider,
		primaryModel:     primaryModel,
		fallbackProvider: fallbackProvider,
		fallbackModel:    fallbackModel,
		fails:            make(map[string][]time.Time),
		openUntil:        make(map[string]time.Time),
		threshold:        3,
		window:           10 * time.Minute,
		openFor:          15 * time.Minute,
	}
}

func (b *ProviderBreaker) isOpenLocked(provider string) bool {
	until, ok := b.openUntil[provider]
	return ok && time.Now().Before(until)
}

func (b *ProviderBreaker) chooseLocked() (string, string) {
	if b.primaryProvider == "" {
		return "", ""
	}
	if !b.isOpenLocked(b.primaryProvider) {
		return b.primaryProvider, b.primaryModel
	}
	if b.fallbackProvider != "" && !b.isOpenLocked(b.fallbackProvider) {
		return b.fallbackProvider, b.fallbackModel
	}
	// Both open (or no fallback): nothing better to offer than the primary.
	return b.primaryProvider, b.primaryModel
}

// Choose returns the provider/model to use for the next spawn, honoring an
// open primary circuit. Returns ("","") when no primary is configured so
// the caller keeps its own resolution.
func (b *ProviderBreaker) Choose() (string, string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.chooseLocked()
}

// Override returns the provider/model to use when a specific provider has
// already been selected (e.g. by phase routing). If the selected provider's
// circuit is open, it falls back to the configured fallback provider/model;
// otherwise it returns the selected pair unchanged. This keeps breaker
// failover from reverting phase-routed providers in normal operation.
func (b *ProviderBreaker) Override(provider, model string) (string, string) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if provider == "" {
		return b.chooseLocked()
	}

	if !b.isOpenLocked(provider) {
		return provider, model
	}

	if b.fallbackProvider != "" && !b.isOpenLocked(b.fallbackProvider) {
		log.Printf("[ProviderBreaker] provider %s circuit open — failing over to %s", provider, b.fallbackProvider)
		return b.fallbackProvider, b.fallbackModel
	}

	// Both selected and fallback are open (or no fallback): return the
	// selected provider/model unchanged so the caller keeps its choice.
	return provider, model
}

// RecordRateLimit records a rate-limit failure against the currently-active
// provider and opens its circuit once the threshold is reached within the
// window. No-op when no fallback is configured (nowhere to fail over to).
func (b *ProviderBreaker) RecordRateLimit() {
	b.mu.Lock()
	defer b.mu.Unlock()

	prov, _ := b.chooseLocked()
	if prov == "" {
		return
	}

	now := time.Now()
	cutoff := now.Add(-b.window)
	kept := b.fails[prov][:0]
	for _, t := range b.fails[prov] {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	kept = append(kept, now)
	b.fails[prov] = kept

	if len(kept) >= b.threshold && b.fallbackProvider != "" {
		b.openUntil[prov] = now.Add(b.openFor)
		log.Printf("[ProviderBreaker] provider %s tripped (%d rate-limits within %s) — open %s, failing over to %s",
			prov, len(kept), b.window, b.openFor, b.fallbackProvider)
	}
}
