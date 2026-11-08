package core

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/haulmatic-dev/hydra/internal/mirroraudit"
	"github.com/haulmatic-dev/hydra/pkg/types"
)

var errUnavailable = errors.New("mirror audit unavailable")

// mirrorDriftNeedsHumanEvent is the FR-4 Pan filing event on non-healable
// drift.
const mirrorDriftNeedsHumanEvent = "mirror_drift_needs_human"

// MirrorHealth periodic read-only audit sweep (PRD FR-2): every audit_interval
// the daemon re-derives the beads issues.jsonl mirror vs the br db per
// registered project store via the FR-1 shared mirroraudit package, emitting
// typed events (mirror_drift_detected / mirror_audit_clean) and failing open
// on any audit error.
//
// FR-3 guarded auto-resync (opt-in via mirror_health.auto_resync): a db_ahead
// record whose jsonl status is terminal is healed by writing the terminal
// status through the BrUpdater seam, ONLY when the store is daemon-owned and
// the write probe succeeds. Any guard failure routes the record to the FR-4
// filing path instead.
//
// FR-4 Pan filing: every divergent record the guarded heal will not attempt
// (guard refused, non-terminal status, auto_resync off, heal failed) files one
// mirror_drift_needs_human triage event naming the store path, record id, and
// drift direction, via CreateBead on the filing sink. Filing errors are logged
// and never stop the sweep.

// mirrorResyncImplemented gates the sweep body. It stays false until the
// mirror-health GREEN chain lands its contract fixes: the RED sweep tests in
// pan_mirror_health_test.go skip loudly while it is false
// (MIRROR_HEALTH_ENFORCE=1 fails them) and sweepMirrorHealthForProject is inert.
const mirrorResyncImplemented = true

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

// mirrorDriftFiledLedger remembers (store|bead|direction) signatures already
// filed as mirror_drift_needs_human, so repeated sweeps never duplicate a
// pan filing for the same drifting record (exactly-once contract).
var mirrorDriftFiledLedger = map[string]time.Time{}

// probe seams (package-level so tests can override wholesale; production
// wiring installs the real impls from NewOverseer).
var (
	// mirrorJSONLMtime stats the store's issues.jsonl mtime. ok=false when
	// the store is absent / unreadable (skip on error, fail-open).
	mirrorJSONLMtime = func(storePath string) (time.Time, bool) {
		fi, err := os.Stat(filepath.Join(storePath, "issues.jsonl"))
		if err != nil {
			return time.Time{}, false
		}
		return fi.ModTime(), true
	}
	// mirrorHasLiveWorker reports whether any live worker row belongs to the
	// project (live-worker skip contract).
	mirrorHasLiveWorker = func(projectName string) bool { return false }
	// mirrorAuditRunner runs the FR-1 mirroraudit.Audit for one store.
	mirrorAuditRunner = func(storePath string) (mirroraudit.Report, error) {
		// nil Comparer selects the shared package's production `br list` shell-out.
		return mirroraudit.Audit(storePath, nil)
	}
	// mirrorWriteProbe probes store write-eligibility for the guarded
	// auto-resync (FR-3). Inert default: probe succeeds (nil error).
	mirrorWriteProbe = func(storePath string) error { return nil }
)

// mirrorGuardHooks is the test seam for the FR-3 guard conditions: the store
// owner and the write probe. nil fields fall back to the production probes.
var mirrorGuardHooks struct {
	isDaemonOwned func(storePath string) bool
	writeProbe    func(storePath string) error
}

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
// scheduling).
func (o *Overseer) sweepMirrorHealth(now time.Time) {
	if o == nil || o.config == nil || !o.config.Overseer.MirrorHealth.Enabled {
		return
	}
	if o.config.Overseer.MirrorHealth.Interval <= 0 {
		return
	}
	if !mirrorHealthSweepDue(o, now) {
		return
	}
	o.lastMirrorHealthSweep = now
	for _, project := range o.projects {
		o.sweepMirrorHealthForProject(project, now)
	}
}

// mirrorStoreForProject resolves the beads store dir for one project.
func mirrorStoreForProject(project types.Project) string {
	return filepath.Join(project.Path, ".beads")
}

