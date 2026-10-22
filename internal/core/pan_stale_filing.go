package core

import (
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/haulmatic-dev/hydra/internal/pan"
	"github.com/haulmatic-dev/hydra/pkg/types"
)

// Stale-filing sweep (FR: "Stale-filing sweep covers in-progress filings with
// cleared premises"). Pan-filed beads that claim a spawn-wedge/cohort premise
// are re-verified deterministically each sweep tick; filings whose premise is
// demonstrably cleared are auto-closed at most one per project per tick.

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

// staleFilingCohortRe extracts the blocked-cohort key a filing's reason text
// claims, e.g. "Publish blocked: 3 beads blocked on publish-wedge" names the
// "publish-wedge" cohort.
var staleFilingCohortRe = regexp.MustCompile(`(?i)blocked on\s+([A-Za-z0-9._-]+)`)

// staleFilingSweepImplemented is true: the real sweep body is live and the
// contract tests in pan_stale_filing_test.go run unskipped.
const staleFilingSweepImplemented = true

// staleFilingSweepInterval throttles production sweeps (the rate limit is one
// auto-close per project per sweep tick, so ticks must not fire per scheduler
// tick). Mirrors the reconcileHardParks 1-min throttle pattern.
const staleFilingSweepInterval = 30 * time.Minute

// Production wiring seam: package-level so the deterministic probe hooks can
// reach Overseer state without an overseer parameter (tests override the
// hooks wholesale via staleFilingEligibleHooks and never hit these). NewOverseer
// installs both; a nil entry makes the probe fail-closed.
var (
	// staleFilingListBeads lists a project's bead store (repoName "" = single).
	staleFilingListBeads func(projectPath, repoName string) ([]types.Bead, error)
	// staleFilingSpawnSuccess records/reads per-project successful spawn times.
	staleFilingSpawnMu sync.Mutex
	staleFilingSpawns  = map[string]time.Time{} // project name -> last successful spawn
)

// noteSpawnSuccessFiling records a successful worker spawn for the project.
func noteSpawnSuccessFiling(project string, now time.Time) {
	if project == "" {
		return
	}
	staleFilingSpawnMu.Lock()
	staleFilingSpawns[project] = now
	staleFilingSpawnMu.Unlock()
}

// lastSpawnSuccessFor returns the project's most recent successful spawn.
func lastSpawnSuccessFor(project string) (time.Time, bool) {
	staleFilingSpawnMu.Lock()
	defer staleFilingSpawnMu.Unlock()
	t, ok := staleFilingSpawns[project]
	return t, ok
}

// Deterministic probes. Package-level so tests can inject fixtures; each has
// a real default body (no inert stubs).
var (
	// staleFilingIsOld reports whether the filing is older than
	// staleFilingMinAge.
	staleFilingIsOld = func(b types.Bead, now time.Time) bool {
		return now.Sub(b.CreatedAt) > staleFilingMinAge
	}
	// staleFilingCohortNamed extracts the blocked-cohort name the filing's
	// reason text claims. Empty string means no cohort named.
	staleFilingCohortNamed = func(b types.Bead) string {
		if m := staleFilingCohortRe.FindStringSubmatch(b.Description); m != nil {
			return m[1]
		}
		return ""
	}
	// staleFilingCohortBlocked reports whether the named cohort still has
	// members blocked with that reason today. A store read failure counts as
	// still blocked (fail-closed: never close a filing on uncertain data).
	staleFilingCohortBlocked = func(projectPath, cohort string) bool {
		if staleFilingListBeads == nil {
			return true
		}
		beads, err := staleFilingListBeads(projectPath, "")
		if err != nil {
			return true
		}
		return hasCohortMember(beads, cohort)
	}
	// staleFilingLastSpawnOK reports whether the project recorded a
	// successful spawn within staleFilingSpawnWindow.
	staleFilingLastSpawnOK = func(projectPath string, now time.Time) bool {
		name := filepathBaseForSpawnProbe(projectPath)
		last, ok := lastSpawnSuccessFor(name)
		return ok && now.Sub(last) <= staleFilingSpawnWindow
	}
	// staleFilingAlreadyClosed reports whether the filing was already closed
	// in this daemon run (prevents duplicate closes when the store read
	// lags the close event).
	staleFilingAlreadyClosed = func(beadID string, now time.Time) bool {
		staleFilingClosedMu.Lock()
		defer staleFilingClosedMu.Unlock()
		_, found := staleFilingClosed[beadID]
		return found
	}
	// staleFilingMarkClosed marks the filing as closed in the in-process
	// ledger so later sweeps skip it until the store read confirms.
	staleFilingMarkClosed = func(beadID string, now time.Time) {
		staleFilingClosedMu.Lock()
		defer staleFilingClosedMu.Unlock()
		staleFilingClosed[beadID] = struct{}{}
	}
	// staleFilingClosedMu guards the in-process closed-ledger map. Kept in
	// this block so the ledger state is owned by the same seam that reads it.
	staleFilingClosedMu sync.Mutex
	staleFilingClosed   = make(map[string]struct{}) // bead ID -> closed marker
)

// hasCohortMember reports whether any non-terminal, non-pan-filed bead in the
// store names the cohort as its blocker ("... blocked on <cohort>").
func hasCohortMember(beads []types.Bead, cohort string) bool {
	for _, b := range beads {
		if isPanFiledBead(b) {
			continue
		}
		if b.Status != types.StatusOpen && b.Status != types.StatusInProgress {
			continue
		}
		if named := staleFilingCohortNamed(b); named != "" && strings.EqualFold(named, cohort) {
			return true
		}
	}
	return false
}

