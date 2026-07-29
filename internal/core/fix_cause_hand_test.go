package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeWorkerLog(t *testing.T, dir, name, content string, age time.Duration) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	mt := time.Now().Add(-age)
	if err := os.Chtimes(p, mt, mt); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestExtractFailureCause_MissingLog(t *testing.T) {
	if got := extractFailureCause(t.TempDir(), "absent-bead", 2048); got != "" {
		t.Errorf("missing log must yield empty cause, got %q", got)
	}
	if got := extractFailureCause("", "b", 2048); got != "" {
		t.Errorf("empty dir must yield empty cause, got %q", got)
	}
}

func TestExtractFailureCause_NamesFailingTest(t *testing.T) {
	dir := t.TempDir()
	writeWorkerLog(t, dir, "p1-abc-100.log",
		"2026-07-29T08:00:00Z [stderr] building...\n"+
			"--- FAIL: TestNotesSurviveRestart (0.00s)\n"+
			"FAIL\tgithub.com/x/notes\t0.01s\n", 0)
	got := extractFailureCause(dir, "p1-abc", 2048)
	if !strings.Contains(got, "TestNotesSurviveRestart") {
		t.Errorf("cause must name the failing test, got %q", got)
	}
}

// TestExtractFailureCause_ModelFailureBeatsNoise: the model-failure JSONL line
// is kept even though passthrough JSONL is otherwise excluded, and it outranks
// everything else in the block.
func TestExtractFailureCause_ModelFailureBeatsNoise(t *testing.T) {
	dir := t.TempDir()
	writeWorkerLog(t, dir, "p1-abc-100.log",
		`2026-07-29T08:00:00Z [stderr] {"type":"message_update","delta":"thinking about --- FAIL: TestDecoy"}`+"\n"+
			"2026-07-29T08:00:01Z [stderr] model call FAILED: 429 rate_limit_error overloaded\n"+
			"some Error: compile broke\n", 0)
	got := extractFailureCause(dir, "p1-abc", 2048)
	if !strings.Contains(got, "model call FAILED") || !strings.Contains(got, "429") {
		t.Errorf("model failure line must survive, got %q", got)
	}
	if strings.Contains(got, "TestDecoy") {
		t.Errorf("passthrough JSONL noise must be excluded, got %q", got)
	}
	if !strings.HasPrefix(got, "2026-07-29T08:00:01Z") && !strings.HasPrefix(got, "model call FAILED") {
		// Highest priority line first.
		first := strings.SplitN(got, "\n", 2)[0]
		if !strings.Contains(first, "model call FAILED") {
			t.Errorf("model failure must be the first line, got %q", first)
		}
	}
}

func TestExtractFailureCause_FallbackLastLines(t *testing.T) {
	dir := t.TempDir()
	var sb strings.Builder
	for i := 0; i < 50; i++ {
		sb.WriteString("ordinary line\n")
	}
	sb.WriteString("final interesting line\n")
	writeWorkerLog(t, dir, "p1-abc-100.log", sb.String(), 0)
	got := extractFailureCause(dir, "p1-abc", 2048)
	if got == "" || !strings.Contains(got, "final interesting line") {
		t.Errorf("fallback must keep the tail of the log, got %q", got)
	}
}

func TestExtractFailureCause_TruncatesToBudget(t *testing.T) {
	dir := t.TempDir()
	writeWorkerLog(t, dir, "p1-abc-100.log",
		"Error: "+strings.Repeat("x", 5000)+"IMPORTANT_TAIL\n", 0)
	got := extractFailureCause(dir, "p1-abc", 100)
	if len(got) > 100 {
		t.Errorf("block is %d bytes, budget 100", len(got))
	}
	if !strings.Contains(got, "IMPORTANT_TAIL") {
		t.Errorf("overlong line must keep its end, got %q", got)
	}
}

func TestExtractFailureCause_PicksNewestLog(t *testing.T) {
	dir := t.TempDir()
	writeWorkerLog(t, dir, "p1-abc-100.log", "--- FAIL: TestOldAttempt\n", time.Hour)
	writeWorkerLog(t, dir, "p1-abc-200.log", "--- FAIL: TestNewAttempt\n", 0)
	got := extractFailureCause(dir, "p1-abc", 2048)
	if !strings.Contains(got, "TestNewAttempt") || strings.Contains(got, "TestOldAttempt") {
		t.Errorf("must read the newest attempt's log, got %q", got)
	}
}
