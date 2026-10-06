package core

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// Gate artifact states. running is written BEFORE the command starts (with
// the runner PID) so a daemon crash leaves a recoverable marker; the final
// state is written atomically over it.
const (
	GateStatusRunning     = "running"
	GateStatusPassed      = "passed"
	GateStatusFailed      = "failed"
	GateStatusInterrupted = "interrupted"
)

// GateArtifact is the persisted, machine-checked evidence for one gate run.
// It is the ONLY input the close path reads — agent-quoted output is not
// evidence.
type GateArtifact struct {
	Project   string    `json:"project"`
	BeadID    string    `json:"bead_id"`
	Gate      string    `json:"gate"`
	Status    string    `json:"status"`
	ExitCode  int       `json:"exit_code"`
	Summary   string    `json:"summary"` // parsed, ~2KB cap
	RawLog    string    `json:"raw_log"` // absolute path, outside the repo
	PID       int       `json:"pid"`
	StartedAt time.Time `json:"started_at"`
	EndedAt   time.Time `json:"ended_at,omitempty"`
}

func gateDir(root, project, beadID string) string {
	return filepath.Join(root, "gates", project, beadID)
}

// WriteGateArtifact persists the artifact atomically (tmp + rename) so a
// crash mid-write never leaves a torn JSON file for the close path to read.
func WriteGateArtifact(root string, a GateArtifact) error {
	dir := gateDir(root, a.Project, a.BeadID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("gate artifact dir: %w", err)
	}
	data, err := json.MarshalIndent(a, "", "  ")
	if err != nil {
		return fmt.Errorf("gate artifact marshal: %w", err)
	}
	tmp := filepath.Join(dir, "."+a.Gate+".json.tmp")
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("gate artifact write: %w", err)
	}
	if err := os.Rename(tmp, filepath.Join(dir, a.Gate+".json")); err != nil {
		return fmt.Errorf("gate artifact rename: %w", err)
	}
	return nil
}

// LoadGateArtifact reads one artifact. A missing artifact is an error, not a
// zero value — the close path must fail closed when evidence is absent.
func LoadGateArtifact(root, project, beadID, gate string) (GateArtifact, error) {
	var a GateArtifact
	data, err := os.ReadFile(filepath.Join(gateDir(root, project, beadID), gate+".json"))
	if err != nil {
		return a, fmt.Errorf("gate artifact missing (%s/%s/%s): %w", project, beadID, gate, err)
	}
	if err := json.Unmarshal(data, &a); err != nil {
		return a, fmt.Errorf("gate artifact corrupt (%s/%s/%s): %w", project, beadID, gate, err)
	}
	return a, nil
}

// FinalizeStaleGateArtifacts runs at daemon start: artifacts left "running"
// with a dead PID belong to gates the previous daemon incarnation died inside.
// They are marked interrupted (the runner may re-run them) — never trusted.
// Returns the number finalized.
func FinalizeStaleGateArtifacts(root string) (int, error) {
	base := filepath.Join(root, "gates")
	finalized := 0
	err := filepath.Walk(base, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || filepath.Ext(path) != ".json" {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil // unreadable artifact is not ours to judge
		}
		var a GateArtifact
		if err := json.Unmarshal(data, &a); err != nil || a.Status != GateStatusRunning {
			return nil
		}
		if processAlive(a.PID) {
			return nil
		}
		a.Status = GateStatusInterrupted
		a.EndedAt = time.Now()
		if WriteGateArtifact(root, a) == nil {
			finalized++
		}
		return nil
	})
	if os.IsNotExist(err) {
		return 0, nil
	}
	return finalized, err
}

// GateCloseInProgress reports whether any gate artifact for the bead still
// claims a live running gate. Shared by Charon's orphan probe (a bead whose
// close gates are running is not an orphan) and the fleet digest (the same
// bead must not be counted as an orphaned claim, which re-fired the P0
// campaign anomaly every judgment round while close gates ran — hydra
// 2026-10-06: 6 in_progress beads mid-close counted as orphans).
func GateCloseInProgress(root, project, beadID string) bool {
	if root == "" {
		return false
	}
	matches, err := filepath.Glob(filepath.Join(root, "gates", project, beadID, "*.json"))
	if err != nil {
		return false
	}
	for _, m := range matches {
		data, err := os.ReadFile(m)
		if err != nil {
			continue
		}
		var a GateArtifact
		if json.Unmarshal(data, &a) != nil {
			continue
		}
		if a.Status != GateStatusRunning {
			continue
		}
		if !processAlive(a.PID) {
			continue
		}
		if time.Since(a.StartedAt) > 45*time.Minute {
			continue
		}
		return true
	}
	return false
}

// processAlive reports whether a PID exists and belongs to a live process.
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	return syscall.Kill(pid, 0) == nil
}
