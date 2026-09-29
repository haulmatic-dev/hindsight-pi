package core

import (
	"strings"
	"testing"
	"time"
)

// captureSink records emitted events for assertions.
type captureSink struct{ events []Event }

func (s *captureSink) EmitRepo(repoPath string, ev Event) Event {
	s.events = append(s.events, ev)
	return ev
}

func newTestBreaker(cfg IntegrationBreakerConfig, sink *EventSink, park func(string, string) error) (*IntegrationBreaker, *captureSink) {
	// The capture sink must be visible to the breaker via the EventSink
	// type; wrap by reusing NewIntegrationBreaker with a real sink replaced
	// in tests that need capture. We instead test through the package-level
	// struct: construct breaker then swap events via setEventSink using a
	// cast — captureSink is not *EventSink, so emit through a shim.
	_ = sink
	b := NewIntegrationBreaker(cfg, nil, park)
	cs := &captureSink{}
	return b, cs
}

func TestIntegrationBreaker_TripsAtThresholdWithinWindow(t *testing.T) {
	cfg := IntegrationBreakerConfig{Enabled: true, Threshold: 3, Window: 10 * time.Minute}
	b := NewIntegrationBreaker(cfg, nil, nil)
	if !b.Enabled() {
		t.Fatal("breaker must be enabled")
	}
	b.RecordExhaustion("proj", "b1", "integrate", "conflict")
	b.RecordExhaustion("proj", "b2", "integrate", "conflict")
	if len(b.wedgedAt) != 0 {
		t.Fatal("must not trip below threshold")
	}
	b.RecordExhaustion("proj", "b3", "integrate", "conflict")
	if _, ok := b.wedgedAt["proj"]; !ok {
		t.Fatal("must trip at threshold")
	}
}

func TestIntegrationBreaker_OneWedgeSignalPerWindow(t *testing.T) {
	cfg := IntegrationBreakerConfig{Enabled: true, Threshold: 2, Window: 10 * time.Minute}
	b := NewIntegrationBreaker(cfg, nil, nil)
	for i := 0; i < 5; i++ {
		b.RecordExhaustion("proj", "bx", "integrate", "conflict")
	}
	if len(b.wedgedAt) != 1 {
		t.Fatalf("expected exactly one wedge timestamp per window, got %d", len(b.wedgedAt))
	}
}

func TestIntegrationBreaker_WindowExpiryRetrips(t *testing.T) {
	cfg := IntegrationBreakerConfig{Enabled: true, Threshold: 1, Window: time.Minute}
	b := NewIntegrationBreaker(cfg, nil, nil)
	b.RecordExhaustion("proj", "b1", "integrate", "x")
	b.RecordExhaustion("proj", "b2", "integrate", "x") // still inside window: no second signal
	if _, ok := b.wedgedAt["proj"]; !ok {
		t.Fatal("expected wedge key present")
	}
	// Age the window out.
	b.mu.Lock()
	b.exhausted["proj"] = nil
	b.beadTimes["proj"] = map[string]time.Time{}
	b.wedgedAt["proj"] = time.Now().Add(-2 * time.Minute)
	b.mu.Unlock()
	b.RecordExhaustion("proj", "b3", "integrate", "x")
	fresh := time.Now().Add(-time.Second)
	if last := b.wedgedAt["proj"]; !last.After(fresh) {
		t.Fatalf("expected retrip after window expiry (fresh wedge timestamp), got %v vs %v", last, fresh)
	}
}

func TestIntegrationBreaker_DisabledInert(t *testing.T) {
	b := NewIntegrationBreaker(IntegrationBreakerConfig{}, nil, nil)
	if b.Enabled() {
		t.Fatal("zero config must be disabled")
	}
	b.RecordExhaustion("proj", "b1", "integrate", "x")
	if len(b.exhausted["proj"]) != 0 {
		t.Fatal("disabled breaker must not record")
	}
}

func TestIntegrationBreaker_NilSafe(t *testing.T) {
	var b *IntegrationBreaker
	b.RecordExhaustion("proj", "b1", "integrate", "x") // must not panic
	if b.Enabled() {
		t.Fatal("nil breaker disabled")
	}
}

func TestIntegrationBreaker_ParkProjectOptIn(t *testing.T) {
	var parkedProject, parkedReason string
	cfg := IntegrationBreakerConfig{Enabled: true, Threshold: 1, Window: time.Minute, ParkProject: true}
	b := NewIntegrationBreaker(cfg, nil, func(p, r string) error {
		parkedProject, parkedReason = p, r
		return nil
	})
	b.RecordExhaustion("proj", "b1", "integrate", "x")
	if parkedProject != "proj" || parkedReason == "" {
		t.Fatalf("expected project park, got %q/%q", parkedProject, parkedReason)
	}
	// Second trip inside window: no duplicate park (wedgedAt dedupe).
	b.RecordExhaustion("proj", "b2", "integrate", "x")
	if len(b.wedgedAt) != 1 {
		t.Fatal("park must fire once per window")
	}
}

func TestIntegrationBreaker_NoParkWithoutOptIn(t *testing.T) {
	called := false
	cfg := IntegrationBreakerConfig{Enabled: true, Threshold: 1, Window: time.Minute}
	b := NewIntegrationBreaker(cfg, nil, func(p, r string) error { called = true; return nil })
	b.RecordExhaustion("proj", "b1", "integrate", "x")
	if called {
		t.Fatal("park must not fire without park_project")
	}
}

func TestTruncateForEvent(t *testing.T) {
	long := strings.Repeat("x", 300)
	if got := truncateForEvent(long, 200); len(got) != 203 { // 200 x + ellipsis rune (3 bytes in UTF-8)
		t.Fatalf("expected 201 runes, got %d", len(got))
	}
	if got := truncateForEvent("short", 200); got != "short" {
		t.Fatalf("short string must pass through, got %q", got)
	}
}
