package core

// FR-5 contract tests (PRD docs/PRDs/mirror-health-prd-20261022.md):
// Config docs, runbook line, SLO check.
//
// Pins four behaviors:
//  1. scripts/mirror-health-slo.sh is syntactically valid and exits 0 on a
//     healthy mirror (0 mirror_drift_detected, >=1 mirror_audit_clean per
//     registered store in the window), 1 on a breach, 2 on infrastructure
//     errors, and never mutates the store it audits (read-only contract).
//  2. docs/architecture/shepherd.md carries a mirror-health subsection
//     naming every FR-2 knob: mirror_health.enabled,
//     mirror_health.audit_interval, and the guarded auto_resync knob with
//     its OFF-by-default posture.
//  3. AGENTS.md operating-knobs table covers the mirror_health knobs.
//  4. The runbook carries the post-emergency-surgery checklist line: after
//     any direct-sqlite repair, run `br sync --import-only` in that store.

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const (
	sloScriptRel = "../.." + "/scripts/mirror-health-slo.sh"
	shepherdRel  = "../.." + "/docs/architecture/shepherd.md"
	agentsRel    = "../.." + "/AGENTS.md"
	hostSetupRel = "../.." + "/docs/operations/host-setup.md"
)

// runSLOScript executes scripts/mirror-health-slo.sh with the given env
// overrides layered over os.Environ() (later keys win; duplicates for the
// overridden keys are removed first so getenv order cannot bite).
func runSLOScript(t *testing.T, env map[string]string) (int, string) {
	t.Helper()

	overridden := map[string]bool{}
	for k := range env {
		overridden[k] = true
	}
	environ := make([]string, 0, len(os.Environ())+len(env))
	for _, kv := range os.Environ() {
		name := kv
		if i := strings.Index(kv, "="); i >= 0 {
			name = kv[:i]
		}
		if !overridden[name] {
			environ = append(environ, kv)
		}
	}
	for k, v := range env {
		environ = append(environ, k+"="+v)
	}

	var stdout, stderr bytes.Buffer
	cmd := exec.Command("bash", sloScriptRel)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	cmd.Env = environ
	err := cmd.Run()
	exitCode := 0
	if exitErr, ok := err.(*exec.ExitError); ok {
		exitCode = exitErr.ExitCode()
	} else if err != nil {
		t.Fatalf("run %s: %v", sloScriptRel, err)
	}
	return exitCode, stdout.String() + stderr.String()
}

// sloFixture builds a hermetic SLO fixture: an events dir with the given
// JSONL body and (optionally) a registered store whose path line lands in a
// hydra.yaml fixture. Returns the env overrides to run the script with.
func sloFixture(t *testing.T, eventsJSONL string, withStore bool) map[string]string {
	t.Helper()

	dir := t.TempDir()
	env := map[string]string{}

	evts := filepath.Join(dir, "events")
	if err := os.MkdirAll(evts, 0o755); err != nil {
		t.Fatalf("mkdir events: %v", err)
	}
	env["HYDRA_EVENTS_DIR"] = evts

	if withStore {
		proj := filepath.Join(dir, "proj")
		store := filepath.Join(proj, ".beads")
		if err := os.MkdirAll(store, 0o755); err != nil {
			t.Fatalf("mkdir store: %v", err)
		}
		// The mirror jsonl the script must never touch (read-only contract).
		jsonl := filepath.Join(store, "issues.jsonl")
		if err := os.WriteFile(jsonl, []byte("{\"id\":\"x-1\",\"status\":\"open\"}\n"), 0o644); err != nil {
			t.Fatalf("write fixture jsonl: %v", err)
		}
		cfg := filepath.Join(dir, "hydra.yaml")
		body := "projects:\n  list:\n    - name: proj\n      path: " + proj + "\n"
		if err := os.WriteFile(cfg, []byte(body), 0o644); err != nil {
			t.Fatalf("write fixture config: %v", err)
		}
		env["HYDRA_CONFIG"] = cfg
	} else {
		// No registered store: point the config at a nonexistent path so the
		// ambient host config cannot leak into the fixture.
		env["HYDRA_CONFIG"] = filepath.Join(dir, "nonexistent.yaml")
	}

	if eventsJSONL != "" {
		line := eventsJSONL
		if !strings.HasSuffix(line, "\n") {
			line += "\n"
		}
		if err := os.WriteFile(filepath.Join(evts, "fixture.jsonl"), []byte(line), 0o644); err != nil {
			t.Fatalf("write fixture events: %v", err)
		}
	}
	return env
}

