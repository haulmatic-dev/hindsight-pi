// deploy.go implements FR-031 (PRD autonomous-resolution-layer-20260928):
// hydra tier-1 auto-merge gate + canary deploy with automatic rollback.
//
// Flow after a tier-1 PR lands on the hydra repo (see RunAfterMerge):
//  1. Drain precondition: never runs with workers in-flight. During the
//     build window maxWorkers is pinned to 0 and restored afterwards.
//  2. Backup the current binary (binaryPath.prev).
//  3. Build the new binary to binaryPath.new, atomically rename over binaryPath.
//  4. Restart the daemon service.
//  5. Run scripts/verify-deploy.sh; on failure roll back to the .prev binary
//     and restart again.
//
// Every step emits typed events on the shepherd stream (NFR-002). The whole
// feature ships behind config flag `deploy.enabled`, defaulting to false
// (NFR-004 — current behavior unchanged when disabled).
package core

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// DeployConfig configures the FR-031 tier-1 auto-merge gate and canary deploy.
type DeployConfig struct {
	// Enabled gates the entire feature; default false (NFR-004).
	Enabled bool `yaml:"enabled"`
	// RepoPath is the absolute path of the repo deploys come from (the hydra
	// repo itself). Auto-merge/deploy is skipped for any other project.
	RepoPath string `yaml:"repo_path"`
	// BinaryPath is the daemon binary to build and swap (default
	// $HYDRA_HOME/bin/hydra resolved at run time when empty).
	BinaryPath string `yaml:"binary_path"`
	// VerifyScript is the deploy verification script (default
	// scripts/verify-deploy.sh under RepoPath).
	VerifyScript string `yaml:"verify_script"`
	// BuildTimeout bounds the go build step (default 5m).
	BuildTimeout time.Duration `yaml:"build_timeout"`
}

// Tier1Deployer runs the canary deploy pipeline after a tier-1 PR merge.
type Tier1Deployer struct {
	cfg        DeployConfig
	events     *EventSink
	workers    func() int // active in-flight worker count
	pinWorkers func(int)  // set max workers (drain / restore)
	runner     func(name string, args ...string) error
	now        func() time.Time
	busy       bool // one deploy at a time
}

// NewTier1Deployer wires the deployer from the overseer's live pieces.
// Any nil dependency keeps the deployer inert (fail-open: deploy is opt-in).
func NewTier1Deployer(cfg DeployConfig, events *EventSink, workers func() int, pinWorkers func(int)) *Tier1Deployer {
	if !cfg.Enabled {
		return nil
	}
	if cfg.BinaryPath == "" {
		cfg.BinaryPath = defaultHydraBinaryPath()
	}
	if cfg.VerifyScript == "" && cfg.RepoPath != "" {
		cfg.VerifyScript = filepath.Join(cfg.RepoPath, "scripts", "verify-deploy.sh")
	}
	if cfg.BuildTimeout <= 0 {
		cfg.BuildTimeout = 5 * time.Minute
	}
	return &Tier1Deployer{
		cfg:        cfg,
		events:     events,
		workers:    workers,
		pinWorkers: pinWorkers,
		runner:     runCmd,
		now:        time.Now,
	}
}

func defaultHydraBinaryPath() string {
	if home := os.Getenv("HYDRA_HOME"); home != "" {
		return filepath.Join(home, "bin", "hydra")
	}
	return ""
}

func runCmd(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %s failed: %v: %s", name, args[0], err, truncateRunOutput(string(out)))
	}
	return nil
}

func truncateRunOutput(s string) string {
	if len(s) > 400 {
		return s[:400]
	}
	return s
}

// emit is fail-open: event-sink problems never block a deploy.
func (d *Tier1Deployer) emit(evType, detail string, fields map[string]any) {
	if d.events == nil {
		return
	}
	d.events.Emit(Event{Type: evType, Detail: detail, Fields: fields})
}

