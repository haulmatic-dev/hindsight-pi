package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

func TestEmitIntelUsageEvent_WritesJSONLBeforeFields(t *testing.T) {
	home := t.TempDir()
	emitIntelUsageEvent(home, "demo", types.Bead{ID: "hydra-test"}, true, false, true)

	entries, err := os.ReadDir(filepath.Join(home, "events"))
	if err != nil || len(entries) != 1 {
		t.Fatalf("expected one events file, err=%v entries=%d", err, len(entries))
	}
	raw, err := os.ReadFile(filepath.Join(home, "events", entries[0].Name()))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var ev intelUsageEvent
	if err := json.Unmarshal(raw, &ev); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if ev.Type != "intel_usage" {
		t.Fatalf("type = %q", ev.Type)
	}
	if ev.Project != "demo" || ev.Bead != "hydra-test" {
		t.Fatalf("project/bead = %q/%q", ev.Project, ev.Bead)
	}
	if ev.Fields["hindsight"] != true || ev.Fields["cass"] != false || ev.Fields["graphify_fallback"] != true {
		t.Fatalf("fields = %+v", ev.Fields)
	}
	if !strings.Contains(ev.Time, "T") || ev.Time == "" {
		t.Fatalf("time stamp missing: %q", ev.Time)
	}
}

func TestEmitIntelUsageEvent_FailOpen(t *testing.T) {
	emitIntelUsageEvent("", "demo", types.Bead{ID: "x"}, true, false, false) // no HYDRA_HOME: no panic, no file
}

func TestGraphifyFallbackFor(t *testing.T) {
	dir := t.TempDir()
	if !graphifyFallbackFor(dir) {
		t.Fatalf("missing graph must report fallback=true")
	}
	if err := os.MkdirAll(filepath.Join(dir, "graphify-out"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "graphify-out", "graph.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if graphifyFallbackFor(dir) {
		t.Fatalf("present graph must report fallback=false")
	}
}
