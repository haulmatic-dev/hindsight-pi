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

// SetGateDoctor attaches the pi-based verification RED triage agent. Nil
// doctor + verification-command RED falls back to the standard rejectClose
// escalation path (fail-closed, no special repair).
func (c *BeadCloser) SetGateDoctor(d *GateDoctor) { c.gateDoctor = d }

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

	// Scoped gates: the bead's first target file substitutes <scope> (e.g.
	// npx jest <scope>), so a bead fixing suite X is gated on suite X — not
	// on every other red suite in the repo. No targets -> full command.
	scope := ""
	if targets := beadTargetFiles(bead); len(targets) > 0 {
		scope = targets[0]
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
			Scope:        scope,
		})
		if err != nil {
			log.Printf("[DaemonGates] %s gate %s runner fault: %v", bead.ID, g.name, err)
			return c.rejectClose(bead, projectPath, repoName, "gate-runner-fault-"+g.name)
		}
		c.events.EmitRepo(projectPath, Event{Type: "gate_run", Project: repoName,
			Bead: bead.ID.String(), Reason: g.name,
			Fields: map[string]any{"status": a.Status, "exit_code": a.ExitCode}})
		if a.Status != GateStatusPassed {
			// 2026-11-06: foreign-failure attribution — with parallel PRD chains
			// sharing the worktree branch, a failing test from ANOTHER chain
			// must not block THIS bead's close (the 6× gate-test-failed loop
			// burned ~300M tokens). Parse the failing test packages from the
			// gate output; if none intersect this bead's changed files, the
			// failure is foreign — log, label, and let the close proceed.
			if g.name == "test" {
				// Foreign-failure attribution: a failing test from ANOTHER chain
				// must not block THIS bead's close. RED beads are excluded here —
				// their contract check inverts (foreign-only failure = own tests
				// passed = contract violated), handled below.
				if !isRedBead(bead) && c.isForeignTestFailure(bead, worktreePath, a) {
					log.Printf("[DaemonGates] %s gate test: FAILED but all failing packages are foreign to this bead's changes — allowing close (foreign failure)", bead.ID)
					c.events.EmitRepo(projectPath, Event{Type: "gate_test_foreign_failure", Project: repoName,
						Bead: bead.ID.String(), Reason: "foreign-test-failure",
						Detail: fmt.Sprintf("gate test failed but no failing test intersects bead %s changed files", bead.ID),
						Fields: map[string]any{"exit_code": a.ExitCode}})
					if c.beadsClient != nil {
						_ = c.beadsClient.UpdateBeadLabels(projectPath, repoName, bead.ID, []string{"gate-test-foreign-failure"})
					}
					continue // proceed to next gate (or close)
				}
				// RED-phase inverse check: a RED bead's own tests MUST fail —
				// that is the TDD contract (prove the feature is missing).
				// The old behavior rejected RED closes because the tests fail
				// "as designed", creating a close-retry loop (~2M tokens/cycle,
				// s6sy/dubj escalated 11h+, Pan repairEfficacy capped out).
				// Bead's changed files in a failing package = contract satisfied
				// = allow close. Foreign-only failures = the bead's own tests
				// passed = contract violated = reject.
				if isRedBead(bead) {
					if c.ownFilesInFailingPackages(bead, worktreePath, a) {
						log.Printf("[DaemonGates] %s gate test: FAILED in bead's own packages — RED contract satisfied, allowing close", bead.ID)
						c.events.EmitRepo(projectPath, Event{Type: "gate_test_red_contract", Project: repoName,
							Bead: bead.ID.String(), Reason: "red-contract-satisfied",
							Detail: "RED bead's own tests fail as designed — TDD contract verified",
							Fields: map[string]any{"exit_code": a.ExitCode}})
						if c.beadsClient != nil {
							_ = c.beadsClient.UpdateBeadLabels(projectPath, repoName, bead.ID, []string{"gate-test-red-contract"})
						}
						continue
					}
					log.Printf("[DaemonGates] %s gate test: RED bead but no failing package intersects its changes — own tests passed, contract violated", bead.ID)
					c.annotateGateFailure(bead, projectPath, repoName, g.name, a)
					return c.rejectClose(bead, projectPath, repoName, "gate-test-red-passed", "RED bead's own tests passed — expected to fail (TDD contract: prove the feature is missing)")
				}
			}
			c.annotateGateFailure(bead, projectPath, repoName, g.name, a)
			return c.rejectClose(bead, projectPath, repoName, "gate-"+g.name+"-failed", a.Summary)
		}
		log.Printf("[DaemonGates] %s gate %s passed (exit %d)", bead.ID, g.name, a.ExitCode)
		// RED bead + test gate PASSED = contract violated. The RED phase's
		// whole point is failing tests; a passing suite means the feature
		// already exists or the test is trivially true. Reject so the worker
		// rewrites the test to actually exercise the missing behavior.
		if g.name == "test" && isRedBead(bead) {
			log.Printf("[DaemonGates] %s: RED bead passed test gate — contract violated (tests must fail)", bead.ID)
			c.events.EmitRepo(projectPath, Event{Type: "gate_test_red_contract", Project: repoName,
				Bead: bead.ID.String(), Reason: "red-contract-violated-pass",
				Detail: "RED bead's tests all passed — TDD contract requires them to fail",
				Fields: map[string]any{"exit_code": a.ExitCode}})
			return c.rejectClose(bead, projectPath, repoName, "gate-test-red-passed", "RED bead's tests passed — expected to fail (TDD contract: prove the feature is missing)")
		}
	}

	// Exception-share gate: verify the unfiltered coverage total is within
	// the allowed gap of the enforced threshold. This catches workers that
	// excluded low-coverage files to make the ratchet pass.
	if gates.CoverageTruth != nil {
		a, err := c.gateRunner.RunCoverageTruthGate(GateRequest{
			Project: projectName, BeadID: string(bead.ID), Gate: "coverage-truth",
			ProjectPath:  projectPath,
			WorktreePath: worktreePath,
		}, *gates.CoverageTruth)
		if err != nil {
			log.Printf("[DaemonGates] %s coverage-truth runner fault: %v", bead.ID, err)
			return c.rejectClose(bead, projectPath, repoName, "gate-coverage-truth-fault")
		}
		c.events.EmitRepo(projectPath, Event{Type: "gate_run", Project: repoName,
			Bead: bead.ID.String(), Reason: "coverage-truth",
			Fields: map[string]any{"status": a.Status, "exit_code": a.ExitCode}})
		if a.Status != GateStatusPassed {
			c.annotateGateFailure(bead, projectPath, repoName, "coverage-truth", a)
			return c.rejectClose(bead, projectPath, repoName, "gate-coverage-truth-failed", a.Summary)
		}
		log.Printf("[DaemonGates] %s coverage-truth passed (exit %d)", bead.ID, a.ExitCode)
	}
	return nil
}