func isoNow() string { return time.Now().UTC().Format(time.RFC3339) }

func TestMirrorHealthSLOScript_SyntaxAndHealthyFixture(t *testing.T) {
	mirrorHealthDocsRequireImpl(t)
	// bash -n must pass.
	if out, err := exec.Command("bash", "-n", sloScriptRel).CombinedOutput(); err != nil {
		t.Fatalf("bash -n %s failed: %v: %s", sloScriptRel, err, out)
	}

	// Build a self-contained healthy fixture so the event store path matches
	// the registered store path derived from the fixture config.
	dir := t.TempDir()
	proj := filepath.Join(dir, "proj")
	if err := os.MkdirAll(filepath.Join(proj, ".beads"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	cfg := filepath.Join(dir, "hydra.yaml")
	if err := os.WriteFile(cfg, []byte("projects:\n  list:\n    - name: proj\n      path: "+proj+"\n"), 0o644); err != nil {
		t.Fatalf("write cfg: %v", err)
	}
	evts := filepath.Join(dir, "events")
	if err := os.MkdirAll(evts, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// Clean event for the registered store, now (inside the 24h window).
	body := `{"time":"` + isoNow() + `","type":"mirror_audit_clean","fields":{"store":"` + proj + `/.beads"}}` + "\n"
	if err := os.WriteFile(filepath.Join(evts, "f.jsonl"), []byte(body), 0o644); err != nil {
		t.Fatalf("write events: %v", err)
	}

	code, out := runSLOScript(t, map[string]string{
		"HYDRA_EVENTS_DIR": evts,
		"HYDRA_CONFIG":     cfg,
	})
	if code != 0 {
		t.Fatalf("RED: healthy mirror must exit 0, got %d (%s)", code, out)
	}
	if !strings.Contains(out, "SLO-OK") {
		t.Fatalf("RED: healthy run must print SLO-OK, got %q", out)
	}
}

func TestMirrorHealthSLOScript_HealthyMirrorExitsZero(t *testing.T) {
	mirrorHealthDocsRequireImpl(t)
	dir := t.TempDir()
	proj := filepath.Join(dir, "proj")
	if err := os.MkdirAll(filepath.Join(proj, ".beads"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	cfg := filepath.Join(dir, "hydra.yaml")
	if err := os.WriteFile(cfg, []byte("projects:\n  list:\n    - name: proj\n      path: "+proj+"\n"), 0o644); err != nil {
		t.Fatalf("write cfg: %v", err)
	}
	evts := filepath.Join(dir, "events")
	if err := os.MkdirAll(evts, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// Clean event for the registered store, now (inside the 24h window).
	body := `{"time":"` + isoNow() + `","type":"mirror_audit_clean","fields":{"store":"` + proj + `/.beads"}}` + "\n"
	if err := os.WriteFile(filepath.Join(evts, "f.jsonl"), []byte(body), 0o644); err != nil {
		t.Fatalf("write events: %v", err)
	}

	code, out := runSLOScript(t, map[string]string{
		"HYDRA_EVENTS_DIR": evts,
		"HYDRA_CONFIG":     cfg,
	})
	if code != 0 {
		t.Fatalf("RED: healthy mirror must exit 0, got %d (%s)", code, out)
	}
	if !strings.Contains(out, "SLO-OK") {
		t.Fatalf("RED: healthy run must print SLO-OK, got %q", out)
	}
}

func TestMirrorHealthSLOScript_DriftExitsOne(t *testing.T) {
	mirrorHealthDocsRequireImpl(t)
	dir := t.TempDir()
	evts := filepath.Join(dir, "events")
	if err := os.MkdirAll(evts, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	body := `{"time":"` + isoNow() + `","type":"mirror_drift_detected","fields":{"store":"` + dir + `/proj/.beads","bead":"x-1"}}` + "\n"
	if err := os.WriteFile(filepath.Join(evts, "f.jsonl"), []byte(body), 0o644); err != nil {
		t.Fatalf("write events: %v", err)
	}

	code, out := runSLOScript(t, map[string]string{
		"HYDRA_EVENTS_DIR": evts,
		"HYDRA_CONFIG":     filepath.Join(dir, "nonexistent.yaml"), // no registered stores
	})
	if code != 1 {
		t.Fatalf("RED: drift must exit 1, got %d (%s)", code, out)
	}
	if !strings.Contains(out, "SLO-BREACH") || !strings.Contains(out, "mirror_drift_detected") {
		t.Fatalf("RED: drift diagnostic must name SLO-BREACH + mirror_drift_detected, got %q", out)
	}
}

func TestMirrorHealthSLOScript_RegisteredStoreWithoutCleanEventExitsOne(t *testing.T) {
	mirrorHealthDocsRequireImpl(t)
	dir := t.TempDir()
	proj := filepath.Join(dir, "proj")
	if err := os.MkdirAll(filepath.Join(proj, ".beads"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	cfg := filepath.Join(dir, "hydra.yaml")
	if err := os.WriteFile(cfg, []byte("projects:\n  list:\n    - name: proj\n      path: "+proj+"\n"), 0o644); err != nil {
		t.Fatalf("write cfg: %v", err)
	}
	evts := filepath.Join(dir, "events")
	if err := os.MkdirAll(evts, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// Clean event only for an UNregistered store: the registered store has
	// no coverage in the window -> breach.
	body := `{"time":"` + isoNow() + `","type":"mirror_audit_clean","fields":{"store":"` + filepath.Join(dir, "other", ".beads") + `"}}` + "\n"
	if err := os.WriteFile(filepath.Join(evts, "f.jsonl"), []byte(body), 0o644); err != nil {
		t.Fatalf("write events: %v", err)
	}

	code, out := runSLOScript(t, map[string]string{
		"HYDRA_EVENTS_DIR": evts,
		"HYDRA_CONFIG":     cfg,
	})
	if code != 1 {
		t.Fatalf("RED: registered store without clean event must exit 1, got %d (%s)", code, out)
	}
	if !strings.Contains(out, "mirror_audit_clean") {
		t.Fatalf("RED: diagnostic must name the missing mirror_audit_clean coverage, got %q", out)
	}
}

func TestMirrorHealthSLOScript_InfraFailureExitsTwo(t *testing.T) {
	mirrorHealthDocsRequireImpl(t)
	dir := t.TempDir()
	evts := filepath.Join(dir, "events")
	if err := os.MkdirAll(evts, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(evts, "f.jsonl"), []byte("{\"type\":\"mirror_audit_clean\"}"), 0o644); err != nil {
		t.Fatalf("write events: %v", err)
	}
	env := map[string]string{
		"HYDRA_EVENTS_DIR": evts,
		"HYDRA_CONFIG":     filepath.Join(dir, "nonexistent.yaml"),
		"PATH":             "/nonexistent", // jq/date unavailable -> infra error
	}
	code, out := runSLOScript(t, env)
	if code != 2 {
		t.Fatalf("RED: infrastructure failure (missing tools) must exit 2 per the script header contract, got %d (%s)", code, out)
	}
}

func TestMirrorHealthSLOScript_ReadOnlyNeverMutatesStore(t *testing.T) {
	mirrorHealthDocsRequireImpl(t)
	dir := t.TempDir()
	proj := filepath.Join(dir, "proj")
	store := filepath.Join(proj, ".beads")
	if err := os.MkdirAll(store, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	jsonl := filepath.Join(store, "issues.jsonl")
	orig := []byte("{\"id\":\"x-1\",\"status\":\"open\"}\n")
	if err := os.WriteFile(jsonl, orig, 0o644); err != nil {
		t.Fatalf("write jsonl: %v", err)
	}
	cfg := filepath.Join(dir, "hydra.yaml")
	if err := os.WriteFile(cfg, []byte("projects:\n  list:\n    - name: proj\n      path: "+proj+"\n"), 0o644); err != nil {
		t.Fatalf("write cfg: %v", err)
	}
	evts := filepath.Join(dir, "events")
	if err := os.MkdirAll(evts, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	body := `{"time":"` + isoNow() + `","type":"mirror_audit_clean","fields":{"store":"` + store + `"}}` + "\n"
	if err := os.WriteFile(filepath.Join(evts, "f.jsonl"), []byte(body), 0o644); err != nil {
		t.Fatalf("write events: %v", err)
	}

	code, out := runSLOScript(t, map[string]string{
		"HYDRA_EVENTS_DIR": evts,
		"HYDRA_CONFIG":     cfg,
	})
	if code != 0 {
		t.Fatalf("RED: run must be healthy before the read-only probe, got %d (%s)", code, out)
	}
	after, err := os.ReadFile(jsonl)
	if err != nil {
		t.Fatalf("read jsonl after run: %v", err)
	}
	if !bytes.Equal(orig, after) {
		t.Fatalf("RED: SLO script mutated the store mirror (read-only contract violated): %q -> %q", orig, after)
	}
}

func TestMirrorHealthDocs_ShepherdNamesAllKnobs(t *testing.T) {
	mirrorHealthDocsRequireImpl(t)
	data, err := os.ReadFile(shepherdRel)
	if err != nil {
		t.Fatalf("read %s: %v", shepherdRel, err)
	}
	src := string(data)
	// PRD FR-2 knobs + FR-3 guarded auto-resync must be named in the
	// mirror-health docs section.
	for _, knob := range []string{"mirror_health.enabled", "mirror_health.audit_interval", "auto_resync"} {
		if !strings.Contains(src, knob) {
			t.Fatalf("RED: %s mirror-health section must name knob %s", shepherdRel, knob)
		}
	}
}

func TestMirrorHealthDocs_ShepherdAutoResyncOffByDefault(t *testing.T) {
	mirrorHealthDocsRequireImpl(t)
	data, err := os.ReadFile(shepherdRel)
	if err != nil {
		t.Fatalf("read %s: %v", shepherdRel, err)
	}
	src := string(data)
	idx := strings.Index(src, "auto_resync")
	if idx < 0 {
		t.Fatalf("RED: shepherd.md must document the auto_resync knob")
	}
	window := src[max(0, idx-200):min(len(src), idx+400)]
	lower := strings.ToLower(window)
	if !strings.Contains(lower, "off") || (!strings.Contains(lower, "default") && !strings.Contains(lower, "by default")) {
		t.Fatalf("RED: shepherd.md must pin auto_resync OFF by default (guarded healing posture), got context %q", window)
	}
}

func TestMirrorHealthDocs_AgentsOperatingTableCoversKnobs(t *testing.T) {
	mirrorHealthDocsRequireImpl(t)
	data, err := os.ReadFile(agentsRel)
	if err != nil {
		t.Fatalf("read %s: %v", agentsRel, err)
	}
	src := string(data)
	if !strings.Contains(src, "mirror_health") {
		t.Fatalf("RED: AGENTS.md operating knobs table must cover the mirror_health knobs")
	}
}

func TestMirrorHealthDocs_RunbookPostRepairImportOnlyLine(t *testing.T) {
	mirrorHealthDocsRequireImpl(t)
	found := false
	var missing []string
	for _, path := range []string{shepherdRel, hostSetupRel} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		if strings.Contains(string(data), "br sync --import-only") {
			found = true
			break
		}
		missing = append(missing, path)
	}
	if !found {
		t.Fatalf("RED: runbook line missing — after any direct-sqlite / emergency repair, operators must be told to run `br sync --import-only` in that store (checked %v)", missing)
	}
}
