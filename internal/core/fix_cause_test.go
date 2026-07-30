package core

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeTestLog(t *testing.T, logDir, beadID string, lines ...string) {
	t.Helper()
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		t.Fatalf("mkdir log dir: %v", err)
	}
	name := strings.NewReplacer("/", "-", " ", "-").Replace(beadID) + "-123.log"
	f, err := os.Create(filepath.Join(logDir, name))
	if err != nil {
		t.Fatalf("create log: %v", err)
	}
	defer f.Close()
	for _, line := range lines {
		fmt.Fprintln(f, line)
	}
}

func TestExtractFailureCause_MissingLogReturnsEmpty(t *testing.T) {
	dir := t.TempDir()
	if got := ExtractFailureCause(dir, "missing-bead", 2048); got != "" {
		t.Errorf("expected empty string for missing log, got %q", got)
	}
}

func TestExtractFailureCause_FailingTestName(t *testing.T) {
	dir := t.TempDir()
	writeTestLog(t, dir, "notes-bead",
		"2026-07-29T05:00:00Z [stderr] some setup",
		"2026-07-29T05:00:00Z [stderr] --- FAIL: TestNotesSurviveRestart (0.00s)",
		"2026-07-29T05:00:00Z [stderr] cleanup",
	)

	got := ExtractFailureCause(dir, "notes-bead", 2048)
	if !strings.Contains(got, "TestNotesSurviveRestart") {
		t.Errorf("expected failing test name in cause block, got:\n%s", got)
	}
	if strings.Contains(got, "some setup") {
		t.Errorf("cause block should not contain low-priority setup lines, got:\n%s", got)
	}
}

func TestExtractFailureCause_ModelCallFailed(t *testing.T) {
	dir := t.TempDir()
	writeTestLog(t, dir, "model-bead",
		"2026-07-29T05:00:00Z [stderr] {\"type\":\"model_call_failed\",\"detail\":\"model call FAILED: 429 rate limit exceeded\"}",
	)

	got := ExtractFailureCause(dir, "model-bead", 2048)
	if !strings.Contains(got, "model call FAILED") {
		t.Errorf("expected model call FAILED marker, got:\n%s", got)
	}
	if !strings.Contains(got, "429") {
		t.Errorf("expected 429 rate limit in cause block, got:\n%s", got)
	}
}

func TestExtractFailureCause_FallbackLastLines(t *testing.T) {
	dir := t.TempDir()
	var lines []string
	for i := 0; i < 50; i++ {
		lines = append(lines, fmt.Sprintf("2026-07-29T05:00:00Z [stderr] ordinary line %d", i))
	}
	writeTestLog(t, dir, "ordinary-bead", lines...)

	got := ExtractFailureCause(dir, "ordinary-bead", 2048)
	if got == "" {
		t.Fatalf("expected non-empty fallback block")
	}
	if !strings.Contains(got, "ordinary line 49") {
		t.Errorf("expected final lines in fallback block, got:\n%s", got)
	}
	if strings.Contains(got, "ordinary line 10") {
		t.Errorf("fallback block should keep only the last 10 lines, got:\n%s", got)
	}
}

func TestExtractFailureCause_MaxBytesCapsBlock(t *testing.T) {
	dir := t.TempDir()
	writeTestLog(t, dir, "long-bead",
		"2026-07-29T05:00:00Z [stderr] Error: "+strings.Repeat("x", 5000),
	)

	got := ExtractFailureCause(dir, "long-bead", 100)
	if len(got) > 100 {
		t.Errorf("expected block at most 100 bytes, got %d:\n%s", len(got), got)
	}
	if len(got) == 0 {
		t.Errorf("expected non-empty truncated block")
	}
}

func TestExtractFailureCause_SkipsPassthroughJSONL(t *testing.T) {
	dir := t.TempDir()
	writeTestLog(t, dir, "jsonl-bead",
		"2026-07-29T05:00:00Z [stderr] {\"type\":\"message_update\",\"content\":\"hello\"}",
		"2026-07-29T05:00:00Z [stderr] --- FAIL: TestJSONLFiltered (0.00s)",
	)

	got := ExtractFailureCause(dir, "jsonl-bead", 2048)
	if strings.Contains(got, "message_update") {
		t.Errorf("passthrough JSONL should be skipped, got:\n%s", got)
	}
	if !strings.Contains(got, "TestJSONLFiltered") {
		t.Errorf("expected FAIL line to appear, got:\n%s", got)
	}
}

