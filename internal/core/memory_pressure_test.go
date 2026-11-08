package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParsePSIAvg10(t *testing.T) {
	cases := []struct {
		in   string
		want float64
	}{
		{"some avg10=0.00 avg60=0.00 avg300=0.00 total=73843605\nfull avg10=1.50 avg60=2.00 avg300=0.00 total=64156148\n", 0},
		{"some avg10=37.25 avg60=12.00 avg300=0.00 total=1\n", 37.25},
		{"full avg10=99.99 avg60=0.00\n", 0},    // no "some" line
		{"some avg10=abc avg60=0.00\n", 0},      // malformed
		{"", 0},                                 // empty
		{"some avg10=100.00 avg60=0.00\n", 100}, // saturated
	}
	for _, c := range cases {
		if got := parsePSIAvg10(c.in); got != c.want {
			t.Errorf("parsePSIAvg10(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestParseMemBytes(t *testing.T) {
	if got := parseMemBytes("max\n"); got != 0 {
		t.Errorf("max should be 0, got %d", got)
	}
	if got := parseMemBytes("17179869184\n"); got != 17179869184 {
		t.Errorf("numeric parse wrong: %d", got)
	}
	if got := parseMemBytes(""); got != 0 {
		t.Errorf("empty should be 0, got %d", got)
	}
}

func TestMemoryThrottlePressure(t *testing.T) {
	dir := t.TempDir()
	psi := filepath.Join(dir, "memory.pressure")
	cur := filepath.Join(dir, "memory.current")
	high := filepath.Join(dir, "memory.high")

	// Missing files: fail-open, no signal.
	if got := memoryThrottlePressure(psi, cur, high); got != 0 {
		t.Errorf("missing files should report 0, got %v", got)
	}

	os.WriteFile(psi, []byte("some avg10=42.00 avg60=0.00\n"), 0o644)
	os.WriteFile(cur, []byte("1000\n"), 0o644)
	os.WriteFile(high, []byte("max\n"), 0o644)
	if got := memoryThrottlePressure(psi, cur, high); got != 42 {
		t.Errorf("psi-only signal = %v, want 42", got)
	}

	// current >= high overrides to 100 even with low PSI.
	os.WriteFile(cur, []byte("16000000000\n"), 0o644)
	os.WriteFile(high, []byte("16000000000\n"), 0o644)
	os.WriteFile(psi, []byte("some avg10=0.00\n"), 0o644)
	if got := memoryThrottlePressure(psi, cur, high); got != 100 {
		t.Errorf("at memory.high = %v, want 100", got)
	}
}

func newMemPressureGovernor(t *testing.T, psi, cur, high string) *ConcurrencyGovernor {
	t.Helper()
	g := NewConcurrencyGovernor(10, GovernorConfig{Enabled: true})
	g.SetPressurePaths(psi, cur, high)
	return g
}

func writePressureFixture(t *testing.T, psiVal string, curVal, highVal string) (psi, cur, high string) {
	t.Helper()
	dir := t.TempDir()
	psi = filepath.Join(dir, "memory.pressure")
	cur = filepath.Join(dir, "memory.current")
	high = filepath.Join(dir, "memory.high")
	os.WriteFile(psi, []byte(psiVal), 0o644)
	os.WriteFile(cur, []byte(curVal), 0o644)
	os.WriteFile(high, []byte(highVal), 0o644)
	return psi, cur, high
}

func TestMaybeScaleDownForMemoryPressure_HalvesLimit(t *testing.T) {
	psi, cur, high := writePressureFixture(t,
		"some avg10=80.00 avg60=0.00\n", "1000\n", "max\n")
	g := newMemPressureGovernor(t, psi, cur, high)
	if !g.MaybeScaleDownForMemoryPressure() {
		t.Fatal("expected scale down at 80% pressure")
	}
	if got := g.Limit(); got != 5 {
		t.Errorf("limit = %d, want 5 (halved from 10)", got)
	}
}

func TestMaybeScaleDownForMemoryPressure_BelowThresholdNoop(t *testing.T) {
	psi, cur, high := writePressureFixture(t,
		"some avg10=10.00 avg60=0.00\n", "1000\n", "max\n")
	g := newMemPressureGovernor(t, psi, cur, high)
	if g.MaybeScaleDownForMemoryPressure() {
		t.Fatal("10% pressure must not scale down")
	}
	if got := g.Limit(); got != 10 {
		t.Errorf("limit = %d, want 10", got)
	}
}

func TestMaybeScaleDownForMemoryPressure_DisabledNegativeThreshold(t *testing.T) {
	psi, cur, high := writePressureFixture(t,
		"some avg10=100.00 avg60=0.00\n", "16000000000\n", "16000000000\n")
	g := NewConcurrencyGovernor(10, GovernorConfig{Enabled: true, MemoryPressureThreshold: -1})
	g.SetPressurePaths(psi, cur, high)
	if g.MaybeScaleDownForMemoryPressure() {
		t.Fatal("negative threshold disables memory feedback")
	}
	if got := g.Limit(); got != 10 {
		t.Errorf("limit = %d, want 10", got)
	}
}

func TestMaybeScaleDownForMemoryPressure_FloorAtOne(t *testing.T) {
	psi, cur, high := writePressureFixture(t,
		"some avg10=90.00 avg60=0.00\n", "1000\n", "max\n")
	g := newMemPressureGovernor(t, psi, cur, high)
	g.mu.Lock()
	g.limit = 1
	g.mu.Unlock()
	if g.MaybeScaleDownForMemoryPressure() {
		t.Fatal("limit already at floor; no change expected")
	}
	if got := g.Limit(); got != 1 {
		t.Errorf("limit = %d, want 1", got)
	}
}

func TestMemoryThrottle_BlocksRecoveryThenRecovers(t *testing.T) {
	psi, cur, high := writePressureFixture(t,
		"some avg10=80.00 avg60=0.00\n", "1000\n", "max\n")
	g := NewConcurrencyGovernor(8, GovernorConfig{Enabled: true})
	g.SetPressurePaths(psi, cur, high)

	if !g.MaybeScaleDownForMemoryPressure() {
		t.Fatal("expected scale down")
	}
	if got := g.Limit(); got != 4 {
		t.Fatalf("limit = %d, want 4", got)
	}

	// While the throttle is fresh, MaybeRecover must not climb back — the
	// scale-down armed lastFailure, mirroring a provider-pressure failure.
	if g.MaybeRecover() {
		t.Fatal("recovery climbed immediately after memory throttle scale-down")
	}

	// Pressure clears and the recovery interval elapses → limit climbs.
	os.WriteFile(psi, []byte("some avg10=0.00 avg60=0.00\n"), 0o644)
	base := time.Now()
	g.SetNow(func() time.Time { return base.Add(g.recovery + time.Minute) })
	if !g.MaybeRecover() {
		t.Fatal("expected quiet recovery after pressure cleared and interval elapsed")
	}
	if got := g.Limit(); got != 5 {
		t.Errorf("limit = %d, want 5", got)
	}
}

func TestMemoryThrottle_EmitsAimdScaleDownEvent(t *testing.T) {
	psi, cur, high := writePressureFixture(t,
		"some avg10=75.00 avg60=0.00\n", "1000\n", "max\n")
	sinkDir := t.TempDir()
	events := NewEventSink(sinkDir)
	g := newMemPressureGovernor(t, psi, cur, high)
	g.SetEventSink(events)
	if !g.MaybeScaleDownForMemoryPressure() {
		t.Fatal("expected scale down")
	}
	found := false
	for _, ev := range readEventsToday(t, sinkDir) {
		if ev.Type == "aimd_scale_down" && strings.HasPrefix(ev.Reason, "memory_throttle") {
			found = true
		}
	}
	if !found {
		t.Fatal("expected aimd_scale_down event with reason memory_throttle")
	}
}
