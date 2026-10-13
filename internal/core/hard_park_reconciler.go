package core

import (
	"log"
	"time"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// worktreeUnparkedEvent is the typed event emitted when the hard-park
// reconciler clears a worktree-unavailable label after probe recovery.
const worktreeUnparkedEvent = "worktree_unparked"

// hardParkReconcileInterval throttles the reconciler: it runs at most once
// per interval regardless of tick cadence.
const hardParkReconcileInterval = time.Minute

// hardParkHysteresisTicks is how many CONSECUTIVE successful probe ticks are
// required before a worktree-unavailable label is cleared. Hysteresis against
// flapping: one successful probe can be a transient recovery window, two
// consecutive ones indicate the underlying worktree failure is actually gone
// (the fms dubious-ownership class, 2026-10-02: root cause fixed at host
// level, but 26 labeled beads stayed parked until an operator intervened).
const hardParkHysteresisTicks = 2

// hardParkProber is the repo-level probe run as the daemon user (hydra). A nil
// error means the worktree environment is usable again.
type hardParkProber func(projectPath string) error

// defaultHardParkProbe mirrors the discovery-path worktree check: the repo
// must still be a git repository. Runs in-process as the daemon (hydra) user,
// so git safe.directory resolution matches what a real spawn would see.
func defaultHardParkProbe(projectPath string) error {
	_, err := gitOut(projectPath, "rev-parse", "--git-dir")
	return err
}

// ReconcileHardParkedWorktrees clears worktree-unavailable hard-park labels
// once their root cause is demonstrably gone. For every bead labeled
// worktree-unavailable it runs the repo-level probe; on hardParkHysteresisTicks
// CONSECUTIVE successful ticks it removes the label (making the bead
// schedulable again on the normal paths) and emits the typed event
// worktree_unparked.
//
// Scope guard: only worktreeUnavailableLabel is ever removed. Escalated and
// needs-human labels are human-only and never touched, even when the same bead
// carries them — RemoveBeadLabels is called with exactly one label.
//
// Fail-open: a probe error resets that bead's hysteresis counter and logs once
// per tick; the label stays. Enumeration errors are logged and skipped.
//
// consecutive maps bead ID -> consecutive successful probe count; callers own
// the map (the Overseer holds it across ticks). unparked returns the bead IDs
// whose labels were cleared this call.
func ReconcileHardParkedWorktrees(bc BeadsClient, projects []types.Project, probe hardParkProber, consecutive map[string]int, events *EventSink) (unparked []string) {
	if bc == nil || len(projects) == 0 {
		return nil
	}
	if probe == nil {
		probe = defaultHardParkProbe
	}
	if consecutive == nil {
		consecutive = map[string]int{}
	}

	var probeFailures int
	for _, project := range projects {
		beads, err := bc.ListBeads(project.Path, "")
		if err != nil {
			log.Printf("[HardParkReconcile] list beads for %s: %v", project.Path, err)
			continue
		}
		probedThisProject := false
		for _, bead := range beads {
			if !hasBeadLabel(bead.Labels, worktreeUnavailableLabel) {
				continue
			}
			id := bead.ID.String()
			// Probe once per project per tick: the probe is repo-level, so a
			// result covers every labeled bead in that project.
			if !probedThisProject {
				if err := probe(project.Path); err != nil {
					log.Printf("[HardParkReconcile] probe for %s failed (labels left untouched): %v", project.Path, err)
					probeFailures++
					// Reset hysteresis for every labeled bead in this project.
					for _, b := range beads {
						if hasBeadLabel(b.Labels, worktreeUnavailableLabel) {
							delete(consecutive, b.ID.String())
						}
					}
					break
				}
				probedThisProject = true
			}
			consecutive[id]++
			if consecutive[id] < hardParkHysteresisTicks {
				continue
			}
			// Hysteresis satisfied: clear exactly the worktree-unavailable
			// label. escalated / needs-human are never passed here.
			if err := bc.RemoveBeadLabels(project.Path, "", bead.ID, []string{worktreeUnavailableLabel}); err != nil {
				log.Printf("[HardParkReconcile] remove label on %s: %v", id, err)
				continue
			}
			delete(consecutive, id)
			unparked = append(unparked, id)
			if events != nil {
				events.EmitRepo(project.Path, Event{
					Type:    worktreeUnparkedEvent,
					Project: project.Name,
					Bead:    id,
					Reason:  "probe_recovered",
					Detail:  "worktree-unavailable label cleared after 2 consecutive successful probes",
				})
			}
			log.Printf("[HardParkReconcile] cleared %q on %s (probe recovered)", worktreeUnavailableLabel, id)
		}
	}
	if probeFailures > 0 {
		log.Printf("[HardParkReconcile] %d project probe(s) failed this tick", probeFailures)
	}
	return unparked
}