func TestExtractFailureCause_NewestLogFileWins(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	oldName := filepath.Join(dir, "multi-bead-1.log")
	newName := filepath.Join(dir, "multi-bead-2.log")

	if err := os.WriteFile(oldName, []byte("old log\n--- FAIL: TestOld\n"), 0o644); err != nil {
		t.Fatalf("write old: %v", err)
	}
	if err := os.WriteFile(newName, []byte("new log\n--- FAIL: TestNew\n"), 0o644); err != nil {
		t.Fatalf("write new: %v", err)
	}
	// Ensure modification times differ even on fast filesystems.
	oldTime := time.Now().Add(-time.Hour)
	if err := os.Chtimes(oldName, oldTime, oldTime); err != nil {
		t.Fatalf("chtimes old: %v", err)
	}

	got := ExtractFailureCause(dir, "multi-bead", 2048)
	if !strings.Contains(got, "TestNew") {
		t.Errorf("expected newest log file to be used, got:\n%s", got)
	}
	if strings.Contains(got, "TestOld") {
		t.Errorf("expected old log file to be ignored, got:\n%s", got)
	}
}

func TestExtractFailureCause_NoExecNoHTTPNoNetImports(t *testing.T) {
	// This is a compile-time guard: fix_cause.go already imports only os,
	// path/filepath, sort, strings, and bufio. Adding net/http or os/exec would
	// be caught by the compiler and fail the package build.
	t.Log("NFR-001: fix_cause.go is a pure log parser; no exec/net/http imports")
}

// Regression (run D3): a worker implementing a log parser echoed source code
// containing the literal `--- FAIL:` into its log; the extractor harvested
// that stringified code as the failure cause. Escaped source must never win
// over (or pollute) real failure lines.
func TestExtractFailureCause_IgnoresEscapedSourceCode(t *testing.T) {
	dir := t.TempDir()
	log := `2026-07-29T20:00:01Z [stdout] writing fix_parse.go:
2026-07-29T20:00:01Z [stdout] ntinue\n\t\t}\n\n\t\tif strings.Contains(line, \"--- FAIL:\") {\n\t\t\tfailTestLines = append(failTestLines, l
2026-07-29T20:00:02Z [stderr] --- FAIL: TestRealFailure (0.01s)
2026-07-29T20:00:02Z [stderr] FAIL	github.com/example/pkg	0.5s
`
	if err := os.WriteFile(filepath.Join(dir, "b-1-100.log"), []byte(log), 0o644); err != nil {
		t.Fatal(err)
	}
	got := ExtractFailureCause(dir, "b-1", 2048)
	if !strings.Contains(got, "--- FAIL: TestRealFailure") {
		t.Errorf("real failure line must be extracted, got %q", got)
	}
	if strings.Contains(got, `\t\t`) || strings.Contains(got, "failTestLines = append") {
		t.Errorf("escaped source code leaked into the cause block: %q", got)
	}
}

// A marker mid-prose (model narrating, not go test output) must not be
// harvested as a failing-test line: the marker anchors at payload start.
func TestExtractFailureCause_MarkerMidLineIsNotATestFailure(t *testing.T) {
	dir := t.TempDir()
	log := `2026-07-29T20:00:01Z [stdout] I will now grep for --- FAIL: markers in the output
2026-07-29T20:00:02Z [stderr]     --- FAIL: TestSubtest (0.00s)
`
	if err := os.WriteFile(filepath.Join(dir, "b-2-100.log"), []byte(log), 0o644); err != nil {
		t.Fatal(err)
	}
	got := ExtractFailureCause(dir, "b-2", 2048)
	if !strings.Contains(got, "TestSubtest") {
		t.Errorf("indented real subtest failure must be extracted, got %q", got)
	}
	if strings.Contains(got, "I will now grep") {
		t.Errorf("prose mentioning the marker mid-line must not be harvested: %q", got)
	}
}