// isRedBead reports whether the bead is in the TDD RED phase, keyed on the
// title prefix (the same convention as phaseGuidance in cmd/worker/main.go
// and worker_env_builder.go: RED:/GREEN:/REFACTOR:/SYNTAX:).
func isRedBead(bead *types.Bead) bool {
	return strings.HasPrefix(bead.Title, "RED:")
}

// ownFilesInFailingPackages reports whether any failing test package in the
// gate output intersects the bead's changed files — the RED-contract check.
// True = the bead's own tests are among the failures (expected for RED).
// False = no intersection (own tests passed or unparseable → fail-closed).
func (c *BeadCloser) ownFilesInFailingPackages(bead *types.Bead, worktreePath string, artifact GateArtifact) bool {
	failingPkgs := parseFailingGoPackages(artifact.Summary + "\n" + string(artifact.RawLog))
	if len(failingPkgs) == 0 {
		return false // can't parse → assume tests pass → fail-closed
	}
	changed, err := c.changedFilePaths(worktreePath)
	if err != nil || len(changed) == 0 {
		return false // can't determine → fail-closed
	}
	for _, pkg := range failingPkgs {
		pkgDir := goPackageToDir(pkg)
		for _, f := range changed {
			if strings.HasPrefix(f, pkgDir) {
				return true // bead's own files are in a failing package
			}
		}
	}
	return false // all failing packages are outside this bead's changes
}

