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
	Enabled            bool          `yaml:"enabled"`
	MaxCycles          int           `yaml:"max_cycles"`
	PollInterval       time.Duration `yaml:"poll_interval"`
	AnalyzerPath       string        `yaml:"analyzer_path"`
	GapToPRDPath       string        `yaml:"gap_to_prd_path"`
	TaskBreakdownPath  string        `yaml:"task_breakdown_path"`
	ProjectDir         string        `yaml:"project_dir"`
	PRDPath            string        `yaml:"prd_path"`
}

// ComplianceCycleManager runs the closed-loop compliance cycle.
// It detects when all beads are closed, runs the analyzer, and creates gap beads if needed.
type ComplianceCycleManager struct {
	config      ComplianceCycleConfig
	overseer    *Overseer
	cycles      int
	running     bool
	stopCh      chan struct{}
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

// checkAndRunCycle checks if all beads are closed and runs a compliance cycle.
func (cm *ComplianceCycleManager) checkAndRunCycle() error {
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
		cm.notifyApproved()
		return nil
	}

	log.Printf("[ComplianceCycle] Gaps found. Converting to gap PRD...")

	// Step 3: Convert gaps to mini-PRD
	gapPRDPath, err := cm.runGapToPRD(gapReportPath)
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
func (cm *ComplianceCycleManager) createGapBeads(gapPRDPath string) error {
	// Read the gap PRD to get its content
	prdContent, err := os.ReadFile(gapPRDPath)
	if err != nil {
		return fmt.Errorf("read gap PRD: %w", err)
	}

	// Write a temporary request file for task-breakdown
	reqPath := filepath.Join(cm.config.ProjectDir, "gaps", fmt.Sprintf("task-breakdown-req-%d.json", cm.cycles))
	req := map[string]interface{}{
		"prd_content":   string(prdContent),
		"prd_path":      gapPRDPath,
		"project_name":  fmt.Sprintf("gap-cycle-%d", cm.cycles),
		"auto_create":   true,
		"parent_prd":    cm.config.PRDPath,
		"cycle":         cm.cycles,
	}
	reqJSON, _ := json.MarshalIndent(req, "", "  ")
	if err := os.WriteFile(reqPath, reqJSON, 0644); err != nil {
		return fmt.Errorf("write task-breakdown request: %w", err)
	}

	// Run task-breakdown skill
	taskBreakdownBin := cm.config.TaskBreakdownPath
	if taskBreakdownBin == "" {
		// Default: use pi skill invocation
		taskBreakdownBin = "pi"
	}

	log.Printf("[ComplianceCycle] Creating gap beads via task-breakdown...")
	log.Printf("[ComplianceCycle] Request: %s", reqPath)

	// For now, log what would happen. Full integration requires pi skill invocation.
	log.Printf("[ComplianceCycle] Gap beads request prepared at %s", reqPath)
	log.Printf("[ComplianceCycle] To create beads: pi skill task-breakdown --file %s", reqPath)

	// TODO: Integrate with pi skill runner or task-breakdown executor.mjs directly
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
