package core

import (
	"fmt"
	"log"
	"os/exec"
	"strconv"
	"strings"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// Close-time hard gates (FR-002, docs/PRDs/executable-acceptance-postclose-
// verification-20261002.md): deterministic daemon-side checks that run before
// the VerifiedClose judge round. A gate failure rejects the close through the
// shared rejectClose path (rejected → reopen → attempt cap → escalate), so
// the existing VerifiedClose round/escalation semantics are unchanged.

// closeGateRejectionEvent is the typed event emitted whenever a hard gate
// rejects a close; Detail carries the verbatim evidence (command output,
// quarantine branch, dependency IDs).
const closeGateRejectionEvent = "close_gate_rejected"

// closeDepOpenEvent is emitted when a bead is closed while dependencies are
// open: hard rejection for chain members, warn-only for singletons.
const closeDepOpenEvent = "close_dep_open"

// runCloseHardGates runs all close-time hard gates. A nil error means every
// gate passed (or failed open) and the close may proceed.
func (c *BeadCloser) runCloseHardGates(bead *types.Bead, projectPath, repoName, worktreePath string) error {
	if bead == nil {
		return nil
	}
	if err := c.gateExecutableAcceptance(bead, projectPath, repoName, worktreePath); err != nil {
		return err
	}
	if err := c.gateQuarantineContradiction(bead, projectPath, repoName); err != nil {
		return err
	}
	return c.gateDependencyOrder(bead, projectPath, repoName)
}

// emitGateEvent emits a typed close-gate event with the bead/project/repo
// context filled in.
func (c *BeadCloser) emitGateEvent(eventType string, bead *types.Bead, projectPath, repoName, reason, detail string) {
	c.events.EmitRepo(projectPath, Event{
		Type:    eventType,
		Project: repoName,
		Bead:    string(bead.ID),
		Reason:  reason,
		Detail:  detail,
	})
}

// rejectViaGate records the gate evidence, emits the typed rejection event,
// and runs the shared rejectClose path (reopen / attempt cap / escalate).
func (c *BeadCloser) rejectViaGate(bead *types.Bead, projectPath, repoName, reason, evidence string) error {
	c.emitGateEvent(closeGateRejectionEvent, bead, projectPath, repoName, reason, evidence)
	return c.rejectClose(bead, projectPath, repoName, reason, evidence)
}

// --- Gate 1: executable acceptance ---

// verificationCommandMarker introduces the fenced machine-parseable command
// block in a bead description.
const verificationCommandMarker = "**Verification Command:**"

// extractVerificationCommand returns the first fenced code block that follows
// the marker, or "" when the bead declares no verification command.
func extractVerificationCommand(description string) string {
	idx := strings.Index(description, verificationCommandMarker)
	if idx < 0 {
		return ""
	}
	rest := description[idx+len(verificationCommandMarker):]
	// Line-based fence matching: the closing fence must be a line whose
	// trimmed content is exactly "```". Substring matching truncated any
	// command that itself contains a backtick run (e.g. grep -c '```' in a
	// docs-lint check), producing "Unterminated quote" shell errors and
	// bogus verification-failed escalations (srv-jobfile-eci, 2026-10-05).
	lines := strings.Split(rest, "\n")
	open := -1
	for i, l := range lines {
		t := strings.TrimSpace(l)
		if strings.HasPrefix(t, "```") {
			open = i
			break
		}
	}
	if open < 0 {
		return ""
	}
	var body []string
	for _, l := range lines[open+1:] {
		if strings.TrimSpace(l) == "```" {
			break
		}
		body = append(body, l)
	}
	if body == nil {
		return ""
	}
	return strings.TrimSpace(strings.Join(body, "\n"))
}

// gateExecutableAcceptance executes the bead's declared verification command
// in its worktree. Non-zero exit rejects the close with the verbatim output
// as evidence. No command = fail-open for legacy beads, with a typed
// verification_command_missing event.
func (c *BeadCloser) gateExecutableAcceptance(bead *types.Bead, projectPath, repoName, worktreePath string) error {
	cmdStr := extractVerificationCommand(bead.Description)
	if cmdStr == "" {
		c.emitGateEvent("verification_command_missing", bead, projectPath, repoName, "fail-open",
			fmt.Sprintf("bead %s declares no verification command; acceptance gate fails open (legacy bead)", bead.ID))
		return nil
	}
	dir := worktreePath
	if dir == "" {
		dir = projectPath
	}
	// The command MUST run in the worktree (or project path): running it in
	// the daemon's own cwd made every npm/jest verification fail with
	// "npm error code ENOENT" (no package.json there), rejecting every close
	// 3/3 and escalating whole chains (srv-jobfile 2026-10-05: 6 beads
	// escalated in one hour). The error message printed dir while executing
	// elsewhere — the message is evidence, not decoration.
	cmd := exec.Command("/bin/sh", "-c", cmdStr)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err == nil {
		c.clearInfraRejectLabels(bead, projectPath, repoName)
		return nil
	}
	exitCode := gateExitCode(err)
	log.Printf("[CloseGate] bead %s verification command failed (exit err %v): %s", bead.ID, err, strings.TrimSpace(string(out)))
	// Environment-shaped failures (missing tool/dependency, external kill) are
	// infra, not project faults: reopen without a verification strike, and
	// escalate only after infraMaxRejections consecutive infra rejections.
	if isInfraGateFailure(exitCode, string(out)) {
		return c.rejectInfraGate(bead, projectPath, repoName, cmdStr, dir, exitCode, string(out))
	}
	return c.rejectViaGate(bead, projectPath, repoName, "verification-failed",
		fmt.Sprintf("verification command %q failed in %s; verbatim output: %s", cmdStr, dir, strings.TrimSpace(string(out))))
}

// --- Gate 2: quarantine contradiction ---

// gateQuarantineContradiction rejects a close when the bead's worktree branch
// still sits in hydra/quarantine/<beadID> and the bead carries no
// "integrated" label. Git evidence errors fail open.
func (c *BeadCloser) gateQuarantineContradiction(bead *types.Bead, projectPath, repoName string) error {
	if hasBeadLabel(bead.Labels, "integrated") {
		return nil
	}
	dir := projectPath
	if dir == "" {
		return nil
	}
	branch := "hydra/quarantine/" + string(bead.ID)
	// Fail-open: a git plumbing error here must not block legitimate closes.
	cmd := exec.Command("git", "-C", dir, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch)
	if err := cmd.Run(); err != nil {
		return nil
	}
	return c.rejectViaGate(bead, projectPath, repoName, "closed-vs-quarantined",
		fmt.Sprintf("bead %s is being closed but its worktree branch %s is still quarantined without an integration record", bead.ID, branch))
}

// --- Gate 3: dependency order ---

// chainTitlePrefixes are the TDD-chain phase markers; a bead whose title
// starts with one is a chain member and closes hard on open dependencies.
var chainTitlePrefixes = []string{"RED:", "GREEN:", "REFACTOR:", "SYNTAX:"}

func isChainTitle(title string) bool {
	for _, p := range chainTitlePrefixes {
		if strings.HasPrefix(title, p) {
			return true
		}
	}
	return false
}

// gateDependencyOrder rejects chain members with non-terminal dependencies
// (fail-closed on fetch errors), and warns-only for singletons.
func (c *BeadCloser) gateDependencyOrder(bead *types.Bead, projectPath, repoName string) error {
	if len(bead.Dependencies) == 0 || c.beadsClient == nil {
		c.clearDepOpenRequeueLabels(bead, projectPath, repoName)
		return nil
	}
	var openDeps []string
	for _, depID := range bead.Dependencies {
		dep, err := c.beadsClient.GetBead(projectPath, repoName, depID)
		if err != nil || dep == nil {
			// Fail-open: an unreadable dependency must not block the close.
			continue
		}
		if !dep.Status.IsTerminal() {
			openDeps = append(openDeps, string(depID))
		}
	}
	if len(openDeps) == 0 {
		c.clearDepOpenRequeueLabels(bead, projectPath, repoName)
		return nil
	}
	evidence := fmt.Sprintf("bead %s has open dependencies: %s", bead.ID, strings.Join(openDeps, ", "))
	if !isChainTitle(bead.Title) {
		// Singleton: warn-only.
		c.emitGateEvent(closeDepOpenEvent, bead, projectPath, repoName, "warn",
			"warn: "+evidence+" (singleton, closing anyway)")
		return nil
	}
	c.emitGateEvent(closeDepOpenEvent, bead, projectPath, repoName, "dep-open", evidence)
	return c.requeueDepOpen(bead, projectPath, repoName, evidence)
}

// depOpenRequeueLabelPrefix is the durable counter of consecutive dep-open
// requeues (dep-open-requeue-N); depOpenMaxRequeues is the rejection on which
// the bead escalates instead of requeueing.
const (
	depOpenRequeueLabelPrefix = "dep-open-requeue-"
	depOpenMaxRequeues        = 5
)

// depOpenRequeueCount returns the highest N among dep-open-requeue-N labels.
func depOpenRequeueCount(labels []string) int {
	n := 0
	for _, l := range labels {
		if s, ok := strings.CutPrefix(l, depOpenRequeueLabelPrefix); ok {
			if v, err := strconv.Atoi(s); err == nil && v > n {
				n = v
			}
		}
	}
	return n
}

// depOpenLabels returns the bead's labels as stored, falling back to the
// in-memory copy when the store read fails. The second result is the fresh
// bead (nil when unavailable).
func (c *BeadCloser) depOpenLabels(bead *types.Bead, projectPath, repoName string) ([]string, *types.Bead) {
	if c.beadsClient != nil {
		if f, err := c.beadsClient.GetBead(projectPath, repoName, bead.ID); err == nil && f != nil {
			return f.Labels, f
		}
	}
	return bead.Labels, nil
}

// clearDepOpenRequeueLabels removes every dep-open-requeue-* label once the
// dependency gate passes, so the bound counts consecutive rejections only.
func (c *BeadCloser) clearDepOpenRequeueLabels(bead *types.Bead, projectPath, repoName string) {
	if c.beadsClient == nil {
		return
	}
	labels, _ := c.depOpenLabels(bead, projectPath, repoName)
	var stale []string
	for _, l := range labels {
		if strings.HasPrefix(l, depOpenRequeueLabelPrefix) {
			stale = append(stale, l)
		}
	}
	if len(stale) == 0 {
		return
	}
	if err := c.beadsClient.RemoveBeadLabels(projectPath, repoName, bead.ID, stale); err != nil {
		log.Printf("[CloseGate] Failed to clear %s* labels on %s: %v", depOpenRequeueLabelPrefix, bead.ID, err)
	}
}

// requeueDepOpen reopens a chain member that closed before its dependencies
// without taking a verification strike. The requeue count is a durable label;
// the depOpenMaxRequeues-th consecutive rejection escalates to a human.
func (c *BeadCloser) requeueDepOpen(bead *types.Bead, projectPath, repoName, evidence string) error {
	beadID := string(bead.ID)
	labels, fresh := c.depOpenLabels(bead, projectPath, repoName)
	if fresh != nil && fresh.Status.IsTerminal() {
		log.Printf("[CloseGate] Bead %s already terminal (%s); skipping dep-open requeue", beadID, fresh.Status)
		return nil
	}
	n := depOpenRequeueCount(labels) + 1

	if n >= depOpenMaxRequeues {
		log.Printf("[CloseGate] Bead %s dep-open %d consecutive times; escalating to human", beadID, n)
		if c.beadsClient != nil {
			if err := c.beadsClient.UpdateBeadLabels(projectPath, repoName, bead.ID, []string{"needs-human", "verified-close-escalated", escalatedLabel}); err != nil {
				log.Printf("[CloseGate] Failed to label %s for escalation: %v", beadID, err)
			}
		}
		c.events.EmitRepo(projectPath, Event{Type: "bead.escalated", Project: repoName,
			Bead: beadID, Reason: "dep-open",
			Detail: fmt.Sprintf("dep-open rejected %d consecutive times (%s); escalated to human, bead left in_progress", n, evidence),
			Fields: map[string]any{"attempts": n, "max_requeues": depOpenMaxRequeues, "correlation_id": SensorCorrelation("verified-close", beadID), "outcome": "escalated"}})
		return fmt.Errorf("verified-close: bead %s escalated after %d consecutive dep-open rejections (%s)", beadID, n, evidence)
	}

	log.Printf("[CloseGate] Bead %s dep-open requeue %d/%d — reopening without a strike", beadID, n, depOpenMaxRequeues)
	if c.beadsClient != nil {
		if err := c.beadsClient.UpdateBeadStatus(projectPath, bead.ID, types.StatusOpen); err != nil {
			log.Printf("[CloseGate] Failed to reopen %s: %v", beadID, err)
		}
		c.clearDepOpenRequeueLabels(bead, projectPath, repoName)
		label := fmt.Sprintf("%s%d", depOpenRequeueLabelPrefix, n)
		if err := c.beadsClient.UpdateBeadLabels(projectPath, repoName, bead.ID, []string{label}); err != nil {
			log.Printf("[CloseGate] Failed to label %s with %q: %v", beadID, label, err)
		}
	}
	return fmt.Errorf("verified-close: bead %s rejected (dep-open): requeued %d/%d without a strike (%s)", beadID, n, depOpenMaxRequeues, evidence)
}
