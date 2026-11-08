package core

import (
	"fmt"
	"log"
	"sync"
	"time"
)

// GovernorConfig configures the AIMD concurrency governor.
type GovernorConfig struct {
	Enabled           bool    `yaml:"enabled"`
	WindowSize        int     `yaml:"window_size"`
	DecreaseThreshold float64 `yaml:"decrease_threshold"`
	IncreaseThreshold float64 `yaml:"increase_threshold"`
	// CooldownSeconds is the minimum seconds between limit changes. Default 120.
	CooldownSeconds int `yaml:"cooldown_seconds"`
	// RecoveryIntervalSeconds is how long the fleet must go without a
	// provider-pressure failure before the limit climbs by one, independently of
	// how many outcomes have been observed. Default 300.
	RecoveryIntervalSeconds int `yaml:"recovery_interval_seconds"`
	// MemoryPressureThreshold is the cgroup memory-pressure percentage (PSI
	// "some" avg10, or 100 when memory.current reaches memory.high) at which the
	// governor multiplicatively halves the limit. 0 takes the default (50);
	// negative disables the memory feedback loop. See memory_pressure.go.
	MemoryPressureThreshold float64 `yaml:"memory_pressure_threshold"`
}

// applyGovernorDefaults fills in zero values with the documented defaults.
func applyGovernorDefaults(cfg *GovernorConfig) {
	if cfg.WindowSize <= 0 {
		cfg.WindowSize = 8
	}
	if cfg.DecreaseThreshold <= 0 {
		cfg.DecreaseThreshold = 0.5
	}
	if cfg.IncreaseThreshold <= 0 {
		cfg.IncreaseThreshold = 0.125
	}
	if cfg.CooldownSeconds <= 0 {
		cfg.CooldownSeconds = 120
	}
	if cfg.RecoveryIntervalSeconds <= 0 {
		cfg.RecoveryIntervalSeconds = 300
	}
	if cfg.MemoryPressureThreshold == 0 {
		cfg.MemoryPressureThreshold = 50
	}
	// Negative thresholds leave the memory feedback disabled (memThreshold < 0).
}

// ConcurrencyGovernor implements AIMD (additive-increase/multiplicative-decrease)
// concurrency control based on observed worker outcomes. A nil pointer is safe
// to call — callers never need to nil-check.
type ConcurrencyGovernor struct {
	mu           sync.Mutex
	maxWorkers   int
	limit        int
	window       []bool
	windowSize   int
	decThreshold float64
	incThreshold float64
	cooldown     time.Duration
	recovery     time.Duration
	lastChange   time.Time
	// lastFailure is when we last saw provider pressure. Recovery is timed from
	// this rather than from outcome volume, because outcome volume is exactly
	// what a lowered limit suppresses.
	lastFailure time.Time
	onChange    func(old, new int, reason string)
	events      *EventSink // AIMD scale events (sensor symmetry); nil-safe
	now         func() time.Time
	// Memory throttle feedback (see memory_pressure.go). memThreshold is the
	// PSI/current-vs-high percentage that triggers a multiplicative scale-down;
	// negative disables. psiPath/curPath/highPath override the cgroup probe
	// paths (tests); empty = the daemon service slice defaults.
	memThreshold float64
	psiPath      string
	curPath      string
	highPath     string
}

// NewConcurrencyGovernor creates a governor with the supplied configuration.
// The config defaults are applied here. When maxWorkers <= 0 the governor is
// disabled: Limit() returns 0 and RecordOutcome is a no-op.
func NewConcurrencyGovernor(maxWorkers int, cfg GovernorConfig) *ConcurrencyGovernor {
	applyGovernorDefaults(&cfg)
	return &ConcurrencyGovernor{
		maxWorkers:   maxWorkers,
		limit:        maxWorkers,
		window:       make([]bool, 0, cfg.WindowSize),
		windowSize:   cfg.WindowSize,
		decThreshold: cfg.DecreaseThreshold,
		incThreshold: cfg.IncreaseThreshold,
		cooldown:     time.Duration(cfg.CooldownSeconds) * time.Second,
		recovery:     time.Duration(cfg.RecoveryIntervalSeconds) * time.Second,
		lastChange:   time.Time{},
		onChange:     nil,
		now:          time.Now,
		memThreshold: cfg.MemoryPressureThreshold,
	}
}

