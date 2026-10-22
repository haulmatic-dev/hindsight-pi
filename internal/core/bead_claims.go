package core

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// Claim line convention (same as PRD machine-checkable state claims):
//
//	hard: claim: <assertion> (verify: <shell cmd>)
//	claim: <assertion> (verify: <shell cmd>)
//
// `hard:` prefix = fail-closed product gate: a failing probe refuses spawn.
// Without it, a failing probe is a fail-open warning only.
// Probe TOOLING failures (timeout, shell never started) are classified
// unknown and never block a spawn: product gate fail-closed, tooling
// fail-open doctrine.
type BeadClaim struct {
	Hard      bool
	Assertion string
	VerifyCmd string
}

// Claim probe verdicts (deterministic drift-check outcome per claim).
const (
	claimStatusPass    = "pass"    // verify exited zero: claim holds
	claimStatusFail    = "fail"    // verify exited non-zero: claim evidence
	claimStatusUnknown = "unknown" // tooling failure: timeout / no runner / unparseable
)

// claimVerifyTimeout bounds each claim probe at spawn preflight.
const claimVerifyTimeout = 2 * time.Second

func osLookupEnv(key string) (string, bool) {
	return os.LookupEnv(key)
}

// claimEventRecorder is a test seam mirroring the checker's typed events.
// Production emission goes through the EventSink; the recorder is nil in
// prod and only captures in tests.
var claimEventRecorder func(evType string)

// claimLineRe captures `[hard: ]claim: <assertion> [(verify: <cmd>)]`.
var claimLineRe = regexp.MustCompile(`^(hard:\s+)?claim:\s*(.+?)\s*(?:\(verify:\s*(.*?)\)\s*)?$`)

// matchClaimLine parses one payload line into a BeadClaim. It returns
// ok=false for non-claim lines and for claim lines with an empty assertion.
// Verify is optional: an empty VerifyCmd is a valid claim that cannot be
// machine-checked (classified unknown at probe time).
func matchClaimLine(line string) (BeadClaim, bool) {
	if !strings.Contains(line, "claim:") {
		return BeadClaim{}, false
	}
	m := claimLineRe.FindStringSubmatch(line)
	if m == nil {
		return BeadClaim{}, false
	}
	assertion := strings.TrimSpace(m[2])
	if assertion == "" {
		return BeadClaim{}, false
	}
	return BeadClaim{
		Hard:      strings.TrimSpace(m[1]) != "",
		Assertion: assertion,
		VerifyCmd: strings.TrimSpace(m[3]),
	}, true
}

// parseBeadClaims extracts claim lines from a bead payload, in payload order.
// Non-claim lines are ignored.
func parseBeadClaims(payload string) []BeadClaim {
	var claims []BeadClaim
	for _, line := range strings.Split(payload, "\n") {
		claim, ok := matchClaimLine(strings.TrimSpace(line))
		if !ok {
			continue
		}
		claims = append(claims, claim)
	}
	return claims
}

// beadClaimRunner executes a claim verify command. Injectable for tests.
type beadClaimRunner func(cmd string, timeout time.Duration) (output string, err error)

// defaultClaimRun shells the verify command via sh -c under the deadline.
// exec.CommandContext kills the child at expiry; WaitDelay forces Wait to
// return even when pipe-holding grandchildren (sh -c "sleep 30") would
// otherwise block until they exit.
func defaultClaimRun(cmdText string, timeout time.Duration) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "sh", "-c", cmdText)
	cmd.WaitDelay = timeout
	var out, errBuf bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	runErr := cmd.Run()
	if runErr != nil && ctx.Err() != nil {
		// The child was terminated by the context deadline: report a
		// timeout error, not the raw kill signal (tooling failure).
		runErr = fmt.Errorf("claim verify did not finish within %s: %w", timeout, context.DeadlineExceeded)
	}
	combined := strings.TrimSpace(out.String())
	if tail := strings.TrimSpace(errBuf.String()); tail != "" {
		if combined != "" {
			combined += "\n"
		}
		combined += tail
	}
	return combined, runErr
}

