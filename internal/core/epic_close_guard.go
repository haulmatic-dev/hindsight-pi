package core

import (
	"log"
	"strings"
	"time"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// epicCloseRefusalTTL is how long a refused epic close skips re-verification
// against the beads store. CloseReadyEpics re-proposes a store-refused epic on
// every DAG rebuild; without this throttle each rebuild would re-scan every
// project store for that epic until its children actually close.
const epicCloseRefusalTTL = 10 * time.Minute

// epicCloseRefusals tracks epic ID -> last store-verification refusal time.
// Guarded by the overseer's dagMu (persistClosedEpics is called under it via
// buildDAG; the persistence goroutine takes it before calling in).
// Initialized lazily in persistClosedEpics.
// epicCloseRefusalTimes is keyed by epic ID.
type epicCloseGuard struct {
	refusals map[string]time.Time
}

func (g *epicCloseGuard) recentlyRefused(epicID string, now time.Time) bool {
	if g.refusals == nil {
		return false
	}
	t, ok := g.refusals[epicID]
	return ok && now.Sub(t) < epicCloseRefusalTTL
}

func (g *epicCloseGuard) markRefused(epicID string, now time.Time) {
	if g.refusals == nil {
		g.refusals = make(map[string]time.Time)
	}
	g.refusals[epicID] = now
}

// epicHasOpenDependent reports whether the epic still has a non-terminal
// dependent in the beads store. CloseReadyEpics only sees children that made
// it into the in-memory DAG (discovery incident 2026-09-30: epics auto-closed
// while blocked/approval-gated children were still open because those children
// were invisible to the DAG build). This store-level check is the fail-closed
// gate.
//
// Data source: the epic's own `br show` dependent_ids (parser stores them in
// Metadata). The previous implementation scanned every project's ListBeads
// output for Dependencies pointing at the epic — but ParseBeadListPaginated
// never populates Bead.Dependencies (only metadata dependency_count), so the
// scan saw zero dependents and the guard was vacuous (fail-open) from the day
// it shipped (found 2026-10-15, hydra-ysaz).
//
// Status of each dependent is resolved cheaply: session-completed map first,
// then the freshly-built DAG (authoritative for open beads), with a per-bead
// GetBead fallback for dependents missing from both. Any error refuses the
// close (fail-closed).
func (o *Overseer) epicHasOpenDependent(epicID, projectPath string) bool {
	epic, err := o.beadsClient.GetBead(projectPath, "", types.BeadID(epicID))
	if err != nil || epic == nil {
		log.Printf("[DAG] epic close guard: GetBead(%s) failed (%v); refusing close (fail-closed)", epicID, err)
		return true
	}
	for _, depID := range strings.Split(epic.Metadata["dependent_ids"], ",") {
		depID = strings.TrimSpace(depID)
		if depID == "" {
			continue
		}
		if o.completed[depID] {
			continue
		}
		if node, ok := o.dag.GetNode(depID); ok {
			if !node.Bead.Status.IsTerminal() {
				return true
			}
			continue
		}
		// Dependent is neither open in the DAG nor session-completed: verify
		// against the store before trusting that it is terminal.
		dep, derr := o.beadsClient.GetBead(projectPath, "", types.BeadID(depID))
		if derr != nil || dep == nil {
			log.Printf("[DAG] epic close guard: dependent %s of %s unverifiable (%v); refusing close (fail-closed)", depID, epicID, derr)
			return true
		}
		if !dep.Status.IsTerminal() {
			return true
		}
	}
	return false
}
