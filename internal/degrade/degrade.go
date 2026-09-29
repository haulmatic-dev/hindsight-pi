// Package degrade provides in-memory attempt/success counters for fail-open
// subsystems (FR-032). Fail-open paths swallow errors by design — without a
// counter, a subsystem at 0% success (e.g. the hindsight-recall 500ms timeout
// bug, bead hydra-bye7) is invisible because nobody greps routine logs.
//
// Usage: RecordAttempt before the risky call, RecordSuccess when it returns
// nil error. Snapshot() renders Status entries; a subsystem is Degraded when
// it made >= minAttemptsForDegraded attempts with zero successes inside the
// window. The Pan health digest (internal/core/pan_health.go) snapshots the
// registry every tick and surfaces degraded subsystems in the digest.
//
// Counters are process-local. Daemon-side subsystems (pan store scans)
// appear in the digest directly; worker-process subsystems (hindsight
// recall/retain) count locally and are a known open edge for cross-process
// reporting.
package degrade

import (
	"os"
	"sort"
	"strconv"
	"sync"
	"time"
)

// DefaultWindow is the degradation evaluation window (PRD FR-032: 24h).
var DefaultWindow = 24 * time.Hour

// minAttemptsForDegraded is the smallest attempt count that may flag a
// subsystem degraded — one unlucky call is noise, not degradation.
const minAttemptsForDegraded = 3

// MinAttemptsForDegraded exposes the threshold (tests, callers rendering
// counter semantics).
func MinAttemptsForDegraded() int { return minAttemptsForDegraded }

// Status is a point-in-time counter snapshot for one subsystem.
type Status struct {
	Name      string
	Attempts  int64
	Successes int64
	Window    time.Duration
	First     time.Time // first attempt inside the current window
	Last      time.Time // last attempt (success or failure)
}

// Degraded reports 0% success over minAttemptsForDegraded+ attempts.
func (s Status) Degraded() bool {
	return s.Attempts >= minAttemptsForDegraded && s.Successes == 0
}

// String renders the digest line fragment: "name 0/12".
func (s Status) String() string {
	return s.Name + " " + strconv.FormatInt(s.Successes, 10) + "/" + strconv.FormatInt(s.Attempts, 10)
}

type entry struct {
	attempts  int64
	successes int64
	first     time.Time
	last      time.Time
}

// Registry holds per-subsystem counters. All methods are safe for
// concurrent use and never panic on a zero-value registry.
type Registry struct {
	mu      sync.Mutex
	entries map[string]*entry
	window  time.Duration
}

// NewRegistry returns a registry with the supplied evaluation window.
func NewRegistry(window time.Duration) *Registry {
	if window <= 0 {
		window = DefaultWindow
	}
	return &Registry{entries: map[string]*entry{}, window: window}
}

// windowFromEnv reads HYDRA_DEGRADE_WINDOW (Go duration string); falls back
// to DefaultWindow on parse failure — configuration must never gate telemetry.
func windowFromEnv() time.Duration {
	if v := os.Getenv("HYDRA_DEGRADE_WINDOW"); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			return d
		}
	}
	return DefaultWindow
}

var global = NewRegistry(windowFromEnv())

// RecordAttempt counts one fail-open call of the named subsystem in the
// process-global registry. Windows tumble: once the entry's window elapses
// the counters reset, so the snapshot always reflects the most recent
// window rather than a lifetime total.
func RecordAttempt(name string) { global.RecordAttempt(name) }

// RecordSuccess counts one successful call of the named subsystem.
func RecordSuccess(name string) { global.RecordSuccess(name) }

// Snapshot returns the current counters, sorted by name, from the
// process-global registry.
func Snapshot() []Status { return global.Snapshot() }

// Reset clears the process-global registry (test isolation; safe to call
// concurrently — the next attempt recreates the entry).
func Reset() { global.Reset() }

// RecordAttempt is the registry-scoped form of the package function.
func (r *Registry) RecordAttempt(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	e := r.entries[name]
	now := time.Now()
	if e == nil {
		r.entries[name] = &entry{attempts: 1, first: now, last: now}
		return
	}
	if now.Sub(e.first) > r.window {
		*e = entry{attempts: 1, first: now, last: now}
		return
	}
	e.attempts++
	e.last = now
}

// RecordSuccess is the registry-scoped form of the package function.
func (r *Registry) RecordSuccess(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	e := r.entries[name]
	now := time.Now()
	if e == nil {
		r.entries[name] = &entry{successes: 1, attempts: 0, first: now, last: now}
		return
	}
	if now.Sub(e.first) > r.window {
		*e = entry{successes: 1, first: now, last: now}
		return
	}
	e.successes++
	e.last = now
}

// Snapshot is the registry-scoped form of the package function.
func (r *Registry) Snapshot() []Status {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Status, 0, len(r.entries))
	for name, e := range r.entries {
		out = append(out, Status{
			Name:      name,
			Attempts:  e.attempts,
			Successes: e.successes,
			Window:    r.window,
			First:     e.first,
			Last:      e.last,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Reset clears all entries.
func (r *Registry) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.entries = map[string]*entry{}
}
