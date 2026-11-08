package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// RED-phase behavior tests for the Evidence-of-use probes (deep tier).
//
// Contract pinned for GREEN:
//   - IntelUsageEvent(hindsight, cass, graphifyFallback bool) Event — the
//     record worker execution emits BEFORE the agent invocation, carrying
//     fields hindsight / cass / graphify_fallback.
//   - ToolHealthEvidenceScanner: NewToolHealthEvidenceScanner(getProjects
//     func() []types.Project, events *EventSink) *ToolHealthEvidenceScanner,
//     SetActivityProbe(fn func(project string) bool) — "recorded worker
//     activity" for a project, SetHindsightProbe(fn func(project string)
//     (int, error)) — hindsight_memories count, and scan() performing the
//     evidence-tier sweep.
//
// Sweep rules:
//   - worker activity recorded AND zero hindsight_memories → one
//     `tool_degraded` event with Fields["probe"] == "evidence" per project.
//   - zero worker activity → NO evidence-tier events at all.
//   - activity with memories present → no degradation.
//   - probe errors are fail-open (logged, no event, no panic).

func evidenceProjects() []types.Project {
	return []types.Project{{Name: "demo", Path: "/tmp/demo"}}
}

func readEvidenceEvents(t *testing.T, eventsDir string) []Event {
	t.Helper()
	entries, err := os.ReadDir(eventsDir)
	if err != nil {
		t.Fatalf("read events dir: %v", err)
	}
	var evs []Event
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(eventsDir, e.Name()))
		if err != nil {
			t.Fatalf("read %s: %v", e.Name(), err)
		}
		for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
			if line == "" {
				continue
			}
			var ev Event
			if err := json.Unmarshal([]byte(line), &ev); err != nil {
				t.Fatalf("unmarshal event: %v (line=%q)", err, line)
			}
			evs = append(evs, ev)
		}
	}
	return evs
}

func TestToolHealthEvidence_IntelUsageEventFields(t *testing.T) {
	ev := IntelUsageEvent(true, false, true)
	if ev.Type != "intel_usage" {
		t.Fatalf("Type = %q, want intel_usage", ev.Type)
	}
	if _, ok := ev.Fields["hindsight"]; !ok {
		t.Errorf("intel_usage missing hindsight field: %+v", ev.Fields)
	}
	if _, ok := ev.Fields["cass"]; !ok {
		t.Errorf("intel_usage missing cass field: %+v", ev.Fields)
	}
	if _, ok := ev.Fields["graphify_fallback"]; !ok {
		t.Errorf("intel_usage missing graphify_fallback field: %+v", ev.Fields)
	}
	if ev.Fields["hindsight"] != true {
		t.Errorf("hindsight = %v, want true", ev.Fields["hindsight"])
	}
}

func TestToolHealthEvidence_DegradedWhenActivityNoMemories(t *testing.T) {
	eventsDir := t.TempDir()
	s := NewToolHealthEvidenceScanner(evidenceProjects, NewEventSink(eventsDir))
	s.SetActivityProbe(func(project string) bool { return true })
	s.SetHindsightProbe(func(project string) (int, error) { return 0, nil })

	s.scan()

	evs := readEvidenceEvents(t, eventsDir)
	var degraded []Event
	for _, ev := range evs {
		if ev.Type == "tool_degraded" {
			degraded = append(degraded, ev)
		}
	}
	if len(degraded) != 1 {
		t.Fatalf("got %d tool_degraded events, want 1 (all events: %+v)", len(degraded), evs)
	}
	if got := degraded[0].Fields["probe"]; got != "evidence" {
		t.Errorf("probe field = %v, want evidence", got)
	}
	if degraded[0].Project != "demo" {
		t.Errorf("Project = %q, want demo", degraded[0].Project)
	}
}

func TestToolHealthEvidence_NoActivityNoEvents(t *testing.T) {
	eventsDir := t.TempDir()
	s := NewToolHealthEvidenceScanner(evidenceProjects, NewEventSink(eventsDir))
	s.SetActivityProbe(func(project string) bool { return false })
	s.SetHindsightProbe(func(project string) (int, error) { return 0, nil })

	s.scan()

	if evs := readEvidenceEvents(t, eventsDir); len(evs) != 0 {
		t.Fatalf("zero worker activity must emit no evidence-tier events, got %+v", evs)
	}
}

func TestToolHealthEvidence_ActivityWithMemoriesNoDegradation(t *testing.T) {
	eventsDir := t.TempDir()
	s := NewToolHealthEvidenceScanner(evidenceProjects, NewEventSink(eventsDir))
	s.SetActivityProbe(func(project string) bool { return true })
	s.SetHindsightProbe(func(project string) (int, error) { return 7, nil })

	s.scan()

	for _, ev := range readEvidenceEvents(t, eventsDir) {
		if ev.Type == "tool_degraded" {
			t.Fatalf("memories present must not degrade: %+v", ev)
		}
	}
}

func TestToolHealthEvidence_ProbeErrorFailOpen(t *testing.T) {
	eventsDir := t.TempDir()
	s := NewToolHealthEvidenceScanner(evidenceProjects, NewEventSink(eventsDir))
	s.SetActivityProbe(func(project string) bool { return true })
	s.SetHindsightProbe(func(project string) (int, error) { return 0, os.ErrPermission })

	s.scan()

	for _, ev := range readEvidenceEvents(t, eventsDir) {
		if ev.Type == "tool_degraded" {
			t.Fatalf("probe error must fail open (no degradation event): %+v", ev)
		}
	}
}
