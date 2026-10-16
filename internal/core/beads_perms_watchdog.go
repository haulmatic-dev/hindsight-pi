package core

import (
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// BeadsPermsWatchdog periodically verifies that every managed project's
// .beads store still follows the multi-user permission convention (beads
// group, g+rwX, o-rwx, setgid on the directory, group-rw issues.jsonl) and
// repairs it when it drifts — the 2026-10-15 incident class: a store owner's
// raw `br` (not the packaging/br shim) rewrote issues.jsonl with mode 0600
// and an empty ACL mask, cancelling every named ACL entry and locking the
// daemon (uid 999) out of the store with br exit 8 (IO_ERROR permission
// denied). Closes were rejected with cannot-verify-state and the scheduler
// went blind on that project until a root-side repair ran.
//
// The repair attempt reuses the discovery-time onboarding
// (onboardBeadsPerms). When the daemon itself lacks the rights to repair —
// the owner rewrote the file 0600 and the daemon is neither the owner nor in
// the ACL mask — the onboarding fails and the watchdog emits ONE typed
// `beads_store_locked` event (throttled to once per 10m per store, fail-open)
// naming the root-side remedy, instead of failing silently the way the
// discovery-time onboarding does. Root-side timers (beads-fixer.service,
// hydra-onboard-sweep) remain the authority for those stores.
//
// Fail-open throughout: a probe error or repair failure only defers to the
// next tick and never gates scheduling. interval <= 0 disables the watchdog.
type BeadsPermsWatchdog struct {
	getProjects func() []types.Project
	interval    time.Duration
	events      missingEdgeEventSink

	// repair applies the convention to one repo (injectable for tests).
	repair func(repoDir string) error
	// enabled keeps the watchdog inert on hosts without the beads group
	// (the convention is meaningless there).
	enabled  bool
	stopChan chan struct{}

	// lockedSeen throttles the beads_store_locked event per .beads dir.
	lockedSeen map[string]time.Time
	// now is overridable in tests.
	now func() time.Time
}

const (
	// beadsStoreLockedEvent is emitted when a store is non-conforming and the
	// daemon could not repair it (locked out by an owner-side raw-br rewrite).
	beadsStoreLockedEvent = "beads_store_locked"
	// beadsStoreLockedThrottle bounds the event to once per store per window.
	beadsStoreLockedThrottle = 10 * time.Minute
	// beadsStoreLockedRemedy is the exact operator remedy quoted in Detail.
	beadsStoreLockedRemedy = "run as root: packaging/hydra-normalize-beads <repo> (or systemctl restart beads-fixer.service)"
)

// NewBeadsPermsWatchdog creates the watchdog. interval <= 0 disables it.
func NewBeadsPermsWatchdog(getProjects func() []types.Project, interval time.Duration, events missingEdgeEventSink) *BeadsPermsWatchdog {
	return &BeadsPermsWatchdog{
		getProjects: getProjects,
		interval:    interval,
		events:      events,
		stopChan:    make(chan struct{}),
		lockedSeen:  make(map[string]time.Time),
		now:         time.Now,
		repair:      watchdogRepair,
		enabled:     beadsGroupGID >= 0,
	}
}

// Start launches the watchdog loop. No-op when disabled.
func (w *BeadsPermsWatchdog) Start() {
	if w == nil || w.interval <= 0 || w.getProjects == nil || !w.enabled {
		return
	}
	log.Printf("[BeadsPermsWatchdog] 🔒 store-perms watchdog enabled (interval: %v)", w.interval)
	go w.run()
}

// Stop halts the watchdog loop.
func (w *BeadsPermsWatchdog) Stop() {
	if w == nil {
		return
	}
	select {
	case <-w.stopChan:
	default:
		close(w.stopChan)
	}
}

func (w *BeadsPermsWatchdog) run() {
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()
	w.scan() // once at startup so a lockout from before the restart is not invisible for a full interval
	for {
		select {
		case <-w.stopChan:
			return
		case <-ticker.C:
			w.scan()
		}
	}
}

// scan checks every project repo's .beads store (single projects: the project
// path; multi-repo projects: each repo). Conforming stores cost one stat and
// are untouched. Non-conforming stores are repaired via the same onboarding
// the discovery path uses; a repair the daemon cannot apply (owner-locked
// file) emits the throttled typed event with the root-side remedy.
func (w *BeadsPermsWatchdog) scan() {
	if w == nil || w.interval <= 0 || w.getProjects == nil || !w.enabled {
		return
	}
	for _, project := range w.getProjects() {
		for _, repoDir := range watchdogRepoDirs(project) {
			w.checkStore(project.Name, repoDir)
		}
	}
}

// watchdogRepoDirs resolves the directories that hold a .beads store for one
// project. Unresolvable repos are skipped (fail-open).
func watchdogRepoDirs(project types.Project) []string {
	if project.Type == "multi" {
		var dirs []string
		for _, repo := range project.Repos {
			if dir := resolveRepoPath(project, repo); dir != "" {
				dirs = append(dirs, dir)
			}
		}
		return dirs
	}
	if project.Path == "" {
		return nil
	}
	return []string{project.Path}
}

// checkStore probes one repo's .beads store and repairs or escalates it.
func (w *BeadsPermsWatchdog) checkStore(projectName, repoDir string) {
	beadsDir := filepath.Join(repoDir, ".beads")
	fi, err := os.Stat(beadsDir)
	if err != nil || !fi.IsDir() {
		return // no store (or vanished mid-tick): nothing to guard
	}
	if beadsPermsConforming(beadsDir, fi) {
		return // conforming: two stats, no writes
	}

	repairErr := w.repair(repoDir)
	fi2, statErr := os.Stat(beadsDir)
	if statErr == nil && beadsPermsConforming(beadsDir, fi2) {
		if repairErr != nil {
			log.Printf("[BeadsPermsWatchdog] %s: repaired despite walk error: %v", beadsDir, repairErr)
		}
		return
	}

	// Still non-conforming after the repair attempt: the daemon is locked
	// out of the store (owner-side raw br rewrote the files 0600 with an
	// empty ACL mask; chgrp/chmod as a non-owner fails). Escalate loudly,
	// throttled, and fail-open.
	detail := "store non-conforming and daemon could not repair it (locked out by owner-side br rewrite); " + beadsStoreLockedRemedy
	if repairErr != nil {
		detail = repairErr.Error() + "; " + beadsStoreLockedRemedy
	}
	w.emitLocked(repoDir, beadsDir, projectName, detail)
}

// emitLocked emits the throttled beads_store_locked event for one store.
func (w *BeadsPermsWatchdog) emitLocked(repoDir, beadsDir, projectName, detail string) {
	now := w.now()
	if last, ok := w.lockedSeen[beadsDir]; ok && now.Sub(last) < beadsStoreLockedThrottle {
		return
	}
	w.lockedSeen[beadsDir] = now
	log.Printf("[BeadsPermsWatchdog] ⚠️ %s: %s", beadsDir, detail)
	if w.events != nil {
		w.events.EmitRepo(repoDir, Event{
			Type:    beadsStoreLockedEvent,
			Project: projectName,
			Reason:  "owner_locked",
			Detail:  detail,
			Fields: map[string]any{
				"store": beadsDir,
			},
		})
	}
}

// watchdogRepair is the production repair seam, package-var so tests can
// inject a failing repair (owner-locked store) without needing a second uid.
var watchdogRepair = onboardBeadsPerms
