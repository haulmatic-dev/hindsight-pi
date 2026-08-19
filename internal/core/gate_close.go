package core

import (
	"fmt"
	"log"
	"strings"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// gate_close.go wires the daemon gate runner (FR-002) into the close path.
// When a project declares gates.daemon_owned, a successful worker completion
// runs the declared gates daemon-side before the bead closes. The close path
// contains zero LLM calls and zero agent-quoted evidence: pass/fail comes
// from the persisted artifact, written before any LLM involvement.

// SetGateRunner attaches the gate runner. Nil runner + daemon_owned project
// fails closed in runDaemonGates.
func (c *BeadCloser) SetGateRunner(r *GateRunner) { c.gateRunner = r }

// gatesFor returns the gate set declared for a project, or nil.
func (c *BeadCloser) gatesFor(projectName string) *GateSet {
	if c.config == nil {
		return nil
	}
	for i := range c.config.Projects {
		if c.config.Projects[i].Name == projectName {
			return c.config.Projects[i].Gates
		}
	}
	return nil
}

// runDaemonGates executes all declared gates for a completing bead.
// worktreePath must be the bead's chain worktree (the caller resolves it via
// ExistingWorktree); an empty worktree under daemon_owned fails closed —
// without the worker's changes there is nothing to verify.
//
// Gate failure is data: the bead is reopened through rejectClose (bounded by
// validation.max_rounds, then escalated to a human), labelled gate-failed,
// and its description is rewritten with the parsed summary + raw log path so
// the retry worker starts from evidence instead of re-running the suite.
func (c *BeadCloser) runDaemonGates(bead *types.Bead, projectName, projectPath, repoName, worktreePath string) error {
	gates := c.gatesFor(projectName)
	if gates == nil || !gates.DaemonOwned {
		return nil
	}
	if c.gateRunner == nil {
		return fmt.Errorf("daemon gates: project %s has daemon_owned but no gate runner is attached (fail closed)", projectName)
	}
	if worktreePath == "" || worktreePath == projectPath {
		log.Printf("[DaemonGates] %s: daemon_owned but no chain worktree — refusing to close without evidence", bead.ID)
		return c.rejectClose(bead, projectPath, repoName, "gate-no-worktree")
	}

	// Cheap gates first: a lint failure should not pay for a test run.
	named := []struct {
		name string
		spec GateSpec
	}{
		{"lint", gates.Lint},
		{"build", gates.Build},
		{"test", gates.Test},
	}

	for _, g := range named {
		if g.spec.Cmd == "" {
			continue
		}
		a, err := c.gateRunner.RunGate(GateRequest{
			Project: projectName, BeadID: string(bead.ID), Gate: g.name,
			Spec:         g.spec,
			ProjectPath:  projectPath,
			WorktreePath: worktreePath,
		})
		if err != nil {
			log.Printf("[DaemonGates] %s gate %s runner fault: %v", bead.ID, g.name, err)
			return c.rejectClose(bead, projectPath, repoName, "gate-runner-fault-"+g.name)
		}
		c.events.EmitRepo(projectPath, Event{Type: "gate_run", Project: repoName,
			Bead: bead.ID.String(), Reason: g.name,
			Fields: map[string]any{"status": a.Status, "exit_code": a.ExitCode}})
		if a.Status != GateStatusPassed {
			c.annotateGateFailure(bead, projectPath, repoName, g.name, a)
			return c.rejectClose(bead, projectPath, repoName, "gate-"+g.name+"-failed")
		}
		log.Printf("[DaemonGates] %s gate %s passed (exit %d)", bead.ID, g.name, a.ExitCode)
	}
	return nil
}

// annotateGateFailure hands the retry worker the evidence: parsed summary +
// raw log path. The suite belongs to the daemon; the worker fixes code.
func (c *BeadCloser) annotateGateFailure(bead *types.Bead, projectPath, repoName, gate string, a GateArtifact) {
	if c.beadsClient == nil {
		return
	}
	if err := c.beadsClient.UpdateBeadLabels(projectPath, repoName, bead.ID, []string{"gate-failed", "gate-failed-" + gate}); err != nil {
		log.Printf("[DaemonGates] label update failed for %s: %v", bead.ID, err)
	}
	desc := fmt.Sprintf("%s\n\n[daemon gate %s FAILED — fix the code, do NOT re-run the full suite; the daemon re-verifies on completion]\n%s\nraw log: %s",
		bead.Description, gate, a.Summary, a.RawLog)
	if len(desc) > 6000 {
		desc = desc[:6000]
	}
	if err := c.beadsClient.UpdateBeadDescription(projectPath, repoName, bead.ID, desc); err != nil {
		log.Printf("[DaemonGates] description update failed for %s: %v", bead.ID, err)
	}
	bead.Description = desc
	if !strings.Contains(bead.Description, gate) {
		log.Printf("[DaemonGates] warning: annotation for %s may be truncated", bead.ID)
	}
}
