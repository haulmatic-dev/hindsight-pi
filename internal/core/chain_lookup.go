package core

import "github.com/haulmatic-dev/hydra/pkg/types"

// FindChainByBead returns the chain whose AllBeadIDs contains beadID, if any.
// Used to map a bead to its chain's worktree key.
func (d *ChainCompletionDetector) FindChainByBead(beadID types.BeadID) (*ChainInfo, bool) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	for _, ch := range d.chains {
		for _, id := range ch.AllBeadIDs {
			if id == beadID {
				return ch, true
			}
		}
	}
	return nil, false
}

// chainKeyForBead returns the per-chain worktree key for a bead: its chain's
// root bead ID, or the bead's own ID when it belongs to no registered chain
// (singleton beads are not registered as chains).
func (o *Overseer) chainKeyForBead(bead types.Bead) string {
	return o.chainKeyForBeadID(bead.ID)
}

// chainKeyForBeadID is the BeadID-only form of chainKeyForBead, for callers
// (e.g. Charon's worktree sweeps) that hold an ID, not a full bead.
func (o *Overseer) chainKeyForBeadID(beadID types.BeadID) string {
	if o.chainCompletionDetector != nil {
		if ch, ok := o.chainCompletionDetector.FindChainByBead(beadID); ok {
			return ch.RootBeadID.String()
		}
	}
	return beadID.String()
}

// isChainMember reports whether a bead is part of a registered multi-bead chain.
// This is the correct predicate for deciding whether a bead should be excluded
// from singleton integrate-on-close: a chain root also maps to its own ID via
// chainKeyForBead, so an equality check on the key would incorrectly integrate
// roots before their dependents complete.
func (o *Overseer) isChainMember(beadID types.BeadID) bool {
	if o.chainCompletionDetector == nil {
		return false
	}
	_, ok := o.chainCompletionDetector.FindChainByBead(beadID)
	return ok
}
