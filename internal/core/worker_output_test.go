package core

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestIsNotableWorkerLine(t *testing.T) {
	notable := []string{
		"❌ Pi failed", "panic: nil deref", "Error: boom", "task failed",
		"exit code 1", "http 429 rate limit", "returned 401 unauthorized",
		"Unknown provider kimi", "authentication_error", "timed out",
	}
	for _, s := range notable {
		if !isNotableWorkerLine(s) {
			t.Errorf("expected notable: %q", s)
		}
	}
	chatter := []string{
		"Memory 12: 38366491-...", "Added skill flag: golang-pro",
		"reading file internal/core/foo.go", "ok", "Executing step: validate",
		// pi's stream-JSON control events carry failure words as ordinary
		// payload. Promoting these put 1.56 GB into the daemon log in under
		// five hours and made unanchored greps report failures that never
		// happened — see isNotableWorkerLine.
		`{"type":"message_start","message":{"role":"toolResult","content":"error: boom"}}`,
		`{"type":"turn_end","message":{"content":[{"type":"thinking","thinking":"the test failed so I will"}]}}`,
		`  {"type":"stream_event","event":{"delta":{"text":"timed out"}}}`,
		`[{"type":"batch","items":["exit code 1"]}]`,
	}
	for _, s := range chatter {
		if isNotableWorkerLine(s) {
			t.Errorf("expected chatter (not notable): %q", s)
		}
	}
}

func TestStreamWorkerOutput_WritesToFile(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "wl-*.log")
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	streamWorkerOutput(strings.NewReader("chatter line\n❌ bad thing\n"), "bead-1", "", f, &mu)
	_ = f.Close()

	data, _ := os.ReadFile(f.Name())
	s := string(data)
	if !strings.Contains(s, "chatter line") || !strings.Contains(s, "bad thing") {
		t.Errorf("per-bead file should contain all worker lines, got: %q", s)
	}
}

func TestWorkerLogDir_UsesHydraHome(t *testing.T) {
	t.Setenv("HYDRA_HOME", "/opt/hydra")
	if got := workersLogDir(); got != filepath.Join("/opt/hydra", "logs", "workers") {
		t.Errorf("workersLogDir = %q", got)
	}
}

// TestTruncateForDaemonLog: a promoted line must not be able to dominate the
// daemon log, and a clipped line must announce that it was clipped.
func TestTruncateForDaemonLog(t *testing.T) {
	short := "❌ Pi failed"
	if got := truncateForDaemonLog(short); got != short {
		t.Errorf("short line was altered: %q", got)
	}

	long := "Error: " + strings.Repeat("x", 20000)
	got := truncateForDaemonLog(long)
	if len(got) >= len(long) {
		t.Errorf("long line was not truncated (%d bytes)", len(got))
	}
	if !strings.HasPrefix(got, "Error: xxx") {
		t.Errorf("truncation dropped the start of the line, which is where the signal is: %q", got[:40])
	}
	if !strings.Contains(got, "truncated") {
		t.Error("a clipped line must say so, or it reads as the whole story")
	}
}

// TestStreamWorkerOutput_CapsPerBeadLog: the per-bead log is the forensic record,
// but it cannot be unbounded. 30 GB accumulated across 1,590 files in two days on
// 2026-07-31, one bead reaching 853 MB.
func TestStreamWorkerOutput_CapsPerBeadLog(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "wl-*.log")
	if err != nil {
		t.Fatal(err)
	}

	// 512 KB lines, well under the scanner's 1 MB token limit, repeated past the cap.
	line := strings.Repeat("x", 512<<10) + "\n"
	var sb strings.Builder
	for i := 0; i < 80; i++ {
		sb.WriteString(line)
	}

	var mu sync.Mutex
	streamWorkerOutput(strings.NewReader(sb.String()), "bead-big", "", f, &mu)
	_ = f.Close()

	fi, err := os.Stat(f.Name())
	if err != nil {
		t.Fatal(err)
	}
	// The cap is checked after a write, so allow one oversized line plus the notice.
	limit := int64(maxWorkerLogBytes) + int64(2<<20)
	if fi.Size() > limit {
		t.Errorf("per-bead log grew to %d bytes, cap is %d", fi.Size(), maxWorkerLogBytes)
	}

	data, _ := os.ReadFile(f.Name())
	if !strings.Contains(string(data), "per-bead log capped") {
		t.Error("a capped log must say it was capped, or it reads as complete output")
	}
}

// TestStreamWorkerOutput_SmallLogsUncapped guards against the cap changing normal
// behaviour: an ordinary worker log must be written in full.
func TestStreamWorkerOutput_SmallLogsUncapped(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "wl-*.log")
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	streamWorkerOutput(strings.NewReader("first\nsecond\nthird\n"), "bead-small", "", f, &mu)
	_ = f.Close()

	data, _ := os.ReadFile(f.Name())
	s := string(data)
	for _, want := range []string{"first", "second", "third"} {
		if !strings.Contains(s, want) {
			t.Errorf("line %q missing from an uncapped log", want)
		}
	}
	if strings.Contains(s, "capped") {
		t.Error("a small log was reported as capped")
	}
}