// SetMaxWorkers updates the concurrency ceiling at runtime (e.g. after a
// runtime max_workers bump via the API or config reload). Nil-safe.
//
// A decrease clamps the current limit down so the new ceiling takes effect
// immediately. An increase raises the limit to the new ceiling immediately:
// waiting for AIMD additive increase would leave the operator's bump ineffective
// for the full cooldown/recovery window (observed 2026-09-24: runtime bump
// 8->12 kept slots at 8 because the governor kept its construction-time limit).
func (g *ConcurrencyGovernor) SetMaxWorkers(count int) {
	if g == nil {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	old := g.limit
	g.maxWorkers = count
	if count <= 0 {
		// Governor disabled; keep the limit pinned at zero for consistency
		// with Limit()'s disabled semantics.
		g.limit = 0
		return
	}
	if g.limit > count {
		g.limit = count
	} else if g.limit < count && old < count {
		g.limit = count
	}
	if g.limit != old {
		g.lastChange = g.now()
		g.window = g.window[:0]
		g.emitChangeLocked(old, g.limit, "max_workers_updated")
	}
}

// SetOnChange sets the hook called on every limit change. Nil-safe.
func (g *ConcurrencyGovernor) SetOnChange(fn func(old, new int, reason string)) {
	if g == nil {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.onChange = fn
}

// SetNow sets the time source (used by tests). Nil-safe.
func (g *ConcurrencyGovernor) SetNow(fn func() time.Time) {
	if g == nil {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.now = fn
}

// Limit returns the current effective concurrency limit. Safe to call on a
// nil receiver.
func (g *ConcurrencyGovernor) Limit() int {
	if g == nil {
		return 0
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.limit
}

// RecordOutcome records one worker completion outcome. Safe to call on a nil
// receiver. Only provider-shaped outcomes (success, transient failure) should be
// passed by the caller.
func (g *ConcurrencyGovernor) RecordOutcome(success bool) {
	if g == nil || g.maxWorkers <= 0 {
		return
	}

	g.mu.Lock()
	defer g.mu.Unlock()

	g.window = append(g.window, success)
	if len(g.window) > g.windowSize {
		g.window = g.window[len(g.window)-g.windowSize:]
	}

	if !success {
		g.lastFailure = g.now()
	}

	if len(g.window) < g.windowSize {
		return
	}

	failures := 0
	for _, ok := range g.window {
		if !ok {
			failures++
		}
	}
	fraction := float64(failures) / float64(g.windowSize)
	now := g.now()

	if fraction >= g.decThreshold {
		old := g.limit
		// Step down by one, not by half. Halving overshoots at the small limits we
		// actually run at: on 2026-07-31 a single one-hour provider capacity window
		// took the limit 4 -> 2 -> 1, and one is barely distinguishable from
		// stopped. Walking down one at a time finds the real ceiling instead of
		// diving past it.
		newLimit := old - 1
		if newLimit < 1 {
			newLimit = 1
		}
		if newLimit != old {
			g.limit = newLimit
			g.lastChange = now
			g.window = g.window[:0]
			g.emitChangeLocked(old, newLimit, "additive_decrease")
		}
		return
	}

	if fraction <= g.incThreshold {
		if now.Sub(g.lastChange) < g.cooldown && !g.lastChange.IsZero() {
			return
		}
		old := g.limit
		newLimit := old + 1
		if newLimit > g.maxWorkers {
			newLimit = g.maxWorkers
		}
		if newLimit != old {
			g.limit = newLimit
			g.lastChange = now
			g.window = g.window[:0]
			g.emitChangeLocked(old, newLimit, "additive_increase")
		}
	}
}

// MaybeRecover raises the limit by one when the fleet has gone quiet — no
// provider-pressure failure for a full recovery interval — regardless of how
// many outcomes have been observed. Safe on a nil receiver. Returns true when
// the limit changed.
//
// This exists because AIMD recovery gated purely on a full outcome window cannot
// recover from a deep cut: a lower limit produces fewer completions, so the
// window never refills, so the limit never rises. Measured on 2026-07-31 — one
// provider capacity window at 02:00 took the limit to 1, and it stayed there for
// the remaining 2.5 hours on a single completion, having no way to learn that
// the provider had recovered. The caller drives this from the scheduler tick,
// because when the fleet is starved there is no outcome to drive it from.
func (g *ConcurrencyGovernor) MaybeRecover() bool {
	if g == nil || g.maxWorkers <= 0 {
		return false
	}

	g.mu.Lock()
	defer g.mu.Unlock()

	if g.limit >= g.maxWorkers {
		return false
	}

	now := g.now()
	// Time the quiet period from the later of the last failure and the last
	// change, so recovery never stacks two steps inside one interval.
	since := g.lastFailure
	if g.lastChange.After(since) {
		since = g.lastChange
	}
	if since.IsZero() || now.Sub(since) < g.recovery {
		return false
	}

	old := g.limit
	g.limit = old + 1
	g.lastChange = now
	g.window = g.window[:0]
	g.emitChangeLocked(old, g.limit, "quiet_recovery")
	return true
}

func (g *ConcurrencyGovernor) SetEventSink(s *EventSink) {
	if g == nil {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.events = s
}

func (g *ConcurrencyGovernor) emitChangeLocked(old, new int, reason string) {
	if g.onChange != nil {
		g.onChange(old, new, reason)
	}
	log.Printf("[Governor] concurrency %d -> %d (%s)", old, new, reason)
	// Sensor symmetry: aimd_scale_up/aimd_scale_down are a symmetric
	// fire/resolve pair — a scale-down fire is resolved by the next
	// scale-up, and vice versa, under the shared governor correlation ID.
	if g.events == nil {
		return
	}
	switch reason {
	case "additive_increase", "quiet_recovery":
		g.events.Emit(Event{Type: "aimd_scale_up", Reason: reason,
			Detail: fmt.Sprintf("AIMD concurrency %d -> %d (%s)", old, new, reason),
			Fields: map[string]any{"old": old, "new": new, "correlation_id": SensorCorrelation("aimd", "governor")}})
	case "additive_decrease", "memory_throttle":
		g.events.Emit(Event{Type: "aimd_scale_down", Reason: reason,
			Detail: fmt.Sprintf("AIMD concurrency %d -> %d (%s)", old, new, reason),
			Fields: map[string]any{"old": old, "new": new, "correlation_id": SensorCorrelation("aimd", "governor")}})
	}
}

// Describe returns the governor's resolved settings for logging. Reading these
// off the struct rather than the caller's config block is deliberate: defaults
// are applied to a by-value copy inside the constructor, so the caller's block
// still holds zeros for anything it omitted.
func (g *ConcurrencyGovernor) Describe() string {
	if g == nil {
		return "disabled"
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.maxWorkers <= 0 {
		return "disabled (unlimited max_workers)"
	}
	return fmt.Sprintf("limit=%d/%d window=%d decrease=%.3f increase=%.3f cooldown=%s recovery=%s",
		g.limit, g.maxWorkers, g.windowSize, g.decThreshold, g.incThreshold, g.cooldown, g.recovery)
}
