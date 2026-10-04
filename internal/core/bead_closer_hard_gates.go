package core

import (
	"fmt"
	"log"
	"os/exec"
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
	return c.rejectClose(bead, projectPath, repoName, reason)
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
	fence := strings.Index(rest, "```")
	if fence < 0 {
		return ""
	}
	body := rest[fence+3:]
	end := strings.Index(body, "```")
	if end < 0 {
		return ""
	}
	content := body[:end]
	if nl := strings.Index(content, "\n"); nl >= 0 {
		// Skip an optional single-token language tag line (e.g. ```sh).
		if tag := strings.TrimSpace(content[:nl]); tag != "" && !strings.Contains(tag, " ") {
			content = content[nl+1:]
		}
	}
	return strings.TrimSpace(content)
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
	out, err := exec.Command("/bin/sh", "-c", cmdStr).CombinedOutput()
	if err == nil {
		return nil
	}
	log.Printf("[CloseGate] bead %s verification command failed (exit err %v): %s", bead.ID, err, strings.TrimSpace(string(out)))
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
	return c.rejectClose(bead, projectPath, repoName, "dep-open")
}
