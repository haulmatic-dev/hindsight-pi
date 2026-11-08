package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// enforceEnv is the RED gate (established codebase pattern: STALE_FILING_ENFORCE,
// MIRROR_HEALTH_ENFORCE, ...). Tests skip loudly by default so the daemon gate
// stays green pre-GREEN; setting the env fails them, proving RED.
const enforceEnv = "TURN_REPORT_ENFORCE"

func requireImpl(t *testing.T) {
	t.Helper()
	if !turnReportImplemented {
		if os.Getenv(enforceEnv) == "1" {
			t.Fatalf("RED: turn-report contract not yet implemented (%s=1)", enforceEnv)
		}
		t.Skipf("RED: turn-report contract not yet implemented (set %s=1 to fail)", enforceEnv)
	}
}

// writeTurnEvents writes worker_turns events (and one unrelated event) as a
// daily JSONL stream under dir, with `now` as the newest day.
type turnEvent struct {
	Type   string         `json:"type"`
	Bead   string         `json:"bead,omitempty"`
	Detail string         `json:"detail,omitempty"`
	Fields map[string]any `json:"fields,omitempty"`
}

func writeEventsDir(t *testing.T, day time.Time, evs []turnEvent) string {
	t.Helper()
	dir := t.TempDir()
	name := filepath.Join(dir, day.Format("2006-01-02")+".jsonl")
	var b bytes.Buffer
	for _, ev := range evs {
		line, err := json.Marshal(ev)
		if err != nil {
			t.Fatal(err)
		}
		b.Write(line)
		b.WriteByte('\n')
	}
	if err := os.WriteFile(name, b.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func turnEv(turns int, phase string) turnEvent {
	return turnEvent{
		Type: eventTypeWorkerTurns,
		Bead: "hydra-test",
		Fields: map[string]any{
			"turns": turns,
			"phase": phase,
		},
	}
}

// runReport invokes run with --events-dir and returns stdout text.
func runReport(t *testing.T, dir string, now time.Time, days int) (string, int) {
	t.Helper()
	var out bytes.Buffer
	// `now` is injected via the env override the tool must honor (contract).
	t.Setenv("TURN_REPORT_NOW", now.UTC().Format(time.RFC3339))
	code := run([]string{"--events-dir", dir, "--days", fmt.Sprint(days)}, &out)
	return out.String(), code
}

// TestTurnReport_PercentilesPerPhase pins the distribution extraction: from a
// fixture of worker_turns events, the report prints per-phase
// p25/p50/p75/p90/max percentiles (nearest-rank on sorted turn counts).
func TestTurnReport_PercentilesPerPhase(t *testing.T) {
	requireImpl(t)
	now := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	// RED phase
	// sample: 10 turns (RED) = exactly the p25..max grid.
	evs := []turnEvent{turnEv(10, "red")}
	dir := writeEventsDir(t, now, evs)
	out, code := runReport(t, dir, now, 7)
	if code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	re := regexp.MustCompile(`p25=(\d+) p50=(\d+) p75=(\d+) p90=(\d+) max=(\d+)`)
	m := re.FindStringSubmatch(out)
	if m == nil {
		t.Fatalf("no percentile line in output:\n%s", out)
	}
	var got [5]int
	for i := 1; i <= 5; i++ {
		if _, err := fmt.Sscanf(m[i], "%d", &got[i-1]); err != nil {
			t.Fatalf("bad percentile value %q: %v", m[i], err)
		}
	}
	want := [5]int{10, 10, 10, 10, 10}
	if got != want {
		t.Fatalf("percentiles = %v, want %v\n%s", got, want, out)
	}
	if !strings.Contains(out, "red") {
		t.Fatalf("phase name missing from output:\n%s", out)
	}
}

// TestTurnReport_PercentileNearestRank pins the percentile definition with an
// uneven sample: sorted [1,2,3,4,5,6,7,8,9,10] → p25=3, p50=5, p75=8, p90=9
// (nearest-rank: index ceil(p/100*n)-1), max=10.
func TestTurnReport_PercentileNearestRank(t *testing.T) {
	requireImpl(t)
	now := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	var evs []turnEvent
	for i := 1; i <= 10; i++ {
		evs = append(evs, turnEv(i, "green"))
	}
	dir := writeEventsDir(t, now, evs)
	out, code := runReport(t, dir, now, 7)
	if code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	re := regexp.MustCompile(`p25=(\d+) p50=(\d+) p75=(\d+) p90=(\d+) max=(\d+)`)
	m := re.FindStringSubmatch(out)
	if m == nil {
		t.Fatalf("no percentile line:\n%s", out)
	}
	want := "3 5 8 9 10"
	got := fmt.Sprintf("%s %s %s %s %s", m[1], m[2], m[3], m[4], m[5])
	if got != want {
		t.Fatalf("percentiles = %q, want %q\n%s", got, want, out)
	}
}

// TestTurnReport_BudgetsCommentBlock pins the calibration: a suggested
// `turn_budgets:` yaml block emitted as COMMENT lines (each prefixed with `#`),
// one entry per phase, value = round-up-to-10 of that phase's p90. The block
// is a suggestion, never applied to any config.
func TestTurnReport_BudgetsCommentBlock(t *testing.T) {
	requireImpl(t)
	now := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	// red p90 = 47 → budget 50; green p90 = 270 → 270→270? 270 rounds to 270.
	// Use values with clean rounding: red 47→50, green 301→310.
	evs := []turnEvent{
		turnEv(47, "red"),
		turnEv(301, "green"),
	}
	dir := writeEventsDir(t, now, evs)
	out, code := runReport(t, dir, now, 7)
	if code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	if !strings.Contains(out, "turn_budgets:") {
		t.Fatalf("suggested block missing 'turn_budgets:':\n%s", out)
	}
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "red:") && !strings.HasPrefix(strings.TrimSpace(line), "#") {
			t.Fatalf("budget line not commented: %q\n%s", line, out)
		}
	}
	if !strings.Contains(out, "#  red: 50") && !strings.Contains(out, "#  red: 50\n") {
		t.Fatalf("red budget (p90 47 → round-up-to-10 50) missing:\n%s", out)
	}
	if !strings.Contains(out, "green: 310") {
		t.Fatalf("green budget (p90 301 → round-up-to-10 310) missing:\n%s", out)
	}
}

