package core

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/haulmatic-dev/hydra/internal/agents"
	"gopkg.in/yaml.v3"
)

func TestPiConfig_RPCEnabled_DefaultsToTextMode(t *testing.T) {
	var cfg PiConfig
	if cfg.RPCEnabled() {
		t.Errorf("PiConfig{}.RPCEnabled() = true, want false (unset default must be text mode)")
	}
}

func TestPiConfig_RPCEnabled(t *testing.T) {
	cases := []struct {
		mode string
		want bool
	}{
		{"rpc", true},
		{"RPC", true},
		{" rpc ", true},
		{"text", false},
		{"banana", false},
		{"", false},
	}
	for _, tc := range cases {
		t.Run(tc.mode, func(t *testing.T) {
			cfg := PiConfig{Mode: tc.mode}
			if got := cfg.RPCEnabled(); got != tc.want {
				t.Errorf("PiConfig{Mode:%q}.RPCEnabled() = %v, want %v", tc.mode, got, tc.want)
			}
		})
	}
}

func TestPiConfig_AutoConfirm(t *testing.T) {
	if cfg := (PiConfig{RPCAutoConfirm: true}); !cfg.AutoConfirm() {
		t.Errorf("PiConfig{RPCAutoConfirm:true}.AutoConfirm() = false, want true")
	}
	if cfg := (PiConfig{}); cfg.AutoConfirm() {
		t.Errorf("PiConfig{}.AutoConfirm() = true, want false")
	}
}

func TestPiConfig_YAMLModeAndAutoConfirm(t *testing.T) {
	const raw = `
mode: rpc
rpc_auto_confirm: true
`
	var cfg PiConfig
	if err := yaml.Unmarshal([]byte(raw), &cfg); err != nil {
		t.Fatalf("yaml.Unmarshal failed: %v", err)
	}
	if cfg.Mode != "rpc" {
		t.Errorf("Mode = %q, want rpc", cfg.Mode)
	}
	if !cfg.RPCEnabled() {
		t.Errorf("RPCEnabled() = false, want true")
	}
	if !cfg.RPCAutoConfirm {
		t.Errorf("RPCAutoConfirm = false, want true")
	}
	if !cfg.AutoConfirm() {
		t.Errorf("AutoConfirm() = false, want true")
	}
}

// TestIntegration_RPCConnectorUIAndMode is the US-001 integration gate: it
// proves that the RPC connector answers a select ask with a cancelled reply,
// refuses to answer a setStatus notification, and that the default Pi mode is
// text (RPCEnabled false). No internal dependencies are mocked.
func TestIntegration_RPCConnectorUIAndMode(t *testing.T) {
	// Mode unset -> default text mode.
	if cfg := (PiConfig{}); cfg.RPCEnabled() {
		t.Fatalf("agents.pi.mode unset must keep RPC disabled")
	}

	// select is a blocking ask and must be answered with cancelled=true.
	reply, ok := agents.UIResponseFor("ask-1", "select", false)
	if !ok {
		t.Fatalf("UIResponseFor(select) ok = false, want true")
	}
	var m map[string]interface{}
	if err := json.Unmarshal(reply, &m); err != nil {
		t.Fatalf("failed to unmarshal select reply: %v", err)
	}
	if m["type"] != "extension_ui_response" || m["id"] != "ask-1" {
		t.Errorf("select reply has wrong envelope: %v", m)
	}
	if m["cancelled"] != true {
		t.Errorf("select reply cancelled = %v, want true", m["cancelled"])
	}
	if _, hasConfirmed := m["confirmed"]; hasConfirmed {
		t.Errorf("select reply must not contain a confirmed key")
	}

	// setStatus is a notification and must never be answered.
	reply, ok = agents.UIResponseFor("ask-1", "setStatus", false)
	if ok {
		t.Fatalf("UIResponseFor(setStatus) ok = true, want false")
	}
	if len(reply) > 0 && !strings.Contains(string(reply), "extension_ui_response") {
		// Strictly no reply at all; tolerate empty bytes but never a real response.
		t.Errorf("setStatus must produce no reply, got %q", reply)
	}
}
