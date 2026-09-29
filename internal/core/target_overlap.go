package core

import (
	"log"
	"path/filepath"
	"strings"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// target_overlap.go — target-scope overlap serialization at spawn (FR-003).
//
// Two ready beads whose declared target files overlap must not run
// concurrently: their workers race on the same files, the close-path
// target-scope gate then attributes the other worker's edits as off-scope
// violations, and merge integration hits structural conflicts. The scheduler
// therefore serializes: a bead whose targets overlap a live worker's targets
// is deferred (skipped this tick, still ready) instead of spawned.
//
// This is fail-open by design: if a running bead's targets cannot be read
// from the store, no conflict is reported and the spawn proceeds — the
// close-path scope gate remains the backstop.

// stripTargetSymbol removes a trailing ":symbol" suffix from a declared
// target (e.g. "internal/core/foo.go:Bar()" -> "internal/core/foo.go"),
// mirroring the close-path scope gate's target normalization.
func stripTargetSymbol(p string) string {
	if i := strings.Index(p, ":"); i > 0 {
		p = p[:i]
	}
	return filepath.Clean(strings.TrimSpace(p))
}

// targetsOverlap reports whether any normalized target path appears in both
// lists. Pure function; used by the spawn funnel and tests.
func targetsOverlap(a, b []string) bool {
	set := make(map[string]bool, len(a))
	for _, p := range a {
		set[stripTargetSymbol(p)] = true
	}
	for _, p := range b {
		if set[stripTargetSymbol(p)] {
			return true
		}
	}
	return false
}

// activeBeadTargets returns declared target files for every bead with a live
// worker in this daemon. Lookups go through the beads store (stateless, no
// bookkeeping to leak across retry/close paths); a failed lookup contributes
// nothing (fail-open).
func (o *Overseer) activeBeadTargets() map[string][]string {
	o.workerMutex.RLock()
	beadIDs := make([]string, 0, len(o.activeWorkers))
	for beadID := range o.activeWorkers {
		beadIDs = append(beadIDs, beadID)
	}
	o.workerMutex.RUnlock()

	out := make(map[string][]string, len(beadIDs))
	if o.beadsClient == nil {
		return out
	}
	for _, beadID := range beadIDs {
		projectPath, id := o.findProjectPathForBead(beadID)
		if projectPath == "" {
			continue
		}
		bead, err := o.beadsClient.GetBead(projectPath, "", id)
		if err != nil || bead == nil {
			log.Printf("[Spawn] %s target-overlap check: store read failed (fail-open): %v", beadID, err)
			continue
		}
		if targets := beadTargetFiles(bead); len(targets) > 0 {
			out[beadID] = targets
		}
	}
	return out
}

// targetScopeConflict returns the bead ID of a live worker whose declared
// target files overlap the candidate bead's targets, if any. Beads without
// declared targets never conflict (the close-path gate handles them).
func (o *Overseer) targetScopeConflict(bead *types.Bead) (string, bool) {
	candidate := beadTargetFiles(bead)
	if len(candidate) == 0 {
		return "", false
	}
	for beadID, targets := range o.activeBeadTargets() {
		if beadID == string(bead.ID) {
			continue
		}
		if targetsOverlap(candidate, targets) {
			return beadID, true
		}
	}
	return "", false
}

// emitSpawnDeferredOverlap writes a structured event when a spawn is deferred
// because another live worker holds overlapping target files. Fail-open: the
// stream never gates the pipeline.
func (o *Overseer) emitSpawnDeferredOverlap(bead types.Bead, holderID string) {
	ev := Event{
		Type:    "spawn-deferred-target-overlap",
		Reason:  "target-overlap",
		Project: string(bead.Project),
		Bead:    bead.ID.String(),
		Detail:  "Spawn deferred for " + bead.ID.String() + ": target files overlap live worker " + holderID,
		Fields:  map[string]any{"held_by": holderID},
	}
	if o.beadCloser != nil && o.beadCloser.events != nil {
		projectPath, _ := o.findProjectPathForBead(bead.ID.String())
		if projectPath == "" {
			projectPath = beadProject(&bead)
		}
		o.beadCloser.events.EmitRepo(projectPath, ev)
	} else {
		log.Printf("[Spawn] %s", ev.Detail)
	}
}
