package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// run is the entry point the tool is expected to expose. It takes argv-style
// args, writes report output to stdout, diagnostics to stderr, and returns an
// exit code.
var run func(args []string, stdout, stderr *bytes.Buffer) int

func writeEvents(t *testing.T, dir string, name string, events string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(events), 0o644); err != nil {
		t.Fatal(err)
	}
}

func event(phase string, turns int) string {
	return fmt.Sprintf(`{"type":"worker_turns","phase":%q,"turns":%d}`+"\n", phase, turns)
}

// AC: zero events → prints "insufficient data: N events" and exits 0.
func TestZeroEventsFailsOpen(t *testing.T) {
	dir := t.TempDir()
	var out, errb bytes.Buffer
	code := run([]string{"--events-dir", dir, "--days", "7"}, &out, &errb)
	if code != 0 {
		t.Fatalf("want exit 0 (fail-open), got %d", code)
	}
	if !strings.Contains(out.String(), "insufficient data: 0 events") {
		t.Fatalf("want 'insufficient data: 0 events' in stdout, got %q", out.String())
	}
}

// AC: missing events dir also fails open with exit 0.
func TestMissingEventsDirFailsOpen(t *testing.T) {
	var out, errb bytes.Buffer
	code := run([]string{"--events-dir", filepath.Join(t.TempDir(), "does-not-exist"), "--days", "7"}, &out, &errb)
	if code != 0 {
		t.Fatalf("want exit 0 (fail-open), got %d", code)
	}
	if !strings.Contains(out.String(), "insufficient data") {
		t.Fatalf("want 'insufficient data' in stdout, got %q", out.String())
	}
}

// AC: per-phase percentiles p25/p50/p75/p90/max are printed.
func TestPerPhasePercentilesPrinted(t *testing.T) {
	dir := t.TempDir()
	var b strings.Builder
	// phase "red": 21 samples 1..21 so p50=11, p90=19 (nearest-rank on sorted 1..21)
	for i := 1; i <= 21; i++ {
		b.WriteString(event("red", i))
	}
	writeEvents(t, dir, "events-2026-01-01.jsonl", b.String())
	var out, errb bytes.Buffer
	code := run([]string{"--events-dir", dir, "--days", "7"}, &out, &errb)
	if code != 0 {
		t.Fatalf("want exit 0, got %d", code)
	}
	s := out.String()
	for _, phase := range []string{"red"} {
		if !strings.Contains(s, phase) {
			t.Fatalf("want phase %q in report, got %q", phase, s)
		}
	}
	for _, p := range []string{"p25", "p50", "p75", "p90", "max"} {
		if !strings.Contains(s, p) {
			t.Fatalf("want percentile %q in report, got %q", p, s)
		}
	}
	// p90 of 1..21 (nearest-rank, ceil(0.9*21)=19th) = 19
	if !strings.Contains(s, "19") {
		t.Fatalf("want p90 value 19 in report, got %q", s)
	}
}

// AC: suggested turn_budgets yaml block = round-up-to-10 of p90 per phase,
// emitted as a COMMENT for operator review — never auto-written to config.
func TestSuggestedTurnBudgetsCommentBlock(t *testing.T) {
	dir := t.TempDir()
	var b strings.Builder
	// 21 samples 1..21 → p90=19 → round up to 20
	for i := 1; i <= 21; i++ {
		b.WriteString(event("red", i))
	}
	// 25 samples 1..25 → p90=ceil(0.9*25)=23rd value=23 → round up to 30
	for i := 1; i <= 25; i++ {
		b.WriteString(event("green", i))
	}
	writeEvents(t, dir, "events-2026-01-01.jsonl", b.String())
	var out, errb bytes.Buffer
	code := run([]string{"--events-dir", dir, "--days", "7"}, &out, &errb)
	if code != 0 {
		t.Fatalf("want exit 0, got %d", code)
	}
	s := out.String()
	if !strings.Contains(s, "turn_budgets:") {
		t.Fatalf("want 'turn_budgets:' yaml block in output, got %q", s)
	}
	// comment for operator review: suggested block must be commented out
	for _, line := range strings.Split(s, "\n") {
		if strings.Contains(line, "turn_budgets:") && !strings.HasPrefix(strings.TrimSpace(line), "#") {
			t.Fatalf("turn_budgets suggestion must be a comment, got bare line %q", line)
		}
	}
	if !strings.Contains(s, "20") {
		t.Fatalf("want rounded p90 20 for phase red in output, got %q", s)
	}
	if !strings.Contains(s, "30") {
		t.Fatalf("want rounded p90 30 for phase green in output, got %q", s)
	}
}

// AC: drift warning when a phase's sample size < 20.
func TestDriftWarningSmallSample(t *testing.T) {
	dir := t.TempDir()
	var b strings.Builder
	for i := 1; i <= 5; i++ {
		b.WriteString(event("red", i*10))
	}
	writeEvents(t, dir, "events-2026-01-01.jsonl", b.String())
	var out, errb bytes.Buffer
	code := run([]string{"--events-dir", dir, "--days", "7"}, &out, &errb)
	if code != 0 {
		t.Fatalf("want exit 0, got %d", code)
	}
	if !strings.Contains(errb.String(), "warning") {
		t.Fatalf("want drift warning on stderr for small sample, got %q", errb.String())
	}
}

// No drift warning when sample size >= 20.
func TestNoDriftWarningLargeSample(t *testing.T) {
	dir := t.TempDir()
	var b strings.Builder
	for i := 1; i <= 20; i++ {
		b.WriteString(event("red", i))
	}
	writeEvents(t, dir, "events-2026-01-01.jsonl", b.String())
	var out, errb bytes.Buffer
	run([]string{"--events-dir", dir, "--days", "7"}, &out, &errb)
	if strings.Contains(errb.String(), "warning") {
		t.Fatalf("want no drift warning for n>=20, got %q", errb.String())
	}
}

// AC: --days N filters events to the last N days.
func TestDaysFilter(t *testing.T) {
	dir := t.TempDir()
	old := `{"type":"worker_turns","phase":"red","turns":50,"ts":"2020-01-01T00:00:00Z"}` + "\n"
	recent := `{"type":"worker_turns","phase":"red","turns":10,"ts":"2026-01-01T00:00:00Z"}` + "\n"
	writeEvents(t, dir, "events.jsonl", old+recent)
	var out, errb bytes.Buffer
	// Days=1 relative to a "now" far from 2020-01-01: only the recent event counts.
	// The tool must exclude the 2020 event; with only 1 sample the report still prints.
	code := run([]string{"--events-dir", dir, "--days", "30"}, &out, &errb)
	if code != 0 {
		t.Fatalf("want exit 0, got %d", code)
	}
	if strings.Contains(out.String(), "50") {
		t.Fatalf("event outside --days window should be excluded, got %q", out.String())
	}
}