// beadClaimChecker runs claim probes at spawn preflight and emits typed
// events. Zero claims is a no-op (backward compatible): no runner calls,
// no events, spawn proceeds.
type beadClaimChecker struct {
	events *EventSink
	run    beadClaimRunner
}

func newBeadClaimChecker(events *EventSink, run beadClaimRunner) *beadClaimChecker {
	if run == nil {
		run = defaultClaimRun
	}
	return &beadClaimChecker{events: events, run: run}
}

// emit records one typed drift-check event through the EventSink (production)
// and the package-level recorder (test seam). Nil sink stays silent: the
// event stream observes the system and must never break it.
func (c *beadClaimChecker) emit(beadID, evType, detail string) {
	c.events.Emit(Event{Type: evType, Bead: beadID, Detail: detail})
	if claimEventRecorder != nil {
		claimEventRecorder(evType)
	}
}

// classifyClaimStatus maps a verify-command outcome to the drift-check
// verdict triple: nil error → pass; tooling failure (timeout, missing
// shell) → unknown (never claim evidence); non-zero exit → fail (the claim
// itself failed — the only status that can gate a hard: claim).
func classifyClaimStatus(output string, err error) string {
	switch {
	case err == nil:
		return claimStatusPass
	case isClaimToolingFailure(err):
		return claimStatusUnknown
	default:
		return claimStatusFail
	}
}

// checkBeadClaims evaluates every claim in the bead payload.
// Returns true when spawn may proceed (fail-open EXCEPT a hard: claim FAIL,
// which refuses the spawn — product gate fail-closed).
func (c *beadClaimChecker) checkBeadClaims(beadID string, payload string) bool {
	claims := parseBeadClaims(payload)
	if len(claims) == 0 {
		return true
	}
	allow := true
	for _, claim := range claims {
		if !c.evaluateClaim(beadID, claim) {
			allow = false
		}
	}
	return allow
}

// evaluateClaim verifies a single claim and emits its typed event.
// Returns false only when the claim is hard: and FAILED — the fail-closed
// product gate that refuses the spawn. All other outcomes (pass, unknown,
// soft fail) allow the spawn to proceed.
func (c *beadClaimChecker) evaluateClaim(beadID string, claim BeadClaim) bool {
	// A claim without a verify clause is not machine-checkable: it is
	// tooling failure → unknown → fail open with a typed event.
	if claim.VerifyCmd == "" {
		c.emit(beadID, "claim_unknown",
			fmt.Sprintf("Drift-check: claim %q has no verify clause — unknown", claim.Assertion))
		return true
	}
	out, err := c.run(claim.VerifyCmd, claimVerifyTimeout)
	switch classifyClaimStatus(out, err) {
	case claimStatusPass:
		// claim confirmed at spawn time; nothing to report.
		return true
	case claimStatusUnknown:
		c.emit(beadID, "claim_unknown",
			fmt.Sprintf("Drift-check: claim %q probe unknown (verify %q): %v",
				claim.Assertion, claim.VerifyCmd, err))
		return true
	case claimStatusFail:
		if claim.Hard {
			c.emit(beadID, "claim_hard_failed",
				fmt.Sprintf("Drift-check: hard claim %q failed verify %q — %s",
					claim.Assertion, claim.VerifyCmd, strings.TrimSpace(out)))
			return false // hard: fail-closed product gate
		}
		c.emit(beadID, "claim_soft_failed",
			fmt.Sprintf("Drift-check: claim %q failed verify %q — %s",
				claim.Assertion, claim.VerifyCmd, strings.TrimSpace(out)))
	}
	return true
}

// isClaimToolingFailure classifies probe failures that are the TOOL's fault
// (never claim evidence): context deadline (timeout kill, single well-known
// case) or the shell executable itself missing. Everything else — any
// non-zero exit from a probe that ran — is claim evidence.
func isClaimToolingFailure(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	if _, ok := err.(*exec.Error); ok {
		return true // fork/exec failure: shell or interpreter missing
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return false // the probe ran and reported a verdict
	}
	return false
}
