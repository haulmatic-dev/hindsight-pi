package core

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/haulmatic-dev/hydra/internal/pi"
)

// TestPiManageConfig_DefaultsToReadOnly is the guard that matters: the shipped
// default must not write to the Pi config directory, because that directory is
// shared with every interactive shell via ~/.pi/agent.
func TestPiManageConfig_DefaultsToReadOnly(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Pi.ManageConfig {
		t.Error("pi.manage_config defaults to true — the daemon would reshape the team's shared config on startup")
	}
}

// TestPiSetup_SkippedWhenNotManaging proves the skip is real by pointing
// ConfigDir at an empty directory and asserting nothing appeared in it. Setup
// would otherwise create the structure and write skills-installed.json.
func TestPiSetup_SkippedWhenNotManaging(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "shared-pi")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatal(err)
	}

	cfg := DefaultConfig()
	cfg.Pi.ConfigDir = target
	cfg.Pi.ManageConfig = false

	if err := NewPiSetupManager(cfg).RunSetup(); err != nil {
		t.Fatalf("RunSetup returned an error while skipping: %v", err)
	}

	entries, err := os.ReadDir(target)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("setup wrote into a shared config dir it does not manage: %v", names)
	}
}

// TestSkillUpdater_NotCreatedWhenNotManaging: the updater rewrites skills on a
// timer, so a shared config must not get one. service_starter only starts a
// non-nil updater, so leaving it nil is the off switch.
func TestSkillUpdater_NotCreatedWhenNotManaging(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Pi.ConfigDir = t.TempDir()
	cfg.Pi.ManageConfig = false

	var updater *pi.SkillUpdater
	if err := NewSkillUpdaterInitializer(cfg, &updater).Initialize(); err != nil {
		t.Fatalf("Initialize returned an error while skipping: %v", err)
	}
	if updater != nil {
		t.Error("a skill updater was created for a config directory hydra does not manage")
	}
}

// TestSkillUpdater_CreatedWhenManaging keeps the opt-in honest — the flag must
// actually turn the behaviour back on, or it is a disguised removal.
func TestSkillUpdater_CreatedWhenManaging(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Pi.ConfigDir = t.TempDir()
	cfg.Pi.ManageConfig = true

	var updater *pi.SkillUpdater
	if err := NewSkillUpdaterInitializer(cfg, &updater).Initialize(); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	if updater == nil {
		t.Error("pi.manage_config: true did not produce a skill updater")
	}
}

// TestPiManageConfig_ParsedFromYAML: the field has to survive the raw-config
// copy. Several Pi settings have been silently inert before for exactly this
// reason (min_score, mcp.enabled, fallback_provider).
func TestPiManageConfig_ParsedFromYAML(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hydra.yaml")
	yaml := `
pi:
  config_dir: /opt/dev-tools/config/pi
  worker_config_dir: /opt/dev-tools/config/pi
  manage_config: true
`
	if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if !cfg.Pi.ManageConfig {
		t.Error("manage_config: true did not survive config loading")
	}
	if cfg.Pi.ConfigDir != cfg.Pi.WorkerConfigDir {
		t.Errorf("config_dir %q != worker_config_dir %q — workers and shells would diverge again",
			cfg.Pi.ConfigDir, cfg.Pi.WorkerConfigDir)
	}
}
