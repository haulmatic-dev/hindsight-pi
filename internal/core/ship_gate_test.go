package core

import "testing"

func TestRunShipGate(t *testing.T) {
	dir := t.TempDir()
	cases := []struct {
		name     string
		sg       ShipGateConfig
		wantPass bool
	}{
		{"disabled", ShipGateConfig{Enabled: false}, true},
		{"empty-cmds", ShipGateConfig{Enabled: true}, true},
		{"build+e2e pass", ShipGateConfig{Enabled: true, BuildCmd: "true", E2ECmd: "true"}, true},
		{"build fail short-circuits", ShipGateConfig{Enabled: true, BuildCmd: "false", E2ECmd: "true"}, false},
		{"e2e fail", ShipGateConfig{Enabled: true, BuildCmd: "true", E2ECmd: "false"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cm := &ComplianceCycleManager{config: ComplianceCycleConfig{ProjectDir: dir, ShipGate: tc.sg}}
			pass, fails := cm.runShipGate()
			if pass != tc.wantPass {
				t.Fatalf("want pass=%v, got pass=%v (failures=%v)", tc.wantPass, pass, fails)
			}
			if !pass && len(fails) == 0 {
				t.Fatalf("failed gate must report at least one failure")
			}
		})
	}
}