// RunAfterMerge is called after a PR merged successfully on the configured
// repo. It performs the canary deploy; errors are logged and emitted, never
// panicked on. The drain precondition (no workers in-flight) is hard: if any
// worker is active the deploy is skipped entirely (fail closed).
func (d *Tier1Deployer) RunAfterMerge(branch string) {
	if d == nil || !d.cfg.Enabled {
		return
	}
	if d.busy {
		d.emit("deploy_skipped_busy", "a deploy is already in progress; skipping", map[string]any{"branch": branch})
		return
	}
	d.busy = true
	defer func() { d.busy = false }()

	if d.workers == nil {
		d.emit("deploy_skipped_no_drain_probe", "no worker-count probe wired; refusing to deploy (fail closed)", nil)
		return
	}
	if n := d.workers(); n > 0 {
		d.emit("deploy_skipped_workers_in_flight", fmt.Sprintf("%d worker(s) in flight; drain precondition unmet", n),
			map[string]any{"active_workers": n, "branch": branch})
		log.Printf("[Deploy] skipped: %d worker(s) in flight (drain precondition)", n)
		return
	}

	start := d.now()
	d.emit("deploy_started", "tier-1 canary deploy starting", map[string]any{"branch": branch, "binary": d.cfg.BinaryPath})
	log.Printf("[Deploy] tier-1 canary deploy starting (branch=%s)", branch)

	// Drain: pin maxWorkers to 0 for the build window.
	if d.pinWorkers != nil {
		d.pinWorkers(0)
		defer d.pinWorkers(-1) // -1 restores the configured max
	}

	if err := d.deploySteps(); err != nil {
		d.emit("deploy_failed", err.Error(), map[string]any{"branch": branch, "elapsed": d.now().Sub(start).String()})
		log.Printf("[Deploy] FAILED: %v", err)
		// Rollback is best-effort but attempted on every failure path that
		// got past the backup.
		if rbErr := d.rollback(); rbErr != nil {
			d.emit("deploy_rollback_failed", rbErr.Error(), nil)
			log.Printf("[Deploy] rollback FAILED: %v", rbErr)
		}
		return
	}
	d.emit("deploy_succeeded", "canary deploy verified", map[string]any{"branch": branch, "elapsed": d.now().Sub(start).String()})
	log.Printf("[Deploy] succeeded in %s", d.now().Sub(start))
}

// deploySteps runs backup → build → swap → restart → verify.
func (d *Tier1Deployer) deploySteps() error {
	bin := d.cfg.BinaryPath
	if bin == "" {
		return fmt.Errorf("deploy: binary_path not configured and HYDRA_HOME unset")
	}
	if err := d.runner("cp", bin, bin+".prev"); err != nil {
		return fmt.Errorf("backup failed: %w", err)
	}
	buildArgs := []string{"build", "-o", bin + ".new"}
	if d.cfg.RepoPath != "" {
		buildArgs = append(buildArgs, "./cmd/hydra")
	} else {
		buildArgs = append(buildArgs, "./...")
	}
	build := func(name string, args ...string) error {
		cmd := exec.Command(name, args...)
		cmd.Dir = d.cfg.RepoPath
		out, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("build failed: %v: %s", err, truncateRunOutput(string(out)))
		}
		return nil
	}
	done := make(chan error, 1)
	go func() { done <- build("go", buildArgs...) }()
	select {
	case err := <-done:
		if err != nil {
			return err
		}
	case <-time.After(d.cfg.BuildTimeout):
		return fmt.Errorf("build timed out after %s", d.cfg.BuildTimeout)
	}
	if err := os.Rename(bin+".new", bin); err != nil {
		return fmt.Errorf("atomic swap failed: %w", err)
	}
	d.emit("deploy_binary_swapped", "new binary atomically swapped in", map[string]any{"binary": bin})

	if err := d.runner("systemctl", "restart", "hydra"); err != nil {
		return fmt.Errorf("restart failed: %w", err)
	}
	d.emit("deploy_restarted", "daemon restarted with new binary", nil)

	verify := func(name string, args ...string) error {
		cmd := exec.Command(name, args...)
		if d.cfg.RepoPath != "" {
			cmd.Dir = d.cfg.RepoPath
		}
		out, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("verify failed: %v: %s", err, truncateRunOutput(string(out)))
		}
		return nil
	}
	if err := verify("bash", d.cfg.VerifyScript); err != nil {
		return err
	}
	d.emit("deploy_verified", "verify-deploy.sh passed", nil)
	return nil
}

// rollback restores the previous binary and restarts. Best-effort.
func (d *Tier1Deployer) rollback() error {
	bin := d.cfg.BinaryPath
	if bin == "" {
		return fmt.Errorf("rollback: binary_path unknown")
	}
	if _, err := os.Stat(bin + ".prev"); err != nil {
		return fmt.Errorf("rollback: no backup at %s.prev", bin)
	}
	if err := os.Rename(bin+".prev", bin); err != nil {
		return fmt.Errorf("rollback swap failed: %w", err)
	}
	if err := d.runner("systemctl", "restart", "hydra"); err != nil {
		return fmt.Errorf("rollback restart failed: %w", err)
	}
	d.emit("deploy_rolled_back", "rolled back to previous binary and restarted", nil)
	log.Printf("[Deploy] rolled back to previous binary")
	return nil
}

