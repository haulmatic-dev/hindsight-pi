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

// No implicit KIMI_API_KEY: workers ride the llm-router lane where the router
// injects the upstream key, so an unconfigured worker env must carry no API
// key var at all (routing-audit leftover; see bead "Remove inert KIMI_API_KEY").
func TestWorkerEnv_NoImplicitKimiAPIKey(t *testing.T) {
	t.Setenv("KIMI_API_KEY", "sk-test-inert")
	b := NewWorkerEnvBuilder(DefaultConfig())
	env := b.Build(types.Bead{ID: "x", Project: "p"}, "/tmp/p", "orchestrator", 0)
	for _, e := range env {
		if strings.HasPrefix(e, "KIMI_API_KEY=") {
			t.Errorf("worker env must not carry inert KIMI_API_KEY, got %q", e)
		}
		if e == "HYDRA_PI_API_KEY_NAME=KIMI_API_KEY" || e == "HYDRA_PI_API_KEY_NAME=" {
			t.Errorf("HYDRA_PI_API_KEY_NAME must stay unset when api_key_name is not configured, got %q", e)
		}
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

// TestWorkerEnv_SetsPiCacheRetention verifies workers run Pi with
// PI_CACHE_RETENTION=long so pi-ai marks cache blocks with ttl:1h instead of
// the default 5m TTL, keeping re-prompts of the same bead cache-warm past the
// default expiry (cache hardening, 2026-09-06).
func TestWorkerEnv_SetsPiCacheRetention(t *testing.T) {
	b := NewWorkerEnvBuilder(DefaultConfig())
	env := b.Build(types.Bead{ID: "x", Project: "p"}, "/tmp/p", "orchestrator", 0)

	found := false
	for _, e := range env {
		if e == "PI_CACHE_RETENTION=long" {
			found = true
			break
		}
	}
	if !found {
		t.Error("worker env must set PI_CACHE_RETENTION=long")
	}
}

// TestWorkerEnv_ForwardsTurnBudget verifies the daemon computes and forwards
// the per-phase soft turn budget and the reminder cadence to the worker.
func TestWorkerEnv_ForwardsTurnBudget(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Agents.Pi.TurnBudgetReminderCadence = 2
	cfg.Agents.Pi.TurnBudgets = map[string]int{
		"red":   150,
		"green": 300,
	}

	b := NewWorkerEnvBuilder(cfg)

	redEnv := b.Build(types.Bead{ID: "b1", Title: "RED: write tests"}, "/tmp/p", "orchestrator", 0)
	if got := envLastValue(redEnv, "HYDRA_PI_TURN_BUDGET"); got != "150" {
		t.Errorf("RED turn budget = %q, want 150", got)
	}
	if got := envLastValue(redEnv, "HYDRA_PI_TURN_BUDGET_CADENCE"); got != "2" {
		t.Errorf("cadence = %q, want 2", got)
	}

	greenEnv := b.Build(types.Bead{ID: "b2", Title: "GREEN: implement"}, "/tmp/p", "orchestrator", 0)
	if got := envLastValue(greenEnv, "HYDRA_PI_TURN_BUDGET"); got != "300" {
		t.Errorf("GREEN turn budget = %q, want 300", got)
	}
}

// TestWorkerEnv_TurnBudgetDefaults verifies the fleet defaults sit above the
// observed p75 (red 150, green/refactor 300) so the hint never binds early.
func TestWorkerEnv_TurnBudgetDefaults(t *testing.T) {
	b := NewWorkerEnvBuilder(DefaultConfig())

	redEnv := b.Build(types.Bead{ID: "b1", Title: "RED: write tests"}, "/tmp/p", "orchestrator", 0)
	if got := envLastValue(redEnv, "HYDRA_PI_TURN_BUDGET"); got != "150" {
		t.Errorf("default RED turn budget = %q, want 150", got)
	}

	greenEnv := b.Build(types.Bead{ID: "b2", Title: "GREEN: implement"}, "/tmp/p", "orchestrator", 0)
	if got := envLastValue(greenEnv, "HYDRA_PI_TURN_BUDGET"); got != "300" {
		t.Errorf("default GREEN turn budget = %q, want 300", got)
	}
}

// TestWorkerEnv_ForwardsTurnBudgetFromYAML verifies the daemon passes the
// per-phase soft turn budget and reminder cadence to the worker. The budget
// is visible in the prompt tail as a pacing hint, not a hard cap.
func TestWorkerEnv_ForwardsTurnBudgetFromYAML(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "turn_budget.yaml")
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
    provider: kimi-coding
    model: kimi-for-coding
    turn_budget_reminder_cadence: 2
    turn_budgets:
      red: 150
      green: 300
      refactor: 300

pi:
  config_dir: /tmp/pi
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
	if got := envLastValue(redEnv, "HYDRA_PI_TURN_BUDGET"); got != "150" {
		t.Errorf("RED bead turn budget = %q, want 150", got)
	}
	if got := envLastValue(redEnv, "HYDRA_PI_TURN_BUDGET_CADENCE"); got != "2" {
		t.Errorf("turn budget cadence = %q, want 2", got)
	}

	greenEnv := b.Build(types.Bead{ID: "b2", Title: "GREEN: implement"}, "/tmp/p", "orchestrator", 0)
	if got := envLastValue(greenEnv, "HYDRA_PI_TURN_BUDGET"); got != "300" {
		t.Errorf("GREEN bead turn budget = %q, want 300", got)
	}

	// A phase with no explicit budget uses the default above the fleet p75.
	discoveryEnv := b.Build(types.Bead{ID: "b3", Title: "Discovery: research"}, "/tmp/p", "orchestrator", 0)
	if got := envLastValue(discoveryEnv, "HYDRA_PI_TURN_BUDGET"); got != "200" {
		t.Errorf("Discovery bead turn budget = %q, want 200", got)
	}
}
