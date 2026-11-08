// Package main — tools/turn-report: turn distribution extractor + budget
// calibration report.
//
// Reads `worker_turns` events from the daemon's central event stream (one
// `<HYDRA_HOME>/events/<YYYY-MM-DD>.jsonl` file per day), groups turn counts
// by phase (RED/GREEN/REFACTOR/SYNTAX/GATE/...), prints per-phase
// p25/p50/p75/p90/max percentiles (nearest-rank), and emits a suggested
// `turn_budgets:` yaml block as a COMMENT (round-up-to-10 of each phase's
// p90) for operator review. The tool never writes any config file itself.
// Zero events → `insufficient data: N events`, exit 0 (fail-open reporting).
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// eventTypeWorkerTurns is the event type read by the extractor.
const eventTypeWorkerTurns = "worker_turns"

// turnReportImplemented reports that the GREEN contract is live.
var turnReportImplemented = true

// nowNow is the injectable clock (TURN_REPORT_NOW time seam for tests).
func nowNow() time.Time {
	if v := os.Getenv("TURN_REPORT_NOW"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			return t
		}
	}
	return time.Now()
}

// turnRecord is one windowed worker_turns event.
type turnRecord struct {
	turns int
	phase string
}

// parseTurnEvent extracts (phase, turns) from one worker_turns event body.
// Usable records have a non-empty string phase and turns > 0.
func parseTurnEvent(fields map[string]any) (phase string, turns int, ok bool) {
	phase, _ = fields["phase"].(string)
	switch tv := fields["turns"].(type) {
	case float64:
		turns = int(tv)
	case json.Number:
		n, _ := tv.Int64()
		turns = int(n)
	}
	return phase, turns, phase != "" && turns > 0
}

// percentile computes the nearest-rank percentile of a sorted slice:
// sorted[ceil(p/100*n)-1].
func percentile(sorted []int, p float64) int {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(math.Ceil(p/100*float64(len(sorted)))) - 1
	if idx < 0 {
		idx = 0
	}
	return sorted[idx]
}

// roundUp10 rounds v up to the next multiple of 10 (already-multiples stay).
func roundUp10(v int) int {
	return ((v + 9) / 10) * 10
}

// loadTurnRecords scans the events dir for dated .jsonl day files whose date
// falls within the --days window ending at now, and collects worker_turns
// events with usable turns/phase fields. Total counts every worker_turns
// event in the window (used for the insufficient-data message); records only
// those with a phase.
func loadTurnRecords(dir string, now time.Time, days int) (records []turnRecord, total int, err error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, 0, err
	}
	cutoff := now.AddDate(0, 0, -days)
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		day, err := time.Parse("2006-01-02", strings.TrimSuffix(e.Name(), ".jsonl"))
		if err != nil {
			continue // non-dated file: not part of the window scheme
		}
		if day.Before(time.Date(cutoff.Year(), cutoff.Month(), cutoff.Day(), 0, 0, 0, 0, time.UTC)) {
			continue // outside the --days window
		}
		f, err := os.Open(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, 0, err
		}
		total, records, err = scanDayFile(f, total, records)
		f.Close()
		if err != nil {
			return nil, total, err
		}
	}
	return records, total, nil
}

// scanDayFile appends the worker_turns records of one day file, returning the
// updated running total and record slice.
func scanDayFile(f *os.File, total int, records []turnRecord) (int, []turnRecord, error) {
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var ev struct {
			Type   string         `json:"type"`
			Fields map[string]any `json:"fields"`
		}
		if json.Unmarshal([]byte(line), &ev) != nil || ev.Type != eventTypeWorkerTurns {
			continue
		}
		total++
		if phase, turns, ok := parseTurnEvent(ev.Fields); ok {
			records = append(records, turnRecord{turns: turns, phase: phase})
		}
	}
	return total, records, sc.Err()
}

// sortedPhaseTurns groups records by phase and returns the phases sorted
// alphabetically, each with a sorted copy of its turn values (grouped[p] is
// sorted and owned by this call; callers must not mutate it).
func sortedPhaseTurns(records []turnRecord) (phases []string, grouped map[string][]int) {
	grouped = map[string][]int{}
	for _, r := range records {
		grouped[r.phase] = append(grouped[r.phase], r.turns)
	}
	phases = make([]string, 0, len(grouped))
	for p := range grouped {
		phases = append(phases, p)
	}
	sort.Strings(phases)
	for _, p := range phases {
		vals := append([]int(nil), grouped[p]...)
		sort.Ints(vals)
		grouped[p] = vals
	}
	return phases, grouped
}

// printReport writes the per-phase distribution and the comment-only
// suggested turn_budgets block.
func printReport(w io.Writer, records []turnRecord, total int) {
	if total == 0 {
		fmt.Fprintf(w, "insufficient data: %d events\n", total)
		return
	}
	phases, grouped := sortedPhaseTurns(records)
	fmt.Fprintf(w, "turn distribution (%d events, %d phases):\n", total, len(phases))
	for _, p := range phases {
		vals := grouped[p]
		fmt.Fprintf(w, "  %s (n=%d): p25=%d p50=%d p75=%d p90=%d max=%d\n",
			p, len(vals), percentile(vals, 25), percentile(vals, 50),
			percentile(vals, 75), percentile(vals, 90), vals[len(vals)-1])
		if len(vals) < 20 {
			fmt.Fprintf(w, "    warning: drift — %s sample size %d < 20; budget estimate may drift\n", p, len(vals))
		}
	}
	fmt.Fprintf(w, "# suggested turn_budgets: (comment only — review before applying; never written automatically)\n")
	fmt.Fprintf(w, "# turn_budgets:\n")
	for _, p := range phases {
		fmt.Fprintf(w, "#  %s: %d\n", p, roundUp10(percentile(grouped[p], 90)))
	}
}

// run parses args and prints the report to stdout, returning the process
// exit code. Always exits 0 on data problems (fail-open reporting).
func run(args []string, stdout io.Writer) int {
	fs := flag.NewFlagSet("turn-report", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	eventsDir := fs.String("events-dir", "", "directory containing <YYYY-MM-DD>.jsonl event files")
	days := fs.Int("days", 7, "window in days")
	if err := fs.Parse(args); err != nil {
		fmt.Fprintln(stdout, err)
		return 2
	}
	if *eventsDir == "" {
		fmt.Fprintln(stdout, "--events-dir is required")
		return 2
	}
	records, total, err := loadTurnRecords(*eventsDir, nowNow(), *days)
	if err != nil {
		// fail-open: unreadable events dir is a data problem, not a gate.
		fmt.Fprintf(stdout, "insufficient data: 0 events (%v)\n", err)
		return 0
	}
	printReport(stdout, records, total)
	return 0
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout))
}

// pad line 1