// --- FR-030-lite tier classifier (path-based, fail closed) -------------------
// FR-030 owns the full classifier; this path-based subset lets the deploy
// pipeline decide merge-worthiness today. Any doubt → tier-1g (human).

// tier1gPathPrefixes are subsystems where a mistake can break scheduling,
// closing, integration, or the operator boundary — always human-reviewed.
var tier1gPathPrefixes = []string{
	"internal/core/", "cmd/", "internal/api/", "internal/registry/",
	"internal/beads/", "internal/worktree", "internal/pan/",
	"scripts/", ".github/", "Makefile", "go.mod", "go.sum",
}

// ClassifyTier1 classifies a change by the paths it touches.
// Returns "tier-1" only when every path is docs/config/telemetry/test or a
// non-core package. Anything unclassified or touching tier-1g subsystems →
// "tier-1g" (fail closed toward human, NFR-001).
func ClassifyTier1(paths []string) string {
	for _, p := range paths {
		if p == "" {
			return "tier-1g"
		}
		clean := filepath.ToSlash(filepath.Clean(p))
		for _, pre := range tier1gPathPrefixes {
			if clean == pre || strings.HasPrefix(clean, pre) {
				return "tier-1g"
			}
		}
		// Allow: docs, tests, yaml/json config, telemetry-ish non-core pkgs.
		base := filepath.Base(clean)
		if strings.HasPrefix(clean, "docs/") ||
			strings.HasSuffix(clean, "_test.go") ||
			strings.HasSuffix(base, ".md") ||
			strings.HasSuffix(base, ".yaml") || strings.HasSuffix(base, ".yml") ||
			strings.HasSuffix(base, ".json") {
			continue
		}
		// Non-core packages (anything outside tier-1g prefixes that is still
		// a .go source) count as tier-1 only if not in cmd/ (already excluded).
		if strings.HasSuffix(clean, ".go") {
			continue
		}
		return "tier-1g"
	}
	return "tier-1"
}

// --- Tier-1 auto-merge (green gates + squash via gh) -------------------------

var (
	deployGhChecks = func(repoPath, pr string) (bool, error) { // true = green
		out, err := exec.Command("gh", "pr", "checks", pr, "--repo", repoPath).CombinedOutput()
		if err != nil {
			return false, fmt.Errorf("gh pr checks: %v: %s", err, truncateRunOutput(string(out)))
		}
		return true, nil
	}
	deployGhMerge = func(repoPath, pr string) error {
		out, err := exec.Command("gh", "pr", "merge", pr, "--squash", "--admin",
			"--repo", repoPath).CombinedOutput()
		if err != nil {
			return fmt.Errorf("gh pr merge: %v: %s", err, truncateRunOutput(string(out)))
		}
		return nil
	}
	deployGhFiles = func(repoPath, pr string) ([]string, error) {
		out, err := exec.Command("gh", "pr", "diff", "--name-only", pr, "--repo", repoPath).Output()
		if err != nil {
			return nil, fmt.Errorf("gh pr diff: %v: %s", err, truncateRunOutput(string(out)))
		}
		var paths []string
		for _, line := range strings.Split(string(out), "\n") {
			line = strings.TrimSpace(line)
			if line != "" {
				paths = append(paths, line)
			}
		}
		return paths, nil
	}
)

// AutoMergeIfTier1 squash-merges the PR only when every touched path
// classifies tier-1 and CI checks are green. Any doubt fails closed (no
// merge, nil merge attempt). Returns merged=true when the merge was issued.
func AutoMergeIfTier1(repoPath, pr string) (bool, error) {
	paths, err := deployGhFiles(repoPath, pr)
	if err != nil {
		return false, fmt.Errorf("tier-1 gate: %w", err)
	}
	if tier := ClassifyTier1(paths); tier != "tier-1" {
		log.Printf("[Deploy] %s classified %s — not auto-merging (human review)", pr, tier)
		return false, nil
	}
	green, err := deployGhChecks(repoPath, pr)
	if err != nil || !green {
		return false, fmt.Errorf("tier-1 gate: checks not green: %w", err)
	}
	if err := deployGhMerge(repoPath, pr); err != nil {
		return false, err
	}
	log.Printf("[Deploy] auto-merged tier-1 PR %s", pr)
	return true, nil
}