// TestTurnReport_ZeroEventsInsufficientData pins the fail-open reporting path:
// an empty events dir prints `insufficient data: N events` and exits 0.
func TestTurnReport_ZeroEventsInsufficientData(t *testing.T) {
	requireImpl(t)
	now := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	dir := t.TempDir()
	out, code := runReport(t, dir, now, 7)
	if code != 0 {
		t.Fatalf("exit = %d, want 0 (fail-open)", code)
	}
	if !strings.Contains(out, "insufficient data: 0 events") {
		t.Fatalf("expected 'insufficient data: 0 events', got:\n%s", out)
	}
}

// TestTurnReport_DriftWarningUnderTwentySamples pins the drift guard: when a
// phase's sample size is below 20, the report includes a drift warning naming
// the phase and the sample count. With ≥ 20 samples there is no warning.
func TestTurnReport_DriftWarningUnderTwentySamples(t *testing.T) {
	requireImpl(t)
	now := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	var evs []turnEvent
	for i := 0; i < 19; i++ { // 19 < 20 → warn
		evs = append(evs, turnEv(100+i, "red"))
	}
	for i := 0; i < 20; i++ { // 20 → no warn
		evs = append(evs, turnEv(200+i, "green"))
	}
	dir := writeEventsDir(t, now, evs)
	out, code := runReport(t, dir, now, 7)
	if code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	if !strings.Contains(out, "drift") || !strings.Contains(out, "19") {
		t.Fatalf("drift warning for 19-sample phase missing:\n%s", out)
	}
	// The green phase block (20 samples) must not be flagged.
	greenIdx := strings.Index(out, "green")
	if greenIdx < 0 {
		t.Fatalf("green phase missing:\n%s", out)
	}
	greenSection := out[greenIdx:]
	if n := strings.Index(greenSection, "red"); n >= 0 && n < len(greenSection) {
		greenSection = greenSection[:n]
	}
	if strings.Contains(greenSection, "drift") {
		t.Fatalf("drift warning in 20-sample phase section:\n%s", out)
	}
}

// TestTurnReport_DaysWindowFiltersOldEvents pins --days: worker_turns events
// strictly older than the window are excluded from distributions.
func TestTurnReport_DaysWindowFiltersOldEvents(t *testing.T) {
	requireImpl(t)
	now := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	dir := writeEventsDir(t, now, []turnEvent{turnEv(100, "red")})
	oldName := filepath.Join(dir, now.AddDate(0, 0, -30).Format("2006-01-02")+".jsonl")
	oldLine, _ := json.Marshal(turnEv(9999, "green"))
	if err := os.WriteFile(oldName, append(oldLine, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	out, code := runReport(t, dir, now, 7)
	if code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	if strings.Contains(out, "9999") || strings.Contains(out, "green") {
		t.Fatalf("old event leaked through --days 7 window:\n%s", out)
	}
	if !strings.Contains(out, "red") {
		t.Fatalf("in-window event missing:\n%s", out)
	}
}

// TestTurnReport_UnrelatedEventsIgnored pins type filtering: only
// worker_turns events feed the distributions; other event types are ignored.
func TestTurnReport_UnrelatedEventsIgnored(t *testing.T) {
	requireImpl(t)
	now := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	evs := []turnEvent{
		{Type: "bead_closed", Bead: "hydra-x", Fields: map[string]any{"turns": 5000, "phase": "red"}},
		turnEv(9, "red"),
	}
	dir := writeEventsDir(t, now, evs)
	out, code := runReport(t, dir, now, 7)
	if code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	if strings.Contains(out, "5000") {
		t.Fatalf("non-worker_turns event counted:\n%s", out)
	}
	if !strings.Contains(out, "p90=9") {
		t.Fatalf("worker_turns event missing from red distribution:\n%s", out)
	}
}

// TestTurnReport_InsufficientDataCountsOnlyWindowedEvents pins the N in the
// zero-event message: it counts worker_turns events inside the --days window,
// not files or raw lines.
func TestTurnReport_InsufficientDataCountsOnlyWindowedEvents(t *testing.T) {
	requireImpl(t)
	now := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	// One unrelated event inside the window → still 0 worker_turns events.
	dir := writeEventsDir(t, now, []turnEvent{{Type: "bead_closed", Bead: "hydra-x"}})
	out, code := runReport(t, dir, now, 7)
	if code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	if !strings.Contains(out, "insufficient data: 0 events") {
		t.Fatalf("want 'insufficient data: 0 events', got:\n%s", out)
	}
}
