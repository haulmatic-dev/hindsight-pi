package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// requireImpl is the RED-phase gate: while auditImplemented is false,
// behavior tests skip loudly (GREEN not yet built). With
// BEADS_MIRROR_AUDIT_ENFORCE=1 the same tests fail — proving RED detects
// the missing contract.
func requireImpl(t *testing.T) {
	t.Helper()
	if !auditImplemented {
		if os.Getenv(enforceEnv) == "1" {
			t.Fatal("RED: beads mirror audit contract not yet implemented")
		}
		t.Skip("RED: beads mirror audit contract not yet implemented")
	}
}

// buildBin compiles the audit binary once per test run.
func buildBin(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "beads-mirror-audit")
	out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput()
	if err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	return bin
}

// fakeBR installs a fake `br` double on a temp PATH. listJSON is emitted
// for `br list --json`; any invocation of failCmd (e.g. "list" or "show")
// exits failCode with a stderr line.
func fakeBR(t *testing.T, listJSON, failCmd string, failCode int) (binDir string) {
	t.Helper()
	binDir = t.TempDir()
	var sb strings.Builder
	sb.WriteString("#!/bin/sh\n")
	if failCmd != "" {
		sb.WriteString("if [ \"$1\" = \"" + failCmd + "\" ]; then echo 'br: simulated failure' >&2; exit " + itoa(failCode) + "; fi\n")
	}
	sb.WriteString("if [ \"$1\" = \"list\" ]; then cat <<'EOF'\n" + listJSON + "\nEOF\nexit 0\nfi\n")
	sb.WriteString("echo 'br show: simulated failure' >&2\nexit 1\n")
	if err := os.WriteFile(filepath.Join(binDir, "br"), []byte(sb.String()), 0o755); err != nil {
		t.Fatal(err)
	}
	return binDir
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	s := ""
	for n > 0 {
		s = string(rune('0'+n%10)) + s
		n /= 10
	}
	return s
}

func writeJSONL(t *testing.T, dir, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "issues.jsonl"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// runTool runs the compiled audit binary in projDir with the fake br on
// PATH. Skips under RED gate.
func runTool(t *testing.T, bin, binDir, projDir string, args ...string) (stdout string, code int) {
	t.Helper()
	requireImpl(t)
	cmd := exec.Command(bin, args...)
	cmd.Dir = projDir
	cmd.Env = append(os.Environ(), "PATH="+binDir+":"+os.Getenv("PATH"))
	out, runErr := cmd.CombinedOutput()
	code = 0
	if exitErr, ok := runErr.(*exec.ExitError); ok {
		code = exitErr.ExitCode()
	}
	return string(out), code
}

// storeDir returns the store directory the tool audits (--store arg is the
// directory CONTAINING issues.jsonl per the contract).
func storeFixture(t *testing.T, jsonl, dbJSON string) (bin, binDir, dir string) {
	t.Helper()
	bin = buildBin(t)
	binDir = fakeBR(t, dbJSON, "", 0)
	dir = t.TempDir()
	writeJSONL(t, dir, jsonl)
	return bin, binDir, dir
}

// TestCompileGate always runs: pins the tool's public surface exists so the
// package compiles and the --store/--json contract pieces are present.
func TestCompileGate(t *testing.T) {
	if !auditImplemented {
		t.Log("RED: inert stub compiles; behavior under GREEN")
	}
	if exitClean != 0 || exitDrift != 1 || exitUnavail != 2 {
		t.Fatalf("exit code constants wrong: clean=%d drift=%d unavail=%d", exitClean, exitDrift, exitUnavail)
	}
	if auditUnavailablePrefix != "audit unavailable: " {
		t.Fatalf("audit unavailable prefix changed: %q", auditUnavailablePrefix)
	}
}

// TestAudit_DivergenceLine: fixture jsonl with one divergent record prints
// "<bead-id> jsonl=<status> db=<status>" and exits 1.
func TestAudit_DivergenceLine(t *testing.T) {
	dbJSON := `[{"id":"proj-001","status":"open"},{"id":"proj-002","status":"closed"},{"id":"proj-003","status":"open"}]`
	jsonl := "proj-001 open\nproj-002 closed\nproj-003 closed\n"
	bin, binDir, dir := storeFixture(t, jsonl, dbJSON)
	out, code := runTool(t, bin, binDir, dir, "--store", dir)
	if code != exitDrift {
		t.Fatalf("exit=%d want %d; output: %s", code, exitDrift, out)
	}
	if strings.Contains(out, "mirror clean") {
		t.Fatalf("unexpected clean message on drift")
	}
	if !strings.Contains(out, "proj-003 jsonl=closed db=open") {
		t.Fatalf("divergence line missing, got: %s", out)
	}
}

// TestAudit_CleanMirror: all statuses agree → "mirror clean (N beads)",
// exit 0.
func TestAudit_CleanMirror(t *testing.T) {
	dbJSON := `[{"id":"proj-001","status":"open"},{"id":"proj-002","status":"closed"}]`
	jsonl := "proj-001 open\nproj-002 closed\n"
	bin, binDir, dir := storeFixture(t, jsonl, dbJSON)
	out, code := runTool(t, bin, binDir, dir, "--store", dir)
	if code != exitClean {
		t.Fatalf("exit=%d want 0; output: %s", code, out)
	}
	if !strings.Contains(out, "mirror clean (2 beads)") {
		t.Fatalf("clean line wrong, got: %s", out)
	}
}

// TestAudit_JSON: --json emits {"divergences":[...],"checked":N}.
func TestAudit_JSON(t *testing.T) {
	dbJSON := `[{"id":"proj-001","status":"open"},{"id":"proj-002","status":"open"}]`
	jsonl := "proj-001 open\nproj-002 closed\n"
	bin, binDir, dir := storeFixture(t, jsonl, dbJSON)
	out, code := runTool(t, bin, binDir, dir, "--store", dir, "--json")
	if code != exitDrift {
		t.Fatalf("exit=%d want 1; output: %s", code, out)
	}
	var rep Report
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &rep); err != nil {
		t.Fatalf("invalid JSON report: %v; output: %s", err, out)
	}
	if rep.Checked != 2 || len(rep.Divergences) != 1 || rep.Divergences[0].BeadID != "proj-002" || rep.Divergences[0].JSONLStatus != "closed" || rep.Divergences[0].DBStatus != "open" {
		t.Fatalf("report mismatch: %+v", rep)
	}
}

