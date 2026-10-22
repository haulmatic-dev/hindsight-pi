package core

import (
	"regexp"
	"time"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// RED-phase inert stub for the stale-filing sweep (FR: "Stale-filing sweep
// covers in-progress filings with cleared premises"). GREEN implements the
// real behavior pinned by pan_stale_filing_test.go; this stub only carries
// the contract surface so the package compiles with zero behavior.

// staleFilingReasonCleared is the exact close reason a premise-cleared filing
// receives (mirrors the f1y5 closure style).
const staleFilingReasonCleared = "stale pan filing: premise cleared (cohort empty, spawns healthy)"

// staleFilingMinAge is probe (a): the filing must be older than this to be
// considered stale.
const staleFilingMinAge = 48 * time.Hour

// staleFilingSpawnWindow is probe (c): the project must have recorded at
// least one successful spawn within this window.
const staleFilingSpawnWindow = 24 * time.Hour

// branchProtectionReasonRe matches human-triage-only filing reasons. A filing
// whose reason text matches this pattern is NEVER auto-closed by the
// stale-filing sweep (existing pan.go campaign-anomaly contract).
var branchProtectionReasonRe = regexp.MustCompile(`(branch protection|required approval|permissions|GH013|rules URL)`)

// staleFilingSweepImplemented flips to true when GREEN implements the real
// sweep body; the RED test suite skips loudly (or, under
// STALE_FILING_ENFORCE=1, fails) until then.
const staleFilingSweepImplemented = false

// RED stubs — GREEN replaces these with real deterministic probes.
var (
	// staleFilingIsOld reports whether the filing is older than
	// staleFilingMinAge. Inert default: never stale.
	staleFilingIsOld = func(b types.Bead, now time.Time) bool { return false }
	// staleFilingCohortNamed extracts the blocked-cohort name the filing's
	// reason text claims. Inert default: no cohort named.
	staleFilingCohortNamed = func(b types.Bead) string { return "" }
	// staleFilingCohortBlocked reports whether the named cohort still has
	// members blocked with that reason today. Inert default: still blocked.
	staleFilingCohortBlocked = func(projectPath, cohort string) bool { return true }
	// staleFilingLastSpawnOK reports whether the project recorded a
	// successful spawn within staleFilingSpawnWindow. Inert default: no.
	staleFilingLastSpawnOK = func(projectPath string, now time.Time) bool { return false }
)

// sweepStalePanFilings re-verifies pan-filed beads (open or in_progress)
// whose premise was a spawn-wedge/cohort claim and closes those whose
// premise is demonstrably cleared. RED stub: does nothing.
func (o *Overseer) sweepStalePanFilings(now time.Time) {}

// staleFilingHookSnapshot pins the hook surface for GREEN (compile check).
var _ = struct{}{}
