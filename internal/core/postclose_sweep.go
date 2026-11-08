package core

import (
	"fmt"
	"log"
	"regexp"
	"strings"
	"time"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// PostCloseSweeper re-verifies recently closed beads by re-running their
// recorded verification command in the project's default-branch checkout.
//
// Contract (pinned by postclose_sweep_test.go):
//   - Only beads with StatusClosed, metadata["test_command"] set, and
//     UpdatedAt within 24h are considered.
type PostCloseSweeper struct {
	beadsClient BeadsClient
	getProjects func() []types.Project
	events      *EventSink // nil-safe
	runCommand  func(projectPath, command string) (output string, err error)
	addComment  func(projectPath string, beadID types.BeadID, body string) error
	stopChan    chan struct{}
	interval    time.Duration
}

// NewPostCloseSweeper constructs the sweeper.
func NewPostCloseSweeper(
	beadsClient BeadsClient,
	getProjects func() []types.Project,
	events *EventSink,
	runCommand func(projectPath, command string) (output string, err error),
	addComment func(projectPath string, beadID types.BeadID, body string) error,
) *PostCloseSweeper {
	return &PostCloseSweeper{
		beadsClient: beadsClient,
		getProjects: getProjects,
		events:      events,
		runCommand:  runCommand,
		addComment:  addComment,
		stopChan:    make(chan struct{}),
		interval:    time.Hour,
	}
}

// Start launches the periodic scan loop (1h cadence + once at startup).
func (s *PostCloseSweeper) Start() {
	if s == nil || s.beadsClient == nil || s.getProjects == nil || s.runCommand == nil {
		return
	}
	log.Printf("[PostCloseSweep] 🔎 post-close verification sweep enabled (interval: %v)", s.interval)
	go s.run()
}

func (s *PostCloseSweeper) run() {
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	s.scan()
	for {
		select {
		case <-s.stopChan:
			return
		case <-ticker.C:
			s.scan()
		}
	}
}

// Stop halts the scan loop.
func (s *PostCloseSweeper) Stop() {
	if s == nil {
		return
	}
	select {
	case <-s.stopChan:
	default:
		close(s.stopChan)
	}
}

// scan performs one sweep pass over every project's closed beads carrying a
// recent test_command, implementing the reopen / flake-guard / bounded
// auto-reopen contract pinned by postclose_sweep_test.go.
func (s *PostCloseSweeper) scan() {
	if s == nil || s.beadsClient == nil || s.getProjects == nil || s.runCommand == nil {
		return
	}
	for _, project := range s.getProjects() {
		beads, err := s.beadsClient.ListBeads(project.Path, "")
		if err != nil {
			continue // fail-open: a listing error never blocks the sweep
		}
		for _, bead := range beads {
			s.verifyBead(project, bead)
		}
	}
}

// postCloseMaxAge bounds the sweep to recently closed beads.
const postCloseMaxAge = 24 * time.Hour

func (s *PostCloseSweeper) verifyBead(project types.Project, bead types.Bead) {
	if bead.Status != types.StatusClosed {
		return
	}
	command := bead.Metadata["test_command"]
	if command == "" {
		return
	}
	if time.Since(bead.UpdatedAt) > postCloseMaxAge {
		return
	}

	out1, err1 := s.runCommand(project.Path, command)
	if err1 == nil {
		return
	}
	out1 = runEvidence(out1, err1)
	// Flake guard: one failing run is re-run once.
	out2, err2 := s.runCommand(project.Path, command)
	out2 = runEvidence(out2, err2)
	if err2 == nil {
		s.addLabels(project, bead, "postclose-flake")
		return
	}

	if hasBeadLabel(bead.Labels, "postclose-reopened") {
		// Bounded auto-reopen: failure after a prior reopen escalates to a human.
		s.addLabels(project, bead, "needs-human")
		return
	}

	if s.addComment != nil {
		if err := s.addComment(project.Path, bead.ID, postCloseCommentBody(command, out1, err1, out2, err2)); err != nil {
			return // fail-open: do not reopen without evidence attached (grafted from PR #293)
		}
	}
	s.addLabels(project, bead, "postclose-reopened")
	_ = s.beadsClient.UpdateBeadStatus(project.Path, bead.ID, types.StatusOpen)
}

func (s *PostCloseSweeper) addLabels(project types.Project, bead types.Bead, label string) {
	if s.beadsClient == nil {
		return
	}
	_ = s.beadsClient.UpdateBeadLabels(project.Path, "", bead.ID, []string{label})
}

// postCloseCommentBody renders both failing-run outputs for the reopen comment.
func postCloseCommentBody(command, out1 string, err1 error, out2 string, err2 error) string {
	var b strings.Builder
	b.WriteString("Post-close verification sweep reopened this bead: the verification command failed twice in a row.\n\n")
	b.WriteString("**Command:** `" + command + "`\n\n")
	b.WriteString("### Run 1 output\n\n```\n" + out1 + "\n```\n")
	if err1 != nil {
		b.WriteString("Run 1 error: `" + err1.Error() + "`\n")
	}
	b.WriteString("\n### Run 2 output\n\n```\n" + out2 + "\n```\n")
	if err2 != nil {
		b.WriteString("Run 2 error: `" + err2.Error() + "`\n")
	}
	return b.String()
}

// runEvidence returns the command output, falling back to the error string
// when the command produced no output of its own (e.g. `false` fails with
// only the exit status). Grafted from PR #294.
func runEvidence(out string, err error) string {
	if strings.TrimSpace(out) == "" && err != nil {
		return err.Error()
	}
	return out
}

// escapeCooldown suppresses repeat escape events for the same bead inside one
// sweep window, so a chronically red command cannot flood the event stream.
const escapeCooldown = 7 * 24 * time.Hour

// verificationMetadataKeys are the bead metadata keys that carry an explicit
// verification command (task-breakdown direct-task synthesis writes
// test_command; the alias keeps other producers honest).
var verificationMetadataKeys = []string{"test_command", "verification_command"}

// verificationCmdLinePattern matches a description line that names a
// verification command: "Test Command:", "**Verification command:**", etc.,
// with the command in backticks.
var verificationCmdLinePattern = regexp.MustCompile(`(?i)\*{0,2}(?:test|verification|acceptance)\s+command\*{0,2}\s*[:\-]`)

// verificationCommandFor extracts the bead's declared verification command.
// Metadata wins (it is machine-written); a "Test Command:" description line
// with a backticked command is the fallback. Absent both → no contract.
func verificationCommandFor(bead *types.Bead) (string, bool) {
	if bead == nil {
		return "", false
	}
	for _, key := range verificationMetadataKeys {
		if cmd := strings.TrimSpace(bead.Metadata[key]); cmd != "" {
			return cmd, true
		}
	}
	lines := strings.Split(bead.Description, "\n")
	for i, raw := range lines {
		line := strings.TrimSpace(raw)
		if !verificationCmdLinePattern.MatchString(line) {
			continue
		}
		if cmd, ok := extractBacktickCommand(line); ok {
			return cleanCommand(cmd), true
		}
		// Same-line command without backticks (e.g. "Test Command: npm test").
		rest := strings.TrimSpace(verificationCmdLinePattern.ReplaceAllString(line, ""))
		if rest != "" && looksLikeCommand(rest) && !looksLikeSentence(rest) {
			return cleanCommand(rest), true
		}
		// Fenced command block on the lines following the marker (the real
		// task-breakdown shape puts a prose instruction line before the block).
		if cmd, ok := fencedCommandAfter(lines[i+1:]); ok {
			return cmd, true
		}
	}
	return "", false
}

// looksLikeSentence reports whether s reads as sentence prose (dash-led
// bullet or common prose connective) rather than a runnable command —
// extracted prose was passed to sh -c which died "Illegal option" in 4ms.
func looksLikeSentence(s string) bool {
	if strings.HasPrefix(s, "-") || strings.HasPrefix(s, "•") {
		return true
	}
	lower := " " + strings.ToLower(s) + " "
	for _, w := range []string{" the ", " and ", " with ", " then ", " your ", " this ", " that ", " when ", " if ", " should ", " must ", " exact ", " please ", " run the "} {
		if strings.Contains(lower, w) {
			return true
		}
	}
	return false
}

// fencedCommandAfter extracts the command from a ```sh fenced block in the
// lines following a "Test Command:" marker. Rejects content that is prose
// (sentence fragments) rather than a runnable command. Returns the first
// non-empty, prose-free line inside the fence.
func fencedCommandAfter(lines []string) (string, bool) {
	for i, raw := range lines {
		line := strings.TrimSpace(raw)
		if !strings.HasPrefix(line, "```") {
			continue
		}
		// Opening fence found; collect until closing fence.
		for _, inner := range lines[i+1:] {
			inner = strings.TrimSpace(inner)
			if strings.HasPrefix(inner, "```") {
				return "", false
			}
			if inner == "" {
				continue
			}
			if looksLikeSentence(inner) {
				// Bullet/sentence prose leaked into the block — not a command.
				return "", false
			}
			return cleanCommand(inner), true
		}
	}
	return "", false
}

// verificationCommandGuard runs the bead's declared verification command and
// rejects the close when it exits nonzero. No declared command, no gate
// runner, or a runner fault → fail open (the daemon gates and the close-path
// diff/scope gates remain the primary verification). A command that RUNS and
// exits nonzero is a product gate: fail closed via rejectClose.
func (c *BeadCloser) verificationCommandGuard(bead *types.Bead, projectPath string) error {
	cmd, ok := verificationCommandFor(bead)
	if !ok {
		return nil
	}
	if c.gateRunner == nil {
		log.Printf("[VerificationCmd] %s: verification command declared but no gate runner attached (fail open): %s", bead.ID, cmd)
		return nil
	}
	// Run in the bead's chain worktree when one exists: the worktree is where
	// the work AND the dependency install live. Running in the main checkout
	// pre-integration verifies the wrong tree — its node_modules may be stale
	// or empty and the bead's code is not there yet, so the gate is RED no
	// matter what the worker did (2026-10-15: ts-jest-missing in an empty
	// main-checkout node_modules burned 5 attempts on 269, escalated vv3 3/3,
	// and would have mass-escalated 15 NFR beads). Main checkout remains the
	// fallback for beads with no live worktree; the weekly postclose sweep
	// re-verifies integrated state on main.
	gatePath := projectPath
	if wt := closeGateWorktree(c, projectPath, bead); wt != "" {
		gatePath = wt
	}
	a, err := c.gateRunner.RunGate(GateRequest{
		Project: bead.Project, BeadID: bead.ID.String(), Gate: "verification-command",
		Spec:              GateSpec{Cmd: cmd, Parser: "exit-code"},
		ProjectPath:       projectPath,
		WorktreePath:      gatePath,
		AllowMainCheckout: true,
	})
	if err != nil {
		log.Printf("[VerificationCmd] %s: runner fault (fail open): %v", bead.ID, err)
		return nil
	}
	if a.Status != GateStatusPassed {
		log.Printf("[VerificationCmd] %s: verification command RED (exit %d) — close rejected", bead.ID, a.ExitCode)
		return fmt.Errorf("verification-command-failed: bead %s verification command red (exit %d): %s", bead.ID, a.ExitCode, a.Summary)
	}
	log.Printf("[VerificationCmd] %s: verification command passed (exit %d)", bead.ID, a.ExitCode)
	return nil
}

// PostCloseSweeper is the weekly escape-rate regression budget monitor.
// Fail-open: listing/runner faults are logged and retried next tick; they
// never gate anything.
