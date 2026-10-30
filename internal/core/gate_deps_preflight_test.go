package core

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// gateDepsRequireImpl is the RED-phase gate (same contract as the
// verification-command and bead-claims suites): the feature is not
// implemented yet, so by default the contract tests SKIP loudly and the
// suite exits 0 (the daemon gate stays green). Setting GATE_DEPS_ENFORCE=1
// turns every skip into a loud failure, proving the RED phase detects the
// missing contract.
func gateDepsRequireImpl(t *testing.T) {
	t.Helper()
	if depsPreflightImplemented {
		return // implemented
	}
	msg := "RED: gate deps preflight contract not yet implemented"
	if enforce := strings.TrimSpace(strings.ToLower(os.Getenv(gateDepsEnforceEnv))); enforce == "1" || enforce == "true" {
		t.Fatal(msg)
	}
	t.Skip(msg)
}

// gateDepsCall is one recorded runner invocation.
type gateDepsCall struct {
	dir string
	cmd string
}

// gateDepsRecorder is a fake command runner that records every invocation in
// order and answers through respond. It lets the contract tests pin the
// preflight behavior (install step, ordering, directory, output forwarding)
// without shelling out to a real package manager.
type gateDepsRecorder struct {
	calls   []gateDepsCall
	respond func(dir, cmd string) (string, error)
}

func (r *gateDepsRecorder) run(dir, cmd string) (string, error) {
	r.calls = append(r.calls, gateDepsCall{dir: dir, cmd: cmd})
	return r.respond(dir, cmd)
}

// installCalls returns the recorded calls that are not the verification
// command — i.e. the install step(s).
func (r *gateDepsRecorder) installCalls(verifyCmd string) []gateDepsCall {
	var out []gateDepsCall
	for _, c := range r.calls {
		if c.cmd != verifyCmd {
			out = append(out, c)
		}
	}
	return out
}

