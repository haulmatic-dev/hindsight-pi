package core

// IntegrationBreaker (FR-034) is the fleet-wide circuit breaker for
// integration-exhausted hard-parks. Individually, each exhausted bead is
// handled correctly (durable label, respawn paths skip it), but a shared
// integration wedge — stale checkout branch, wrong PR base, contested
// worktree — parks bead after bead in minutes with no signal above the
// per-bead noise (2026-09-29 srv-telematics: 11 beads in one window).
//
// The breaker watches the same failure stream failIntegration produces:
// every `integration-exhausted` hard-park is recorded per project inside a
// sliding window. When a project crosses the threshold within the window it
// emits ONE typed `integration-wedge` event naming every bead parked inside
// the window (fail-open observability), and — only when explicitly enabled
// via `overseer.integration_breaker.park_project` — parks the project so
// the scheduler stops feeding more beads into the same wedge.
//
// The breaker never blocks or mutates the close path: RecordExhaustion is
// called after the per-bead hard-park is already applied and returns
// nothing. Nil-safe: a nil breaker or nil event sink is a no-op.

import (
	"fmt"
	"log"
	"strings"
	"sync"
	"time"
)

// IntegrationBreakerConfig tunes the fleet-wide integration-failure circuit
// breaker. Zero value = disabled; DefaultConfig enables observability-only
// breaking (threshold 5 hard-parks / 30m window). park_project defaults
// false: parking a whole project strands unrelated ready beads, so it is a
// deliberate operator opt-in.
type IntegrationBreakerConfig struct {
	// Enabled turns the breaker on. False (zero value) makes it inert.
	Enabled bool `yaml:"enabled"`
	// Threshold is the number of integration-exhausted hard-parks within
	// Window that constitute a project-wide wedge.
	Threshold int `yaml:"threshold"`
	// Window is the sliding evaluation window.
	Window time.Duration `yaml:"window"`
	// ParkProject additionally parks the project (scheduler stops spawning
	// into the wedge) when the breaker trips. Off by default.
	ParkProject bool `yaml:"park_project"`
}

// IntegrationBreaker aggregates integration-exhausted hard-parks per project
// and emits one wedge event when a project crosses the threshold.
type IntegrationBreaker struct {
	mu          sync.Mutex
	cfg         IntegrationBreakerConfig
	events      *EventSink                         // nil-safe
	parkProject func(project, reason string) error // nil-safe; wired from the registry store when ParkProject is set
	exhausted   map[string][]time.Time             // project -> hard-park timestamps inside the window
	beadTimes   map[string]map[string]time.Time    // project -> bead ID -> last hard-park time
	wedgedAt    map[string]time.Time               // project -> last wedge signal (one per window)
}

// NewIntegrationBreaker constructs a breaker. events and parkProject may be
// nil; the breaker still counts (harmless) but cannot signal.
func NewIntegrationBreaker(cfg IntegrationBreakerConfig, events *EventSink, parkProject func(project, reason string) error) *IntegrationBreaker {
	return &IntegrationBreaker{
		cfg:         cfg,
		events:      events,
		parkProject: parkProject,
		exhausted:   map[string][]time.Time{},
		beadTimes:   map[string]map[string]time.Time{},
		wedgedAt:    map[string]time.Time{},
	}
}

// setParkProject attaches the optional park callback after construction
// (the registry store opens later than the overseer, in service_starter).
func (b *IntegrationBreaker) setParkProject(fn func(project, reason string) error) {
	b.mu.Lock()
	b.parkProject = fn
	b.mu.Unlock()
}

// setEventSink attaches the structured event stream after construction
// (the sink is created after the overseer's bead closer).
func (b *IntegrationBreaker) setEventSink(s *EventSink) {
	b.mu.Lock()
	b.events = s
	b.mu.Unlock()
}

// Enabled reports whether the breaker will act on recorded exhaustions.
func (b *IntegrationBreaker) Enabled() bool { return b != nil && b.cfg.Enabled }

// RecordExhaustion records one integration-exhausted hard-park for project
// and trips the breaker when the project crosses the threshold inside the
// window. Fail-open by contract: no error, no panic, close path unaffected.
func (b *IntegrationBreaker) RecordExhaustion(project, beadID, stage, detail string) {
	if !b.Enabled() || project == "" {
		return
	}
	now := time.Now()
	b.mu.Lock()
	times := append(b.exhausted[project], now)
	window := b.cfg.Window
	if window <= 0 {
		window = 30 * time.Minute
	}
	kept := times[:0]
	for _, t := range times {
		if now.Sub(t) <= window {
			kept = append(kept, t)
		}
	}
	b.exhausted[project] = kept
	if b.beadTimes[project] == nil {
		b.beadTimes[project] = map[string]time.Time{}
	}
	b.beadTimes[project][beadID] = now
	threshold := b.cfg.Threshold
	if threshold <= 0 {
		threshold = 5
	}
	tripped := len(kept) >= threshold
	alreadyWedged := false
	if last, ok := b.wedgedAt[project]; ok && now.Sub(last) <= window {
		alreadyWedged = true
	}
	if tripped && !alreadyWedged {
		b.wedgedAt[project] = now
		// Expire the window for the wedged project: after the signal fires the
		// per-window count restarts, so a wedge that persists past the window
		// (or recurs in a later one) re-signals instead of latching forever.
		b.exhausted[project] = nil
	}
	beads := b.beadsLocked(project, now, window)
	park := tripped && !alreadyWedged && b.cfg.ParkProject && b.parkProject != nil
	b.mu.Unlock()

	if !tripped || alreadyWedged {
		return
	}

	reason := fmt.Sprintf("integration wedge: %d beads hard-parked integration-exhausted within %s (threshold %d)",
		len(beads), window, threshold)
	if b.events != nil {
		b.events.EmitRepo(project, Event{Type: "integration-wedge", Project: project,
			Bead: beadID, Reason: "fleet_wedge_detected",
			Detail: fmt.Sprintf("%s — last bead %s at stage %s: %s", reason, beadID, stage, truncateForEvent(detail, 200)),
			Fields: map[string]any{"beads": beads, "threshold": threshold, "window": window.String()}})
	}
	log.Printf("[IntegrationBreaker] %s — %s; per-bead parks stand, triage the shared integration cause before clearing", project, reason)
	if park {
		if err := b.parkProject(project, reason); err != nil {
			log.Printf("[IntegrationBreaker] project park for %s failed (fail-open): %v", project, err)
		} else {
			log.Printf("[IntegrationBreaker] parked project %s pending integration-wedge triage", project)
		}
	}
}

// beadsLocked returns the distinct bead IDs recorded inside the window.
// Callers must hold b.mu so the wedge event names what it observed.
func (b *IntegrationBreaker) beadsLocked(project string, now time.Time, window time.Duration) []string {
	var out []string
	for id, ts := range b.beadTimes[project] {
		if now.Sub(ts) <= window {
			out = append(out, id)
		}
	}
	return out
}

// truncateForEvent bounds a failure detail so the wedge event stays a signal,
// not a log dump. The per-bead integration-exhausted event already carries
// the full detail.
func truncateForEvent(s string, n int) string {
	if len(s) <= n {
		return strings.TrimSpace(s)
	}
	return strings.TrimSpace(s[:n]) + "…"
}