// filepathBaseForSpawnProbe resolves the project key for spawn-success lookup.
// The tracker is keyed by project NAME (bead.Project at spawn time); the probe
// receives the project PATH, so it resolves the name via the wiring callback —
// falling back to the path's basename.
var staleFilingProjectName func(projectPath string) string

// filepathBaseForSpawnProbe resolves the tracker key: project NAME via wiring,
// falling back to the path's basename (both are stable from the spawn site).
func filepathBaseForSpawnProbe(projectPath string) string {
	if staleFilingProjectName != nil {
		if name := staleFilingProjectName(projectPath); name != "" {
			return name
		}
	}
	return pathBase(projectPath)
}

// pathBase returns the final path element (filepath.Base without the import).
func pathBase(p string) string {
	if i := strings.LastIndexByte(p, '/'); i >= 0 {
		return p[i+1:]
	}
	return p
}

// sweepStalePanFilings re-verifies pan-filed beads (open or in_progress)
// whose premise was a spawn-wedge/cohort claim and closes those whose
// premise is demonstrably cleared. Rate limit: at most one auto-close per
// project per sweep tick.
func (o *Overseer) sweepStalePanFilings(now time.Time) {
	for _, project := range o.projects {
		o.sweepStalePanFilingsForProject(project, now)
	}
}

// staleFilingCandidate is one pan-filed filing whose named-cohort premise is
// demonstrably cleared and is therefore eligible for auto-close this tick.
type staleFilingCandidate struct {
	bead   types.Bead
	cohort string
}

// staleFilingCandidates filters stale pan-filed filings whose named-cohort
// premise is demonstrably cleared. Probe order: spawn health (once per
// project, before the loop) → pan-filed label → open/in_progress → age →
// closed-ledger → human-triage-only reason → named cohort → cohort-membership
// probe (cached per cohort, so a store read happens at most once per distinct
// cohort per sweep).
func staleFilingCandidates(project types.Project, beads []types.Bead, now time.Time) []staleFilingCandidate {
	if !staleFilingLastSpawnOK(project.Path, now) {
		return nil
	}
	blockedCache := map[string]bool{}
	cohortBlocked := func(cohort string) bool {
		if b, ok := blockedCache[cohort]; ok {
			return b
		}
		b := staleFilingCohortBlocked(project.Path, cohort)
		blockedCache[cohort] = b
		return b
	}
	var candidates []staleFilingCandidate
	for _, b := range beads {
		if !isPanFiledBead(b) ||
			(b.Status != types.StatusOpen && b.Status != types.StatusInProgress) ||
			!staleFilingIsOld(b, now) ||
			staleFilingAlreadyClosed(string(b.ID), now) ||
			branchProtectionReasonRe.MatchString(b.Description) {
			continue
		}
		cohort := staleFilingCohortNamed(b)
		if cohort == "" || cohortBlocked(cohort) {
			continue
		}
		candidates = append(candidates, staleFilingCandidate{bead: b, cohort: cohort})
	}
	return candidates
}

// sweepStalePanFilingsForProject inspects one project's pan-filed beads.
func (o *Overseer) sweepStalePanFilingsForProject(project types.Project, now time.Time) {
	if o.beadsClient == nil {
		return
	}
	beads, err := o.beadsClient.ListBeads(project.Path, "")
	if err != nil {
		// Fail-open for the sweep itself; a listing problem must never break
		// the tick — but never close anything on uncertain data, which the
		// cohort/spawn probes fail-closed already guarantees.
		return
	}
	candidates := staleFilingCandidates(project, beads, now)
	if len(candidates) == 0 {
		return
	}

	// Deterministic order: oldest first, then bead ID.
	sort.Slice(candidates, func(i, j int) bool {
		if !candidates[i].bead.CreatedAt.Equal(candidates[j].bead.CreatedAt) {
			return candidates[i].bead.CreatedAt.Before(candidates[j].bead.CreatedAt)
		}
		return candidates[i].bead.ID < candidates[j].bead.ID
	})

	// Rate limit: at most one auto-close per project per sweep tick.
	// Rate limit: at most one auto-close per project per sweep tick.
	c := candidates[0]
	spawnOK := staleFilingLastSpawnOK(project.Path, now)
	if err := o.beadsClient.CloseBead(project.Path, "", c.bead.ID, staleFilingReasonCleared); err != nil {
		return
	}
	staleFilingMarkClosed(string(c.bead.ID), now)
	o.eventSink.Emit(Event{
		Type:    "pan_filing_stale_closed",
		Project: project.Name,
		Bead:    string(c.bead.ID),
		Reason:  "premise_cleared",
		Detail:  staleFilingReasonCleared,
		Fields: map[string]any{
			"cohort_named":  c.cohort,
			"cohort_now":    "cleared",
			"last_spawn_ok": spawnOK,
		},
	})
}

// wireStaleFilingProbes installs the production probes on package-level seams.
// Safe to call repeatedly (single daemon per process); tests never call this.
func wireStaleFilingProbes(o *Overseer) {
	if o.beadsClient != nil {
		client := o.beadsClient
		staleFilingListBeads = client.ListBeads
	}
	staleFilingProjectName = func(projectPath string) string {
		for _, p := range o.projects {
			if p.Path == projectPath {
				return p.Name
			}
		}
		return ""
	}
}

// isPanFiledBead reports whether the bead carries the pan-filed label.
func isPanFiledBead(b types.Bead) bool {
	for _, l := range b.Labels {
		if l == pan.FiledLabel {
			return true
		}
	}
	return false
}

// staleFilingHookSnapshot pins the hook surface (compile check).
var _ = struct{}{}
