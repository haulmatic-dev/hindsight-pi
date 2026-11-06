package core

import (
	"fmt"
	"log"
	"os/exec"
	"strings"

	"github.com/haulmatic-dev/hydra/internal/junksig"
	"github.com/haulmatic-dev/hydra/pkg/types"
)

// FR-003 Close-time junk gate — real implementation.
//
// gateJunkArtifacts evaluates the close-time junk changed set (committed
// paths at HEAD unioned with staged paths, classified by the shared
// internal/junksig structural-signature matcher), auto-drops junk artifacts
// exactly once per generation (staged-only junk unstaged without a commit;
// committed junk removed from the index with a single removal commit while
// leaving the files on disk as ignored), and — when junk is still being
// committed after maxJunkDrops auto-drops on the same bead — rejects the
// close with the distinct reason/label "junk-artifacts" without ever
// touching the escalation ladder. The FR-001 ignore-injection (test-junksig
// chain) makes re-staging impossible in new worktrees; this gate covers
// legacy and staged files. Durable drop counter is the "junk-drop-N" label.
//
// Every git probe failure is fail-open (junk gate cannot block the close
// path on its own tooling).

const (
	// junkArtifactsReason is the intentionally distinct rejection reason and
	// durable label for junk artifacts that persist after the drop window.
	junkArtifactsReason = "junk-artifacts"
	// junkDropEventType is the typed event emitted when the gate auto-drops
	// junk artifacts (staged and/or committed paths) and the close proceeds.
	junkDropEventType = "close_gate_junk_dropped"
	// maxJunkPathsInEvent caps how many junk paths a single drop/rejection
	// event carries verbatim.
	maxJunkPathsInEvent = 50
)

// gateJunkImplemented gates the RED-phase contract tests. GREEN: real
// drop/reject implementation below.
const gateJunkImplemented = true

const (
	// junkDropLabelPrefix is the durable counter of completed auto-drops
	// (junk-drop-N). After maxJunkDrops drops, the next junk generation is
	// rejected instead of being dropped again.
	junkDropLabelPrefix = "junk-drop-"
	// maxJunkDrops is how many generations this gate auto-drops per bead
	// before the persistent re-commit becomes a hard junk-artifacts reject.
	maxJunkDrops = 2
)

// junkGitLines runs a git command in dir and returns trimmed non-empty
// stdout lines. dir=="" or a git failure returns a nil error-free empty
// result (fail-open).
func junkGitLines(dir string, args ...string) []string {
	if dir == "" {
		return nil
	}
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		log.Printf("[CloseGate] junk gate git %v in %s failed: %v", args, dir, err)
		return nil
	}
	var lines []string
	for _, l := range strings.Split(string(out), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			lines = append(lines, l)
		}
	}
	return lines
}

// junkFromPaths classifies raw path lines with junksig.IsJunkPath, keeping
// at most maxJunkPathsInEvent paths.
func junkFromPaths(lines []string) []string {
	var junk []string
	for _, p := range lines {
		if junksig.IsJunkPath(p) {
			junk = append(junk, p)
			if len(junk) >= maxJunkPathsInEvent {
				break
			}
		}
	}
	return junk
}

// junkGitRun runs a state-changing git command in dir. Failures are logged
// with the verbatim output and returned; drop/reject call sites decide to
// fail open. Every mutation carries dir explicitly — the gate never touches
// the daemon's own working directory.
func junkGitRun(dir string, args ...string) error {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		log.Printf("[CloseGate] junk gate git %v in %s failed: %v\n%s", args, dir, err, out)
		return err
	}
	return nil
}

// gateJunkArtifacts is the FR-003 close-time junk gate: drop junk once,
// then reject persistent re-commits with reason junk-artifacts.
func (c *BeadCloser) gateJunkArtifacts(bead *types.Bead, projectPath, repoName, worktreePath string) error {
	dir := worktreePath
	if dir == "" {
		dir = projectPath
	}
	committed := junkFromPaths(junkGitLines(dir, "ls-tree", "-r", "--name-only", "HEAD"))
	staged := junkFromPaths(junkGitLines(dir, "diff", "--cached", "--name-only"))
	if len(committed) == 0 && len(staged) == 0 {
		return nil
	}

	labels, _ := c.currentLabels(bead, projectPath, repoName)
	if maxLabelCount(labels, junkDropLabelPrefix) >= maxJunkDrops {
		return c.rejectJunkArtifacts(bead, projectPath, repoName, committed, staged)
	}
	return c.dropJunkArtifacts(bead, projectPath, repoName, dir, committed, staged, labels)
}

