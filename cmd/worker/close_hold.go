package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/haulmatic-dev/hydra/internal/beads"
	"github.com/haulmatic-dev/hydra/pkg/types"
)

// Bounded close-hold on self-reported `done:false` (FR-004, fleet-loop-degradation):
// when the agent's own verdict says the task is NOT done, the worker must not
// close the bead as completed — a done:false verdict is an honest unmet claim,
// and auto-closing it as "completed" promotes unmet work to terminal state.
// Instead the worker holds the close: it emits a typed `worker_close_held`
// event, persists the hold count as the label `close-hold-N`, and leaves the
// bead open for the retry loop. The hold is BOUNDED: at holdCount == 2 the
// worker closes anyway (fail-open path) with `forced: true` in the event, so
// a permanently failing bead cannot wedge the pipeline. Malformed verdict JSON
// is ignored entirely: close proceeds exactly as today (fail-open).

const (
	// closeHoldLabelPrefix is the label prefix persisting the hold count on
	// the bead (`close-hold-1`, `close-hold-2`, ...). Only one is present at
	// a time — the previous label is replaced on each hold.
	closeHoldLabelPrefix = "close-hold-"

	// maxCloseHolds is the bounded-hold ceiling: at this count the worker
	// closes anyway (forced). 1 hold = skipped close; 2nd = forced close.
	maxCloseHolds = 2

	// closeHoldEventType is the typed event written to the central stream.
	closeHoldEventType = "worker_close_held"
)

// closeHoldEvent mirrors the daemon-side core.Event JSONL contract, exactly
// like intelUsageEvent — workers run in a separate process from the daemon's
// EventSink, so they stamp their own records into the central stream.
type closeHoldEvent struct {
	Time    string         `json:"time"`
	Type    string         `json:"type"`
	Project string         `json:"project,omitempty"`
	Bead    string         `json:"bead,omitempty"`
	Fields  map[string]any `json:"fields,omitempty"`
}

// closeHoldFor reports whether this verdict triggers a close-hold: a
// self-reported done:false with a summary. A nil verdict (malformed JSON,
// never emitted) fails open — the caller closes exactly as today. A done
// verdict is a success closure and never holds. done:false WITHOUT a summary
// is no closure either (the nudge loop already handles that shape), so it
// does not hold either.
func closeHoldFor(v *Verdict) bool {
	return v != nil && !v.Done && v.Summary != ""
}

// closeHoldCount returns the hold count persisted on the bead's labels, 0
// when no hold label is present or the number suffix is malformed.
func closeHoldCount(labels []string) int {
	count := 0
	for _, label := range labels {
		if !strings.HasPrefix(label, closeHoldLabelPrefix) {
			continue
		}
		n, err := strconv.Atoi(strings.TrimPrefix(label, closeHoldLabelPrefix))
		if err != nil || n <= 0 {
			continue
		}
		if n > count {
			count = n
		}
	}
	return count
}

// closeHeldRecord builds the typed worker_close_held event record with the
// contract fields {bead, hold_count, verdict_summary} plus `forced` on the
// fail-open (cap-reached) path.
func closeHeldRecord(project, beadID string, holdCount int, verdictSummary string, forced bool) closeHoldEvent {
	return closeHoldEvent{
		Time:    time.Now().UTC().Format("2006-01-02T15:04:05.000Z07:00"),
		Type:    closeHoldEventType,
		Project: project,
		Bead:    beadID,
		Fields: map[string]any{
			"bead":           beadID,
			"hold_count":     holdCount,
			"verdict_summary": verdictSummary,
			"forced":         forced,
		},
	}
}

// emitCloseHeldEvent writes the worker_close_held record to the central
// event stream. Fail-open: a missing HYDRA_HOME or write error logs and
// continues — the event stream observes the system and must never break
// execution.
func emitCloseHeldEvent(hydraHome, project string, beadID string, holdCount int, verdictSummary string, forced bool) {
	if hydraHome == "" {
		return
	}
	line, err := json.Marshal(closeHeldRecord(project, beadID, holdCount, verdictSummary, forced))
	if err != nil {
		log.Printf("close-hold: marshal failed: %v", err)
		return
	}
	if err := appendEventLine(hydraHome, line); err != nil {
		log.Printf("close-hold: append failed: %v", err)
	}
}

// runCloseHold decides what happens at the close site when the agent
// self-reported done:false.
//
// Below the cap: emit the hold event, persist the incremented hold label
// (`close-hold-N`, replacing any previous hold label), and return true — the
// caller must NOT close the bead; it stays open for the retry loop.
//
// At the cap (holdCount >= maxCloseHolds): emit the event with forced:true
// and return false — the caller closes anyway (fail-open path).
//
// Every side effect (label writes, event write) is fail-open: errors are
// logged and the close-decision still comes from the persisted count we
// could read, so a label write failure can at worst repeat a hold, never
// close a bead the agent said is not done.
func runCloseHold(bead types.Bead, projectPath string, v *Verdict) (held bool) {
	holdCount := closeHoldCount(bead.Labels) + 1
	forced := holdCount >= maxCloseHolds
	emitCloseHeldEvent(os.Getenv("HYDRA_HOME"), string(bead.Project), string(bead.ID), holdCount, v.Summary, forced)
	if forced {
		log.Printf("[Worker:close-hold] %s hold_count=%d (cap %d) — closing anyway (forced)", bead.ID, holdCount, maxCloseHolds)
		return false
	}
	if err := persistCloseHoldLabel(bead, projectPath, holdCount); err != nil {
		log.Printf("[WARN] close-hold: label persistence failed (non-fatal): %v", err)
	}
	log.Printf("[Worker:close-hold] %s self-reported done:false (hold %d/%d) — bead stays open", bead.ID, holdCount, maxCloseHolds)
	return true
}

// persistCloseHoldLabel replaces any previous close-hold label with
// close-hold-<holdCount> via the br CLI, run in the project directory. The
// old label is removed first so the ladder holds exactly one hold label.
func persistCloseHoldLabel(bead types.Bead, projectPath string, holdCount int) error {
	beadID := bead.ID.String()
	if !beads.IsWorkspace(projectPath) {
		return fmt.Errorf("not a beads workspace: %s", projectPath)
	}
	for _, label := range bead.Labels {
		if strings.HasPrefix(label, closeHoldLabelPrefix) {
			remove := exec.Command("br", "label", "remove", beadID, "-l", label)
			remove.Dir = projectPath
			if out, err := remove.CombinedOutput(); err != nil {
				log.Printf("   [WARN] close-hold: label remove %s failed: %s", label, strings.TrimSpace(string(out)))
			}
		}
	}
	add := exec.Command("br", "label", "add", beadID, "-l", fmt.Sprintf("%s%d", closeHoldLabelPrefix, holdCount))
	add.Dir = projectPath
	if out, err := add.CombinedOutput(); err != nil {
		return fmt.Errorf("br label add %s%d failed: %s", closeHoldLabelPrefix, holdCount, strings.TrimSpace(string(out)))
	}
	return nil
}
