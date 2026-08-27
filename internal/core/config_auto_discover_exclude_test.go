package core

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfig_AutoDiscoverExclude(t *testing.T) {
	data := []byte(`
projects:
  auto_discover: true
  auto_discover_exclude:
    - demo
    - /path/to/noise
  list:
    - name: real-project
      path: /path/to/real-project
      type: single
`)
	tmp := t.TempDir()
	path := filepath.Join(tmp, "hydra.yaml")
	os.WriteFile(path, data, 0644)

	cfg, err := LoadConfigUncached(path)
	if err != nil {
		t.Fatalf("LoadConfigUncached: %v", err)
	}
	if len(cfg.AutoDiscoverExclude) != 2 {
		t.Fatalf("AutoDiscoverExclude = %v, want 2 entries", cfg.AutoDiscoverExclude)
	}
	if cfg.AutoDiscoverExclude[0] != "demo" || cfg.AutoDiscoverExclude[1] != "/path/to/noise" {
		t.Errorf("AutoDiscoverExclude = %v", cfg.AutoDiscoverExclude)
	}
}
