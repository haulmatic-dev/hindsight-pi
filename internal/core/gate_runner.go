package core

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// GateSpec is one declared gate (Class B): the command, its parser, and the
// pass threshold (parser-defined semantics; unused by exit-code).
type GateSpec struct {
	Cmd       string        `yaml:"cmd"`
	Parser    string        `yaml:"parser"`
	Threshold int           `yaml:"threshold,omitempty"`
	Timeout   time.Duration `yaml:"timeout,omitempty"`
}

// CoverageTruthGate verifies the *unfiltered* total coverage (no exception
// patterns) is within MaxGapPoints of the enforced ThresholdPct. This closes
// the loophole where workers exclude low-coverage files so a ratchet passes.
// Config absent -> gate inert (fail-open for unconfigured projects).
type CoverageTruthGate struct {
	Cmd             string        `yaml:"cmd"`
	SummaryJSONPath string        `yaml:"summary_json_path"`
	ThresholdPct    float64       `yaml:"threshold_pct"`
	MaxGapPoints    float64       `yaml:"max_gap_points"`
	Timeout         time.Duration `yaml:"timeout,omitempty"`
}

// GateSet is the per-project gate block in hydra.yaml. DaemonOwned is the
// flag: default false keeps the legacy agent-runs-verification path.
type GateSet struct {
	DaemonOwned   bool               `yaml:"daemon_owned"`
	Test          GateSpec           `yaml:"test,omitempty"`
	Lint          GateSpec           `yaml:"lint,omitempty"`
	Build         GateSpec           `yaml:"build,omitempty"`
	CoverageTruth *CoverageTruthGate `yaml:"coverage_truth,omitempty"`
}

const gateDefaultTimeout = 10 * time.Minute

// gateSummaryCap bounds what ever reaches a prompt or bead annotation.
const gateSummaryCap = 2048

// GateRequest is one gate invocation.
type GateRequest struct {
	Project      string
	BeadID       string
	Gate         string // "test" | "lint" | "build" (free-form, artifact key)
	Spec         GateSpec
	ProjectPath  string // main checkout — never executed in
	WorktreePath string // execution cwd
	// Scope is substituted for <scope> in Spec.Cmd — the bead's target file
	// for scoped gates (npx jest <scope>). Empty scope drops the token
	// carrying the placeholder, so the full gate command runs instead. The
	// value is single-quote escaped before reaching the shell.
	Scope string
	// AllowMainCheckout permits running with WorktreePath == ProjectPath.
	// Reserved for `hydra gates verify`, whose whole job is proving the
	// toolchain on the real checkout before daemon_owned is enabled.
	AllowMainCheckout bool
}

// GateRunner executes deterministic verification commands daemon-side.
// Gates serialize per project (two test runs in one repo collide on caches,
// ports, and coverage files) and never hold worker slots.
type GateRunner struct {
	root  string
	mu    sync.Mutex
	locks map[string]*sync.Mutex
}

// NewGateRunner roots the artifact store at <root>/gates (root = HYDRA_HOME).
func NewGateRunner(root string) *GateRunner {
	return &GateRunner{root: root, locks: make(map[string]*sync.Mutex)}
}

func (r *GateRunner) projectLock(project string) *sync.Mutex {
	r.mu.Lock()
	defer r.mu.Unlock()
	l, ok := r.locks[project]
	if !ok {
		l = &sync.Mutex{}
		r.locks[project] = l
	}
	return l
}

