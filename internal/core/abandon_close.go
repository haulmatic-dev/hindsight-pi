package core

import (
	"errors"
	"fmt"
	"log"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// BuildAbandonWithSummary returns a Pan handler that closes a workerless bead
// as failed from the daemon side. Without this, a terminal abandon verdict for a
// bead with no live worker is enqueued as a worker command and never consumed,
// producing the stable pending-abandon loop observed on srv-smart-engine-iz5z7
// (2026-08-30).
//
// The handler uses the normal close-as-failed path, so it creates fix beads
// and applies the closed-failed label under the same rules as a worker failure.
func BuildAbandonWithSummary(o *Overseer) func(beadID string) error {
	return func(beadID string) error {
		id := types.BeadID(beadID)
		repoName := id.Repo()
		projectPath := o.beadSource.Lookup(id.String())
		if projectPath == "" {
			projectPath = resolveProjectPathByName(o.config, id.Project(), repoName)
		}
		if projectPath == "" {
			return fmt.Errorf("abandon-with-summary %s: project path not found", id)
		}

		bead, err := o.beadsClient.GetBead(projectPath, repoName, id)
		if err != nil {
			return fmt.Errorf("abandon-with-summary %s GetBead failed: %w", id, err)
		}
		if bead == nil {
			return fmt.Errorf("abandon-with-summary %s not found", id)
		}
		if bead.Status.IsTerminal() {
			log.Printf("[Pan] %s already terminal (%s); skipping abandon-with-summary", id, bead.Status)
			return nil
		}

		// Keep the DAG in sync with the beads store. A normal worker failure
		// calls MarkFailed before CloseBead.
		if o.dag != nil {
			o.dagMu.Lock()
			if statusErr := o.dag.MarkFailed(id.String()); statusErr != nil {
				if !errors.Is(statusErr, ErrNodeNotFound) {
					log.Printf("[Pan] %s MarkFailed failed (fail open): %v", id, statusErr)
				}
			}
			o.dagMu.Unlock()
		}

		// Close the bead via the same path a worker failure would use.
		// success=false and exitCode=1 create the fix bead and label.
		if o.beadCloser == nil {
			return fmt.Errorf("abandon-with-summary %s: no bead closer attached", id)
		}
		if err := o.beadCloser.Close(&types.Worker{BeadID: id}, false, 1); err != nil {
			return fmt.Errorf("abandon-with-summary %s Close failed: %w", id, err)
		}
		log.Printf("[Pan] %s abandoned-with-summary closed as failed", id)
		return nil
	}
}