// dropJunkArtifacts performs the one-time drop: committed junk is removed
// from the index (files kept on disk), .gitignore is extended with the
// FR-001 ignore lines, a single removal commit lands; staged-only junk is
// unstaged without a commit. A typed drop event names the paths verbatim
// and the durable drop counter is incremented.
func (c *BeadCloser) dropJunkArtifacts(bead *types.Bead, projectPath, repoName, dir string, committed, staged, labels []string) error {
	log.Printf("[CloseGate] Bead %s auto-dropping junk artifacts (committed=%d staged=%d) in %s",
		bead.ID, len(committed), len(staged), dir)

	if len(committed) > 0 {
		dropErr := junkGitRun(dir, append([]string{"rm", "--cached", "-r", "-q", "--"}, committed...)...)
		if dropErr != nil {
			return nil // fail-open: never block the close on our own tooling
		}
	}
	for _, l := range junksig.IgnorePatterns() {
		ensureGitignoreLine(dir+"/.gitignore", l)
	}
	if len(committed) > 0 {
		if junkGitRun(dir, "commit", "-q", "-m", "chore: auto-drop junk artifacts (close gate)") != nil {
			return nil // fail-open
		}
	}
	if len(staged) > 0 {
		if junkGitRun(dir, append([]string{"reset", "-q", "--"}, staged...)...) != nil {
			return nil // fail-open
		}
	}

	if c.beadsClient != nil {
		n := maxLabelCount(labels, junkDropLabelPrefix) + 1
		if err := c.beadsClient.UpdateBeadLabels(projectPath, repoName, bead.ID,
			[]string{fmt.Sprintf("%s%d", junkDropLabelPrefix, n)}); err != nil {
			log.Printf("[CloseGate] Failed to record %s drop %d on %s: %v", junkDropLabelPrefix, n, bead.ID, err)
		}
	}
	c.emitGateEvent(junkDropEventType, bead, projectPath, repoName, "dropped",
		fmt.Sprintf("auto-dropped %d committed, %d staged junk artifact(s): committed=[%s] staged=[%s]",
			len(committed), len(staged),
			strings.Join(capJunkPaths(committed), ", "), strings.Join(capJunkPaths(staged), ", ")))
	return nil
}

// capJunkPaths trims a junk path list to maxJunkPathsInEvent entries.
func capJunkPaths(paths []string) []string {
	if len(paths) > maxJunkPathsInEvent {
		return paths[:maxJunkPathsInEvent]
	}
	return paths
}

// rejectJunkArtifacts hard-rejects a close whose junk artifacts have been
// re-committed after maxJunkDrops auto-drops. The distinct junk-artifacts
// reason and label isolate the failure from the off-scope warn ladder; the
// escalation ladder is deliberately left untouched.
func (c *BeadCloser) rejectJunkArtifacts(bead *types.Bead, projectPath, repoName string, committed, staged []string) error {
	evidence := fmt.Sprintf(
		"junk artifacts re-committed after %d auto-drops; committed=[%s] staged=[%s]",
		maxJunkDrops, strings.Join(capJunkPaths(committed), ", "), strings.Join(capJunkPaths(staged), ", "))
	c.emitGateEvent(closeGateRejectionEvent, bead, projectPath, repoName, junkArtifactsReason, evidence)
	if c.beadsClient != nil {
		if err := c.beadsClient.UpdateBeadLabels(projectPath, repoName, bead.ID,
			[]string{junkArtifactsReason}); err != nil {
			log.Printf("[CloseGate] Failed to label %s with %q: %v", bead.ID, junkArtifactsReason, err)
		}
	}
	log.Printf("[CloseGate] Bead %s rejected (%s): %s", bead.ID, junkArtifactsReason, evidence)
	return fmt.Errorf("verified-close: bead %s rejected (%s): %s", bead.ID, junkArtifactsReason, evidence)
}
