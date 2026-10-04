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

// VerificationRunner executes verification commands in a sandboxed, argv-only
// manner: no shell is ever involved, credential environment variables are
// stripped from the child, timeouts kill the process group, and a vacuous
// zero-output success is rejected.
type VerificationRunner struct {
	credentials map[string]bool
}

type VerificationRunnerConfig struct {
	CredentialEnvVars []string
}

func NewVerificationRunner(cfg VerificationRunnerConfig) *VerificationRunner {
	creds := make(map[string]bool, len(cfg.CredentialEnvVars))
	for _, name := range cfg.CredentialEnvVars {
		creds[name] = true
	}
	return &VerificationRunner{credentials: creds}
}

type VerificationSpec struct {
	Command  []string
	ExtraEnv []string
	Timeout  time.Duration
}

// VerificationProvenance records how the verification ran: the exact argv,
// wall-clock bounds, exit code, and whether the run ended in a timeout kill.
type VerificationProvenance struct {
	Argv       []string  `json:"argv"`
	ExitCode   int       `json:"exit_code"`
	StartedAt  time.Time `json:"started_at"`
	FinishedAt time.Time `json:"finished_at"`
	TimedOut   bool      `json:"timed_out"`
}

type VerificationResult struct {
	Passed     bool
	ExitCode   int
	Stdout     string
	Stderr     string
	TimedOut   bool
	Vacuous    bool
	Provenance VerificationProvenance
}

func (r *VerificationRunner) Run(ctx context.Context, spec VerificationSpec) VerificationResult {
	prov := VerificationProvenance{Argv: append([]string(nil), spec.Command...)}
	prov.StartedAt = time.Now()

	res := r.execute(ctx, spec)
	res.Provenance = finalizeProvenance(&prov, res)
	classifyResult(&res)
	return res
}

// finalizeProvenance stamps the end-of-run fields onto prov and attaches it to res.
func finalizeProvenance(prov *VerificationProvenance, res VerificationResult) VerificationProvenance {
	prov.FinishedAt = time.Now()
	prov.ExitCode = res.ExitCode
	prov.TimedOut = res.TimedOut
	return *prov
}

// classifyResult marks a run as vacuous (zero-output exit 0, rejected) or passed
// (exit 0, or exit 1 with output — printenv-class probes report via stdout presence).
func classifyResult(res *VerificationResult) {
	if res.ExitCode != 0 {
		if res.ExitCode == 1 && strings.TrimSpace(res.Stdout) != "" {
			res.Passed = true
		}
		return
	}
	if strings.TrimSpace(res.Stdout) == "" && strings.TrimSpace(res.Stderr) == "" {
		res.Vacuous = true
		return
	}
	res.Passed = true
}

func (r *VerificationRunner) execute(ctx context.Context, spec VerificationSpec) VerificationResult {
	res := VerificationResult{}
	if len(spec.Command) == 0 {
		res.Stderr = "verification: empty command"
		return res
	}

	runCtx, cancel := context.WithTimeout(ctx, spec.Timeout)
	defer cancel()

	// Setpgid puts the child in its own process group so a timeout kill takes
	// the whole tree; WaitDelay unblocks stdout/stderr pipes still held by
	// grandchildren after the child is killed.
	cmd := exec.CommandContext(runCtx, spec.Command[0], spec.Command[1:]...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.WaitDelay = 2 * time.Second
	cmd.Env = r.childEnv(spec.ExtraEnv)

	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	res.Stdout = stdout.String()
	res.Stderr = stderr.String()
	res.applyRunError(err, runCtx)
	return res
}

// applyRunError translates a cmd.Run error into result fields: timeout detection
// (deadline exceeded or kill signal), exit code extraction, and -1 for errors
// that carry neither (e.g. start failures).
func (res *VerificationResult) applyRunError(err error, runCtx context.Context) {
	if err == nil {
		return
	}
	if runCtx.Err() == context.DeadlineExceeded || isTimeoutKill(err) {
		res.TimedOut = true
	}
	if exitErr, ok := err.(*exec.ExitError); ok {
		res.ExitCode = exitErr.ExitCode()
	} else if res.TimedOut {
		res.ExitCode = 124
	}
	if res.ExitCode == 0 && !res.TimedOut {
		res.ExitCode = -1
	}
}

// childEnv builds the child environment: inherited variables with known
// credential variables stripped, plus ExtraEnv entries (also scrubbed).
func (r *VerificationRunner) childEnv(extra []string) []string {
	env := make([]string, 0, len(os.Environ())+len(extra))
	env = appendUncredentialed(env, os.Environ(), r)
	env = appendUncredentialed(env, extra, r)
	return env
}

// appendUncredentialed appends only KEY=VALUE entries whose key is not a known
// credential variable.
func appendUncredentialed(dst, entries []string, r *VerificationRunner) []string {
	for _, kv := range entries {
		if !r.isCredentialEntry(kv) {
			dst = append(dst, kv)
		}
	}
	return dst
}

func (r *VerificationRunner) isCredentialEntry(kv string) bool {
	eq := strings.Index(kv, "=")
	if eq <= 0 {
		return false
	}
	return r.credentials[kv[:eq]]
}

// RunWithEvents runs the verification and emits a typed verification_passed or
// verification_failed event with provenance fields on the given sink.
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

	eventType, detail := verificationEvent(res, spec.Timeout)
	sink.Emit(Event{Type: eventType, Detail: detail, Fields: fields})
	return res
}

// verificationEvent maps a result to its typed event and human detail string.
// Vacuous runs exit 0 but pass nothing, so they still report as failed.
func verificationEvent(res VerificationResult, timeout time.Duration) (string, string) {
	if res.Passed {
		return "verification_passed", fmt.Sprintf("verification exited 0 (argv=%v)", res.Provenance.Argv)
	}
	switch {
	case res.TimedOut:
		return "verification_failed", fmt.Sprintf("verification timed out after %s (argv=%v)", timeout, res.Provenance.Argv)
	case res.Vacuous:
		return "verification_failed", "verification exited 0 with zero output — rejected as vacuous"
	default:
		return "verification_failed", fmt.Sprintf("verification exited %d (argv=%v): %s", res.ExitCode, res.Provenance.Argv, excerpt(res.Stderr, 300))
	}
}

func excerpt(s string, max int) string {
	s = strings.TrimSpace(s)
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}

// isTimeoutKill reports whether err indicates the child was killed by a signal
// (the timeout kill path rather than a normal exit).
func isTimeoutKill(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "killed")
}
