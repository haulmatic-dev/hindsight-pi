package main

import (
	"bytes"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/haulmatic-dev/hydra/internal/degrade"
	"github.com/haulmatic-dev/hydra/pkg/types"
)

// findStatus finds the degrade status entry for name (nil when absent).
func findStatus(name string) *degrade.Status {
	for _, st := range degrade.Snapshot() {
		if st.Name == name {
			return &st
		}
	}
	return nil
}

// Table test for the pure decision core.
func TestHindsightSensorVerdict(t *testing.T) {
	cases := []struct {
		name               string
		expected, resolved bool
		statsOK            bool
		calls              int
		wantFired          bool
		wantReason         string
	}{
		{"not allowlisted: nothing to sense", false, false, false, 0, false, ""},
		{"not allowlisted even when resolved", false, true, true, 5, false, ""},
		{"allowlisted but bootstrap died", true, false, false, 0, true, hindsightReasonBootstrapDied},
		{"allowlisted+resolved, telemetry unreadable: fail open silently", true, true, false, 0, false, ""},
		{"allowlisted+resolved, zero hindsight calls", true, true, true, 0, true, hindsightReasonNoHindsightUse},
		{"healthy: hindsight_search used", true, true, true, 1, false, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := hindsightSensorVerdict(tc.expected, tc.resolved, tc.statsOK, tc.calls)
			if got.Fired != tc.wantFired || got.Reason != tc.wantReason {
				t.Fatalf("verdict = %+v, want fired=%v reason=%q", got, tc.wantFired, tc.wantReason)
			}
		})
	}
}

func TestHindsightToolCalls(t *testing.T) {
	stats := toolMixStats{Counts: map[string]int{
		"hindsight_search": 1, "hindsight_retain": 2, "bash": 9, "read": 4,
	}}
	if got := hindsightToolCalls(stats); got != 3 {
		t.Fatalf("hindsightToolCalls = %d, want 3", got)
	}
	if got := hindsightToolCalls(toolMixStats{Counts: map[string]int{"bash": 1}}); got != 0 {
		t.Fatalf("no-hindsight stats counted as %d, want 0", got)
	}
}

func TestHindsightSensorRecordsDegrade(t *testing.T) {
	degrade.Reset()
	defer degrade.Reset()

	// Bootstrap-failure path: one attempt, zero successes.
	runHindsightSensor(PiConfig{Extensions: []string{"hindsight"}, ConfigDir: "/nonexistent-hindsight-test"}, "", types.Bead{})
	st := findStatus(hindsightSensorSubsystem)
	if st == nil || st.Attempts != 1 || st.Successes != 0 {
		t.Fatalf("bootstrap path: status=%+v, want 1 attempt 0 success", st)
	}

	// Healthy path: resolvable extension + session jsonl with a hindsight
	// toolCall → attempt + success recorded.
	dir := t.TempDir()
	if err := writeFakeHindsightExtension(dir); err != nil {
		t.Fatalf("fixture: %v", err)
	}
	hydraHome := t.TempDir()
	bead := types.Bead{ID: "sensor-hindsight-test"}
	_, sessionDir, err := buildPiSession(bead, hydraHome)
	if err != nil {
		t.Fatalf("buildPiSession: %v", err)
	}
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatalf("mkdir session: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sessionDir, "s.jsonl"),
		[]byte(`{"type":"toolCall","name":"hindsight_search"}`), 0o644); err != nil {
		t.Fatalf("session fixture: %v", err)
	}
	runHindsightSensor(PiConfig{Extensions: []string{"hindsight"}, ConfigDir: dir}, hydraHome, bead)
	st = findStatus(hindsightSensorSubsystem)
	if st == nil || st.Successes != 1 || st.Attempts != 2 {
		t.Fatalf("healthy path: status=%+v, want 2 attempts 1 success", st)
	}

	// Session with zero hindsight calls while extension resolved → fired.
	degrade.Reset()
	if err := os.WriteFile(filepath.Join(sessionDir, "s.jsonl"),
		[]byte(`{"type":"toolCall","name":"bash"}`), 0o644); err != nil {
		t.Fatalf("session fixture: %v", err)
	}
	runHindsightSensor(PiConfig{Extensions: []string{"hindsight"}, ConfigDir: dir}, hydraHome, bead)
	st = findStatus(hindsightSensorSubsystem)
	if st == nil || st.Attempts != 1 || st.Successes != 0 {
		t.Fatalf("no-hindsight-use path: status=%+v, want 1 attempt 0 success", st)
	}

	// Not allowlisted: no further records.
	runHindsightSensor(PiConfig{Extensions: []string{"rtk"}, ConfigDir: dir}, hydraHome, bead)
	st = findStatus(hindsightSensorSubsystem)
	if st.Attempts != 1 || st.Successes != 0 {
		t.Fatalf("not-allowlisted path recorded: status=%+v", st)
	}
}

func TestResolveHindsightEntry_BootstrapsFailLoud(t *testing.T) {
	if _, err := resolveHindsightEntry(PiConfig{ConfigDir: ""}); err != errHindsightNoAgentDir {
		t.Fatalf("empty agent dir err = %v, want errHindsightNoAgentDir", err)
	}
	if _, err := resolveHindsightEntry(PiConfig{ConfigDir: t.TempDir()}); err != errHindsightUnresolved {
		t.Fatalf("unresolved err = %v, want errHindsightUnresolved", err)
	}
	dir := t.TempDir()
	if err := writeFakeHindsightExtension(dir); err != nil {
		t.Fatalf("fixture: %v", err)
	}
	entry, err := resolveHindsightEntry(PiConfig{ConfigDir: dir})
	if err != nil || !strings.HasSuffix(entry, "ext.ts") {
		t.Fatalf("resolved entry = %q err = %v, want ext.ts, nil", entry, err)
	}
}

func TestLoudLogMentionsSubsystem(t *testing.T) {
	// The loud log line is the only human surface when the sensor fires from
	// a worker log; it must name the subsystem so greps land on it.
	degrade.Reset()
	defer degrade.Reset()
	out := captureWorkerLog(t, func() {
		runHindsightSensor(PiConfig{Extensions: []string{"hindsight"}, ConfigDir: "/nonexistent-hindsight-test"}, "", types.Bead{})
	})
	if !strings.Contains(out, "[HindsightSensor]") || !strings.Contains(out, hindsightSensorSubsystem) {
		t.Fatalf("loud log missing sensor markers:\n%s", out)
	}
}

// captureWorkerLog redirects the std logger to a buffer for the duration of fn.
func captureWorkerLog(t *testing.T, fn func()) string {
	t.Helper()
	var buf bytes.Buffer
	w := log.Writer()
	flags := log.Flags()
	log.SetOutput(&buf)
	log.SetFlags(0)
	defer func() {
		log.SetOutput(w)
		log.SetFlags(flags)
	}()
	fn()
	return buf.String()
}

// writeFakeHindsightExtension builds a minimal pi agent dir whose npm tree
// contains a fake hindsight package with a declared extension entry file —
// enough for resolveExtensionPaths/extensionEntryPoint to resolve it.
func writeFakeHindsightExtension(agentDir string) error {
	pkg := filepath.Join(agentDir, "npm", "node_modules", "@walodayeet", "hindsight-pi")
	if err := os.MkdirAll(pkg, 0o755); err != nil {
		return err
	}
	manifest := `{"main":"ext.ts","pi":{"extensions":["ext.ts"]}}`
	if err := os.WriteFile(filepath.Join(pkg, "package.json"), []byte(manifest), 0o644); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(pkg, "ext.ts"), []byte("export default {};\n"), 0o644)
}
