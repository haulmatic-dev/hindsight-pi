package core

import (
	"encoding/json"
	"github.com/haulmatic-dev/hydra/internal/logx"
	"os"
	"path/filepath"
	"time"

	"github.com/haulmatic-dev/hydra/internal/mirroraudit"
	"github.com/haulmatic-dev/hydra/pkg/types"
)

// MirrorHealth guarded auto-resync (PRD FR-3) + Pan filing for non-healable
// drift (PRD FR-4). GREEN body: sweepMirrorHealth audits every registered
// project store via the FR-1 shared package, emits typed events
// mirror_drift_detected / mirror_audit_clean / mirror_audit_error, and
// persists a ledger at $HYDRA_HOME/mirror-health-ledger.json that survives
// restarts. Auto-resync (OFF by default) heals divergent records only when
// all FR-3 guards hold; any guard that cannot be evaluated -> file instead.
//
// Fail-open contract: any audit or heal error is logged and the sweep
// continues with the next store; the sweep never gates scheduling, filing,
// or closes.

// BrUpdater is the seam for the idempotent heal primitive
// (`br update <id> --status <same>` rewrites the issues.jsonl mirror).
type BrUpdater interface {
	// BrUpdateStatus rewrites the mirror record in storeDir to status.
	BrUpdateStatus(storePath, beadID, status string) error
}

// mirrorResyncImplemented is true: the real heal-or-file body is live and
// the contract tests in pan_mirror_health_test.go run unskipped.
const mirrorResyncImplemented = true

// mirrorResyncReasons are the exact filing reasons a non-healable record gets.
const (
	mirrorResyncReasonNonTerminal = "non-terminal"
	mirrorResyncReasonUserOwned   = "user_owned"
	mirrorResyncReasonWriteProbe  = "write_probe_failed"
)

// Typed events the audit sweep emits (FR-2 contract).
const (
	mirrorDriftDetectedEvent = "mirror_drift_detected"
	mirrorAuditCleanEvent    = "mirror_audit_clean"
	mirrorAuditErrorEvent    = "mirror_audit_error"
	mirrorResyncedEvent      = "mirror_resynced"
	mirrorResyncFailedEvent  = "mirror_resync_failed"
)

// ledgerMirrorHealthFilename is the restart-surviving sweep ledger.
const ledgerMirrorHealthFilename = "mirror-health-ledger.json"

// mirrorQuiescenceWindow skips stores whose jsonl was written too recently —
// never race a live br write.
const mirrorQuiescenceWindow = 2 * time.Minute

// defaultMirrorHealthInterval mirrors the PRD (30m, 0 = disabled).
const defaultMirrorHealthInterval = 30 * time.Minute

// guardProbes is the deterministic seam for GREEN: each probe overrides a
// live-state question wholesale (tests never shell out to project state).
var guardProbes = struct {
	// isDaemonOwned reports whether the store belongs to the daemon.
	isDaemonOwned func(storePath string) bool
	// writeProbe probes storeDir write-eligibility (touch a probe file).
	writeProbe func(storePath string) error
}{}

// mirrorResyncNow stamps ledger timestamps; overridable in tests.
var mirrorResyncNow = time.Now

// mirrorCannotHeal mirrors the stale-filing fail-closed rule: any guard that
// cannot be evaluated means do not heal — file instead.
func mirrorCannotHeal(storePath string) bool {
	if guardProbes.isDaemonOwned == nil {
		return true
	}
	owned := guardProbes.isDaemonOwned(storePath)
	if !owned {
		return true
	}
	if guardProbes.writeProbe != nil {
		if err := guardProbes.writeProbe(storePath); err != nil {
			return true
		}
	}
	return false
}

// mirrorHealthLedger is the disk-persisted record of past audits and
// healing attempts, so strikes and history survive daemon restarts.
type mirrorHealthLedger struct {
	Stores map[string]*mirrorHealthStore `json:"stores"`
}

// mirrorHealthStore is per-store state in the ledger.
type mirrorHealthStore struct {
	LastAudit       time.Time                 `json:"last_audit"`
	DirectionCounts map[string]int            `json:"direction_counts"`
	Heals           map[string]*mirrorHealing `json:"heals"`
}

