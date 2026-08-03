package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

func TestExtractPatternsAtClose_StoresOnDoneVerdict(t *testing.T) {
	dir := t.TempDir()
	config := WorkerConfig{}
	config.Patterns.Enabled = true
	config.Patterns.StoragePath = dir

	bead := types.Bead{ID: "proj-abc123", Project: "proj"}
	result := &ExecutionResult{
		Success: true,
		Verdict: &Verdict{
			Done:         true,
			ACMet:        []string{"AC1"},
			FilesChanged: []string{"main.go"},
			Summary:      "implemented",
		},
	}

	extractPatternsAtClose(config, bead, result)

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("pattern files = %d, want 2", len(entries))
	}
}

func TestExtractPatternsAtClose_DisabledOrNoVerdict(t *testing.T) {
	dir := t.TempDir()
	bead := types.Bead{ID: "proj-abc123", Project: "proj"}
	result := &ExecutionResult{Success: true, Verdict: &Verdict{Done: true, ACMet: []string{"AC1"}}}

	// Disabled config: no files.
	config := WorkerConfig{}
	config.Patterns.StoragePath = dir
	extractPatternsAtClose(config, bead, result)

	// Enabled but no verdict: no files.
	config.Patterns.Enabled = true
	extractPatternsAtClose(config, bead, &ExecutionResult{Success: false})

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected no pattern files, got %d", len(entries))
	}
}

func TestParseConfig_PatternsEnv(t *testing.T) {
	clearHydraEnvVars(t)
	t.Setenv("HYDRA_PATTERNS_ENABLED", "true")
	t.Setenv("HYDRA_PATTERNS_STORAGE_PATH", filepath.Join(t.TempDir(), "patterns"))
	t.Setenv("HYDRA_PATTERNS_MIN_CONFIDENCE", "0.8")
	t.Setenv("HYDRA_PATTERNS_MAX_PATTERNS_PER_BEAD", "3")

	config := parseConfig()
	if !config.Patterns.Enabled {
		t.Error("Patterns.Enabled = false, want true")
	}
	if config.Patterns.MinConfidence != 0.8 {
		t.Errorf("MinConfidence = %v, want 0.8", config.Patterns.MinConfidence)
	}
	if config.Patterns.MaxPatternsPerBead != 3 {
		t.Errorf("MaxPatternsPerBead = %v, want 3", config.Patterns.MaxPatternsPerBead)
	}
	if config.Patterns.StoragePath == "" {
		t.Error("StoragePath empty")
	}
}
