package core

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ExtractRecordedVerdict must return the HYDRA_VERDICT block verbatim from the
// newest worker log, so fix-bead prompts can show what the original attempt
// already claimed (tin0/jx78, 2026-09-25: fix worker redid green work because
// the recorded done verdict was never shown to it).
func TestExtractRecordedVerdict(t *testing.T) {
	dir := t.TempDir()
	origID := "proj-abc-1"
	safe := safeBeadID(origID)
	f, err := os.Create(filepath.Join(dir, safe+"-42.log"))
	if err != nil {
		t.Fatalf("create log: %v", err)
	}
	lines := []string{
		"2026-09-25T10:00:00Z [stderr] some build noise",
		"2026-09-25T10:01:00Z [stderr] HYDRA_VERDICT: {\"done\": true, \"ac_met\": [\"req-001\", \"req-002\"], \"ac_unmet\": []}",
		"2026-09-25T10:01:01Z [stderr] exit status 1",
	}
	for _, l := range lines {
		fmt.Fprintln(f, l)
	}
	f.Close()

	got := ExtractRecordedVerdict(dir, origID, 600)
	if !strings.Contains(got, `"ac_met": ["req-001", "req-002"]`) {
		t.Errorf("verdict block missing ac_met list, got:\n%s", got)
	}
	if !strings.Contains(got, "HYDRA_VERDICT") {
		t.Errorf("block must contain the verdict line, got:\n%s", got)
	}
}

func TestExtractRecordedVerdict_MissingLogReturnsEmpty(t *testing.T) {
	if got := ExtractRecordedVerdict(t.TempDir(), "no-such-bead", 600); got != "" {
		t.Errorf("ExtractRecordedVerdict = %q, want empty", got)
	}
}

func TestExtractRecordedVerdict_NoVerdictReturnsEmpty(t *testing.T) {
	dir := t.TempDir()
	f, err := os.Create(filepath.Join(dir, "bead-1-1.log"))
	if err != nil {
		t.Fatalf("create log: %v", err)
	}
	fmt.Fprintln(f, "2026-09-25T10:00:00Z [stderr] ordinary failure line")
	f.Close()
	if got := ExtractRecordedVerdict(dir, "bead-1", 600); got != "" {
		t.Errorf("ExtractRecordedVerdict = %q, want empty", got)
	}
}
