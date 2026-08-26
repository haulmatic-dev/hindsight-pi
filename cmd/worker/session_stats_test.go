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
	if strings.Contains(got, "gate_runs=") {
		t.Errorf("log line must not contain gate_runs when HYDRA_GATE_CMDS is unset: %s", got)
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

func TestGatherSessionToolMix_ParseErrorSkipsMalformedLine(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, "bad.jsonl", []string{
		`{"type":"message","message":{"role":"assistant","content":[{"type":"toolCall","name":"bash","arguments":{"command":"echo hi"}}]}}`,
		`this is not json`,
	})

	var buf bytes.Buffer
	old := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(old)

	stats, ok := gatherSessionToolMix(dir)
	if !ok {
		t.Fatal("expected ok=true when only one jsonl line is malformed")
	}
	if stats.Counts["bash"] != 1 {
		t.Errorf("Counts[bash] = %d, want 1", stats.Counts["bash"])
	}
	if !strings.Contains(buf.String(), "[SessionStats] debug:") {
		t.Errorf("expected debug log for malformed line, got: %q", buf.String())
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

func TestGatherSessionToolMix_GateCmdRuns(t *testing.T) {
	t.Setenv("HYDRA_GATE_CMDS", `["npm run test:agent -- <scope>","go test <scope>","npm run lint"]`)
	dir := t.TempDir()
	writeFixture(t, dir, "session.jsonl", []string{
		`{"type":"session","id":"test-bead"}`,
		`{"type":"message","message":{"role":"assistant","content":[{"type":"toolCall","name":"bash","arguments":{"command":"npm run test:agent -- foo"}},{"type":"toolCall","name":"bash","arguments":{"command":"go test ./cmd/worker"}},{"type":"toolCall","name":"bash","arguments":{"command":"npm   run  lint"}},{"type":"toolCall","name":"bash","arguments":{"command":"npm run build"}}]}}`,
		`{"type":"message","message":{"role":"assistant","content":[{"type":"toolCall","name":"bash","arguments":{"command":"npx jest foo.spec.ts"}}]}}`,
	})

	stats, ok := gatherSessionToolMix(dir)
	if !ok {
		t.Fatal("gatherSessionToolMix returned ok=false for valid fixture")
	}

	want := map[string]int{
		"npm run test:agent": 1,
		"go test":            1,
		"npm run lint":       1,
	}
	if len(stats.GateCmdRuns) != len(want) {
		t.Errorf("GateCmdRuns = %v, want %v", stats.GateCmdRuns, want)
	}
	for k, v := range want {
		if got := stats.GateCmdRuns[k]; got != v {
			t.Errorf("GateCmdRuns[%q] = %d, want %d", k, got, v)
		}
	}
	if stats.GateCmdRuns["npm run build"] != 0 {
		t.Errorf("npm run build must not be counted as a gate run: %v", stats.GateCmdRuns)
	}
	if stats.BashJestRuns != 2 {
		t.Errorf("BashJestRuns = %d, want 2", stats.BashJestRuns)
	}

	got := formatSessionToolMix("test-bead", stats)
	if !strings.Contains(got, `gate_runs={`) {
		t.Errorf("log line missing gate_runs field: %s", got)
	}
	if !strings.Contains(got, `"go test":1`) || !strings.Contains(got, `"npm run lint":1`) || !strings.Contains(got, `"npm run test:agent":1`) {
		t.Errorf("log line missing expected gate run counts: %s", got)
	}
}

func TestGatherSessionToolMix_GateCmdRunsFailOpen(t *testing.T) {
	t.Setenv("HYDRA_GATE_CMDS", "not-json")
	dir := t.TempDir()
	writeFixture(t, dir, "session.jsonl", []string{
		`{"type":"message","message":{"role":"assistant","content":[{"type":"toolCall","name":"bash","arguments":{"command":"go test ./..."}}]}}`,
	})

	var buf bytes.Buffer
	old := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(old)

	stats, ok := gatherSessionToolMix(dir)
	if !ok {
		t.Fatal("expected ok=true when HYDRA_GATE_CMDS is malformed")
	}
	if len(stats.GateCmdRuns) != 0 {
		t.Errorf("GateCmdRuns must be empty when env is malformed, got %v", stats.GateCmdRuns)
	}
	if !strings.Contains(buf.String(), "[SessionStats] debug:") {
		t.Errorf("expected debug log for malformed HYDRA_GATE_CMDS, got: %q", buf.String())
	}
}

func writeFixture(t *testing.T, dir, name string, lines []string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
}
