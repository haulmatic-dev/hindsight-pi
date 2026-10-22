package core

import (
	"os"
	"time"
)

// Claim line convention (same as PRD machine-checkable state claims):
//
//	hard: claim: <assertion> (verify: <shell cmd>)
//	claim: <assertion> (verify: <shell cmd>)
//
// `hard:` prefix = fail-closed product gate: a failing probe refuses spawn.
// Without it, a failing probe is a fail-open warning only.
type BeadClaim struct {
	Hard      bool
	Assertion string
	VerifyCmd string
}

// claimVerifyTimeout bounds each claim probe at spawn preflight.
const claimVerifyTimeout = 2 * time.Second

func osLookupEnv(key string) (string, bool) {
	return os.LookupEnv(key)
}

// parseBeadClaims extracts claim lines from a bead payload.
// Inert stub for RED phase: no behavior yet.
func parseBeadClaims(payload string) []BeadClaim {
	return nil
}

// beadClaimRunner executes a claim verify command. Injectable for tests.
type beadClaimRunner func(cmd string, timeout time.Duration) (output string, err error)

// beadClaimChecker runs claim probes at spawn preflight and emits typed
// events. Inert stub for RED phase: always allows spawn, no events.
type beadClaimChecker struct {
	events *EventSink
	run    beadClaimRunner
}

func newBeadClaimChecker(events *EventSink, run beadClaimRunner) *beadClaimChecker {
	return &beadClaimChecker{events: events, run: run}
}

// checkBeadClaims evaluates every claim in the bead payload.
// Returns true when spawn may proceed (fail-open except hard:).
func (c *beadClaimChecker) checkBeadClaims(beadID string, payload string) bool {
	return true
}
