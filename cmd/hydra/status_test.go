package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/haulmatic-dev/hydra/internal/core"
	"github.com/haulmatic-dev/hydra/internal/registry"
)

// TestStatusFleetRollup verifies hydra status prints all four expected sections
// from a fixture registry database, log file, and br-managed project.
func TestStatusFleetRollup(t *testing.T) {
	hydraHome := t.TempDir()
	t.Setenv("HYDRA_HOME", hydraHome)

	// Registry fixture: one active worker and one parked worker with a stale heartbeat.
	regPath := core.RegistryPath()
	store, err := registry.Open(regPath)
	if err != nil {
		t.Fatalf("open registry: %v", err)
	}
	now := time.Now()
	if err := store.UpsertWorker(registry.WorkerRecord{
		BeadID:        "test-abc",
		PID:           123,
		PGID:          123,
		Project:       "test",
		StartedAt:     now.Add(-30 * time.Minute),
		LastHeartbeat: now.Add(-10 * time.Second),
		State:         registry.StateWorking,
		TokensSoFar:   1234567,
	}); err != nil {
		t.Fatalf("upsert worker: %v", err)
	}
	if err := store.UpsertWorker(registry.WorkerRecord{
		BeadID:        "test-parked",
		PID:           124,
		PGID:          124,
		Project:       "test",
		StartedAt:     now.Add(-2 * time.Hour),
		LastHeartbeat: now.Add(-10 * time.Minute),
		State:         registry.StateParked,
		TokensSoFar:   0,
	}); err != nil {
		t.Fatalf("upsert parked worker: %v", err)
	}
	store.Close()

	// Log fixture.
	logDir := t.TempDir()
	logPath := filepath.Join(logDir, "hydra-error.log")
	logLines := "2026/08/25 08:00:00 [CloseEvent] {\"bead_id\":\"test-abc\",\"verdict\":\"accepted\",\"reason\":\"verified\",\"attempt\":0}\n" +
		"2026/08/25 08:01:00 [PanRepair] reset-retries for test-parked (previous fault count: 1)\n"
	if err := os.WriteFile(logPath, []byte(logLines), 0o644); err != nil {
		t.Fatalf("write log: %v", err)
	}

	// Beads project fixture.
	projectDir := t.TempDir()
	initCmd := exec.Command("br", "init")
	initCmd.Dir = projectDir
	if out, err := initCmd.Output(); err != nil {
		t.Fatalf("br init failed: %v\n%s", err, out)
	}

	br := func(args ...string) string {
		cmd := exec.Command("br", args...)
		cmd.Dir = projectDir
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("br %v failed: %v\n%s", args, err, out)
		}
		return string(out)
	}
	br("create", "RED: write tests", "--type", "test", "--priority", "4", "--silent")
	br("create", "GREEN: implement", "--type", "implement", "--priority", "4", "--silent")
	br("create", "P1 fix", "--type", "fix", "--priority", "1", "--silent")

	cfg := &core.Config{
		Overseer: core.OverseerConfig{MaxWorkers: 4},
		Shepherd: core.ShepherdConfig{
			Pan: core.PanConfig{HungAfterSeconds: 60},
		},
		Status: core.StatusConfig{LogPath: logPath},
		Projects: []core.ProjectConfig{
			{Name: "test", Path: projectDir},
		},
	}

	var buf bytes.Buffer
	writeStatusReport(cfg, &buf)
	out := buf.String()

	for _, section := range []string{"Workers", "Recent verdicts", "Boards", "Queue health"} {
		if !strings.Contains(out, section) {
			t.Errorf("missing section %q\noutput:\n%s", section, out)
		}
	}
	if !strings.Contains(out, "test-abc") {
		t.Errorf("missing worker row test-abc\n%s", out)
	}
	if !strings.Contains(out, "[CloseEvent]") || !strings.Contains(out, "[PanRepair]") {
		t.Errorf("missing verdict lines\n%s", out)
	}
	if !strings.Contains(out, "open=") || !strings.Contains(out, "in_progress=") {
		t.Errorf("missing board counts\n%s", out)
	}
	if !strings.Contains(out, "parked-stale-heartbeat: 1") {
		t.Errorf("missing stale parked count\n%s", out)
	}
	if !strings.Contains(out, "starved-open-P1: 1") {
		t.Errorf("missing starved P1 count\n%s", out)
	}
	if !strings.Contains(out, "pool utilisation: 2/4 (50%)") {
		t.Errorf("missing utilisation\n%s", out)
	}
}

// TestStatusMissingLogAndRegistry verifies missing log and registry degrade to
// notes rather than failing the command.
func TestStatusMissingLogAndRegistry(t *testing.T) {
	// Point HYDRA_HOME at a file so MkdirAll fails, making the registry unavailable.
	badHome := filepath.Join(t.TempDir(), "notadir")
	if err := os.WriteFile(badHome, []byte("x"), 0o644); err != nil {
		t.Fatalf("create blocker file: %v", err)
	}
	t.Setenv("HYDRA_HOME", badHome)

	cfg := &core.Config{
		Status: core.StatusConfig{LogPath: filepath.Join(t.TempDir(), "missing.log")},
	}

	var buf bytes.Buffer
	writeStatusReport(cfg, &buf)
	out := buf.String()

	if !strings.Contains(out, "registry unavailable") {
		t.Errorf("expected registry unavailable note\n%s", out)
	}
	if !strings.Contains(out, "log unavailable") {
		t.Errorf("expected log unavailable note\n%s", out)
	}
	if !strings.Contains(out, "no projects configured") {
		t.Errorf("expected no projects note\n%s", out)
	}
}
