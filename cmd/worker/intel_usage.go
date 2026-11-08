package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// intelUsageEvent mirrors the daemon-side core.Event JSONL contract
// ({"time","type","project","bead","fields"}) so the central events stream
// parses worker-emitted records identically. Workers write their own records
// because they run in a separate process from the daemon's EventSink, and the
// intel_usage record must be stamped before the agent invocation — a fact only
// the worker can attest.
type intelUsageEvent struct {
	Time    string         `json:"time"`
	Type    string         `json:"type"`
	Project string         `json:"project,omitempty"`
	Bead    string         `json:"bead,omitempty"`
	Fields  map[string]any `json:"fields,omitempty"`
}

// emitIntelUsageEvent writes the intel_usage record to the central event
// stream ($HYDRA_HOME/events/YYYY-MM-DD.jsonl) before the agent invocation.
// Fail-open: a missing HYDRA_HOME or write error logs and continues — the
// event stream observes the system and must never break execution.
func emitIntelUsageEvent(hydraHome, project string, bead types.Bead, hindsight, cass, graphifyFallback bool) {
	if hydraHome == "" {
		return
	}
	ev := intelUsageRecord(project, string(bead.ID), hindsight, cass, graphifyFallback)
	line, err := json.Marshal(ev)
	if err != nil {
		log.Printf("intel_usage: marshal failed: %v", err)
		return
	}
	if err := appendEventLine(hydraHome, line); err != nil {
		log.Printf("intel_usage: append failed: %v", err)
	}
}

// intelUsageRecord builds a worker-emitted intel_usage record with the
// fields hindsight / cass / graphify_fallback.
func intelUsageRecord(project, bead string, hindsight, cass, graphifyFallback bool) intelUsageEvent {
	return intelUsageEvent{
		Time:    time.Now().UTC().Format("2006-01-02T15:04:05.000Z07:00"),
		Type:    "intel_usage",
		Project: project,
		Bead:    bead,
		Fields: map[string]any{
			"hindsight":         hindsight,
			"cass":              cass,
			"graphify_fallback": graphifyFallback,
		},
	}
}

// appendEventLine appends one JSONL record to the central event stream
// ($HYDRA_HOME/events/YYYY-MM-DD.jsonl). A write error is returned for
// logging only — the event stream observes the system and must never
// break execution.
func appendEventLine(hydraHome string, line []byte) error {
	eventsDir := filepath.Join(hydraHome, "events")
	if err := os.MkdirAll(eventsDir, 0o755); err != nil {
		return fmt.Errorf("events dir: %w", err)
	}
	path := filepath.Join(eventsDir, time.Now().UTC().Format("2006-01-02")+".jsonl")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open events file: %w", err)
	}
	defer f.Close()
	if _, err := f.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("append: %w", err)
	}
	return nil
}

// graphifyFallbackFor reports whether the worker has no graphify graph
// available for the project — the condition under which agents fall back to
// reading files directly instead of querying the code graph.
func graphifyFallbackFor(projectPath string) bool {
	_, err := os.Stat(filepath.Join(projectPath, "graphify-out", "graph.json"))
	return err != nil
}
