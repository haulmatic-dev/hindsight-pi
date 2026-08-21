package core

import (
	"log"
	"strings"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// prespawn_probe.go — pre-spawn gate probe (2026-08-20, srv 65wa0: 7.9M
// tokens over 3 empty-diff reject rounds because the suite was already green
// on main). When a project is daemon_owned and a gate is scoped (<scope>),
// the daemon runs the scoped gate BEFORE spawning a worker. Already green:
// the bead closes at zero token cost. Red: spawn as usual — the worker fixes
// it and the close-path gate re-verifies.
//
// The probe is an OPTIMIZATION: it fails open (any error → spawn normally)
// and only considers scoped gates (full-suite probes are too expensive).

// beadProject resolves a bead's project: explicit field first, then the ID
// prefix (scheduler-path beads carry an empty Project field — the codebase
// pattern is worker.BeadID.Project()). The probe's silent no-fire on 65wa0
// (20.7M tokens, 2026-08-21) was exactly this lookup missing.
func beadProject(bead *types.Bead) string {
	if bead.Project != "" {
		return bead.Project
	}
	return bead.ID.Project()
}

// PrespawnProbePasses reports whether every scoped gate for the bead passes
// in the project checkout. Any fault, missing scope, unscoped gate, or
// daemon_owned=false returns false (spawn normally).
func PrespawnProbePasses(runner *GateRunner, gates *GateSet, bead *types.Bead, projectPath string) bool {
	if runner == nil || gates == nil || !gates.DaemonOwned {
		return false
	}
	targets := beadTargetFiles(bead)
	if len(targets) == 0 {
		return false
	}
	scope := targets[0]

	named := []struct {
		name string
		spec GateSpec
	}{
		{"lint", gates.Lint},
		{"build", gates.Build},
		{"test", gates.Test},
	}
	probed := false
	for _, g := range named {
		if g.spec.Cmd == "" || !strings.Contains(g.spec.Cmd, "<scope>") {
			continue
		}
		probed = true
		a, err := runner.RunGate(GateRequest{
			Project: beadProject(bead), BeadID: string(bead.ID), Gate: "prespawn-" + g.name,
			Spec:              g.spec,
			ProjectPath:       projectPath,
			WorktreePath:      projectPath,
			AllowMainCheckout: true, // probe is read-only verification; the worktree guard protects writes, and a pre-spawn probe has no worktree yet
			Scope:             scope,
		})
		if err != nil {
			log.Printf("[Prespawn] %s probe %s fault (failing open, spawning): %v", bead.ID, g.name, err)
			return false
		}
		if a.Status != GateStatusPassed {
			log.Printf("[Prespawn] %s probe %s FAILED — spawning worker (summary: %.200s)", bead.ID, g.name, a.Summary)
			return false
		}
	}
	if probed {
		log.Printf("[Prespawn] %s already green on scoped gates — closing without a worker (0 tokens)", bead.ID)
	}
	return probed
}

// prespawnCloseIfGreen runs the probe and, when green, closes the bead and
// marks it completed in the DAG exactly as a successful worker completion
// would. Returns true when the bead was closed (caller skips spawning).
func (o *Overseer) prespawnCloseIfGreen(bead types.Bead) bool {
	if o.beadCloser == nil || o.beadCloser.gateRunner == nil {
		return false
	}
	projectName := beadProject(&bead)
	var gates *GateSet
	if o.config != nil {
		for i := range o.config.Projects {
			if o.config.Projects[i].Name == projectName {
				gates = o.config.Projects[i].Gates
				break
			}
		}
	}
	if gates == nil {
		return false
	}
	projectPath := o.projectResolver.ResolveProjectPath(&bead)
	if projectPath == "" {
		return false
	}
	if !PrespawnProbePasses(o.beadCloser.gateRunner, gates, &bead, projectPath) {
		return false
	}

	repoName := bead.ID.Repo()
	if err := o.beadsClient.CloseBead(projectPath, repoName, bead.ID, "completed"); err != nil {
		log.Printf("[Prespawn] %s probe passed but close failed (spawning anyway): %v", bead.ID, err)
		return false
	}
	o.dagMu.Lock()
	newlyReady, err := o.dag.MarkCompleted(bead.ID.String())
	o.dagMu.Unlock()
	if err != nil {
		log.Printf("[Prespawn] %s closed but MarkCompleted failed: %v", bead.ID, err)
	}
	o.completed[bead.ID.String()] = true
	if len(newlyReady) > 0 {
		log.Printf("[Prespawn] %d dependents now ready after %s probe-close", len(newlyReady), bead.ID)
	}
	o.beadCloser.events.EmitRepo(projectPath, Event{Type: "prespawn_closed", Project: repoName,
		Bead: bead.ID.String(), Reason: "already-green",
		Fields: map[string]any{"tokens": 0}})
	return true
}