// RunChainSuiteGate runs the project's full test gate in the chain worktree
// BEFORE the chain integrates — the compensating control for per-bead gate
// relaxations (RED inverse check, foreign-failure attribution). Per-bead
// gates are scoped or attributed; neither can see cross-bead breakage that
// only manifests when all beads' changes combine (e.g., a RED bead's test
// helper breaking another chain's package). The chain worktree holds the
// union of the chain's changes, so a full run here is the net.
//
// Fail-OPEN on runner faults (a gate infrastructure fault must not wedge
// integration — the work would strand); fail-CLOSED on real test failures
// (quarantine, do not integrate broken work). Returns nil to proceed with
// integration, non-nil to quarantine.
func (c *BeadCloser) RunChainSuiteGate(projectName, projectPath, worktreePath, chainKey string) error {
	gates := c.gatesFor(projectName)
	if gates == nil || !gates.DaemonOwned || gates.Test.Cmd == "" {
		return nil // no daemon-owned test gate declared → nothing to enforce
	}
	if c.gateRunner == nil {
		log.Printf("[ChainSuiteGate] %s: no gate runner attached — skipping full-suite check (fail-open)", chainKey)
		return nil
	}
	if worktreePath == "" || worktreePath == projectPath {
		log.Printf("[ChainSuiteGate] %s: no chain worktree — skipping full-suite check (fail-open)", chainKey)
		return nil
	}
	a, err := c.gateRunner.RunGate(GateRequest{
		Project:      projectName,
		BeadID:       chainKey,
		Gate:         "chain-suite",
		Spec:         gates.Test,
		ProjectPath:  projectPath,
		WorktreePath: worktreePath,
	})
	if err != nil {
		// Fail-open: gate infrastructure fault must not strand the chain's work.
		log.Printf("[ChainSuiteGate] %s: runner fault (fail-open, proceeding): %v", chainKey, err)
		if c.events != nil {
			c.events.EmitRepo(projectPath, Event{Type: "chain_suite_gate_fault", Reason: "runner-fault",
				Bead: chainKey, Detail: err.Error()})
		}
		return nil
	}
	if c.events != nil {
		c.events.EmitRepo(projectPath, Event{Type: "chain_suite_gate_run", Reason: "chain-suite",
			Bead: chainKey, Fields: map[string]any{"status": a.Status, "exit_code": a.ExitCode}})
	}
	if a.Status != GateStatusPassed {
		summary := a.Summary
		if summary == "" {
			summary = "chain full-suite test gate failed"
		}
		log.Printf("[ChainSuiteGate] %s: FULL SUITE FAILED (exit %d) — quarantining chain instead of integrating", chainKey, a.ExitCode)
		if c.events != nil {
			c.events.EmitRepo(projectPath, Event{Type: "chain_suite_gate_failed", Reason: "full-suite-failed",
				Bead: chainKey, Detail: summary})
		}
		return fmt.Errorf("chain suite gate: %s", summary)
	}
	log.Printf("[ChainSuiteGate] %s: full suite passed (exit %d) — safe to integrate", chainKey, a.ExitCode)
	return nil
}