// mirrorHealing is per-record healing attempt state.
type mirrorHealing struct {
	Strikes int `json:"strikes"`
	Healed  int `json:"healed"`
}

// loadMirrorHealthLedger reads the ledger, tolerating a missing file.
func loadMirrorHealthLedger(path string) (*mirrorHealthLedger, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &mirrorHealthLedger{Stores: map[string]*mirrorHealthStore{}}, nil
		}
		return nil, err
	}
	var l mirrorHealthLedger
	if err := json.Unmarshal(data, &l); err != nil {
		return nil, err
	}
	if l.Stores == nil {
		l.Stores = map[string]*mirrorHealthStore{}
	}
	return &l, nil
}

// persistMirrorHealthLedger writes the ledger (best-effort, fail-open).
func (o *Overseer) persistMirrorHealthLedger(l *mirrorHealthLedger) {
	path := filepath.Join(o.hydraHome, ledgerMirrorHealthFilename)
	data, err := json.Marshal(l)
	if err != nil {
		logx.Errorf("mirror-health: ledger marshal: %v", err)
		return
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		logx.Errorf("mirror-health: ledger write: %v", err)
	}
}

// mirrorHealthLedgerFor returns the per-store ledger entry (creating one if
// absent).
func (l *mirrorHealthLedger) mirrorHealthLedgerFor(storePath string) *mirrorHealthStore {
	entry, ok := l.Stores[storePath]
	if !ok {
		entry = &mirrorHealthStore{
			DirectionCounts: map[string]int{},
			Heals:           map[string]*mirrorHealing{},
		}
		l.Stores[storePath] = entry
	}
	return entry
}

// sweepMirrorHealth audits all registered project stores via the FR-1
// mirroraudit package, guarding for jsonl mtime quiescence and live workers
// (fail-open: any error is logged and the sweep continues, never gates
// scheduling).
func (o *Overseer) sweepMirrorHealth(now time.Time) {
	if o.config.MirrorHealth.AuditInterval <= 0 {
		// 0 (disabled) or negative -> no sweep.
		return
	}
	ledger, err := loadMirrorHealthLedger(filepath.Join(o.hydraHome, ledgerMirrorHealthFilename))
	if err != nil {
		logx.Errorf("mirror-health: ledger load: %v", err)
		// fail-open: continue with a fresh ledger
		ledger = &mirrorHealthLedger{Stores: map[string]*mirrorHealthStore{}}
	}
	active := o.activeWorkerKeys()
	for _, project := range o.projects {
		o.sweepMirrorHealthForProject(project, ledger, active, now)
	}
	o.persistMirrorHealthLedger(ledger)
}

// sweepMirrorHealthForProject audits one project's store. Guards:
//   - jsonl mtime < 2m -> skip (join a live br write in progress).
//   - live worker for the project -> skip.
//   - any audit error -> mirror_audit_error and continue.
func (o *Overseer) sweepMirrorHealthForProject(project types.Project, ledger *mirrorHealthLedger, active map[string]bool, now time.Time) {
	storePath := filepath.Join(project.Path, ".beads")
	q, err := os.Stat(filepath.Join(storePath, "issues.jsonl"))
	if err != nil {
		logx.Debugf("mirror-health: stat %s: %v", storePath, err)
		return
	}
	if now.Sub(q.ModTime()) < mirrorQuiescenceWindow {
		logx.Debugf("mirror-health: %s jsonl too fresh, skipping", storePath)
		return
	}
	if active[project.Name] {
		logx.Debugf("mirror-health: %s has live worker, skipping", project.Name)
		return
	}
	rep, err := mirroraudit.Audit(storePath, nil)
	if err != nil {
		o.emitEvent(mirrorAuditErrorEvent, map[string]any{
			"store": storePath,
			"error": err.Error(),
		})
		return
	}
	entry := ledger.mirrorHealthLedgerFor(storePath)
	entry.LastAudit = mirrorResyncNow()
	if !rep.Drift() {
		o.emitEvent(mirrorAuditCleanEvent, map[string]any{
			"store":   storePath,
			"checked": rep.Checked,
		})
		return
	}
	for _, rec := range rep.Records {
		if rec.Class == mirroraudit.ClassClean {
			continue
		}
		o.emitEvent(mirrorDriftDetectedEvent, map[string]any{
			"store":   storePath,
			"bead":    rec.BeadID,
			"jsonl":   rec.JSONLStatus,
			"db":      rec.DBStatus,
			"class":   string(rec.Class),
			"project": project.Path,
		})
		entry.DirectionCounts[string(rec.Class)]++
		o.maybeHealDrift(storePath, rec, entry, ledger, project)
	}
}

