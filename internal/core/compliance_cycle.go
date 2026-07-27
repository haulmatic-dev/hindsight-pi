package core

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// ComplianceCycleConfig configures the closed-loop compliance system.
type ComplianceCycleConfig struct {
	Enabled           bool          `yaml:"enabled"`
	MaxCycles         int           `yaml:"max_cycles"`
	PollInterval      time.Duration `yaml:"poll_interval"`
	AnalyzerPath      string        `yaml:"analyzer_path"`
	GapToPRDPath      string        `yaml:"gap_to_prd_path"`
	TaskBreakdownPath string        `yaml:"task_breakdown_path"`
	ProjectDir        string        `yaml:"project_dir"`
	PRDPath           string        `yaml:"prd_path"`

	// AdjudicatorModel overrides the pi model used for gap adjudication
	// (the high-IQ "is this gap real or a false positive" judgment). When
	// empty, the configured agents.pi model is used.
	AdjudicatorModel string `yaml:"adjudicator_model"`

	// AdjudicatorThinking overrides the reasoning effort for the adjudication
	// call. Valid values: "low", "high", "max" (the endpoint maps "medium" to
	// "high"); empty inherits the agent default. Adjudication is the one
	// high-consequence, low-volume judgment in the loop — a wrong verdict
	// either drops a real defect or spawns a wasted bead chain — so it is the
	// place where paying for maximum reasoning effort is justified, unlike the
	// high-volume build phases.
	AdjudicatorThinking string `yaml:"adjudicator_thinking"`

	// ShipGate is the product-level acceptance gate run before a PRD is
	// declared shippable (build/boot/e2e). Disabled by default.
	ShipGate ShipGateConfig `yaml:"ship_gate"`
}

// ComplianceCycleManager runs the closed-loop compliance cycle.
// It detects when all beads are closed, runs the analyzer, and creates gap beads if needed.
type ComplianceCycleManager struct {
	config        ComplianceCycleConfig
	overseer      *Overseer
	cycles        int
	running       bool
	approved      bool // sticky in-memory approval flag; see isApproved
	stopCh        chan struct{}
	prevSurviving map[string]bool // fingerprints of last cycle's real gaps (progress-delta)
}

// NewComplianceCycleManager creates a new compliance cycle manager.
func NewComplianceCycleManager(overseer *Overseer, config ComplianceCycleConfig) *ComplianceCycleManager {
	return &ComplianceCycleManager{
		overseer: overseer,
		config:   config,
		stopCh:   make(chan struct{}),
	}
}

// Start begins the compliance cycle polling loop.
func (cm *ComplianceCycleManager) Start(ctx context.Context) {
	if !cm.config.Enabled {
		log.Println("[ComplianceCycle] Disabled, not starting")
		return
	}
	if cm.config.PollInterval == 0 {
		cm.config.PollInterval = 30 * time.Second
	}
	if cm.config.MaxCycles == 0 {
		cm.config.MaxCycles = 5
	}

	cm.running = true
	log.Printf("[ComplianceCycle] Starting with max_cycles=%d, poll_interval=%s", cm.config.MaxCycles, cm.config.PollInterval)

	ticker := time.NewTicker(cm.config.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			cm.running = false
			return
		case <-cm.stopCh:
			cm.running = false
			return
		case <-ticker.C:
			if err := cm.checkAndRunCycle(); err != nil {
				log.Printf("[ComplianceCycle] Error: %v", err)
			}
		}
	}
}

// Stop stops the compliance cycle manager.
func (cm *ComplianceCycleManager) Stop() {
	close(cm.stopCh)
}

// approvalSentinelPath is the on-disk marker that records a PRD approval so
// it survives daemon restarts.
func (cm *ComplianceCycleManager) approvalSentinelPath() string {
	return filepath.Join(cm.config.ProjectDir, ".hydra", "compliance-approved")
}

// isApproved reports whether this PRD has already been approved, either in
// this process (in-memory flag) or by a prior run (sentinel file). Once
// approved, the cycle stops re-running the analyzer every poll — previously
// it re-ran until max_cycles and then falsely alarmed "manual intervention
// required". Operator escape hatch: delete the sentinel and restart.
func (cm *ComplianceCycleManager) isApproved() bool {
	if cm.approved {
		return true
	}
	if _, err := os.Stat(cm.approvalSentinelPath()); err == nil {
		cm.approved = true
		return true
	}
	return false
}