// TestAudit_BrMissing: br absent → "audit unavailable: ..." + exit 2.
func TestAudit_BrMissing(t *testing.T) {
	bin := buildBin(t)
	dir := t.TempDir()
	writeJSONL(t, dir, "proj-001 open\n")
	emptyBin := t.TempDir() // PATH without br
	out, code := runTool(t, bin, emptyBin, dir, "--store", dir)
	if code != exitUnavail {
		t.Fatalf("exit=%d want 2; output: %s", code, out)
	}
	if !strings.HasPrefix(strings.TrimSpace(out), auditUnavailablePrefix) {
		t.Fatalf("audit unavailable line missing, got: %s", out)
	}
}

// TestAudit_BrFailure: br exists but fails → exit 2 with unavailable line.
func TestAudit_BrFailure(t *testing.T) {
	bin := buildBin(t)
	binDir := fakeBR(t, "[]", "list", 3)
	dir := t.TempDir()
	writeJSONL(t, dir, "proj-001 open\n")
	out, code := runTool(t, bin, binDir, dir, "--store", dir)
	if code != exitUnavail {
		t.Fatalf("exit=%d want 2; output: %s", code, out)
	}
	if !strings.Contains(out, auditUnavailablePrefix) {
		t.Fatalf("unavailable line missing, got: %s", out)
	}
}

// TestAudit_Readonly: audit must not mutate either store — a divergent run
// leaves issues.jsonl byte-identical.
func TestAudit_Readonly(t *testing.T) {
	dbJSON := `[{"id":"proj-001","status":"open"}]`
	jsonl := "proj-001 closed\n"
	bin, binDir, dir := storeFixture(t, jsonl, dbJSON)
	before, _ := os.ReadFile(filepath.Join(dir, "issues.jsonl"))
	runTool(t, bin, binDir, dir, "--store", dir)
	after, err := os.ReadFile(filepath.Join(dir, "issues.jsonl"))
	if err != nil || string(after) != string(before) {
		t.Fatalf("jsonl mutated or unreadable: %v", err)
	}
}
