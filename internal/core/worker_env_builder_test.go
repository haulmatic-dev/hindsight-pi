package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// TestWorkerEnv_SetsPiOffline verifies workers run Pi with PI_OFFLINE=1 so Pi
// skips its startup provider-list fetch (which flakes under concurrent worker
// launches) and uses the local models.json cache instead.
func TestWorkerEnv_SetsPiOffline(t *testing.T) {
	b := NewWorkerEnvBuilder(DefaultConfig())
	env := b.Build(types.Bead{ID: "x", Project: "p"}, "/tmp/p", "orchestrator", 0)

	found := false
	for _, e := range env {
		if e == "PI_OFFLINE=1" {
			found = true
			break
		}
	}
	if !found {
		t.Error("worker env must set PI_OFFLINE=1")
	}
}

func TestBeadPhase(t *testing.T) {
	tests := []struct {
		name  string
		bead  types.Bead
		phase string
	}{
		{"RED prefix", types.Bead{Title: "RED: write tests"}, "red"},
		{"GREEN prefix", types.Bead{Title: "GREEN: implement"}, "green"},
		{"REFACTOR prefix", types.Bead{Title: "REFACTOR: clean up"}, "refactor"},
		{"SYNTAX prefix", types.Bead{Title: "SYNTAX: fix types"}, "syntax"},
		{"DISCOVERY prefix", types.Bead{Title: "Discovery: research"}, "discovery"},
		{"FIX prefix", types.Bead{Title: "Fix: bug"}, "fix"},
		{"fix type", types.Bead{Title: "anything", Type: "fix"}, "fix"},
		{"unknown epic", types.Bead{Title: "EPIC: x", Type: "epic"}, ""},
		{"empty title/type", types.Bead{}, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := beadPhase(tt.bead); got != tt.phase {
				t.Errorf("beadPhase(%+v) = %q, want %q", tt.bead, got, tt.phase)
			}
		})
	}
}

// envLastValue returns the last value for a key in a potentially duplicated
// environment slice, matching the semantics Go uses when spawning a process.
func envLastValue(env []string, key string) string {
	prefix := key + "="
	var value string
	for _, e := range env {
		if strings.HasPrefix(e, prefix) {
			value = strings.TrimPrefix(e, prefix)
		}
	}
	return value
}

func TestWorkerEnv_PhaseModelRoutingFromYAML(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "phase_models.yaml")

	configContent := `
overseer:
  bead_discovery_interval: 5s
  max_workers: 5

projects:
  - name: test-project
    path: /tmp/test
    type: single

agents:
  enabled: false
  default_agent: pi
  pi:
    enabled: true
    provider: primary-p
    model: primary-m
    phase_models:
      red:
        provider: p1
        model: m1
      green:
        model: m2

pi:
  config_dir: /tmp/pi
  standard-agent:
    enabled: true
    llm:
      provider: std-p
      model: std-m
      thinking_level: medium
      timeout: 10m
`

	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to write config file: %v", err)
	}

	cfg, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	b := NewWorkerEnvBuilder(cfg)

	redEnv := b.Build(types.Bead{ID: "b1", Title: "RED: write tests"}, "/tmp/p", "orchestrator", 0)
	if got := envLastValue(redEnv, "HYDRA_PI_PROVIDER"); got != "p1" {
		t.Errorf("RED bead provider = %q, want p1", got)
	}
	if got := envLastValue(redEnv, "HYDRA_PI_MODEL"); got != "m1" {
		t.Errorf("RED bead model = %q, want m1", got)
	}

	greenEnv := b.Build(types.Bead{ID: "b2", Title: "GREEN: implement"}, "/tmp/p", "orchestrator", 0)
	if got := envLastValue(greenEnv, "HYDRA_PI_PROVIDER"); got != "primary-p" {
		t.Errorf("GREEN bead provider = %q, want primary-p", got)
	}
	if got := envLastValue(greenEnv, "HYDRA_PI_MODEL"); got != "m2" {
		t.Errorf("GREEN bead model = %q, want m2", got)
	}

	epicEnv := b.Build(types.Bead{ID: "b3", Title: "EPIC: planning", Type: "epic"}, "/tmp/p", "orchestrator", 0)
	if got := envLastValue(epicEnv, "HYDRA_PI_PROVIDER"); got != "primary-p" {
		t.Errorf("EPIC bead provider = %q, want primary-p", got)
	}
	if got := envLastValue(epicEnv, "HYDRA_PI_MODEL"); got != "primary-m" {
		t.Errorf("EPIC bead model = %q, want primary-m", got)
	}
}

func TestWorkerEnv_PhaseModelRoutingBreakerOverrides(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Agents.Pi.Provider = "primary-p"
	cfg.Agents.Pi.Model = "primary-m"
	cfg.Agents.Pi.PhaseModels = map[string]PhaseModel{
		"red": {Provider: "p1", Model: "m1"},
	}
	cfg.Agents.Pi.FallbackProvider = "fallback-p"
	cfg.Agents.Pi.FallbackModel = "fallback-m"

	b := NewWorkerEnvBuilder(cfg)
	breaker := NewProviderBreaker("p1", "m1", "fallback-p", "fallback-m")
	for i := 0; i < 3; i++ {
		breaker.RecordRateLimit()
	}
	b.SetProviderBreaker(breaker)

	redEnv := b.Build(types.Bead{ID: "b1", Title: "RED: write tests"}, "/tmp/p", "orchestrator", 0)
	if got := envLastValue(redEnv, "HYDRA_PI_PROVIDER"); got != "fallback-p" {
		t.Errorf("open breaker should override phase route, provider = %q, want fallback-p", got)
	}
	if got := envLastValue(redEnv, "HYDRA_PI_MODEL"); got != "fallback-m" {
		t.Errorf("open breaker should override phase route, model = %q, want fallback-m", got)
	}
}
