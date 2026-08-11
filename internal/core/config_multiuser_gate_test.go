package core

import (
	"os"
	"path/filepath"
	"testing"
)

// projects.multi_user_discovery gates the unconditional multi-user project
// scan. Default must stay enabled (existing deployments rely on it); an
// explicit false narrows the fleet to projects.list only — needed to focus
// execution on a single user's project (2026-08-11 operator request).
func TestMultiUserDiscoveryGate(t *testing.T) {
	write := func(t *testing.T, projectsBlock string) string {
		t.Helper()
		dir := t.TempDir()
		path := filepath.Join(dir, "hydra.yaml")
		content := "overseer:\n  max_workers: 2\n" + projectsBlock
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("write config: %v", err)
		}
		return path
	}

	t.Run("absent defaults to enabled", func(t *testing.T) {
		cfg, err := LoadConfigUncached(write(t, "projects:\n  list:\n    - name: a\n      path: /tmp/a\n"))
		if err != nil {
			t.Fatalf("LoadConfig: %v", err)
		}
		if !cfg.MultiUserDiscovery {
			t.Error("MultiUserDiscovery = false, want default true")
		}
	})

	t.Run("explicit false disables", func(t *testing.T) {
		cfg, err := LoadConfigUncached(write(t, "projects:\n  multi_user_discovery: false\n  list:\n    - name: a\n      path: /tmp/a\n"))
		if err != nil {
			t.Fatalf("LoadConfig: %v", err)
		}
		if cfg.MultiUserDiscovery {
			t.Error("MultiUserDiscovery = true, want false (explicitly disabled)")
		}
		if len(cfg.Projects) != 1 || cfg.Projects[0].Name != "a" {
			t.Errorf("projects list broken by the new key: %+v", cfg.Projects)
		}
	})
}
