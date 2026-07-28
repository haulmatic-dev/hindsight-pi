package core

import (
	"os"
	"path/filepath"
	"strings"
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

// TestWorkerEnv_DefaultsToRPCWithExtensions pins the shipped defaults: a config
// that says nothing about the protocol gets rpc plus the standard allowlist.
//
// rpc is the default because text mode initialises no extension host, so rtk,
// caveman and context-mode cannot run there at all — the whole reason for the
// protocol. The allowlist is the default because Pi's own discovery loads every
// installed package (48,315 tokens on one measured turn against 12,059 for the
// allowlist), most of which a headless worker cannot use.
func TestWorkerEnv_DefaultsToRPCWithExtensions(t *testing.T) {
	cfg := writeRPCModeConfig(t, "")

	if !cfg.Agents.Pi.RPCEnabled() {
		t.Errorf("an omitted mode must default to rpc, got Mode=%q", cfg.Agents.Pi.Mode)
	}
	// rtk + context-mode, on each extension's own accounting: context-mode's
	// stats report 1,249,024 tokens saved across worker sessions, and rtk shows a
	// measured 77-81% reduction on high-output commands. caveman is excluded — at
	// ultra it produced MORE output tokens (1,103 vs 927) on a prose-heavy prompt.
	if got := strings.Join(cfg.Agents.Pi.ExtensionsForPhase("green"), ","); got != "rtk,context-mode" {
		t.Errorf("default extensions = %q, want rtk,context-mode", got)
	}

	env := NewWorkerEnvBuilder(cfg).Build(
		types.Bead{ID: "b1", Title: "GREEN: implement"}, "/tmp/p", "orchestrator", 0)

	if got := envLastValue(env, "HYDRA_PI_MODE"); got != "rpc" {
		t.Errorf("HYDRA_PI_MODE = %q, want \"rpc\"", got)
	}
	if got := envLastValue(env, "HYDRA_PI_EXTENSIONS"); got != "rtk,context-mode" {
		t.Errorf("HYDRA_PI_EXTENSIONS = %q, want the default allowlist", got)
	}
	// Confirms stay denied by default even with rpc on: a worker running
	// unreviewed commands is a worse failure than a stalled bead.
	if got := envLastValue(env, "HYDRA_PI_RPC_AUTO_CONFIRM"); got != "false" {
		t.Errorf("HYDRA_PI_RPC_AUTO_CONFIRM = %q, want \"false\"", got)
	}
}

// TestWorkerEnv_ExplicitTextModeOptsOut: `mode: text` must still win. Defaults
// are only for what the operator did not say.
func TestWorkerEnv_ExplicitTextModeOptsOut(t *testing.T) {
	cfg := writeRPCModeConfig(t, "    mode: text\n")

	if cfg.Agents.Pi.RPCEnabled() {
		t.Error("an explicit mode: text must not be overridden by the rpc default")
	}
	env := NewWorkerEnvBuilder(cfg).Build(types.Bead{ID: "b1", Title: "t"}, "/tmp/p", "orchestrator", 0)
	if got := envLastValue(env, "HYDRA_PI_MODE"); got != "text" {
		t.Errorf("HYDRA_PI_MODE = %q, want \"text\"", got)
	}
}

// TestWorkerEnv_ExplicitEmptyExtensionsOptsOut: `extensions: []` disables them.
// This is the distinction the defaulting relies on — an omitted key is nil, an
// explicit empty list is not — so it needs a test of its own.
func TestWorkerEnv_ExplicitEmptyExtensionsOptsOut(t *testing.T) {
	cfg := writeRPCModeConfig(t, "    mode: rpc\n    extensions: []\n")

	if got := cfg.Agents.Pi.ExtensionsForPhase("green"); len(got) != 0 {
		t.Errorf("extensions: [] must disable extensions, got %v", got)
	}
	env := NewWorkerEnvBuilder(cfg).Build(types.Bead{ID: "b1", Title: "t"}, "/tmp/p", "orchestrator", 0)
	if got := envLastValue(env, "HYDRA_PI_EXTENSIONS"); got != "" {
		t.Errorf("HYDRA_PI_EXTENSIONS = %q, want empty", got)
	}
}

// TestWorkerEnv_PerPhaseExtensions pins the per-phase allowlist reaching the
// worker. Extensions are opt-in and phase-aware because loading everything is
// expensive — measured on one identical turn, all installed extensions cost
// 48,315 tokens against 12,059 for rtk alone.
func TestWorkerEnv_PerPhaseExtensions(t *testing.T) {
	cfg := writeRPCModeConfig(t, `    mode: rpc
    extensions: [rtk, caveman, context-mode]
    phase_extensions:
      red: [rtk]
`)

	if got := cfg.Agents.Pi.ExtensionsForPhase("green"); strings.Join(got, ",") != "rtk,caveman,context-mode" {
		t.Errorf("a phase with no override must use the default list, got %v", got)
	}
	if got := cfg.Agents.Pi.ExtensionsForPhase("red"); strings.Join(got, ",") != "rtk" {
		t.Errorf("RED override = %v, want [rtk]", got)
	}
	if got := cfg.Agents.Pi.ExtensionsForPhase("RED"); strings.Join(got, ",") != "rtk" {
		t.Errorf("phase lookup must be case-insensitive, got %v", got)
	}

	b := NewWorkerEnvBuilder(cfg)
	redEnv := b.Build(types.Bead{ID: "b1", Title: "RED: write the failing test"}, "/tmp/p", "orchestrator", 0)
	if got := envLastValue(redEnv, "HYDRA_PI_EXTENSIONS"); got != "rtk" {
		t.Errorf("RED bead HYDRA_PI_EXTENSIONS = %q, want \"rtk\"", got)
	}
	greenEnv := b.Build(types.Bead{ID: "b2", Title: "GREEN: make it pass"}, "/tmp/p", "orchestrator", 0)
	if got := envLastValue(greenEnv, "HYDRA_PI_EXTENSIONS"); got != "rtk,caveman,context-mode" {
		t.Errorf("GREEN bead HYDRA_PI_EXTENSIONS = %q, want the default list", got)
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
