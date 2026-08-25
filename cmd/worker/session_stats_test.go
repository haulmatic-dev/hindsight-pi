package main

import (
	"bytes"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

func TestGatherSessionToolMix_CountsAndJestRuns(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, "session.jsonl", []string{
		`{"type":"session","id":"test-bead"}`,
		`{"type":"message","message":{"role":"assistant","content":[{"type":"toolCall","name":"bash","arguments":{"command":"go test ./..."}},{"type":"toolCall","name":"read","arguments":{"path":"x.go"}},{"type":"toolCall","name":"edit","arguments":{"oldText":"a","newText":"b"}}]}}`,
		`{"type":"message","message":{"role":"assistant","content":[{"type":"toolCall","name":"bash","arguments":{"command":"npx jest foo.spec.ts"}},{"type":"toolCall","name":"graphify","arguments":{"query":"foo"}},{"type":"toolCall","name":"hindsight_search","arguments":{"query":"bar"}}]}}`,
		`{"type":"message","message":{"role":"toolResult","toolName":"bash","content":"ok"}}`,
	})

	stats, ok := gatherSessionToolMix(dir)
	if !ok {
		t.Fatal("gatherSessionToolMix returned ok=false for valid fixture")
	}

	want := map[string]int{
		"bash":             2,
		"read":             1,
		"edit":             1,
		"graphify":         1,
		"hindsight_search": 1,
	}
	for name, n := range want {
		if got := stats.Counts[name]; got != n {
			t.Errorf("Counts[%q] = %d, want %d", name, got, n)
		}
	}
	if stats.TotalCalls != 6 {
		t.Errorf("TotalCalls = %d, want 6", stats.TotalCalls)
	}
	if stats.BashJestRuns != 1 {
		t.Errorf("BashJestRuns = %d, want 1", stats.BashJestRuns)
	}
	if stats.Payload["bash"] == 0 {
		t.Error("expected non-zero bash payload")
	}
	if stats.Payload["bash"] < stats.Payload["read"] {
		t.Error("bash payload should be at least as large as read payload")
	}

	got := formatSessionToolMix("test-bead", stats)
	if !strings.Contains(got, "[SessionStats] test-bead turns=6") {
		t.Errorf("log line missing expected prefix: %s", got)
	}
	if !strings.Contains(got, "jest_runs=1") {
		t.Errorf("log line missing jest_runs=1: %s", got)
	}
	// graphify and hindsight_search are reported as dedicated fields, not
	// duplicated inside the generic tools/payload lists.
	if strings.Contains(got, "graphify:") || strings.Contains(got, "hindsight_search:") {
		t.Errorf("tools/payload list must not contain graphify or hindsight_search: %s", got)
	}
}

func TestGatherSessionToolMix_MissingDir(t *testing.T) {
	dir := t.TempDir()
	os.RemoveAll(dir)

	var buf bytes.Buffer
	old := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(old)

	_, ok := gatherSessionToolMix(dir)
	if ok {
		t.Error("expected ok=false for missing session dir")
	}
	if buf.String() != "" {
		t.Errorf("missing session dir should stay silent, got logs: %q", buf.String())
	}
}

func TestGatherSessionToolMix_ParseErrorSkipsStats(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, "bad.jsonl", []string{
		`{"type":"message","message":{"role":"assistant","content":[{"type":"toolCall","name":"bash","arguments":{"command":"echo hi"}}]}}`,
		`this is not json`,
	})

	var buf bytes.Buffer
	old := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(old)

	_, ok := gatherSessionToolMix(dir)
	if ok {
		t.Error("expected ok=false when a jsonl line is malformed")
	}
	if !strings.Contains(buf.String(), "[SessionStats] debug:") {
		t.Errorf("expected debug log for parse error, got: %q", buf.String())
	}
}

func TestEmitSessionToolMixStats_NoHydraHome(t *testing.T) {
	var buf bytes.Buffer
	old := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(old)

	emitSessionToolMixStats(types.Bead{ID: "hydra/test-123"}, "")
	if buf.String() != "" {
		t.Errorf("expected no log when HYDRA_HOME is empty, got: %q", buf.String())
	}
}

func writeFixture(t *testing.T, dir, name string, lines []string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
}