// markApproved records the approval both in memory and on disk. The sentinel
// write is best-effort: the in-memory flag alone stops the loop for the
// lifetime of this process.
func (cm *ComplianceCycleManager) markApproved() {
	cm.approved = true
	path := cm.approvalSentinelPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		log.Printf("[ComplianceCycle] approval sentinel dir: %v (in-memory only)", err)
		return
	}
	if err := os.WriteFile(path, []byte(time.Now().Format(time.RFC3339)+"\n"), 0o644); err != nil {
		log.Printf("[ComplianceCycle] approval sentinel write: %v (in-memory only)", err)
	}
}

// checkAndRunCycle checks if all beads are closed and runs a compliance cycle.
func (cm *ComplianceCycleManager) checkAndRunCycle() error {
	// Sticky approval: an approved PRD never re-enters the cycle.
	if cm.isApproved() {
		return nil
	}

	// Only run if overseer is running and not paused
	if !cm.overseer.IsRunning() || cm.overseer.IsPaused() {
		return nil
	}

	// Check if there are active workers
	cm.overseer.workerMutex.RLock()
	activeCount := len(cm.overseer.activeWorkers)
	cm.overseer.workerMutex.RUnlock()
	if activeCount > 0 {
		return nil
	}

	// Check if all beads are closed
	allClosed, openCount, err := cm.areAllBeadsClosed()
	if err != nil {
		return fmt.Errorf("failed to check bead status: %w", err)
	}
	if !allClosed {
		log.Printf("[ComplianceCycle] %d beads still open, waiting...", openCount)
		return nil
	}

	// Check max cycles
	if cm.cycles >= cm.config.MaxCycles {
		log.Printf("[ComplianceCycle] MAX CYCLES (%d) REACHED. Manual intervention required.", cm.config.MaxCycles)
		cm.notifyMaxCyclesReached()
		return fmt.Errorf("max compliance cycles reached")
	}

	cm.cycles++
	log.Printf("[ComplianceCycle] === CYCLE %d/%d START ===", cm.cycles, cm.config.MaxCycles)

	// Step 1: Run compliance analyzer
	gapReportPath, err := cm.runAnalyzer()
	if err != nil {
		return fmt.Errorf("analyzer failed: %w", err)
	}

	// Step 2: Check if gaps found
	gapsFound, err := cm.hasGaps(gapReportPath)
	if err != nil {
		return fmt.Errorf("failed to check gaps: %w", err)
	}

	if !gapsFound {
		log.Printf("[ComplianceCycle] === ZERO GAPS. PRD APPROVED ===")
		cm.finalizeApproval()
		return nil
	}

	// Step 2b: Adjudicate gaps before decomposing them. A high-IQ pass
	// rules each gap real / false_positive / ambiguous so detector false
	// positives don't spawn PRD->bead chains that conclude "no change" and
	// get re-detected every cycle. Only REAL gaps proceed.
	adjudicatedPath, survivors, halted, err := cm.adjudicateGaps(gapReportPath)
	if err != nil {
		log.Printf("[ComplianceCycle] Adjudication failed (%v); falling back to raw gap report", err)
		adjudicatedPath = gapReportPath
	} else if halted {
		log.Printf("[ComplianceCycle] === NO PROGRESS: surviving gaps unchanged from last cycle. Halting, awaiting human. ===")
		cm.notifyMaxCyclesReached()
		return nil
	} else if survivors == 0 {
		log.Printf("[ComplianceCycle] === ALL GAPS RULED FALSE-POSITIVE / RESOLVED. PRD APPROVED ===")
		cm.finalizeApproval()
		return nil
	}

	log.Printf("[ComplianceCycle] %d real gap(s) after adjudication. Converting to gap PRD...", survivors)

	// Step 3: Convert gaps to mini-PRD
	gapPRDPath, err := cm.runGapToPRD(adjudicatedPath)
	if err != nil {
		return fmt.Errorf("gap-to-prd failed: %w", err)
	}

	// Step 4: Create gap beads via task-breakdown
	if err := cm.createGapBeads(gapPRDPath); err != nil {
		return fmt.Errorf("failed to create gap beads: %w", err)
	}

	log.Printf("[ComplianceCycle] === CYCLE %d COMPLETE. New gap beads created. Hydra will pick them up. ===", cm.cycles)
	return nil
}

