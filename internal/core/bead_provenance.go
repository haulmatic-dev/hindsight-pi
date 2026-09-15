package core

// Bead provenance index (2026-09-15): the same repo can be checked out by
// several users (ajantha/webapp, nipuna.j/webapp, ...) and each checkout has
// its own beads store with its own webapp-* beads. Resolving a bead to a
// project by ID prefix ("webapp") alone is fundamentally ambiguous — the
// exact-name pass would hijack every nipuna bead into ajantha's checkout.
// The daemon lists every store at DAG-build time and KNOWS which path each
// bead came from; this index records that provenance so later resolution
// (spawn gates, Pan closes, repairs) uses the real source store.

import "sync"

// BeadProvenanceIndex maps bead ID → the project path whose store listed it.
// Rebuilt (not incrementally updated) on every DAG build so removed beads
// and moved projects converge automatically. Lookups are safe on a nil
// index (return "").
type BeadProvenanceIndex struct {
	mu   sync.RWMutex
	path map[string]string
}

// Note records that the given bead IDs were listed from projectPath.
func (x *BeadProvenanceIndex) Note(projectPath string, ids []string) {
	if x == nil || projectPath == "" {
		return
	}
	x.mu.Lock()
	defer x.mu.Unlock()
	if x.path == nil {
		x.path = make(map[string]string)
	}
	for _, id := range ids {
		if id != "" {
			x.path[id] = projectPath
		}
	}
}

// Lookup returns the project path whose store listed this bead, or "".
func (x *BeadProvenanceIndex) Lookup(beadID string) string {
	if x == nil {
		return ""
	}
	x.mu.RLock()
	defer x.mu.RUnlock()
	return x.path[beadID]
}

// Reset clears the index. Called at the start of each DAG rebuild so stale
// entries from removed projects/beads do not linger.
func (x *BeadProvenanceIndex) Reset() {
	if x == nil {
		return
	}
	x.mu.Lock()
	defer x.mu.Unlock()
	x.path = make(map[string]string)
}
