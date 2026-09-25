package pan

import (
	"fmt"
	"strings"
	"time"
)

// Tier-1 trajectory intelligence: cheap mechanical pre-filters over a live
// worker's pi session telemetry. Pan's LLM judgment (tier 2) is only woken
// for workers a tier-1 signal flags as outliers — healthy trajectories cost
// zero tokens (the tin0 lesson: 73+ minutes redoing committed work while
// every snapshot signal — output flowing, tokens accruing, CPU active —
// looked healthy; nothing judged the TRAJECTORY).

// Trajectory is mechanical telemetry gathered from a live worker's pi
// session by the daemon-side reader (internal/core collectWorkerTrajectory).
// It carries no timestamps: volatile fields would churn candidate reasons
// and defeat judgment dedup (PR #236 lesson).
type Trajectory struct {
	Turns    int // pi turn_end events in the session
	TestRuns int // bash invocations that look like a test run
	// EditsSinceTest counts edit/write tool calls after the most recent test
	// run (or all edits when no test ran yet).
	EditsSinceTest int
	DistinctEdits  int // distinct file paths edited
	// RepeatedCommand/RepeatedCount describe the most frequent normalized
	// bash command; empty when no bash ran.
	RepeatedCommand string
	RepeatedCount   int

	// NewFiles counts file-creating write calls over the whole trajectory.
	// A trajectory with many turns but zero new files is polishing or
	// wandering, not building (the tin0 signature: redoing committed work).
	NewFiles int
	// ReeditedFile/ReeditedCount name the single file edited most often since
	// the last test run: the same-file-reedited-without-test-run signal.
	ReeditedFile  string
	ReeditedCount int
}

// TurnBaselines are per-bead-class expected turn budgets before a trajectory
// is suspicious. Derived from measured p50s: syntax beads are tiny, RED/GREEN
// write tests/implementations and legitimately run long, fix beads are
// targeted. Unknown classes use DefaultTurnBaseline.
var TurnBaselines = map[string]int{
	"syntax":    5,
	"red":       20,
	"green":     30,
	"fix":       10,
	"refactor":  25,
	"discovery": 40,
}

// DefaultTurnBaseline is the outlier bar for bead classes without a measured
// baseline. Deliberately generous: a false tier-1 flag costs one tier-2
// judgment, but a missed loop costs minutes of burned tokens.
const DefaultTurnBaseline = 40

// turnOutlierMultiple: a trajectory is a turn-count outlier only past 2× its
// class baseline — one slow turn or a rich discovery phase must not flag.
const turnOutlierMultiple = 2

// repeatedCommandFloor: the same normalized command this many times means a
// loop, not diligence (retry loops in gate commands were observed at 219
// rejected turns; genuine retest cycles stay well below 8).
const repeatedCommandFloor = 8

// editsSinceTestFloor: this many file edits without an intervening test run
// suggests the worker is re-editing blind (the same-file-reedited-without-
// test-run signal).
const editsSinceTestFloor = 6

// testRunsFloor: this many test runs within one trajectory smells like a
// test-fix-test loop where each fix fails the same way.
const testRunsFloor = 8

// noNewFilesFloorTurns: past this many turns with zero file-creating writes,
// a working trajectory is not building anything new.
const noNewFilesFloorTurns = 15

// reeditedFloor: one file edited this many times since the last test run is
// blind thrashing, not iteration (each pass should be verified by a run).
const reeditedFloor = 3

// BaselineFor returns the turn baseline for a bead class (lowercase).
func BaselineFor(class string) int {
	if b, ok := TurnBaselines[strings.ToLower(class)]; ok {
		return b
	}
	return DefaultTurnBaseline
}

// Anomalies returns the tier-1 flags this trajectory fires for the given
// bead class. Empty means the trajectory is healthy: tier 2 must never be
// woken for it. All thresholds are conservative — a tier-1 flag is a
// candidate for judgment, not a verdict.
func (t Trajectory) Anomalies(class string) []string {
	var out []string
	if t.Turns > turnOutlierMultiple*BaselineFor(class) {
		out = append(out, fmt.Sprintf(
			"turn outlier: %d turns vs baseline≈%d for %q class — converging or looping?",
			t.Turns, BaselineFor(class), class))
	}
	if t.RepeatedCommand != "" && t.RepeatedCount >= repeatedCommandFloor {
		out = append(out, fmt.Sprintf(
			"same command repeated %d times (%q) — possible loop",
			t.RepeatedCount, truncate(t.RepeatedCommand, 80)))
	}
	if t.EditsSinceTest >= editsSinceTestFloor {
		out = append(out, fmt.Sprintf(
			"%d file edits since last test run — re-editing without verifying",
			t.EditsSinceTest))
	}
	if t.TestRuns >= testRunsFloor {
		out = append(out, fmt.Sprintf(
			"%d test runs in one trajectory — possible test-fix-test loop",
			t.TestRuns))
	}
	if t.Turns >= noNewFilesFloorTurns && t.NewFiles == 0 && t.DistinctEdits > 0 {
		out = append(out, fmt.Sprintf(
			"%d turns with edits but no new files created — polishing, wandering, or redoing committed work",
			t.Turns))
	}
	if t.ReeditedFile != "" && t.ReeditedCount >= reeditedFloor {
		out = append(out, fmt.Sprintf(
			"file %q edited %d times since last test run — same-file-reedited-without-verifying",
			truncate(t.ReeditedFile, 60), t.ReeditedCount))
	}
	return out
}

// trajectoryMinAge is the youngest worker age Candidates will trajectory-scan:
// a worker younger than this is mid-first-turn and cannot be an outlier yet.
// Mirrors the young-worker grace in the hung-worker rule (srv-smart-engine-pes,
// 2026-08-07: a 2-minute-old worker was abandoned over zero tokens).
const trajectoryMinAge = 10 * time.Minute