// RunCoverageTruthGate executes the unfiltered coverage command, reads the
// produced JSON summary, and verifies the true total is within the allowed
// gap of the enforced threshold. It persists a gate artifact and returns it.
// The gate fails closed on any runner, command, JSON, or math fault.
func (r *GateRunner) RunCoverageTruthGate(req GateRequest, truth CoverageTruthGate) (GateArtifact, error) {
	if req.WorktreePath == "" || (!req.AllowMainCheckout && req.WorktreePath == req.ProjectPath) {
		return GateArtifact{}, fmt.Errorf("coverage-truth gate for %s: refusing to run in main checkout", req.BeadID)
	}
	if truth.Cmd == "" {
		return GateArtifact{}, fmt.Errorf("coverage-truth gate for %s: empty command", req.BeadID)
	}
	if truth.SummaryJSONPath == "" {
		return GateArtifact{}, fmt.Errorf("coverage-truth gate for %s: empty summary_json_path", req.BeadID)
	}

	timeout := truth.Timeout
	if timeout <= 0 {
		timeout = gateDefaultTimeout
	}

	dir := gateDir(r.root, req.Project, req.BeadID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return GateArtifact{}, fmt.Errorf("coverage-truth artifact dir: %w", err)
	}
	rawLog := filepath.Join(dir, req.Gate+".log")

	a := GateArtifact{
		Project:   req.Project,
		BeadID:    req.BeadID,
		Gate:      req.Gate,
		Status:    GateStatusRunning,
		PID:       os.Getpid(),
		StartedAt: time.Now(),
		RawLog:    rawLog,
	}
	// Artifact-first: the running marker lands before the command starts.
	if err := WriteGateArtifact(r.root, a); err != nil {
		return GateArtifact{}, err
	}

	lock := r.projectLock(req.Project)
	lock.Lock()
	defer lock.Unlock()

	logFile, err := os.Create(rawLog)
	if err != nil {
		return GateArtifact{}, fmt.Errorf("coverage-truth raw log: %w", err)
	}
	defer logFile.Close()

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "sh", "-c", truth.Cmd)
	cmd.Dir = req.WorktreePath
	cmd.Stdout = logFile
	cmd.Stderr = logFile

	// Record the actual gate child PID, not the daemon's: the running marker
	// with the daemon PID stays "alive" while the daemon lives, so a stale
	// running artifact (crash mid-gate before the final write) permanently
	// fooled close-in-progress probes and blocked orphan recovery.
	if err := cmd.Start(); err != nil {
		return GateArtifact{}, fmt.Errorf("coverage-truth gate for %s: start: %w", req.BeadID, err)
	}
	a.PID = cmd.Process.Pid
	if err := WriteGateArtifact(r.root, a); err != nil {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
		return GateArtifact{}, fmt.Errorf("coverage-truth artifact pid rewrite: %w", err)
	}

	runErr := cmd.Wait()
	a.EndedAt = time.Now()

	exitCode := 0
	switch {
	case ctx.Err() == context.DeadlineExceeded:
		exitCode = -1
	case runErr != nil:
		if ee, ok := runErr.(*exec.ExitError); ok {
			exitCode = ee.ExitCode()
		} else {
			return GateArtifact{}, fmt.Errorf("coverage-truth gate for %s: exec fault: %w", req.BeadID, runErr)
		}
	}
	a.ExitCode = exitCode

	summaryPath := truth.SummaryJSONPath
	if !filepath.IsAbs(summaryPath) {
		summaryPath = filepath.Join(req.WorktreePath, summaryPath)
	}

	var summary string
	var passed bool
	if exitCode != 0 {
		passed = false
		summary = fmt.Sprintf("truth command failed: exit %d", exitCode)
	} else {
		total, parseErr := parseCoverageSummaryJSON(summaryPath)
		if parseErr != nil {
			passed = false
			summary = fmt.Sprintf("failed to parse truth summary %s: %v", summaryPath, parseErr)
		} else {
			gap := truth.ThresholdPct - total
			if gap > truth.MaxGapPoints {
				passed = false
				summary = fmt.Sprintf("coverage truth gap exceeded: enforced threshold %.2f%%, true total %.2f%%, gap %.2f points (max %.2f)", truth.ThresholdPct, total, gap, truth.MaxGapPoints)
			} else {
				passed = true
				summary = fmt.Sprintf("coverage truth OK: threshold %.2f%%, true total %.2f%%, gap %.2f points (max %.2f)", truth.ThresholdPct, total, gap, truth.MaxGapPoints)
			}
		}
	}

	a.Summary = summary
	if passed {
		a.Status = GateStatusPassed
	} else {
		a.Status = GateStatusFailed
	}
	if len(a.Summary) > gateSummaryCap {
		a.Summary = a.Summary[:gateSummaryCap]
	}

	if err := WriteGateArtifact(r.root, a); err != nil {
		return a, fmt.Errorf("coverage-truth artifact final write: %w", err)
	}
	return a, nil
}

// parseCoverageSummaryJSON reads a Jest coverage-summary.json total
// statements percentage. Other tools may be added; a missing or malformed
// total is a parse failure so the gate fails closed.
func parseCoverageSummaryJSON(path string) (float64, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, fmt.Errorf("read summary: %w", err)
	}
	var summary struct {
		Total struct {
			Statements struct {
				Pct float64 `json:"pct"`
			} `json:"statements"`
		} `json:"total"`
	}
	if err := json.Unmarshal(data, &summary); err != nil {
		return 0, fmt.Errorf("unmarshal summary: %w", err)
	}
	return summary.Total.Statements.Pct, nil
}

