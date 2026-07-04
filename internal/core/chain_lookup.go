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
	if o.chainCompletionDetector != nil {
		if ch, ok := o.chainCompletionDetector.FindChainByBead(bead.ID); ok {
			return ch.RootBeadID.String()
		}
	}
	return bead.ID.String()
}
