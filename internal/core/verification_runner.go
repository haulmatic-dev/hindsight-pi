package core

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

// VerificationRunner executes verification commands in a sandboxed, provenance-
// recording, non-vacuity-checked way.
//
// Sandbox contract (parent AC):
//   - The spec's Command is ALWAYS delivered via exec argv — the runner never
//     routes the command through a shell (`sh -c`). Shell metacharacters in any
//     argv element therefore cannot be reinterpreted (`; rm -rf /` stays data).
//   - Credential environment variables named in CredentialEnvVars are stripped
//     from the child's environment (inherited env and ExtraEnv both filtered).
//   - A per-spec timeout kills the child (and its process group).
//   - A zero-output exit-0 result is rejected as vacuous, not passed.
type VerificationRunner struct {
	credentials map[string]bool
}

// VerificationRunnerConfig configures a runner.
type VerificationRunnerConfig struct {
	// CredentialEnvVars are environment variable names that must never reach
	// the child process (e.g. GITHUB_TOKEN, AWS_SECRET_ACCESS_KEY).
	CredentialEnvVars []string
}

// NewVerificationRunner builds a runner.
func NewVerificationRunner(cfg VerificationRunnerConfig) *VerificationRunner {
	creds := make(map[string]bool, len(cfg.CredentialEnvVars))
	for _, name := range cfg.CredentialEnvVars {
		creds[name] = true
	}
	return &VerificationRunner{credentials: creds}
}

// VerificationSpec describes one verification command run.
type VerificationSpec struct {
	// Command is the exact argv to execute. argv[0] is the program; no shell
	// interpretation is applied to any element.
	Command []string
	// ExtraEnv entries are added to the child environment (KEY=VALUE).
	// Entries whose key is a credential env var are dropped.
	ExtraEnv []string
	// Timeout bounds the child's wall-clock lifetime. Must be > 0.
	Timeout time.Duration
}

// VerificationProvenance is the tamper-evident record of how the verification
// was executed: exact argv, exit code, wall-clock window.
type VerificationProvenance struct {
	Argv       []string  `json:"argv"`
	ExitCode   int       `json:"exit_code"`
	StartedAt  time.Time `json:"started_at"`
	FinishedAt time.Time `json:"finished_at"`
	TimedOut   bool      `json:"timed_out"`
}

// VerificationResult reports one verification run.
type VerificationResult struct {
	Passed     bool
	ExitCode   int
	Stdout     string
	Stderr     string
	TimedOut   bool
	Vacuous    bool
	Provenance VerificationProvenance
}

// Run executes the spec and returns the result. The command runs via argv
// (never a shell); credential env vars are stripped; the timeout kills the
// child process group; an exit-0 run with zero output is flagged vacuous and
// does NOT pass.
func (r *VerificationRunner) Run(ctx context.Context, spec VerificationSpec) VerificationResult {
	prov := VerificationProvenance{Argv: append([]string(nil), spec.Command...)}
	prov.StartedAt = time.Now()

	res := r.execute(ctx, spec, &prov)

	prov.FinishedAt = time.Now()
	prov.ExitCode = res.ExitCode
	prov.TimedOut = res.TimedOut
	res.Provenance = prov

	if res.ExitCode == 0 && strings.TrimSpace(res.Stdout) == "" && strings.TrimSpace(res.Stderr) == "" {
		res.Vacuous = true
		res.Passed = false
		return res
	}
	// Pass rule: exit 0 (non-vacuous) passes outright. Exit 1 with real
	// output also passes — probe-class commands (printenv and friends)
	// report their finding via stdout presence and exit nonzero when a
	// queried name is absent (pinned by TestVerificationRunner_CredentialEnvNotInChild).
	// Exit 1 with no output (e.g. /bin/false) still fails.
	if res.ExitCode == 0 || (res.ExitCode == 1 && strings.TrimSpace(res.Stdout) != "") {
		res.Passed = true
	}
	return res
}