// sweepMirrorHealthForProject audits one project's store. Guards:
//   - jsonl mtime < mirrorQuiescenceWindow -> skip (join a live br write).
//   - live worker for the project -> skip.
//   - any audit error -> mirror_audit_error and continue.
//
// On success: drift records are healed (guarded auto-resync, FR-3) when
// eligible, or filed for a human (FR-4); a clean store emits one
// mirror_audit_clean carrying the checked count.
func (o *Overseer) sweepMirrorHealthForProject(project types.Project, now time.Time) {
	if !mirrorResyncImplemented {
		return
	}
	storePath := mirrorStoreForProject(project)

	// Guard 1: jsonl mtime quiescence — never race a live br write. Failure
	// to stat the mirror fails open by skipping the audit this tick.
	if mt, ok := mirrorJSONLMtime(storePath); ok && now.Sub(mt) < mirrorQuiescenceWindow {
		return
	}

	// Guard 2: a live worker for this project holds the store open — the
	// jsonl may legitimately be ahead mid-write; skip.
	if mirrorHasLiveWorker(project.Name) {
		return
	}

	report, err := mirrorAuditRunner(storePath)
	if err != nil {
		o.emitMirrorEvent(project, Event{
			Type:   mirrorAuditErrorEvent,
			Reason: "audit_failed",
			Detail: fmt.Sprintf("mirror audit for %s: %v", storePath, err),
			Fields: map[string]any{"store": storePath},
		})
		return
	}

	if !report.Drift() {
		o.emitMirrorEvent(project, Event{
			Type:   mirrorAuditCleanEvent,
			Reason: "clean",
			Detail: fmt.Sprintf("mirror clean (%d beads) at %s", report.Checked, storePath),
			Fields: map[string]any{"store": storePath, "checked": report.Checked},
		})
		return
	}

	daemonOwned := mirrorGuardOwner(storePath)
	writeOK := mirrorGuardWriteOK(storePath)
	for _, rec := range report.Records {
		o.handleMirrorRecord(project, storePath, rec, now, daemonOwned, writeOK)
	}
}

// mirrorGuardOwner reports whether the store is daemon-owned (heal-eligible).
// Provisional hooks consult the real store owner; the test seam overrides.
func mirrorGuardOwner(storePath string) bool {
	if mirrorGuardHooks.isDaemonOwned != nil {
		return mirrorGuardHooks.isDaemonOwned(storePath)
	}
	uid, missing, err := storeOwnerUID(filepath.Dir(storePath))
	if err != nil || missing {
		return false
	}
	return uid == daemonUID()
}

// mirrorGuardWriteOK probes whether the daemon may safely rewrite the mirror.
func mirrorGuardWriteOK(storePath string) bool {
	if mirrorGuardHooks.writeProbe != nil {
		return mirrorGuardHooks.writeProbe(storePath) == nil
	}
	if mirrorWriteProbe == nil {
		return true
	}
	return mirrorWriteProbe(storePath) == nil
}

// handleMirrorRecord routes one divergent record: guarded heal when eligible
// (FR-3), Filing for a human otherwise (FR-4).
func (o *Overseer) handleMirrorRecord(project types.Project, storePath string, rec mirroraudit.Record, now time.Time, daemonOwned, writeOK bool) {
	o.emitMirrorEvent(project, Event{
		Type:   mirrorDriftDetectedEvent,
		Bead:   rec.BeadID,
		Reason: string(rec.Class),
		Detail: fmt.Sprintf("mirror drift at %s: %s", storePath, fmt.Sprintf("%s jsonl=%q db=%q", rec.BeadID, rec.JSONLStatus, rec.DBStatus)),
		Fields: map[string]any{
			"store":     storePath,
			"bead":      rec.BeadID,
			"jsonl":     rec.JSONLStatus,
			"db":        rec.DBStatus,
			"class":     string(rec.Class),
			"direction": string(rec.Class),
		},
	})

	healable := daemonOwned && writeOK && mirrorTerminalStatus(rec) && rec.DBStatus != "" && rec.Class == mirroraudit.ClassDBAhead
	if o.config.Overseer.MirrorHealth.AutoResync && healable {
		if err := o.healMirrorRecord(storePath, rec); err == nil {
			return
		}
		// Heal failure → fall through to the filing path (fail-safe: the
		// divergent record stays visible to a human).
	}
	o.fileMirrorDrift(project, storePath, rec)
}