// areAllBeadsClosed checks if all beads in all projects are closed.
func (cm *ComplianceCycleManager) areAllBeadsClosed() (bool, int, error) {
	for _, project := range cm.overseer.projects {
		repoName := ""
		if len(project.Repos) > 0 {
			repoName = project.Repos[0].Name
		}
		beads, err := cm.overseer.beadsClient.ListBeads(project.Path, repoName)
		if err != nil {
			return false, 0, fmt.Errorf("list beads for %s: %w", project.Name, err)
		}

		openCount := 0
		for _, bead := range beads {
			if bead.Status != types.StatusClosed {
				openCount++
			}
		}
		if openCount > 0 {
			return false, openCount, nil
		}
	}
	return true, 0, nil
}

// runAnalyzer runs the compliance analyzer and returns the gap report path.
func (cm *ComplianceCycleManager) runAnalyzer() (string, error) {
	gapReportPath := filepath.Join(cm.config.ProjectDir, "gaps", fmt.Sprintf("gap-report-cycle-%d.json", cm.cycles))
	if err := os.MkdirAll(filepath.Dir(gapReportPath), 0755); err != nil {
		return "", err
	}

	analyzerBin := cm.config.AnalyzerPath
	if analyzerBin == "" {
		analyzerBin = filepath.Join(cm.config.ProjectDir, "tools", "hydra-compliance-analyzer", "hydra-compliance-analyzer")
	}

	args := []string{
		"--prd", cm.config.PRDPath,
		"--project", cm.config.ProjectDir,
		"--output", gapReportPath,
		"--cycles", fmt.Sprintf("%d", cm.cycles),
	}

	log.Printf("[ComplianceCycle] Running analyzer: %s %s", analyzerBin, strings.Join(args, " "))
	cmd := exec.Command(analyzerBin, args...)
	cmd.Dir = cm.config.ProjectDir
	out, err := cmd.CombinedOutput()

	// Exit 1 means gaps found (expected), exit 0 means zero gaps
	if cmd.ProcessState != nil {
		exitCode := cmd.ProcessState.ExitCode()
		if exitCode != 0 && exitCode != 1 {
			return "", fmt.Errorf("analyzer exited %d: %s", exitCode, string(out))
		}
	} else if err != nil {
		return "", fmt.Errorf("analyzer failed: %v\nOutput: %s", err, string(out))
	}

	return gapReportPath, nil
}

// hasGaps checks if the gap report contains any gaps.
func (cm *ComplianceCycleManager) hasGaps(gapReportPath string) (bool, error) {
	data, err := os.ReadFile(gapReportPath)
	if err != nil {
		return false, err
	}

	var report struct {
		Passed bool `json:"passed"`
	}
	if err := json.Unmarshal(data, &report); err != nil {
		return false, err
	}

	return !report.Passed, nil
}

// runGapToPRD converts the gap report to a mini-PRD.
func (cm *ComplianceCycleManager) runGapToPRD(gapReportPath string) (string, error) {
	gapPRDPath := filepath.Join(cm.config.ProjectDir, "gaps", fmt.Sprintf("gap-prd-cycle-%d.md", cm.cycles))

	gapToPRDBin := cm.config.GapToPRDPath
	if gapToPRDBin == "" {
		gapToPRDBin = filepath.Join(cm.config.ProjectDir, "tools", "gap-to-prd", "gap-to-prd")
	}

	args := []string{
		"--input", gapReportPath,
		"--output", gapPRDPath,
	}

	log.Printf("[ComplianceCycle] Running gap-to-prd: %s %s", gapToPRDBin, strings.Join(args, " "))
	cmd := exec.Command(gapToPRDBin, args...)
	cmd.Dir = cm.config.ProjectDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("gap-to-prd failed: %v\nOutput: %s", err, string(out))
	}

	log.Printf("[ComplianceCycle] Gap PRD created: %s", gapPRDPath)
	return gapPRDPath, nil
}