// RunGate executes the gate and returns the final artifact. Gate failures
// (nonzero exit, below threshold, timeout) are DATA — a failed artifact, nil
// error. Errors are reserved for runner-level faults (bad spec, main-checkout
// violation, unknown parser) which fail closed.
func (r *GateRunner) RunGate(req GateRequest) (GateArtifact, error) {
	if req.WorktreePath == "" || (!req.AllowMainCheckout && req.WorktreePath == req.ProjectPath) {
		return GateArtifact{}, fmt.Errorf("gate %s for %s: refusing to run in main checkout (worktree path required)", req.Gate, req.BeadID)
	}
	parser, ok := GateParserFor(req.Spec.Parser)
	if !ok {
		return GateArtifact{}, fmt.Errorf("gate %s for %s: unknown parser %q (fail closed)", req.Gate, req.BeadID, req.Spec.Parser)
	}
	if req.Spec.Cmd == "" {
		return GateArtifact{}, fmt.Errorf("gate %s for %s: empty command", req.Gate, req.BeadID)
	}

	timeout := req.Spec.Timeout
	if timeout <= 0 {
		timeout = gateDefaultTimeout
	}

	cmdLine := substituteGateScope(req.Spec.Cmd, req.Scope)

	dir := gateDir(r.root, req.Project, req.BeadID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return GateArtifact{}, fmt.Errorf("gate artifact dir: %w", err)
	}
	rawLog := filepath.Join(dir, req.Gate+".log")

	a := GateArtifact{
		Project:   req.Project,
		BeadID:    req.BeadID,
		Gate:      req.Gate,
		Status:    GateStatusRunning,
		PID:       os.Getpid(),
		StartedAt: time.Now(),
		RawLog:    rawLog,
	}
	// Artifact-first: the running marker lands before the command starts, so
	// a daemon crash mid-gate leaves recoverable state, not a blind spot.
	if err := WriteGateArtifact(r.root, a); err != nil {
		return GateArtifact{}, err
	}

	// Serialize per project: gates in one repo cannot overlap.
	lock := r.projectLock(req.Project)
	lock.Lock()
	defer lock.Unlock()

	logFile, err := os.Create(rawLog)
	if err != nil {
		return GateArtifact{}, fmt.Errorf("gate raw log: %w", err)
	}
	defer logFile.Close()

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "sh", "-c", cmdLine)
	cmd.Dir = req.WorktreePath
	cmd.Stdout = logFile
	cmd.Stderr = logFile

	// Record the actual gate child PID, not the daemon's: the running marker
	// with the daemon PID stays "alive" while the daemon lives, so a stale
	// running artifact (crash mid-gate before the final write) permanently
	// fooled close-in-progress probes and blocked orphan recovery.
	if err := cmd.Start(); err != nil {
		return GateArtifact{}, fmt.Errorf("gate %s for %s: start: %w", req.Gate, req.BeadID, err)
	}
	a.PID = cmd.Process.Pid
	if err := WriteGateArtifact(r.root, a); err != nil {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
		return GateArtifact{}, fmt.Errorf("gate artifact pid rewrite: %w", err)
	}

	runErr := cmd.Wait()
	a.EndedAt = time.Now()

	exitCode := 0
	switch {
	case ctx.Err() == context.DeadlineExceeded:
		exitCode = -1
	case runErr != nil:
		if ee, ok := runErr.(*exec.ExitError); ok {
			exitCode = ee.ExitCode()
		} else {
			return GateArtifact{}, fmt.Errorf("gate %s for %s: exec fault: %w", req.Gate, req.BeadID, runErr)
		}
	}
	a.ExitCode = exitCode

	output, _ := os.ReadFile(rawLog)
	verdict := parser.Parse(string(output), exitCode, req.Spec.Threshold)
	if ctx.Err() == context.DeadlineExceeded {
		verdict.Passed = false
		verdict.Summary = fmt.Sprintf("timeout after %s\n%s", timeout, verdict.Summary)
	}
	if len(verdict.Summary) > gateSummaryCap {
		verdict.Summary = verdict.Summary[:gateSummaryCap]
	}
	a.Summary = verdict.Summary
	if verdict.Passed {
		a.Status = GateStatusPassed
	} else {
		a.Status = GateStatusFailed
	}

	if err := WriteGateArtifact(r.root, a); err != nil {
		return a, fmt.Errorf("gate artifact final write: %w", err)
	}
	return a, nil
}

// substituteGateScope resolves the <scope> placeholder in a gate command.
// With a scope: single-quote-escaped substitution. Without one: any token
// containing the placeholder is dropped entirely — a literal <scope> would
// reach sh as a redirection and break the command.
func substituteGateScope(cmd, scope string) string {
	if scope == "" {
		if !strings.Contains(cmd, "<scope>") {
			return cmd
		}
		var kept []string
		for _, tok := range strings.Fields(cmd) {
			if !strings.Contains(tok, "<scope>") {
				kept = append(kept, tok)
			}
		}
		return strings.Join(kept, " ")
	}
	escaped := "'" + strings.ReplaceAll(scope, "'", "'\\''") + "'"
	return strings.ReplaceAll(cmd, "<scope>", escaped)
}
