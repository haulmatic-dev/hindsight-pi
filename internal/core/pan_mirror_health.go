package core

import (
	"errors"
	"time"

	"github.com/haulmatic-dev/hydra/internal/mirroraudit"
	"github.com/haulmatic-dev/hydra/pkg/types"
)

var errUnavailable = errors.New("mirror audit unavailable")

// mirrorDriftNeedsHumanEvent is the FR-4 Pan filing event on non-healable
// (user-owned) drift; declared so the contract tests compile in RED.
const mirrorDriftNeedsHumanEvent = "mirror_drift_needs_human"

// MirrorHealth periodic read-only audit sweep (PRD FR-2): every audit_interval
// the daemon re-derives the beads issues.jsonl mirror vs the br db per
// registered project store via the FR-1 shared mirroraudit package, emitting
// typed events (mirror_drift_detected / mirror_audit_clean) and failing open
// on any audit error.
//
// Contract GREEN must implement (all pinned in pan_mirror_health_test.go):
//   - sweepMirrorHealth iterates o.projects, honoring per-stop guards
//     (jsonl mtime quiescence, live-worker) and per-store classification.
//   - Typed events on drift and clean stores, mirror_audit_error on failures.
//   - Fail-open: errors never panic, never gate scheduling, never mutate
//     the store (read-only audit).
// Contract tests in pan_mirror_health_test.go skip loudly until GREEN
// flips mirrorResyncImplemented to true.

// mirrorResyncImplemented is false: the sweep body is inert (RED phase) and
// the contract tests in pan_mirror_health_test.go skip loudly.
const mirrorResyncImplemented = false

// Typed events the audit sweep emits (FR-2 contract).
const (
	mirrorDriftDetectedEvent = "mirror_drift_detected"
	mirrorAuditCleanEvent    = "mirror_audit_clean"
	mirrorAuditErrorEvent    = "mirror_audit_error"
)

// mirrorQuiescenceWindow skips stores whose jsonl was written too recently —
// never race a live br write.
const mirrorQuiescenceWindow = 2 * time.Minute

// defaultMirrorHealthInterval mirrors the PRD (30m, 0 = disabled).
const defaultMirrorHealthInterval = 30 * time.Minute

// probe seams (package-level so tests can override wholesale; production
// wiring installs the real impls from NewOverseer — GREEN's obligation).
var (
	// mirrorJSONLMtime stats the store's issues.jsonl mtime. ok=false when
	// the store is absent / unreadable (skip on error, fail-open).
	mirrorJSONLMtime = func(storePath string) (time.Time, bool) { return time.Time{}, false }
	// mirrorHasLiveWorker reports whether any live worker row belongs to the
	// project (live-worker skip contract).
	mirrorHasLiveWorker = func(projectName string) bool { return false }
	// mirrorAuditRunner runs the FR-1 mirroraudit.Audit for one store.
	// Inert default: the RED stub returns an unavailable error so nothing
	// runs and no events fire.
	mirrorAuditRunner = func(storePath string) (mirroraudit.Report, error) {
		return mirroraudit.Report{}, errUnavailable
	}
	// mirrorWriteProbe probes store write-eligibility for the guarded
	// auto-resync (FR-3). Inert default: probe succeeds (nil error).
	mirrorWriteProbe = func(storePath string) error { return nil }
)

// mirrorHealthSweepDue reports whether the audit sweep is due at now (used
// by the overseer tick, extracted here so the interval contract is unit
// testable). mirrors the lastStaleFilingSweep throttle contract.
func mirrorHealthSweepDue(o *Overseer, now time.Time) bool {
	if o == nil || o.config == nil || !o.config.Overseer.MirrorHealth.Enabled {
		return false
	}
	if o.config.Overseer.MirrorHealth.Interval <= 0 {
		return false
	}
	return now.Sub(o.lastMirrorHealthSweep) >= o.config.Overseer.MirrorHealth.Interval
}

// sweepMirrorHealth audits all registered project stores via the FR-1
// mirroraudit package, guarding for jsonl mtime quiescence and live workers
// (fail-open: any error is logged and the sweep continues, never gates
// scheduling). Inert until mirrorResyncImplemented flips true.
func (o *Overseer) sweepMirrorHealth(now time.Time) {
	if !mirrorResyncImplemented {
		return
	}
	for _, project := range o.projects {
		o.sweepMirrorHealthForProject(project, now)
	}
}

// sweepMirrorHealthForProject audits one project's store. Guards:
//   - jsonl mtime < mirrorQuiescenceWindow -> skip (join a live br write).
//   - live worker for the project -> skip.
//   - any audit error -> mirror_audit_error and continue.
func (o *Overseer) sweepMirrorHealthForProject(project types.Project, now time.Time) {
	if !mirrorResyncImplemented {
		return
	}
	_ = project
}