// gateDepsWriteManifest drops a minimal package.json into dir.
func gateDepsWriteManifest(t *testing.T, dir string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"name":"fixture","version":"0.0.0"}`), 0o644); err != nil {
		t.Fatalf("write package.json: %v", err)
	}
}

// gateDepsGitOK fails the test when a git command in dir fails.
func gateDepsGitOK(t *testing.T, dir string, args ...string) {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
}

// AC: missing node_modules + package.json → an install step runs BEFORE the
// verification command, in the worktree dir, and the verification output is
// forwarded.
func TestGateDeps_InstallWhenManifestMissingNodeModules(t *testing.T) {
	gateDepsRequireImpl(t)

	dir := t.TempDir()
	gateDepsWriteManifest(t, dir)

	verifyCmd := "npm test"
	verifyOut := "npm test output"
	rec := &gateDepsRecorder{respond: func(_, cmd string) (string, error) {
		if cmd == verifyCmd {
			return verifyOut, nil
		}
		return "added 42 packages", nil
	}}

	out, err := gateDepsPreflight(dir, verifyCmd, rec.run)
	if err != nil {
		t.Fatalf("expected clean install + verification, got error: %v", err)
	}
	installs := rec.installCalls(verifyCmd)
	if len(installs) == 0 {
		t.Fatalf("expected an install step before the verification command, calls: %+v", rec.calls)
	}
	for _, c := range installs {
		if c.dir != dir {
			t.Fatalf("install ran in %q, want worktree %q", c.dir, dir)
		}
	}
	if last := rec.calls[len(rec.calls)-1]; last.cmd != verifyCmd || last.dir != dir {
		t.Fatalf("verification command must run last in the worktree, got %+v", last)
	}
	if out != verifyOut {
		t.Fatalf("verification output not forwarded: got %q, want %q", out, verifyOut)
	}
}

// AC: no manifest → install skipped entirely, verification command still
// runs and its output is forwarded.
func TestGateDeps_NoManifestSkipsInstallAndRunsVerification(t *testing.T) {
	gateDepsRequireImpl(t)

	dir := t.TempDir() // no package.json
	verifyCmd := "go test ./..."
	verifyOut := "ok  \texample.com/x\t0.001s"
	rec := &gateDepsRecorder{respond: func(_, cmd string) (string, error) {
		if cmd == verifyCmd {
			return verifyOut, nil
		}
		return "", nil
	}}

	out, err := gateDepsPreflight(dir, verifyCmd, rec.run)
	if err != nil {
		t.Fatalf("no manifest must skip install, got error: %v", err)
	}
	if len(rec.calls) != 1 || rec.calls[0].cmd != verifyCmd || rec.calls[0].dir != dir {
		t.Fatalf("expected exactly the verification command in the worktree, got %+v", rec.calls)
	}
	if out != verifyOut {
		t.Fatalf("verification output not forwarded: got %q, want %q", out, verifyOut)
	}
}

// AC: an install that dirties tracked files is treated as a failure
// (tree-clean guard) — the verification command must NOT run.
func TestGateDeps_InstallDirtyingTrackedFilesFails(t *testing.T) {
	gateDepsRequireImpl(t)

	dir := t.TempDir()
	gateDepsGitOK(t, dir, "init")
	gateDepsGitOK(t, dir, "config", "user.email", "test@example.com")
	gateDepsGitOK(t, dir, "config", "user.name", "test")
	gateDepsWriteManifest(t, dir)
	tracked := filepath.Join(dir, "tracked.txt")
	if err := os.WriteFile(tracked, []byte("clean\n"), 0o644); err != nil {
		t.Fatalf("write tracked file: %v", err)
	}
	gateDepsGitOK(t, dir, "add", "-A")
	gateDepsGitOK(t, dir, "commit", "-m", "init")

	verifyCmd := "npm test"
	rec := &gateDepsRecorder{respond: func(_, cmd string) (string, error) {
		if strings.Contains(cmd, "status") {
			// Real tree-clean probe: report the true worktree state.
			out, err := exec.Command("git", "-C", dir, "status", "--porcelain").Output()
			return string(out), err
		}
		if cmd != verifyCmd { // the install step dirties a tracked file
			if err := os.WriteFile(tracked, []byte("dirty\n"), 0o644); err != nil {
				return "", err
			}
			return "added 1 package", nil
		}
		return "MUST NOT RUN", nil
	}}

	out, err := gateDepsPreflight(dir, verifyCmd, rec.run)
	if err == nil {
		t.Fatalf("install that dirties tracked files must be treated as a failure, got output %q", out)
	}
	for i, c := range rec.calls {
		if c.cmd == verifyCmd {
			t.Fatalf("verification command must not run after a dirty install (call %d: %+v)", i, c)
		}
	}
}

// AC: the Test Command output is forwarded verbatim — --- PASS lines
// preserved, nothing swallowed.
func TestGateDeps_VerificationOutputPreservesPASSLines(t *testing.T) {
	gateDepsRequireImpl(t)

	dir := t.TempDir()
	verifyCmd := "go test ./... -run TestX -v"
	verifyOut := "=== RUN   TestX\n--- PASS: TestX (0.00s)\n--- PASS: TestX/sub (0.00s)\nPASS\nok  \texample.com/x\t0.01s\n"
	rec := &gateDepsRecorder{respond: func(_, cmd string) (string, error) {
		if cmd == verifyCmd {
			return verifyOut, nil
		}
		return "", nil
	}}

	out, err := gateDepsPreflight(dir, verifyCmd, rec.run)
	if err != nil {
		t.Fatalf("verification must run clean, got error: %v", err)
	}
	if out != verifyOut {
		t.Fatalf("output must be forwarded verbatim:\n got %q\nwant %q", out, verifyOut)
	}
	if got := strings.Count(out, "--- PASS"); got != 2 {
		t.Fatalf("output must list --- PASS lines, got %d", got)
	}
	if strings.Contains(out, "[no tests to run]") {
		t.Fatalf("output must never contain [no tests to run]")
	}
}

// AC (inverse of the install trigger): manifest + node_modules already
// present → no install step.
func TestGateDeps_PresentNodeModulesSkipsInstall(t *testing.T) {
	gateDepsRequireImpl(t)

	dir := t.TempDir()
	gateDepsWriteManifest(t, dir)
	if err := os.MkdirAll(filepath.Join(dir, "node_modules"), 0o755); err != nil {
		t.Fatalf("mkdir node_modules: %v", err)
	}

	verifyCmd := "npm test"
	rec := &gateDepsRecorder{respond: func(_, cmd string) (string, error) {
		if cmd == verifyCmd {
			return "ok", nil
		}
		return "added 42 packages", nil
	}}

	out, err := gateDepsPreflight(dir, verifyCmd, rec.run)
	if err != nil {
		t.Fatalf("existing node_modules must skip install, got error: %v", err)
	}
	if installs := rec.installCalls(verifyCmd); len(installs) != 0 {
		t.Fatalf("install must be skipped when node_modules exists, got %+v", installs)
	}
	if out != "ok" {
		t.Fatalf("verification output not forwarded: got %q", out)
	}
}
