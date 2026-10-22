package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// RED tests pinning the Bounded close-hold contract (FR-004, fleet-loop-
// degradation): a self-reported done:false verdict must hold the close,
// persist the hold count as the close-hold-N label, emit the typed
// worker_close_held event, and at the cap close anyway with forced:true.
// Malformed verdict JSON is ignored: close proceeds exactly as today.

func TestCloseHoldFor(t *testing.T) {
	tests := []struct {
		name string
		v    *Verdict
		want bool
	}{
		{"nil verdict fails open", nil, false},
		{"done verdict closes as today", &Verdict{Done: true, Summary: "all met"}, false},
		{"done:false with summary holds", &Verdict{Done: false, Summary: "one AC unmet"}, true},
		{"done:false without summary is no closure", &Verdict{Done: false}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := closeHoldFor(tc.v); got != tc.want {
				t.Errorf("closeHoldFor(%+v) = %v, want %v", tc.v, got, tc.want)
			}
		})
	}
}

func TestCloseHoldCount(t *testing.T) {
	tests := []struct {
		name   string
		labels []string
		want   int
	}{
		{"no labels", nil, 0},
		{"unrelated labels only", []string{"p0", "fix-attempted"}, 0},
		{"close-hold-1", []string{"close-hold-1"}, 1},
		{"takes the max of several", []string{"close-hold-1", "close-hold-2"}, 2},
		{"malformed suffix ignored", []string{"close-hold-abc", "close-hold-0"}, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := closeHoldCount(tc.labels); got != tc.want {
				t.Errorf("closeHoldCount(%v) = %d, want %d", tc.labels, got, tc.want)
			}
		})
	}
}

func TestCloseHeldRecordFields(t *testing.T) {
	// Below the cap: fields bead/hold_count/verdict_summary present, forced absent/false.
	rec := closeHeldRecord("proj", "hydra-8uw4", 1, "one AC unmet", false)
	if rec.Type != "worker_close_held" {
		t.Errorf("type = %q, want worker_close_held", rec.Type)
	}
	if rec.Fields["bead"] != "hydra-8uw4" {
		t.Errorf("bead = %v, want hydra-8uw4", rec.Fields["bead"])
	}
	if rec.Fields["hold_count"] != 1 {
		t.Errorf("hold_count = %v, want 1", rec.Fields["hold_count"])
	}
	if rec.Fields["verdict_summary"] != "one AC unmet" {
		t.Errorf("verdict_summary = %v, want one AC unmet", rec.Fields["verdict_summary"])
	}
	if forced, _ := rec.Fields["forced"].(bool); forced {
		t.Errorf("forced = true below the cap, want false/absent")
	}

	// At the cap the event carries forced: true.
	forcedRec := closeHeldRecord("proj", "hydra-8uw4", 2, "still unmet", true)
	if forced, _ := forcedRec.Fields["forced"].(bool); !forced {
		t.Errorf("forced = %v at the cap, want true", forcedRec.Fields["forced"])
	}
}

func TestCloseHoldEventMarshals(t *testing.T) {
	raw, err := json.Marshal(closeHeldRecord("proj", "hydra-8uw4", 1, "s", false))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var dec map[string]any
	if err := json.Unmarshal(raw, &dec); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if dec["type"] != "worker_close_held" {
		t.Errorf("type = %v, want worker_close_held", dec["type"])
	}
	if dec["time"] == nil || dec["time"] == "" {
		t.Errorf("time missing: %v", dec)
	}
	fields, ok := dec["fields"].(map[string]any)
	if !ok {
		t.Fatalf("fields missing or wrong shape: %v", dec["fields"])
	}
	if fields["hold_count"] != float64(1) {
		t.Errorf("hold_count = %v, want 1", fields["hold_count"])
	}
}

func TestEmitCloseHeldEventWritesCentralStream(t *testing.T) {
	home := t.TempDir()
	emitCloseHeldEvent(home, "proj", "hydra-8uw4", 1, "one AC unmet", false)
	// appendEventLine writes under the central events dir; find the record.
	var found []byte
	_ = filepath.WalkDir(home, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".jsonl") {
			return nil
		}
		data, rerr := os.ReadFile(path)
		if rerr == nil && strings.Contains(string(data), "worker_close_held") {
			found = data
		}
		return nil
	})
	if len(found) == 0 {
		t.Fatalf("no worker_close_held record written under %s", home)
	}
	if !strings.Contains(string(found), `"hold_count":1`) {
		t.Errorf("record missing hold_count 1: %s", found)
	}
}

func TestEmitCloseHeldEventFailOpenWithoutHydraHome(t *testing.T) {
	// Empty HYDRA_HOME must be a no-op, not a panic or a write attempt.
	emitCloseHeldEvent("", "proj", "hydra-8uw4", 1, "s", false)
}

func TestRunCloseHoldHoldsBelowCap(t *testing.T) {
	// First hold: event emitted, hold returned (caller does NOT close).
	bead := types.Bead{ID: types.BeadID("hydra-8uw4"), Project: "hydra", Labels: []string{"p0"}}
	projectPath := t.TempDir() // not a beads workspace: label write fails, must be non-fatal
	held := runCloseHold(bead, projectPath, &Verdict{Done: false, Summary: "one AC unmet"})
	if !held {
		t.Errorf("first done:false hold must hold the close (bead stays open), got held=false")
	}
}

func TestRunCloseHoldForcedAtCap(t *testing.T) {
	// holdCount == maxCloseHolds: worker closes anyway (fail-open) with forced:true.
	bead := types.Bead{ID: types.BeadID("hydra-8uw4"), Project: "hydra", Labels: []string{"close-hold-1"}}
	projectPath := t.TempDir()
	held := runCloseHold(bead, projectPath, &Verdict{Done: false, Summary: "still unmet after retry"})
	if held {
		t.Errorf("at hold_count == %d the worker must close anyway (forced), got held=true", maxCloseHolds)
	}
}

func TestRunCloseHoldNilVerdictNeverHolds(t *testing.T) {
	// Defense in depth at the close site: nil verdict fails open.
	if closeHoldFor(nil) {
		t.Errorf("nil verdict must not trigger a hold")
	}
}

func TestCloseWithHold_HeldPathReturnsNil(t *testing.T) {
	// done:false verdict: closeWithHold must hold the close (return nil),
	// leaving the bead open — it must NOT call the close path.
	bead := types.Bead{ID: types.BeadID("test-bead"), Project: "test-proj"}
	v := &Verdict{Done: false, Summary: "one AC unmet"}
	err := closeWithHold(bead, t.TempDir(), "test-repo", "completed", v)
	if err != nil {
		t.Errorf("closeWithHold on held path must return nil (bead stays open), got %v", err)
	}
}

func TestCloseWithHold_FallThroughWhenDone(t *testing.T) {
	// done:true verdict: closeHoldFor returns false, so closeWithHold falls
	// through to closeBeadFromWorker. HYDRA_DAEMON_GATES=1 makes that path
	// return nil without touching the real br CLI.
	t.Setenv("HYDRA_DAEMON_GATES", "1")
	bead := types.Bead{ID: types.BeadID("test-bead-ft"), Project: "test-proj"}
	v := &Verdict{Done: true, Summary: "all met"}
	if err := closeWithHold(bead, t.TempDir(), "test-repo", "completed", v); err != nil {
		t.Errorf("closeWithHold fall-through path (done verdict) must return nil, got %v", err)
	}
}
