package core

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// ShipGateConfig configures the product-level acceptance gate that runs
// before a PRD is declared shippable. "All beads closed" is not the same
// as "the assembled product builds, boots, and passes end-to-end tests".
// Disabled by default; behaviour is unchanged until a project sets it.
type ShipGateConfig struct {
	Enabled   bool          `yaml:"enabled"`
	BuildCmd  string        `yaml:"build_cmd"`
	BootCmd   string        `yaml:"boot_cmd"`
	HealthURL string        `yaml:"health_url"`
	E2ECmd    string        `yaml:"e2e_cmd"`
	Timeout   time.Duration `yaml:"timeout"`
}

// runShipGate runs build -> (optional boot + health) -> e2e in ProjectDir.
// Returns passed=true when the gate is disabled or every configured step
// succeeds. A build failure short-circuits; other failures are collected.
func (cm *ComplianceCycleManager) runShipGate() (bool, []string) {
	sg := cm.config.ShipGate
	if !sg.Enabled {
		return true, nil
	}

	timeout := sg.Timeout
	if timeout == 0 {
		timeout = 10 * time.Minute
	}

	var failures []string
	run := func(name, cmdStr string) bool {
		if strings.TrimSpace(cmdStr) == "" {
			return true
		}
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		c := exec.CommandContext(ctx, "bash", "-c", cmdStr)
		c.Dir = cm.config.ProjectDir
		out, err := c.CombinedOutput()
		if err != nil {
			tail := string(out)
			if len(tail) > 300 {
				tail = tail[len(tail)-300:]
			}
			failures = append(failures, fmt.Sprintf("%s (`%s`) failed: %v | %s", name, cmdStr, err, strings.TrimSpace(tail)))
			return false
		}
		return true
	}

	// Build must pass before anything else is meaningful.
	if !run("build", sg.BuildCmd) {
		return false, failures
	}

	// Optional: boot the product and wait for health, then tear it down.
	if strings.TrimSpace(sg.BootCmd) != "" {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		boot := exec.CommandContext(ctx, "bash", "-c", sg.BootCmd)
		boot.Dir = cm.config.ProjectDir
		boot.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		if err := boot.Start(); err != nil {
			failures = append(failures, "boot failed to start: "+err.Error())
			return false, failures
		}
		defer func() {
			if boot.Process != nil {
				_ = syscall.Kill(-boot.Process.Pid, syscall.SIGKILL) // kill the process group
			}
		}()
		if strings.TrimSpace(sg.HealthURL) != "" && !waitHealthy(sg.HealthURL, timeout) {
			failures = append(failures, "health check never passed: "+sg.HealthURL)
			return false, failures
		}
	}

	// End-to-end / acceptance suite.
	run("e2e", sg.E2ECmd)

	return len(failures) == 0, failures
}

// waitHealthy polls url until it returns 2xx or the timeout elapses.
func waitHealthy(url string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 5 * time.Second}
	for time.Now().Before(deadline) {
		resp, err := client.Get(url)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode >= 200 && resp.StatusCode < 300 {
				return true
			}
		}
		time.Sleep(2 * time.Second)
	}
	return false
}

// finalizeApproval runs the product ship-gate before declaring the PRD
// shippable. Gate pass (or disabled) -> notifyApproved. Gate fail -> create
// a ship-gate fix bead so the daemon works it, and do NOT approve.
func (cm *ComplianceCycleManager) finalizeApproval() {
	passed, failures := cm.runShipGate()
	if passed {
		cm.notifyApproved()
		return
	}
	log.Printf("[ComplianceCycle] SHIP-GATE FAILED (%d issue(s)): product not shippable. Creating fix bead.", len(failures))
	for _, f := range failures {
		log.Printf("[ShipGate]   - %s", f)
	}
	cm.createShipGateBead(failures)
}

// createShipGateBead files a fix bead describing the ship-gate failures.
func (cm *ComplianceCycleManager) createShipGateBead(failures []string) {
	bc := cm.overseer.GetBeadsClient()
	if bc == nil {
		return
	}
	desc := "Product-level ship-gate failed; the assembled product is not shippable until these pass:\n\n- " +
		strings.Join(failures, "\n- ")
	req := types.CreateBeadRequest{
		Title:       "Ship-gate: product build/boot/e2e failing",
		Description: desc,
		Type:        "fix",
		Priority:    1,
		Labels:      []string{"ship-gate", "auto-fix"},
	}
	if _, err := bc.CreateBead(cm.config.ProjectDir, req); err != nil {
		log.Printf("[ComplianceCycle] failed to create ship-gate bead: %v", err)
	}
}
