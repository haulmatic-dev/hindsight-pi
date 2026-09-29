package core

import (
	"fmt"
	"log"
	"path/filepath"
	"time"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// quarantine_reattempt.go implements the Pan repair:retry-quarantine verb.
//
// Motivation: integrate-stranded quarantines a conflicted branch to
// hydra/quarantine/<key> and labels the bead stranded-conflict — after which
// the only re-attempt paths were a human, or the 7-day quarantine drain
// (which deletes, never requeues). Meanwhile the base branch keeps moving:
// a conflict from yesterday is frequently already resolved by today's merges
// (the same evaporation that motivated the integration-retest stage).
//
// Contract (all fail-closed: any refusal leaves the quarantine untouched):
//  1. hydra/quarantine/<key> must exist.
//  2. git fetch origin <base> (local base used when the repo has no origin).
//  3. git merge-tree <baseRef> <quarantine branch>. Still conflicted →
//     error naming the branch; the bead stays quarantined and Pan's repair
//     efficacy gate sees the failure.
//  4. Clean → restore the worktree branch hydra/wt-<key> at the quarantine
//     tip, delete the quarantine branch, remove the stranded-conflict
//     label, reopen the bead, and emit quarantine_reattempt{result:requeued}.
//     The reconciler/integration-retest then land it through the ordinary
//     (gated, verified) paths — this verb never merges into the base itself.

// QuarantineReattemptRepair is the repair verb entry point (production git
// runner); see quarantineReattempt for the injectable core.
func QuarantineReattemptRepair(projectPath, key string, beadsClient BeadsClient, events quarantineEventSink) error {
	return quarantineReattempt(projectPath, key, beadsClient, events, gitOut, defaultMergeTree, time.Now)
}

// quarantineEventSink is the narrow event surface the verb needs; *EventSink
// satisfies it (nil-safe on both).
type quarantineEventSink interface {
	EmitRepo(repoPath string, ev Event) Event
}

func quarantineReattempt(projectPath, key string, beadsClient BeadsClient, events quarantineEventSink,
	run func(dir string, args ...string) (string, error),
	mergeTree func(projectPath, base, branch string) (string, int, error),
	now func() time.Time) error {
	if projectPath == "" || key == "" {
		return fmt.Errorf("retry-quarantine: missing project path or key")
	}
	qBranch := quarantinePrefix + key
	if _, err := run(projectPath, "rev-parse", "--verify", "--quiet", "refs/heads/"+qBranch); err != nil {
		return fmt.Errorf("retry-quarantine: no quarantine branch %s", qBranch)
	}
	base, err := (&WorktreeManager{}).WorkingBranch(projectPath)
	if err != nil || base == "" {
		return fmt.Errorf("retry-quarantine: resolve working branch: %v", err)
	}
	baseRef := base
	if _, ferr := run(projectPath, "fetch", "origin", base); ferr != nil {
		// No origin (local-only repo) or transient network: test against the
		// local base — still a real merge-tree verdict.
		log.Printf("[PanRepair] retry-quarantine: fetch origin/%s failed (%v); testing against local %s", base, ferr, base)
	} else {
		baseRef = "origin/" + base
	}

	out, exit, merr := mergeTree(projectPath, baseRef, qBranch)
	clean, perr := parseMergeTreeResult(exit, out, merr)
	if perr != nil {
		return fmt.Errorf("retry-quarantine: merge-tree %s vs %s: %w", qBranch, baseRef, perr)
	}
	if !clean {
		return fmt.Errorf("retry-quarantine: %s still conflicts with %s — stays quarantined", qBranch, baseRef)
	}

	// Clean: move the work back onto the worktree branch family.
	wBranch := (&WorktreeManager{}).branchName(key)
	if _, err := run(projectPath, "branch", "-f", wBranch, qBranch); err != nil {
		return fmt.Errorf("retry-quarantine: restore %s at %s: %w", wBranch, qBranch, err)
	}
	if _, err := run(projectPath, "branch", "-D", qBranch); err != nil {
		// Keep going: the restored worktree branch holds the work; the stale
		// quarantine copy is harmless (drain will classify it as merged).
		log.Printf("[PanRepair] retry-quarantine: delete %s failed (continuing): %v", qBranch, err)
	}

	id, _ := types.ParseBeadID(key)
	if beadsClient != nil && id != "" {
		if lerr := beadsClient.RemoveBeadLabels(projectPath, id.Repo(), id, []string{strandedConflictLabel}); lerr != nil {
			log.Printf("[PanRepair] retry-quarantine: remove stranded-conflict on %s: %v", key, lerr)
		}
		if fresh, gerr := beadsClient.GetBead(projectPath, id.Repo(), id); gerr == nil && fresh != nil && fresh.Status != types.StatusOpen {
			if uerr := beadsClient.UpdateBeadStatus(projectPath, id, types.StatusOpen); uerr != nil {
				log.Printf("[PanRepair] retry-quarantine: reopen %s: %v", key, uerr)
			}
		}
	}
	if events != nil {
		events.EmitRepo(projectPath, Event{
			Type:    "quarantine_reattempt",
			Project: filepath.Base(projectPath),
			Bead:    key,
			Reason:  "requeued",
			Detail:  fmt.Sprintf("quarantine branch %s now merges clean against %s — restored to %s for ordinary integration", qBranch, baseRef, wBranch),
			Fields:  map[string]any{"quarantine_branch": qBranch, "restored_branch": wBranch, "base": baseRef, "at": now().UTC().Format(time.RFC3339)},
		})
	}
	log.Printf("[PanRepair] retry-quarantine: %s merges clean against %s — requeued via %s", qBranch, baseRef, wBranch)
	return nil
}
