package core

import (
	"errors"
	"fmt"
	"log"
	"os/exec"
	"strconv"
	"strings"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// Infra classification at the executable-acceptance gate (bead hydra-b2qe):
// a verification command that fails for ENVIRONMENT reasons (missing
// tooling/dependency, external kill) is not a project-code fault. Treating it
// as verification-failed burned verification rounds and escalated chains on
// pure infra churn (provider capacity rejections, srv-data-harbour 2026-10-19).
// Infra rejections therefore reopen the bead WITHOUT consuming a verification
// strike, and a durable label counts consecutive infra rejections; the 4th
// escalates to a human with reason infra-stuck.

// infraRejectLabelPrefix is the durable counter of consecutive infra
// rejections (infra-reject-N), mirroring dep-open-requeue-N.
const infraRejectLabelPrefix = "infra-reject-"

// infraMaxRejections is the consecutive-infra-rejection count on which the
// bead escalates instead of reopening again.
const infraMaxRejections = 4

// infraRejectionReason marks a non-strike infra rejection (reopen with reason).
const infraRejectionReason = "infra-failure"

// infraEscalationReason marks the escalation after infraMaxRejections.
const infraEscalationReason = "infra-stuck"

// gateExitCode extracts the exit code of a failed verification command.
// A non-ExitError (the command could not be launched at all, e.g. the shell
// itself is missing) returns -1, which classifies as infra.
func gateExitCode(err error) int {
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode()
	}
	return -1
}

// isInfraGateFailure reports whether a failed verification command is an
// infrastructure fault rather than a project-code fault:
//   - exit 127: the shell found no such command — a missing tool/dependency
//     in the environment (e.g. `sh: 1: jest: not found`), never a defect in
//     the bead's own code;
//   - negative exit codes: the command process died by signal (Charon
//     hung-kill, OOM killer, operator kill) — an external event.
//
// Deliberately NOT reusing isInfrastructureExitCode's worker codes (2/3/4/6/7/8):
// those are the worker process's own exit contract, while arbitrary project
// test runners legitimately exit 1/2/3 on real test failures — classifying
// those as infra would suppress genuine project faults.
// output is accepted for signature stability and evidence logging; the
// classification is exit-code based so `exit 127` fakes classify the same as
// a real command-not-found.
func isInfraGateFailure(exitCode int, output string) bool {
	_ = output
	if exitCode < 0 {
		return true
	}
	return exitCode == 127
}

// infraRejectCount returns the highest N among infra-reject-N labels.
func infraRejectCount(labels []string) int {
	n := 0
	for _, l := range labels {
		if s, ok := strings.CutPrefix(l, infraRejectLabelPrefix); ok {
			if v, err := strconv.Atoi(s); err == nil && v > n {
				n = v
			}
		}
	}
	return n
}

// infraLabels returns the bead's labels as stored, falling back to the
// in-memory copy when the store read fails. The second result is the fresh
// bead (nil when unavailable).
func (c *BeadCloser) infraLabels(bead *types.Bead, projectPath, repoName string) ([]string, *types.Bead) {
	if c.beadsClient != nil {
		if f, err := c.beadsClient.GetBead(projectPath, repoName, bead.ID); err == nil && f != nil {
			return f.Labels, f
		}
	}
	return bead.Labels, nil
}

// clearInfraRejectLabels removes every infra-reject-* label once the
// verification command passes, so the counter counts consecutive rejections
// only.
func (c *BeadCloser) clearInfraRejectLabels(bead *types.Bead, projectPath, repoName string) {
	if c.beadsClient == nil {
		return
	}
	labels, _ := c.infraLabels(bead, projectPath, repoName)
	var stale []string
	for _, l := range labels {
		if strings.HasPrefix(l, infraRejectLabelPrefix) {
			stale = append(stale, l)
		}
	}
	if len(stale) == 0 {
		return
	}
	if err := c.beadsClient.RemoveBeadLabels(projectPath, repoName, bead.ID, stale); err != nil {
		log.Printf("[CloseGate] Failed to clear %s* labels on %s: %v", infraRejectLabelPrefix, bead.ID, err)
	}
}

// rejectInfraGate rejects a close whose verification command failed with an
// infrastructure-shaped error. It reopens the bead WITHOUT a verification
// strike (rejectClose is not called), tracks the consecutive count in a
// durable infra-reject-N label, and on the infraMaxRejections-th consecutive
// rejection escalates to a human with reason infra-stuck, leaving the bead
// in_progress (mirrors requeueDepOpen).
func (c *BeadCloser) rejectInfraGate(bead *types.Bead, projectPath, repoName, cmdStr, dir string, exitCode int, output string) error {
	beadID := string(bead.ID)
	evidence := fmt.Sprintf("verification command %q failed in %s with exit %d (classified infra, not a project fault); verbatim output: %s",
		cmdStr, dir, exitCode, strings.TrimSpace(output))
	c.emitGateEvent(closeGateRejectionEvent, bead, projectPath, repoName, infraRejectionReason, evidence)

	labels, fresh := c.infraLabels(bead, projectPath, repoName)
	if fresh != nil && fresh.Status.IsTerminal() {
		log.Printf("[CloseGate] Bead %s already terminal (%s); skipping infra rejection", beadID, fresh.Status)
		return nil
	}
	n := infraRejectCount(labels) + 1

	if n >= infraMaxRejections {
		log.Printf("[CloseGate] Bead %s infra-rejected %d consecutive times; escalating to human", beadID, n)
		if c.beadsClient != nil {
			if err := c.beadsClient.UpdateBeadLabels(projectPath, repoName, bead.ID,
				[]string{"needs-human", "verified-close-escalated", escalatedLabel, infraEscalationReason}); err != nil {
				log.Printf("[CloseGate] Failed to label %s for escalation: %v", beadID, err)
			}
		}
		c.events.EmitRepo(projectPath, Event{Type: "bead.escalated", Project: repoName,
			Bead: beadID, Reason: infraEscalationReason,
			Detail: fmt.Sprintf("infra rejections %d consecutive times (%s); escalated to human, bead left in_progress", n, evidence),
			Fields: map[string]any{"attempts": n, "max_rejections": infraMaxRejections, "correlation_id": SensorCorrelation("verified-close", beadID), "outcome": "escalated"}})
		return fmt.Errorf("verified-close: bead %s escalated (%s) after %d consecutive infra rejections (%s)", beadID, infraEscalationReason, n, evidence)
	}

	log.Printf("[CloseGate] Bead %s infra rejection %d/%d — reopening without a strike", beadID, n, infraMaxRejections)
	if c.beadsClient != nil {
		if err := c.beadsClient.UpdateBeadStatus(projectPath, bead.ID, types.StatusOpen); err != nil {
			log.Printf("[CloseGate] Failed to reopen %s: %v", beadID, err)
		}
		c.clearInfraRejectLabels(bead, projectPath, repoName)
		label := fmt.Sprintf("%s%d", infraRejectLabelPrefix, n)
		if err := c.beadsClient.UpdateBeadLabels(projectPath, repoName, bead.ID, []string{label}); err != nil {
			log.Printf("[CloseGate] Failed to label %s with %q: %v", beadID, label, err)
		}
	}
	return fmt.Errorf("verified-close: bead %s rejected (%s): reopened %d/%d without a strike (%s)", beadID, infraRejectionReason, n, infraMaxRejections, evidence)
}
