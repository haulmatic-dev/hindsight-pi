package core

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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

// GateSet is the per-project gate block in hydra.yaml. DaemonOwned is the
// flag: default false keeps the legacy agent-runs-verification path.
type GateSet struct {
	DaemonOwned bool     `yaml:"daemon_owned"`
	Test        GateSpec `yaml:"test,omitempty"`
	Lint        GateSpec `yaml:"lint,omitempty"`
	Build       GateSpec `yaml:"build,omitempty"`
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
	cmd := exec.CommandContext(ctx, "sh", "-c", req.Spec.Cmd)
	cmd.Dir = req.WorktreePath
	cmd.Stdout = logFile
	cmd.Stderr = logFile

	runErr := cmd.Run()
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