// createGapBeads invokes task-breakdown on the gap PRD to create new beads.
//
// The task-breakdown skill at config/pi/skills/task-breakdown/cli.mjs
// accepts a single positional argument (the PRD path), not --file.
// We invoke the cli.mjs script directly with `node` so we don't have
// to depend on the `pi skill` wrapper's argument conventions.
func (cm *ComplianceCycleManager) createGapBeads(gapPRDPath string) error {
	cliPath := cm.config.TaskBreakdownPath
	if cliPath == "" {
		// Default: use the cli.mjs shipped with the pi-config.
		cliPath = "/opt/dev-tools/config/pi/skills/task-breakdown/cli.mjs"
	}

	// --json makes the runner emit machine-readable output we can
	// parse if needed. --session-id ensures beads land under a
	// known name prefix so the cycle's gap-tracking is consistent.
	sessionID := fmt.Sprintf("gap-cycle-%d", cm.cycles)
	args := []string{cliPath, gapPRDPath, "--json", "--session-id", sessionID}
	log.Printf("[ComplianceCycle] Creating gap beads via task-breakdown...")
	log.Printf("[ComplianceCycle] Running: node %s", strings.Join(args, " "))

	cmd := exec.Command("node", args...)
	cmd.Dir = cm.config.ProjectDir
	cmd.Env = os.Environ()
	cmdOut, cmdErr := cmd.CombinedOutput()
	if cmdErr != nil {
		return fmt.Errorf("task-breakdown failed: %v\nOutput: %s", cmdErr, string(cmdOut))
	}

	log.Printf("[ComplianceCycle] task-breakdown output:\n%s", string(cmdOut))
	log.Printf("[ComplianceCycle] Gap beads created successfully")

	// Auto-approve the approval gate that task-breakdown created.
	// Without this, the cycle stalls at the verify step because
	// the gate has StatusInProgress and never gets to StatusReady.
	// We use the beadsClient directly (br CLI is async and the
	// overseer owns this state).
	if err := cm.autoApproveGapGate(gapPRDPath); err != nil {
		log.Printf("[ComplianceCycle] WARN: auto-approve gate failed: %v", err)
		// Non-fatal: operator can approve manually with
		// `br close <gate-id> -r approved --force`.
	}

	return nil
}

// autoApproveGapGate finds the approval gate that task-breakdown
// created for this cycle's gap PRD and closes it. The gate is
// identified by title prefix ('APPROVE: <feature> Implementation
// Review') and by the dependency on the gap-PRD's master epic.
func (cm *ComplianceCycleManager) autoApproveGapGate(gapPRDPath string) error {
	beadsClient := cm.overseer.GetBeadsClient()
	if beadsClient == nil {
		return fmt.Errorf("no beads client")
	}

	projectPath := cm.config.ProjectDir
	for _, project := range cm.overseer.projects {
		repoName := ""
		if len(project.Repos) > 0 {
			repoName = project.Repos[0].Name
		}
		beads, err := beadsClient.ListBeads(project.Path, repoName)
		if err != nil {
			continue
		}
		for _, b := range beads {
			// Approval gates from task-breakdown have type=approval
			// (set via createApprovalGate in executor.mjs) and a title
			// matching "APPROVE: ...". The cycle's own gate includes
			// the gap-PRD path in the description. We use both as a
			// signature to avoid approving unrelated gates.
			if b.Type != "approval" {
				continue
			}
			if !strings.HasPrefix(b.Title, "APPROVE:") {
				continue
			}
			if !strings.Contains(b.Description, gapPRDPath) {
				continue
			}
			if b.Status.IsTerminal() {
				continue
			}
			log.Printf("[ComplianceCycle] Auto-approving gate %s: %s", b.ID, b.Title)
			if err := beadsClient.CloseBead(projectPath, repoName, b.ID, "auto-approved by compliance cycle (gap-PRD ready for execution)"); err != nil {
				return fmt.Errorf("close %s: %w", b.ID, err)
			}
		}
	}
	return nil
}

// notifyMaxCyclesReached logs and notifies that max cycles were reached.
func (cm *ComplianceCycleManager) notifyMaxCyclesReached() {
	msg := fmt.Sprintf("[ComplianceCycle] MAX CYCLES REACHED (%d/%d)", cm.cycles, cm.config.MaxCycles)
	log.Println(msg)
	// TODO: Send notification (Slack, email, etc.)
}

// notifyApproved logs and notifies that the PRD is approved.
func (cm *ComplianceCycleManager) notifyApproved() {
	log.Println("[ComplianceCycle] PRD FULLY COMPLIANT. All checks passed. Ready to ship.")
	// TODO: Send notification
}
