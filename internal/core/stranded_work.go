package core

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
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
	// Persist a conflict manifest first (the --conflicts-dry-run contract):
	// a no-commit merge in a throwaway detached worktree records the exact
	// conflicted files, unmerged index states and the duplicate signal
	// (quarantine commits not reachable from the working branch), so the
	// human/agent doing triage starts from the file list instead of
	// re-deriving it (telematics pmgh/5gq, 2026-09-25).
	manifestPath, merr := writeConflictManifest(projectPath, key, branch)
	if merr != nil {
		// Manifest generation is best-effort triage aid — fail open.
		log.Printf("[PanRepair] integrate-stranded: conflict manifest for %s failed (continuing): %v", key, merr)
	}
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
		detail := fmt.Sprintf("stranded branch %s quarantined to %s due to merge conflict", branch, qb)
		fields := map[string]any{"branch": branch, "quarantine_branch": qb}
		if merr == nil && manifestPath != "" {
			detail += fmt.Sprintf("; conflict manifest: %s", manifestPath)
			fields["conflict_manifest"] = manifestPath
		}
		events.EmitRepo(projectPath, Event{
			Type:    "stranded_conflict",
			Project: filepath.Base(projectPath),
			Bead:    key,
			Reason:  "merge_conflict",
			Detail:  detail,
			Fields:  fields,
		})
	}
	return nil
}

// writeConflictManifest attempts a no-commit merge of branch into a throwaway
// detached worktree at the working branch tip (the manifest contract for the
// --conflicts-dry-run workflow) and, on conflict, writes
// .hydra/conflict-manifests/<key>.json containing the conflicted files, the
// unmerged index states and the duplicate signal (quarantine commits not
// reachable from the working branch tip, via `git cherry`). A clean merge
// yields a manifest with an empty conflict list. The worktree is always
// removed; the working checkout's index is never touched.
func writeConflictManifest(projectPath, key, branch string) (string, error) {
	if _, err := gitOut(projectPath, "rev-parse", "--git-dir"); err != nil {
		return "", fmt.Errorf("not a git repo: %w", err)
	}
	if !branchExists(projectPath, branch) {
		return "", fmt.Errorf("branch %s not found", branch)
	}
	baseTip, err := gitOut(projectPath, "rev-parse", "HEAD")
	if err != nil {
		return "", fmt.Errorf("rev-parse HEAD: %w", err)
	}
	start := strings.TrimSpace(baseTip)

	tmp, err := os.MkdirTemp("", "hydra-conflict-manifest-")
	if err != nil {
		return "", fmt.Errorf("temp dir: %w", err)
	}
	defer func() {
		_, _ = gitOut(projectPath, "worktree", "remove", tmp, "--force")
		_, _ = gitOut(projectPath, "worktree", "prune")
		_ = os.RemoveAll(tmp)
	}()
	if _, err := gitOut(projectPath, "worktree", "add", "--detach", tmp, start); err != nil {
		return "", fmt.Errorf("scratch worktree: %w", err)
	}

	cleanMerge := true
	var conflicts, unmerged, cherryAdds []string
	if _, merr := gitOut(tmp, "merge", "--no-commit", "--no-ff", branch); merr != nil {
		cleanMerge = false
		if out, err := gitOut(tmp, "diff", "--name-only", "--diff-filter=U"); err == nil {
			conflicts = splitNonEmpty(out)
		}
		if out, err := gitOut(tmp, "status", "--porcelain"); err == nil {
			for _, l := range splitNonEmpty(out) {
				code := strings.TrimSpace(l[:2])
				if strings.ContainsAny(code, "UAD") {
					unmerged = append(unmerged, strings.TrimSpace(l))
				}
			}
		}
		if out, cerr := gitOut(projectPath, "cherry", start, branch); cerr == nil {
			for _, l := range splitNonEmpty(out) {
				if strings.HasPrefix(l, "+") {
					cherryAdds = append(cherryAdds, strings.TrimSpace(strings.TrimPrefix(l, "+"))[:12])
				}
			}
		}
	}

	manifest := map[string]any{
		"key":                 key,
		"branch":              branch,
		"base":                strings.TrimSpace(baseTip),
		"clean_merge":         cleanMerge,
		"conflicts":           conflicts,
		"unmerged":            unmerged,
		"commits_not_in_base": cherryAdds,
	}
	b, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal manifest: %w", err)
	}
	outDir := filepath.Join(projectPath, ".hydra", "conflict-manifests")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return "", fmt.Errorf("manifest dir: %w", err)
	}
	outPath := filepath.Join(outDir, key+".json")
	if err := os.WriteFile(outPath, append(b, '\n'), 0o644); err != nil {
		return "", fmt.Errorf("write manifest: %w", err)
	}
	return outPath, nil
}

func splitNonEmpty(s string) []string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if strings.TrimSpace(l) != "" {
			out = append(out, strings.TrimSpace(l))
		}
	}
	return out
}

func removeStrandedWorkLabel(projectPath string, id types.BeadID, beadsClient BeadsClient) error {
	if beadsClient == nil {
		return nil
	}
	return beadsClient.RemoveBeadLabels(projectPath, id.Repo(), id, []string{strandedWorkLabel})
}