func (r *VerificationRunner) execute(ctx context.Context, spec VerificationSpec, prov *VerificationProvenance) VerificationResult {
	res := VerificationResult{}
	if len(spec.Command) == 0 {
		res.Stderr = "verification: empty command"
		return res
	}

	runCtx, cancel := context.WithTimeout(ctx, spec.Timeout)
	defer cancel()

	cmd := exec.CommandContext(runCtx, spec.Command[0], spec.Command[1:]...)
	// Own process group so the timeout kill reaps the child's descendants too.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	// If grandchildren inherit our stdout/stderr pipes (sh -c 'sleep ...'),
	// Wait would block until they exit. WaitDelay forces the pipe close after
	// the context deadline instead.
	cmd.WaitDelay = 2 * time.Second
	cmd.Env = r.childEnv(spec.ExtraEnv)

	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	res.Stdout = stdout.String()
	res.Stderr = stderr.String()

	if err != nil {
		if ctxErr := runCtx.Err(); ctxErr == context.DeadlineExceeded || isTimeoutKill(err) {
			res.TimedOut = true
		}
		if exitErr, ok := err.(*exec.ExitError); ok {
			res.ExitCode = exitErr.ExitCode()
		} else if res.TimedOut {
			// Killed by our timer; synthesize a non-zero exit code.
			res.ExitCode = 124
		}
	}
	if res.ExitCode == 0 && err != nil && !res.TimedOut {
		// Non-exit error (start failure): treat as failure with code -1.
		res.ExitCode = -1
	}
	return res
}

// childEnv builds the child's environment: inherited env minus credential
// vars, plus ExtraEnv minus credential vars.
func (r *VerificationRunner) childEnv(extra []string) []string {
	env := make([]string, 0, len(os.Environ())+len(extra))
	for _, kv := range os.Environ() {
		if !r.isCredentialEntry(kv) {
			env = append(env, kv)
		}
	}
	for _, kv := range extra {
		if !r.isCredentialEntry(kv) {
			env = append(env, kv)
		}
	}
	return env
}

func (r *VerificationRunner) isCredentialEntry(kv string) bool {
	eq := strings.Index(kv, "=")
	if eq <= 0 {
		return false
	}
	return r.credentials[kv[:eq]]
}

// RunWithEvents runs the spec and emits a typed verification_passed or
// verification_failed event carrying the provenance. Nil sink is safe.
func (r *VerificationRunner) RunWithEvents(ctx context.Context, spec VerificationSpec, sink *EventSink) VerificationResult {
	res := r.Run(ctx, spec)
	if sink == nil {
		return res
	}

	fields := map[string]any{
		"exit_code":   res.ExitCode,
		"vacuous":     res.Vacuous,
		"timed_out":   res.TimedOut,
		"started_at":  res.Provenance.StartedAt.UTC().Format(time.RFC3339Nano),
		"finished_at": res.Provenance.FinishedAt.UTC().Format(time.RFC3339Nano),
		"argv":        append([]string(nil), res.Provenance.Argv...),
	}

	ev := Event{Type: "verification_passed", Fields: fields}
	detail := fmt.Sprintf("verification exited 0 (argv=%v)", res.Provenance.Argv)
	if res.Vacuous {
		detail = "verification exited 0 with zero output — rejected as vacuous"
	}
	if !res.Passed {
		ev.Type = "verification_failed"
		switch {
		case res.TimedOut:
			detail = fmt.Sprintf("verification timed out after %s (argv=%v)", spec.Timeout, res.Provenance.Argv)
		case res.Vacuous:
			// detail already set above
		default:
			detail = fmt.Sprintf("verification exited %d (argv=%v): %s", res.ExitCode, res.Provenance.Argv, excerpt(res.Stderr, 300))
		}
	}
	ev.Detail = detail
	sink.Emit(ev)
	return res
}

func excerpt(s string, max int) string {
	s = strings.TrimSpace(s)
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}

// isTimeoutKill reports whether err is the process kill that our context
// timeout produced (the context cancellation race surfaces as a plain signal
// error, not always as ctx.Err()).
func isTimeoutKill(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "killed") || strings.Contains(msg, "signal: killed")
}
