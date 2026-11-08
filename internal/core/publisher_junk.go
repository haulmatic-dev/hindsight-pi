package core

import (
	"log"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/haulmatic-dev/hydra/internal/junksig"
)

// Publish-path junk guard — GREEN implementation (bead hydra-ai97h chain).
//
// The publish path (PublishRepo and the close-path publish helpers) must
// refuse to push branches whose committed diff contains junk-signature
// paths (junksig.IsJunkPath), fail closed with a bounded, visible exit:
//
//   - one typed publish_junk_blocked event naming the offending paths
//     verbatim per blocked publish attempt;
//   - a per-project consecutive-block counter: after publishJunkMaxBlocks
//     consecutive blocks the guard reports the escalated reason and the
//     publish loop stops re-attempting (bounded retry, no churn);
//   - junk-free branches (e.g. only thread-cache.service.ts changes) are
//     never blocked;
//   - once the junk is removed from the branch, the counter resets and
//     the next publish attempt proceeds normally.
//
// Every git probe failure is fail-open (a broken probe cannot block a
// healthy publish).

const (
	// publishJunkReason is the distinct skip reason for a junk-diff branch.
	publishJunkReason = "publish-junk"
	// publishJunkEscalatedReason is reported after publishJunkMaxBlocks
	// consecutive blocks — the publish loop must stop retrying.
	publishJunkEscalatedReason = "publish-junk-escalated"
	// publishJunkBlockedEvent is the typed event for one visible block.
	publishJunkBlockedEvent = "publish_junk_blocked"
	// publishJunkGuardRefusedEvent is the typed event publisher.go emits for
	// each refused attempt (distinct from the guard's own probe-block event).
	publishJunkGuardRefusedEvent = "publish_junk_guard_refused"
	// publishJunkMaxBlocks caps consecutive guard blocks per project
	// before the escalated (stop-retrying) reason takes over.
	publishJunkMaxBlocks = 3
)

// publishJunkImplemented shifts the RED-phase contract tests. GREEN: real
// diff-probe + counter implementation below.
const publishJunkImplemented = true

// publishJunkDiffProbe is the seam for the guard's junk-path diff probe
// (production: committed diff of the working branch against origin/<branch>,
// classified by junksig.IsJunkPath). Overridden in tests.
var publishJunkDiffProbe = defaultPublishJunkDiffProbe

// junkPathsFromList filters one newline-separated list of file paths down to
// junk-signature paths (junksig.IsJunkPath), deduplicated and sorted.
func junkPathsFromList(out string) []string {
	seen := map[string]bool{}
	var junk []string
	for _, p := range strings.Split(out, "\n") {
		p = strings.TrimSpace(p)
		if p == "" || !junksig.IsJunkPath(p) || seen[p] {
			continue
		}
		seen[p] = true
		junk = append(junk, p)
	}
	sort.Strings(junk)
	return junk
}

// defaultPublishJunkDiffProbe returns the junk paths (junksig.IsJunkPath)
// found in the branch's committed diff, deduplicated and sorted. When
// origin/<branch> exists the probe computes `git diff --name-only
// origin/<branch>...HEAD`; when it does not (fresh repo, no remote yet) the
// probe falls back to every file ever committed (`git log --name-only`) so a
// never-pushed junk branch is still caught. Fail-open: any git failure or a
// detached HEAD returns nil, nil — a broken probe cannot block a healthy
// publish.
func defaultPublishJunkDiffProbe(projectPath string) ([]string, error) {
	if projectPath == "" {
		return nil, nil
	}
	branch, err := gitOut(projectPath, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return nil, nil // detached/broken git — fail open
	}
	branch = strings.TrimSpace(branch)
	if branch == "" {
		return nil, nil
	}
	ref := "origin/" + branch
	var out string
	if _, verr := gitOut(projectPath, "rev-parse", "--verify", ref+"^{commit}"); verr == nil {
		out, err = gitOut(projectPath, "diff", "--name-only", ref+"..."+branch)
	} else {
		// No remote-tracking ref: every file ever committed is eligible.
		out, err = gitOut(projectPath, "log", "--name-only", "--pretty=format:", "HEAD")
	}
	if err != nil {
		return nil, nil // probe failure — fail open
	}
	return junkPathsFromList(out), nil
}

// publishJunkMu serializes access to the per-project consecutive-block
// counters (the publish path may be invoked concurrently).
var (
	publishJunkMu     sync.Mutex
	publishJunkBlocks = map[string]int{}
)

// publishJunkGuard evaluates the junk guard for one publish attempt in the
// given project directory. blocked=false means the publish may proceed and
// the consecutive-block counter resets; blocked=true means the publish must
// be rejected with the returned reason. While the reason is the plain
// publish-junk one a typed publish_junk_blocked event names the offending
// paths verbatim; past the consecutive-block cap the escalated reason is
// returned and events stop (bounded retry churn).
// countJunkBlock increments and returns the per-project consecutive-block
// counter (mutex-protected).
func countJunkBlock(projectPath string) int {
	publishJunkMu.Lock()
	defer publishJunkMu.Unlock()
	publishJunkBlocks[projectPath]++
	return publishJunkBlocks[projectPath]
}

func publishJunkGuard(projectPath string) (blocked bool, reason string) {
	paths, probeErr := publishJunkDiffProbe(projectPath)
	if probeErr != nil {
		// Probe failure — fail open, never touch guard state.
		return false, ""
	}
	if len(paths) == 0 {
		// Clean branch: clear the consecutive-block counter so junk fixed
		// upstream gets a fresh guard budget.
		resetPublishJunkBlocks(projectPath)
		return false, ""
	}
	n := countJunkBlock(projectPath)
	if n > publishJunkMaxBlocks {
		// Consecutive-block cap reached: stop re-attempting, stop the
		// event churn — the escalated reason still refuses the publish.
		return true, publishJunkEscalatedReason
	}
	publishEvents.EmitRepo(projectPath, Event{
		Type:    publishJunkBlockedEvent,
		Project: filepath.Base(projectPath),
		Reason:  publishJunkReason,
		Detail:  strings.Join(paths, ", "),
		Fields: map[string]any{
			"paths":       paths,
			"block_count": n,
		},
	})
	log.Printf("[PublishJunk] blocked %s: junk paths %v (consecutive=%d)", projectPath, paths, n)
	return true, publishJunkReason
}

// resetPublishJunkBlocks clears the consecutive-block counter for the
// project — called on a successful/clean publish so junk fixed upstream
// gets a fresh guard budget.
func resetPublishJunkBlocks(projectPath string) {
	publishJunkMu.Lock()
	delete(publishJunkBlocks, projectPath)
	publishJunkMu.Unlock()
}