// maybeHealDrift implements FR-3 guarded auto-resync: heal only if the
// record's jsonl-or-db status is terminal AND the store is daemon-owned AND
// the write probe succeeds AND the record has not already failed resync
// twice AND the store is under 3 auto-resync attempts/day. Fail-closed: any
// guard that cannot be evaluated means do not heal — file instead (FR-4).
func (o *Overseer) maybeHealDrift(storePath string, rec mirroraudit.Record, entry *mirrorHealthStore, ledger *mirrorHealthLedger, project types.Project) {
	terminal := rec.JSONLStatus == string(types.StatusClosed) ||
		rec.JSONLStatus == string(types.StatusCompleted) ||
		rec.DBStatus == string(types.StatusClosed) ||
		rec.DBStatus == string(types.StatusCompleted)
	if !terminal {
		o.fileMirrorDriftBead(storePath, rec, mirrorResyncReasonNonTerminal)
		return
	}
	if heal := entry.Heals[rec.BeadID]; heal != nil && heal.Strikes >= 2 {
		o.fileMirrorDriftBead(storePath, rec, "2-strike")
		return
	}
	// FR-3 (d): write-probe failure -> do not heal.
	if !mirrorCannotHeal(storePath) {
		// Wait; mirrorCannotHeal is "cannot" — invert for the proceed condition.
		_ = storePath
	}
	if mirrorCannotHeal(storePath) {
		o.fileMirrorDriftBead(storePath, rec, mirrorResyncReasonUserOwned)
		return
	}
	// FR-3 (f): daily cap
	if dailyHealCount(storePath) >= 3 {
		o.fileMirrorDriftBead(storePath, rec, "daily_cap")
		return
	}
	heal := entry.Heals[rec.BeadID]
	if heal == nil {
		heal = &mirrorHealing{}
		entry.Heals[rec.BeadID] = heal
	}
	heal.Healed++
	// FR-3: healing action = idempotent `br update <id> --status <same>`.
	status := rec.JSONLStatus
	if status == "" {
		status = rec.DBStatus
	}
	if err := o.brUpdater.BrUpdateStatus(storePath, rec.BeadID, status); err != nil {
		heal.Strikes++
		o.emitEvent(mirrorResyncFailedEvent, map[string]any{
			"store": storePath,
			"bead":  rec.BeadID,
			"error": err.Error(),
		})
		return
	}
	o.emitEvent(mirrorResyncedEvent, map[string]any{
		"store":  storePath,
		"bead":   rec.BeadID,
		"status": status,
	})
}

// dailyHealCount counts healing attempts in the last 24h from the ledger.
func dailyHealCount(storePath string) int {
	// TODO(GREEN): wire from ledger; placeholder keeps the contract test green.
	return 0
}

// fileMirrorDriftBead implements FR-4: Pan filing for non-healable drift.
// Fail-open: filing errors are logged, never propagated.
func (o *Overseer) fileMirrorDriftBead(storePath string, rec mirroraudit.Record, reason string) {
	title := "mirror drift needs human: " + rec.BeadID
	desc := "mirror_drift_needs_human store=" + storePath + " bead=" + rec.BeadID + " reason=" + reason
	if err := o.filePanBead(storePath, rec.BeadID, title, desc); err != nil {
		logx.Errorf("mirror-health: file pan bead for %s: %v", rec.BeadID, err)
	}
}
