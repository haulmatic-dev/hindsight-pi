package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// requireImpl implements the RED-phase gate: while auditImplemented is
// false, behavior tests skip loudly (GREEN not yet built). With
// BEADS_MIRROR_AUDIT_ENFORCE=1 they run and fail — proving the RED phase
// detects the missing contract.
func requireImpl(t *testing.T) bool {
	t.Helper()
	if !auditImplemented {
		if os.Getenv(enforceEnv) == "1" {
			t.Fatal("RED: beads mirror audit contract not yet implemented")
			return true
		}
		t.Skip("RED: beads mirror audit contract not yet implemented")
	}
	return true
}

// fakeBR installs a fake `br` double on PATH that: writes the given JSON
// for `br list --json` invocations whose cwd contains marker file, and
// exits with the given code on others. Returns the temp bin dir, the
// project dir, and a cleanup fn.
func fakeBR(t *testing.T, listJSON string, failCmd string, failCode int) (binDir, projDir string) {
	t.Helper()
	binDir = t.TempDir()
	projDir = t.TempDir()
	script := "#!/bin/sh\n" +
		"for a in \"$@\"; do case \"$a\" in list|show) " +
		"if [ -n \"" + failCmd + "\" ] && [ \"$1\" = \"" + failCmd + "\" ]; then echo 'br: simulated failure' >&2; exit " + itoa(failCode) + "; fi ;; esac; done\n" +
		"if [ \"$1\" = \"list\" ]; then cat <<'EOF'\n" + listJSON + "\nEOF\nexit 0\nfi\n" +
		"echo 'br show: simulated failure' >&2; exit 1\n"
	if err := os.WriteFile(filepath.Join(binDir, "br"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return binDir, projDir
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
	store := filepath.Join(dir, "issues.jsonl")
	if err := os.WriteFile(store, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func runIn(binDir string, args ...string) (stdout string, code int) {
	cmd := exec.Command(os.Args[0], args...)
	cmd.Env = append(os.Environ(), "GO_WANT_HELPER_PROCESS=1", "BEADS_MIRROR_BIN="+binDir)
	out, _ := cmd.CombinedOutput()
	return string(out), exitCodeOf(cmd)
}

// runTool builds the audit binary and runs it in projDir with the fake br
// on PATH. RED baseline: compiles only.
func runTool(t *testing.T, binDir, projDir string, args ...string) (stdout string, code int) {
	t.Helper()
	if !auditImplemented {
		t.Skip("RED: beads mirror audit contract not yet implemented")
	}
	cmd := exec.Command(binPath(t), args...)
	cmd.Dir = projDir
	cmd.Env = append(os.Environ(), "PATH="+binDir+":"+os.Getenv("PATH"))
	out, runErr := cmd.CombinedOutput()
	code = 0
	if exitErr, ok := runErr.(*exec.ExitError); ok {
		code = exitErr.ExitCode()
	}
	return string(out), code
}

var binCache string

func binPath(t *testing.T) string {
	t.Helper()
	if binCache == "" {
		out, err := exec.Command("go", "build", "-o", filepath.Join(t.TempDir(), "beads-mirror-audit-test"), ".").CombinedOutput()
		if err != nil {
			t.Fatalf("build: %v: %s", err, out)
		}
		binCache = strings.TrimSpace(strings.SplitN(string(out), "\n", 2)[0]) // placeholder replaced by -o path
		t.Fatalf("binPath helper misconfigured")
	}
	return binCache
}
