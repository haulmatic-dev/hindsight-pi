package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeSessionFile(t *testing.T, dir, name string, lines []string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestGatherWorkerTrajectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sessions", "bead-x")
	lines := []string{
		`{"type":"turn_end"}`,
		`{"type":"toolCall","name":"bash","arguments":{"command":"go test ./internal/pan/"}}`,
		`{"type":"turn_end"}`,
		`{"type":"toolCall","name":"edit","arguments":{"file_path":"a.go"}}`,
		`{"type":"toolCall","name":"write","arguments":{"path":"b.go"}}`,
		`{"type":"toolCall","name":"bash","arguments":{"command":"go build ./..."}}`,
		`{"type":"toolCall","name":"bash","arguments":{"command":"go build ./..."}}`,
		`{"type":"toolCall","name":"read","arguments":{"path":"a.go"}}`,
		`garbage not json`,
	}
	writeSessionFile(t, dir, "session.jsonl", lines)

	tra, ok := gatherWorkerTrajectory(dir)
	if !ok {
		t.Fatal("want ok=true")
	}
	if tra.Turns != 2 {
		t.Errorf("Turns = %d, want 2", tra.Turns)
	}
	if tra.TestRuns != 1 {
		t.Errorf("TestRuns = %d, want 1", tra.TestRuns)
	}
	// go test ran after the two edits? No: edits came after the test run, so
	// editsSinceTest = 2 and the later go build does not reset it.
	if tra.EditsSinceTest != 2 {
		t.Errorf("EditsSinceTest = %d, want 2", tra.EditsSinceTest)
	}
	if tra.DistinctEdits != 2 {
		t.Errorf("DistinctEdits = %d, want 2", tra.DistinctEdits)
	}
	if tra.RepeatedCommand != "go build ./..." || tra.RepeatedCount != 2 {
		t.Errorf("RepeatedCommand = %q (%d), want go build (2)", tra.RepeatedCommand, tra.RepeatedCount)
	}

	// Tier-1 must flag nothing for this modest trajectory.
	if anoms := tra.Anomalies("green"); len(anoms) != 0 {
		t.Errorf("healthy trajectory flagged: %v", anoms)
	}
}

func TestGatherWorkerTrajectoryNoFiles(t *testing.T) {
	if _, ok := gatherWorkerTrajectory(filepath.Join(t.TempDir(), "missing")); ok {
		t.Fatal("want ok=false for missing dir")
	}
	empty := t.TempDir()
	if _, ok := gatherWorkerTrajectory(empty); ok {
		t.Fatal("want ok=false for dir without jsonl files")
	}
}

func TestTrajectoryResetAfterTestRun(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sessions", "bead-y")
	lines := []string{
		`{"type":"toolCall","name":"edit","arguments":{"file_path":"a.go"}}`,
		`{"type":"toolCall","name":"bash","arguments":{"command":"npm test"}}`,
		`{"type":"toolCall","name":"edit","arguments":{"file_path":"a.go"}}`,
	}
	writeSessionFile(t, dir, "session.jsonl", lines)
	tra, ok := gatherWorkerTrajectory(dir)
	if !ok {
		t.Fatal("want ok=true")
	}
	if tra.EditsSinceTest != 1 {
		t.Errorf("EditsSinceTest = %d, want 1 (test run resets the counter)", tra.EditsSinceTest)
	}
	if tra.ReeditedCount != 1 || tra.ReeditedFile != "a.go" {
		t.Errorf("ReeditedFile/Count = %q/%d, want a.go/1 (per-file counts reset on test run)", tra.ReeditedFile, tra.ReeditedCount)
	}
}

func TestTrajectoryNewFilesAndReedit(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sessions", "bead-z")
	lines := []string{
		`{"type":"toolCall","name":"write","arguments":{"file_path":"new.go"}}`,
		`{"type":"toolCall","name":"edit","arguments":{"file_path":"main.go"}}`,
		`{"type":"toolCall","name":"edit","arguments":{"file_path":"main.go"}}`,
		`{"type":"toolCall","name":"edit","arguments":{"file_path":"main.go"}}`,
		`{"type":"toolCall","name":"read","arguments":{"file_path":"main.go"}}`,
	}
	writeSessionFile(t, dir, "session.jsonl", lines)
	tra, ok := gatherWorkerTrajectory(dir)
	if !ok {
		t.Fatal("want ok=true")
	}
	if tra.NewFiles != 1 {
		t.Errorf("NewFiles = %d, want 1", tra.NewFiles)
	}
	if tra.ReeditedFile != "main.go" || tra.ReeditedCount != 3 {
		t.Errorf("Reedited = %q/%d, want main.go/3", tra.ReeditedFile, tra.ReeditedCount)
	}
	if tra.DistinctEdits != 2 {
		t.Errorf("DistinctEdits = %d, want 2 (write + edit paths)", tra.DistinctEdits)
	}
	// Tier-1 must flag the blind re-editing.
	anoms := tra.Anomalies("fix")
	if len(anoms) == 0 || !strings.Contains(anoms[0], "same-file-reedited") {
		t.Errorf("expected same-file-reedited flag, got %v", anoms)
	}
}
