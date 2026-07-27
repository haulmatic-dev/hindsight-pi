package core

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// writeRPCModeConfig writes a minimal config with the given agents.pi.mode block
// and loads it through the real loader, so these tests cover the whole chain the
// operator actually touches: YAML file -> Config -> worker env.
func writeRPCModeConfig(t *testing.T, piExtra string) *Config {
	t.Helper()
	configPath := filepath.Join(t.TempDir(), "rpc_mode.yaml")
	content := `
overseer:
  bead_discovery_interval: 5s
  max_workers: 5

projects:
  - name: test-project
    path: /tmp/test
    type: single

agents:
  enabled: true
  default_agent: pi
  pi:
    enabled: true
    provider: kimi-coding
    model: kimi-for-coding
` + piExtra + `
pi:
  config_dir: /tmp/pi
`
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	return cfg
}

// TestWorkerEnv_ForwardsRPCModeFromYAML pins the hand-off that made
// agents.pi.mode actually do something.
//
// Workers are separate processes configured purely through this env block. Before
// HYDRA_PI_MODE was forwarded, the config key existed, unmarshalled correctly and
// had a tested RPCEnabled() accessor — and changed nothing, because the process
// that spawns Pi never saw it. That is the same shape as the three other inert
// settings already found in this config (min_score, a self-referential
// fallback_provider, mcp.enabled with no server): present in the file, dead in
// the process.
func TestWorkerEnv_ForwardsRPCModeFromYAML(t *testing.T) {
	cfg := writeRPCModeConfig(t, "    mode: rpc\n    rpc_auto_confirm: true\n")

	if !cfg.Agents.Pi.RPCEnabled() {
		t.Fatalf("mode: rpc in YAML must reach RPCEnabled(), got Mode=%q", cfg.Agents.Pi.Mode)
	}
	if !cfg.Agents.Pi.AutoConfirm() {
		t.Error("rpc_auto_confirm: true in YAML must reach AutoConfirm()")
	}

	env := NewWorkerEnvBuilder(cfg).Build(
		types.Bead{ID: "b1", Title: "GREEN: implement"}, "/tmp/p", "orchestrator", 0)

	if got := envLastValue(env, "HYDRA_PI_MODE"); got != "rpc" {
		t.Errorf("HYDRA_PI_MODE = %q, want \"rpc\" — without it the worker cannot know the daemon's mode", got)
	}
	if got := envLastValue(env, "HYDRA_PI_RPC_AUTO_CONFIRM"); got != "true" {
		t.Errorf("HYDRA_PI_RPC_AUTO_CONFIRM = %q, want \"true\"", got)
	}
}

// TestWorkerEnv_DefaultsToTextMode: an omitted mode must forward an empty value,
// which the worker reads as text mode. Today's production behaviour has to remain
// the default, so enabling rpc is an explicit, revertible act.
func TestWorkerEnv_DefaultsToTextMode(t *testing.T) {
	cfg := writeRPCModeConfig(t, "")

	if cfg.Agents.Pi.RPCEnabled() {
		t.Error("an omitted mode must not enable rpc")
	}

	env := NewWorkerEnvBuilder(cfg).Build(
		types.Bead{ID: "b1", Title: "GREEN: implement"}, "/tmp/p", "orchestrator", 0)

	if got := envLastValue(env, "HYDRA_PI_MODE"); got != "" {
		t.Errorf("HYDRA_PI_MODE = %q, want empty so the worker keeps text mode", got)
	}
	if got := envLastValue(env, "HYDRA_PI_RPC_AUTO_CONFIRM"); got != "false" {
		t.Errorf("HYDRA_PI_RPC_AUTO_CONFIRM = %q, want \"false\" (confirms denied by default)", got)
	}
}

// TestWorkerEnv_TypoInModeFallsBackToText: only the exact value "rpc" may switch
// protocol. A typo must land on the mode that works, not the one under test.
func TestWorkerEnv_TypoInModeFallsBackToText(t *testing.T) {
	cfg := writeRPCModeConfig(t, "    mode: rcp\n")

	if cfg.Agents.Pi.RPCEnabled() {
		t.Error("mode: rcp (typo) must not enable rpc")
	}

	env := NewWorkerEnvBuilder(cfg).Build(
		types.Bead{ID: "b1", Title: "t"}, "/tmp/p", "orchestrator", 0)

	// The raw value is still forwarded; the worker applies the same exact-match
	// rule and falls back to text. Both ends must agree on that rule.
	if got := envLastValue(env, "HYDRA_PI_MODE"); got != "rcp" {
		t.Errorf("HYDRA_PI_MODE = %q, want the raw \"rcp\" forwarded verbatim", got)
	}
}
