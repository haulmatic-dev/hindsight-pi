package core

import (
	"os"
	"path/filepath"
	"testing"
)

// Regression: LoadConfig copies rawConfig fields manually; Shepherd was
// declared on Config but never copied, so shepherd.pan yaml silently parsed
// to zero values and Pan stayed disabled in production (2026-08-01).
func TestLoadConfig_ShepherdPanSurvivesLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hydra.yaml")
	yaml := `
overseer:
  max_workers: 2
projects:
  auto_discover: false
  list:
    - name: dummy
      path: /tmp/dummy
      type: single
shepherd:
  pan:
    enabled: true
    interval_seconds: 60
    hung_after_seconds: 300
    park_overdue_grace_seconds: 600
    nudge_cap: 3
    provider: minimax
    model: MiniMax-M2.7
    api_key_name: MINIMAX_API_KEY
`
	if err := os.WriteFile(path, []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfigUncached(path)
	if err != nil {
		t.Fatalf("LoadConfigUncached: %v", err)
	}
	p := cfg.Shepherd.Pan
	if !p.Enabled {
		t.Errorf("shepherd.pan.enabled=true in yaml but false after load")
	}
	if p.Provider != "minimax" || p.Model != "MiniMax-M2.7" {
		t.Errorf("provider/model lost: %+v", p)
	}
	if p.IntervalSeconds != 60 || p.HungAfterSeconds != 300 || p.NudgeCap != 3 || p.APIKeyName != "MINIMAX_API_KEY" {
		t.Errorf("fields lost: %+v", p)
	}
}
