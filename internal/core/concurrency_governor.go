package core

import (
	"log"
	"math"
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
	lastChange   time.Time
	onChange     func(old, new int, reason string)
	now          func() time.Time
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
		lastChange:   time.Time{},
		onChange:     nil,
		now:          time.Now,
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
		newLimit := int(math.Ceil(float64(old) / 2.0))
		if newLimit < 1 {
			newLimit = 1
		}
		if newLimit != old {
			g.limit = newLimit
			g.lastChange = now
			g.window = g.window[:0]
			g.emitChangeLocked(old, newLimit, "multiplicative_decrease")
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

func (g *ConcurrencyGovernor) emitChangeLocked(old, new int, reason string) {
	if g.onChange != nil {
		g.onChange(old, new, reason)
	}
	log.Printf("[Governor] concurrency %d -> %d (%s)", old, new, reason)
}