// isForeignTestFailure reports whether all failing test packages in the gate
// output are outside this bead's changed files. With parallel PRD chains
// sharing a worktree branch, a RED bead's tests land on the branch mid-campaign
// and block unrelated chains' REFACTOR/SYNTAX closes. This function parses
// `go test` output for FAIL lines, extracts failing package paths, and checks
// whether ANY of them intersect the bead's own diff (chain worktree vs base).
// True = all failures are foreign → the close should proceed.
func (c *BeadCloser) isForeignTestFailure(bead *types.Bead, worktreePath string, artifact GateArtifact) bool {
	// Parse failing packages from the raw output (go test prints
	// "FAIL\t<package-path>\t<duration>" lines and "--- FAIL: <TestName>" lines)
	failingPkgs := parseFailingGoPackages(artifact.Summary + "\n" + string(artifact.RawLog))
	if len(failingPkgs) == 0 {
		return false // can't parse → assume not foreign (fail-closed)
	}

	// Get this bead's changed files (from worktree diff vs base)
	changed, err := c.changedFilePaths(worktreePath)
	if err != nil || len(changed) == 0 {
		return false // can't determine changes → assume not foreign
	}

	// Map changed files to their directories; a failing package matches if
	// any changed file is in the failing package's directory tree.
	for _, pkg := range failingPkgs {
		pkgDir := goPackageToDir(pkg)
		for _, f := range changed {
			if strings.HasPrefix(f, pkgDir) {
				return false // this bead changed a file in a failing package
			}
		}
	}
	return true // all failing packages are outside this bead's changes
}

// parseFailingGoPackages extracts package paths from `go test` output.
// Looks for lines like: "FAIL\tgithub.com/user/repo/pkg/sub\t1.234s"
func parseFailingGoPackages(output string) []string {
	var pkgs []string
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "FAIL") && strings.Contains(line, "\t") {
			parts := strings.Split(line, "\t")
			if len(parts) >= 2 {
				pkg := strings.TrimSpace(parts[1])
				if pkg != "" && !strings.HasPrefix(pkg, "[") {
					pkgs = append(pkgs, pkg)
				}
			}
		}
	}
	return pkgs
}

// goPackageToDir converts a Go import path to a directory path.
// "github.com/user/repo/internal/core" → "internal/core"
func goPackageToDir(pkg string) string {
	// Strip the module prefix (everything up to and including the repo name).
	// The module path is like "github.com/haulmatic-dev/hydra" — take the
	// part after the last path segment that matches the repo root.
	parts := strings.Split(pkg, "/")
	if len(parts) < 3 {
		return pkg
	}
	// Find the segment that is a known top-level dir (cmd, internal, pkg, tools)
	for i, p := range parts {
		if p == "cmd" || p == "internal" || p == "pkg" || p == "tools" {
			return strings.Join(parts[i:], "/")
		}
	}
	return pkg
}

// changedFilePaths returns the repo-relative paths of files changed in the
// worktree relative to its merge-base with the base branch.
func (c *BeadCloser) changedFilePaths(worktreePath string) ([]string, error) {
	if worktreePath == "" {
		return nil, fmt.Errorf("empty worktree path")
	}
	// Find the merge-base with the default branch (development for hydra)
	base, err := gitOut(worktreePath, "merge-base", "HEAD", "origin/development")
	if err != nil {
		// Try development without origin/ prefix
		base, err = gitOut(worktreePath, "merge-base", "HEAD", "development")
		if err != nil {
			return nil, fmt.Errorf("merge-base: %w", err)
		}
	}
	out, err := gitOut(worktreePath, "diff", "--name-only", strings.TrimSpace(base)+"...HEAD")
	if err != nil {
		return nil, fmt.Errorf("diff: %w", err)
	}
	var files []string
	for _, f := range strings.Split(strings.TrimSpace(out), "\n") {
		if f = strings.TrimSpace(f); f != "" {
			files = append(files, f)
		}
	}
	return files, nil
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

// daemonOwnedGates reports whether a project has daemon-owned gates enabled.
func daemonOwnedGates(cfg *Config, project string) bool {
	if cfg == nil {
		return false
	}
	for i := range cfg.Projects {
		p := &cfg.Projects[i]
		if p.Name == project {
			return p.Gates != nil && p.Gates.DaemonOwned
		}
	}
	return false
}
