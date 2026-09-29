package core

import (
	"fmt"
	"log"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

const strandedWorkLabel = "stranded-work"

// Labels applied by IntegrateStrandedRepair that mark a bead as no longer
// resumable by a worker even though it is not terminal: a quarantined merge
// conflict, or a post-merge verification failure. The stranded detector must
// accept open beads carrying them (FR-021) — otherwise the worktree branch
// stays ahead forever and is never re-surfaced.
const (
	strandedConflictLabel      = "stranded-conflict"
	integrationUnverifiedLabel = "integration-unverified"
)

// strandedWorktreeManager is the narrow surface IntegrateStrandedRepair needs
// from WorktreeManager. Production passes *WorktreeManager; tests can inject
// fakes for failure modes that are hard to stage in real git.
type strandedWorktreeManager interface {
	IntegrateBranchAndRemove(projectPath, key, branch string) error
	QuarantineBranchAndRemove(projectPath, key, branch string) (string, error)
	ResolveBranch(projectPath, key string) string
}

// StrandedUnit describes a worktree branch that is ahead of the working
// branch but has no live worker or active chain and whose owning bead is
// terminal or unknown.
type StrandedUnit struct {
	Project    string
	Key        string
	Branch     string
	AheadBy    int
	OwningBead string
	Age        time.Duration
}

// DetectStrandedWork enumerates hydra/wt-* branches in projectPath and
// returns those that satisfy the stranded predicate. It emits a typed event
// and labels the owning bead the first time a unit is detected.
//
// A branch is stranded when ALL of the following hold:
//   - it has commits not in the working branch (non-empty integrate diff)
//   - no live worker registry row references its key
//   - no active in-progress chain registration references it
//   - the owning bead is terminal or unknown in the project store
//
// The function is fail-open: enumeration errors are logged and the branch is
// skipped.
func DetectStrandedWork(m *WorktreeManager, projectPath string, activeKeys map[string]bool, chainDetector *ChainCompletionDetector, beadsClient BeadsClient, events *EventSink) []StrandedUnit {
	if _, err := gitOut(projectPath, "rev-parse", "--git-dir"); err != nil {
		return nil
	}
	if activeKeys == nil {
		activeKeys = map[string]bool{}
	}

	out, err := gitOut(projectPath, "for-each-ref", "--format=%(refname:short)", "refs/heads/hydra/wt-*")
	if err != nil {
		log.Printf("[StrandedWork] enumerate branches for %s: %v", projectPath, err)
		return nil
	}

	var units []StrandedUnit
	for _, branch := range strings.Split(strings.TrimSpace(out), "\n") {
		if branch == "" {
			continue
		}
		key := strings.TrimPrefix(branch, "hydra/wt-")
		if base, versioned := splitVersionedKey(key); versioned {
			key = base
		}
		if activeKeys[key] {
			continue
		}
		if chainActiveForKey(chainDetector, key) {
			continue
		}
		ahead, err := branchAheadCount(projectPath, branch)
		if err != nil || ahead == 0 {
			continue
		}
		age, err := branchAge(projectPath, branch)
		if err != nil {
			age = 0
		}

		beadID, idErr := types.ParseBeadID(key)
		var status types.BeadStatus
		var labels []string
		var found bool
		if idErr == nil && beadsClient != nil {
			if b, err := beadsClient.GetBead(projectPath, "", beadID); err == nil && b != nil {
				found = true
				status = b.Status
				labels = b.Labels
			}
		}
		if found && !status.IsTerminal() &&
			!hasLabel(labels, strandedConflictLabel) &&
			!hasLabel(labels, integrationUnverifiedLabel) {
			continue // still live / resumable
		}
		if hasLabel(labels, strandedWorkLabel) {
			continue // already reported
		}

		unit := StrandedUnit{
			Project:    filepath.Base(projectPath),
			Key:        key,
			Branch:     branch,
			AheadBy:    ahead,
			OwningBead: key,
			Age:        age,
		}
		units = append(units, unit)

		if events != nil {
			events.EmitRepo(projectPath, Event{
				Type:    "worktree_stranded",
				Project: unit.Project,
				Bead:    key,
				Reason:  "stranded",
				Detail:  fmt.Sprintf("worktree branch %s is %d commit(s) ahead with no live worker or active chain", branch, ahead),
				Fields: map[string]any{
					"branch":  branch,
					"ahead":   ahead,
					"age_sec": age.Seconds(),
				},
			})
		}
		if found && beadsClient != nil {
			if err := beadsClient.UpdateBeadLabels(projectPath, "", beadID, []string{strandedWorkLabel}); err != nil {
				log.Printf("[StrandedWork] label %s for %s: %v", strandedWorkLabel, key, err)
			}
		}
	}
	return units
}

func chainActiveForKey(d *ChainCompletionDetector, key string) bool {
	if d == nil {
		return false
	}
	d.mu.RLock()
	defer d.mu.RUnlock()
	for _, ch := range d.chains {
		if ch.Status != ChainStatusInProgress {
			continue
		}
		if ch.RootBeadID.String() == key {
			return true
		}
		if ch.WorktreeKey != "" && ch.WorktreeKey == key {
			return true
		}
	}
	return false
}

func branchAheadCount(projectPath, branch string) (int, error) {
	out, err := gitOut(projectPath, "rev-list", "--count", "HEAD..refs/heads/"+branch)
	if err != nil {
		return 0, err
	}
	n, err := strconv.Atoi(strings.TrimSpace(out))
	if err != nil {
		return 0, err
	}
	return n, nil
}

func branchAge(projectPath, branch string) (time.Duration, error) {
	out, err := gitOut(projectPath, "log", "-1", "--format=%ct", "refs/heads/"+branch)
	if err != nil {
		return 0, err
	}
	sec, err := strconv.ParseInt(strings.TrimSpace(out), 10, 64)
	if err != nil {
		return 0, err
	}
	return time.Since(time.Unix(sec, 0)), nil
}

func hasLabel(labels []string, want string) bool {
	for _, l := range labels {
		if l == want {
			return true
		}
	}
	return false
}

// IntegrateStrandedRepair implements the Pan repair:integrate-stranded verb.
// It merges a stranded worktree branch into the working branch, removes the
// worktree on success, quarantines on merge conflict, or labels the bead
// integration-unverified on post-merge verification failure.
func IntegrateStrandedRepair(projectPath, key string, wm strandedWorktreeManager, beadsClient BeadsClient, events *EventSink) error {
	if projectPath == "" || wm == nil {
		return fmt.Errorf("integrate-stranded: missing project path or worktree manager")
	}
	id, _ := types.ParseBeadID(key)
	branch := wm.ResolveBranch(projectPath, key)

	err := wm.IntegrateBranchAndRemove(projectPath, key, branch)
	if err == nil {
		if events != nil {
			events.EmitRepo(projectPath, Event{
				Type:    "stranded_integrated",
				Project: filepath.Base(projectPath),
				Bead:    key,
				Reason:  "success",
				Detail:  fmt.Sprintf("stranded branch %s integrated and removed", branch),
				Fields:  map[string]any{"branch": branch},
			})
		}
		_ = removeStrandedWorkLabel(projectPath, id, beadsClient)
		return nil
	}

	if strings.Contains(err.Error(), "merge verification failed") {
		if beadsClient != nil {
			if lerr := beadsClient.UpdateBeadLabels(projectPath, id.Repo(), id, []string{integrationUnverifiedLabel}); lerr != nil {
				log.Printf("[PanRepair] integrate-stranded: label integration-unverified for %s: %v", key, lerr)
			}
		}
		if events != nil {
			events.EmitRepo(projectPath, Event{
				Type:    "stranded_integrated",
				Project: filepath.Base(projectPath),
				Bead:    key,
				Reason:  "verification_failed",
				Detail:  fmt.Sprintf("stranded branch %s merged but verification failed: %v", branch, err),
				Fields:  map[string]any{"branch": branch, "error": err.Error()},
			})
		}
		return err
	}

	// Merge conflict: quarantine the branch and escalate to a human.
	qb, qerr := wm.QuarantineBranchAndRemove(projectPath, key, branch)
	if qerr != nil {
		return fmt.Errorf("integrate-stranded: quarantine %s after conflict: %w", branch, qerr)
	}
	if beadsClient != nil {
		if lerr := beadsClient.UpdateBeadLabels(projectPath, id.Repo(), id, []string{strandedConflictLabel}); lerr != nil {
			log.Printf("[PanRepair] integrate-stranded: label stranded-conflict for %s: %v", key, lerr)
		}
	}
	if events != nil {
		events.EmitRepo(projectPath, Event{
			Type:    "stranded_conflict",
			Project: filepath.Base(projectPath),
			Bead:    key,
			Reason:  "merge_conflict",
			Detail:  fmt.Sprintf("stranded branch %s quarantined to %s due to merge conflict", branch, qb),
			Fields:  map[string]any{"branch": branch, "quarantine_branch": qb},
		})
	}
	return nil
}

func removeStrandedWorkLabel(projectPath string, id types.BeadID, beadsClient BeadsClient) error {
	if beadsClient == nil {
		return nil
	}
	return beadsClient.RemoveBeadLabels(projectPath, id.Repo(), id, []string{strandedWorkLabel})
}
