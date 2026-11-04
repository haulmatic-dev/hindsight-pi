package core

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestLoadConfig_SkillInductionParses verifies the intelligence.skill_induction
// section round-trips through LoadConfig, and that its absence leaves the
// component disabled (zero value — the safe default).
func TestLoadConfig_SkillInductionParses(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hydra.yaml")

	yaml := `
projects:
  auto_discover: false
  list:
    - name: demo
      path: /tmp/demo
overseer:
  max_workers: 1
intelligence:
  skill_induction:
    enabled: true
    interval: 2h
    model: kimi-k2
    provider: kimi
    api_key_env: KIMI_API_KEY
    max_candidates_per_run: 2
    similarity_threshold: 0.5
`
	if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	si := cfg.Intelligence.SkillInduction
	if !si.Enabled {
		t.Error("Enabled should parse true")
	}
	if si.Interval != 2*time.Hour {
		t.Errorf("Interval = %v, want 2h", si.Interval)
	}
	if si.Model != "kimi-k2" || si.Provider != "kimi" || si.APIKeyEnv != "KIMI_API_KEY" {
		t.Errorf("LLM fields wrong: %+v", si)
	}
	if si.MaxCandidatesPerRun != 2 || si.SimilarityThreshold != 0.5 {
		t.Errorf("knobs wrong: %+v", si)
	}

	// Defaults apply without changing the stored config struct.
	applied := si.ApplyDefaults()
	if applied.MaxCandidatesPerRun != 2 {
		t.Errorf("ApplyDefaults must not override explicit values: %+v", applied)
	}
	if applied.MaxProcessedPerRun != 10 || applied.Provider != "kimi" {
		t.Errorf("ApplyDefaults must fill zeros: %+v", applied)
	}
}

// TestLoadConfig_SkillInductionAbsentDisabled verifies zero-value default.
func TestLoadConfig_SkillInductionAbsentDisabled(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hydra.yaml")
	yaml := `
projects:
  auto_discover: false
  list:
    - name: demo
      path: /tmp/demo
`
	if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.Intelligence.SkillInduction.Enabled {
		t.Error("skill induction must default to disabled")
	}
}
