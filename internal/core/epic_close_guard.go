package core

import (
	"log"
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

// epicHasOpenDependent scans every configured project's bead store for a
// non-terminal bead that depends on epicID. CloseReadyEpics only sees children
// that made it into the in-memory DAG (discovery incident 2026-09-30: epics
// auto-closed while blocked/approval-gated children were still open, because
// those children were invisible to the DAG build — e.g. their repo's
// ListBeads failed during the build and was skipped). This store-level check
// is the fail-closed gate: if ANY store listing fails, we treat the epic as
// having open dependents and refuse the close.
func (o *Overseer) epicHasOpenDependent(epicID string) bool {
	for _, project := range o.GetProjects() {
		repos := []types.Repo{{Name: ""}}
		if project.Type == types.RepoTypeMulti {
			repos = project.Repos
		}
		for _, repo := range repos {
			beads, err := o.beadsClient.ListBeads(project.Path, repo.Name)
			if err != nil {
				log.Printf("[DAG] epic close guard: ListBeads failed for %s/%s; refusing close of %s (fail-closed)",
					project.Name, repo.Name, epicID)
				return true
			}
			for _, bead := range beads {
				if string(bead.ID) == epicID {
					continue
				}
				if bead.Status.IsTerminal() || o.completed[string(bead.ID)] {
					continue
				}
				for _, dep := range bead.Dependencies {
					if string(dep) == epicID {
						return true
					}
				}
			}
		}
	}
	return false
}