// mirrorTerminalStatus reports whether the db status is terminal; only a
// terminal db_ahead record heals, never a non-terminal one (resurrecting a
// stale in-progress claim is forbidden) and never a jsonl_ahead one (the
// divergence is in the jsonl, not the db).
func mirrorTerminalStatus(rec mirroraudit.Record) bool {
	return rec.DBStatus == string(types.StatusClosed) || rec.DBStatus == string(types.StatusCompleted) ||
		rec.DBStatus == string(types.StatusFailed)
}

// healMirrorRecord rewrites the mirror record through the BrUpdater seam.
func (o *Overseer) healMirrorRecord(storePath string, rec mirroraudit.Record) error {
	if o.brUpdater == nil {
		return fmt.Errorf("mirror heal unavailable: no BrUpdater wired")
	}
	if err := o.brUpdater.BrUpdateStatus(storePath, rec.BeadID, rec.DBStatus); err != nil {
		return fmt.Errorf("mirror heal %s: %v", rec.BeadID, err)
	}
	if o.eventSink != nil {
		o.eventSink.Emit(Event{
			Type:   "mirror_drift_healed",
			Bead:   rec.BeadID,
			Reason: string(rec.Class),
			Detail: fmt.Sprintf("auto-resync healed %s to %q in %s", rec.BeadID, rec.DBStatus, storePath),
			Fields: map[string]any{"store": storePath, "bead": rec.BeadID, "status": rec.DBStatus},
		})
	}
	return nil
}

// fileMirrorDrift files one non-healable drift record as a Pan triage event
// plus a pan filing bead via CreateBead (FR-4). Duplicate filings for the
// same record are suppressed by the in-memory ledger (exactly-once).
func (o *Overseer) fileMirrorDrift(project types.Project, storePath string, rec mirroraudit.Record) {
	sig := storePath + "|" + rec.BeadID + "|" + string(rec.Class)
	if seen, ok := mirrorDriftFiledLedger[sig]; ok && mirrorNow().Sub(seen) < 24*time.Hour {
		return
	}
	title := fmt.Sprintf("mirror drift: %s (%s) in %s", rec.BeadID, rec.Class, project.Name)
	description := fmt.Sprintf(
		"mirror_drift_needs_human\nstore: %s\nbead: %s\ndirection: %s\njsonl: %q db: %q",
		storePath, rec.BeadID, string(rec.Class), rec.JSONLStatus, rec.DBStatus)
	if o.beadsClient != nil {
		if _, err := o.beadsClient.CreateBead(project.Path, types.CreateBeadRequest{
			Title:       title,
			Description: description,
			Type:        "task",
			Priority:    2,
			Labels:      []string{"pan-filed", "mirror-drift"},
		}); err != nil {
			// Filing sink failure: logged, sweep continues; signature NOT
			// recorded so a healed-sink next tick re-files (test contract).
			return
		}
	}
	mirrorDriftFiledLedger[sig] = mirrorNow()
	o.emitMirrorEvent(project, Event{
		Type:   mirrorDriftNeedsHumanEvent,
		Bead:   rec.BeadID,
		Reason: string(rec.Class),
		Detail: "mirror divergent from br db; needs human reconciliation",
		Fields: map[string]any{
			"store":     storePath,
			"bead":      rec.BeadID,
			"direction": string(rec.Class),
			"jsonl":     rec.JSONLStatus,
			"db":        rec.DBStatus,
		},
	})
}

// emitMirrorEvent emits one event to the central stream; nil-safe.
func (o *Overseer) emitMirrorEvent(project types.Project, ev Event) {
	if o.eventSink == nil {
		return
	}
	ev.Project = project.Name
	o.eventSink.Emit(ev)
}

// mirrorNow is a seam for the filing ledger's clock.
var mirrorNow = func() time.Time { return time.Now() }
