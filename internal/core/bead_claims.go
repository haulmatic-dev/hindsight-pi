package core

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/haulmatic-dev/hydra/pkg/types"
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

// claimCacheImplemented gates the FR-002 claim re-proof cache RED contract
// tests (BEAD_CLAIMS_CACHE_ENFORCE=1 fails them loudly).
const claimCacheImplemented = true

// claimCheckRunner overrides the probe runner used inside
// spawnPreflightBeadClaims (test seam; nil = defaultClaimRun).
var claimCheckRunner beadClaimRunner

// claimHeadProbe resolves the worktree HEAD (base_sha) for cache-fingerprint
// comparison. Production: `git rev-parse HEAD` in the project worktree; a
// missing/failed probe returns "" (unreadable base → every spawn re-proofs,
// fail-open toward re-verification).
var claimHeadProbe = func(projectPath string) (string, error) {
	if projectPath == "" {
		return "", nil
	}
	out, err := exec.Command("git", "-C", projectPath, "rev-parse", "HEAD").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// Durable cache labels written on the bead payload after a fully-passing
// re-proof: `claims-ok-<fp8>` (fingerprint of bead id + payload + base_sha)
// and `claims-ok-at-<unix-milli>`. A respawn whose recomputed fingerprint
// matches the cached label reuses the verdict without re-running probes.
const (
	claimsOkFpPrefix = "claims-ok-"
	claimsOkAtPrefix = "claims-ok-at-"
)

// claimCacheFingerprint is the stable 8-hex cache key: bead id + full payload
// + worktree base_sha. Either the payload or the environment (base_sha)
// changing invalidates every cached pass for the bead.
func claimCacheFingerprint(beadID, payload, head string) string {
	sum := sha256.Sum256([]byte(beadID + "\x00" + payload + "\x00" + head))
	return hex.EncodeToString(sum[:])[:8]
}

func hasLabel(labels []string, want string) bool {
	for _, l := range labels {
		if l == want {
			return true
		}
	}
	return false
}

func hasLabelPrefix(labels []string, prefix string) bool {
	for _, l := range labels {
		if strings.HasPrefix(l, prefix) {
			return true
		}
	}
	return false
}

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
	allow, _ := c.evaluateClaims(beadID, parseBeadClaims(payload), false)
	return allow
}

// evaluateClaims verifies each claim (probing only when a prior cached pass
// is absent, or reproof=true on the respawn cache-miss path) and emits one
// typed event per claim. It reports:
//   - allow: false only when a hard: claim FAILED — the fail-closed product
//     gate that refuses the spawn; all other outcomes (pass, unknown, soft
//     fail) allow the spawn to proceed.
//   - allPass: every claim passed — the precondition for persisting the
//     cache labels (failures and unknowns are never cached).
func (c *beadClaimChecker) evaluateClaims(beadID string, claims []BeadClaim, reproof bool) (allow, allPass bool) {
	allow = true
	allPass = true
	for _, claim := range claims {
		status := c.verifyClaim(beadID, claim, reproof)
		if status != claimStatusPass {
			allPass = false
		}
		if status == claimStatusFail && claim.Hard {
			allow = false
		}
	}
	return allow, allPass
}

// verifyClaim runs one probe and emits its typed event. On reproof (the
// cache-miss path at respawn), a broken soft claim is reported as
// `claim_dropped` (mirror of pack_miss) instead of `claim_soft_failed`.
// Returns the drift-check status; only status=fail on a hard: claim gates.
func (c *beadClaimChecker) verifyClaim(beadID string, claim BeadClaim, reproof bool) string {
	// A claim without a verify clause is not machine-checkable: it is
	// tooling failure → unknown → fail open with a typed event.
	if claim.VerifyCmd == "" {
		c.emit(beadID, "claim_unknown",
			fmt.Sprintf("Drift-check: claim %q has no verify clause — unknown", claim.Assertion))
		return claimStatusUnknown
	}
	out, err := c.run(claim.VerifyCmd, claimVerifyTimeout)
	switch classifyClaimStatus(out, err) {
	case claimStatusPass:
		// claim confirmed at spawn time; nothing to report.
		return claimStatusPass
	case claimStatusUnknown:
		c.emit(beadID, "claim_unknown",
			fmt.Sprintf("Drift-check: claim %q probe unknown (verify %q): %v",
				claim.Assertion, claim.VerifyCmd, err))
		return claimStatusUnknown
	case claimStatusFail:
		if claim.Hard {
			c.emit(beadID, "claim_hard_failed",
				fmt.Sprintf("Drift-check: hard claim %q failed verify %q — %s",
					claim.Assertion, claim.VerifyCmd, strings.TrimSpace(out)))
			return claimStatusFail
		}
		if reproof {
			c.emit(beadID, "claim_dropped",
				fmt.Sprintf("claim dropped: %q no longer holds (verify %q) — %s",
					claim.Assertion, claim.VerifyCmd, strings.TrimSpace(out)))
			return claimStatusFail
		}
		c.emit(beadID, "claim_soft_failed",
			fmt.Sprintf("Drift-check: claim %q failed verify %q — %s",
				claim.Assertion, claim.VerifyCmd, strings.TrimSpace(out)))
	}
	return claimStatusFail
}

// checkBeadClaimsWithCache is the respawn-time entrypoint with durable
// payload persistence: a cached fingerprint label matching the current
// bead+base_sha reuses the last full-pass verdict without re-running probes
// (<5ms); a miss triggers a full re-proof, and a fully-passing re-proof
// (re)writes the cache labels via updateLabels.
func (c *beadClaimChecker) checkBeadClaimsWithCache(bead *types.Bead, projectPath string, update func([]string) error) bool {
	claims := parseBeadClaims(bead.Description)
	if len(claims) == 0 {
		return true
	}
	head, _ := claimHeadProbe(projectPath)
	fp := claimCacheFingerprint(bead.ID.String(), bead.Description, head)
	if hasLabel(bead.Labels, claimsOkFpPrefix+fp) && hasLabelPrefix(bead.Labels, claimsOkAtPrefix) {
		return true // cached pass, unchanged payload and base_sha: reuse.
	}
	allow, allPass := c.evaluateClaims(bead.ID.String(), claims, true)
	if allow && allPass && update != nil {
		// Persist the verdict for the next respawn (timestamped to the
		// millisecond so re-proofs always refresh claims-ok-at).
		_ = update([]string{
			claimsOkFpPrefix + fp,
			fmt.Sprintf("%s%d", claimsOkAtPrefix, time.Now().UnixMilli()),
		})
	}
	return allow
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
