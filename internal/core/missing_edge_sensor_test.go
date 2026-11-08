package core

import (
	"testing"
	"time"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// fakeFiler records alert-bead filings.
type fakeFiler struct {
	titles []string
}

func (f *fakeFiler) file(projectPath, title, description string) error {
	f.titles = append(f.titles, title)
	return nil
}

// captureEmitSink records events emitted by the sensor.
type captureEmitSink struct {
	events []Event
}

func (c *captureEmitSink) EmitRepo(_ string, ev Event) Event {
	c.events = append(c.events, ev)
	return ev
}

// testSensor builds a sensor with a recording sink/filer; caller sets s.clock.
func testSensor(t *testing.T) (*MissingEdgeSensor, *captureEmitSink, *fakeFiler) {
	t.Helper()
	sink := &captureEmitSink{}
	filer := &fakeFiler{}
	s := NewMissingEdgeSensor(sink, filer.file)
	return s, sink, filer
}

// TestSensor_MissingEdgeEventsEmitted pins the typed dag_missing_edge event.
func TestSensor_MissingEdgeEventsEmitted(t *testing.T) {
	s, sink, _ := testSensor(t)
	s.clock = func() time.Time { return time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC) }

	s.OnMissingEdge("/tmp/proj", "refa-1", "synt-9")

	if len(sink.events) != 1 {
		t.Fatalf("want 1 event, got %d", len(sink.events))
	}
	ev := sink.events[0]
	if ev.Type != "dag_missing_edge" {
		t.Fatalf("type = %q, want dag_missing_edge", ev.Type)
	}
	if ev.Bead != "refa-1" || ev.Fields["blocker"] != "synt-9" {
		t.Fatalf("bad event: %+v", ev)
	}
}

// TestSensor_HourlyRollupAndAlert pins: alert event + alert bead over
// threshold, alert cooldown, hourly summary + window reset, no alert after
// reset.
func TestSensor_HourlyRollupAndAlert(t *testing.T) {
	base := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	s, sink, filer := testSensor(t)
	current := base
	s.clock = func() time.Time { return current }
	s.windowStart = base // deterministic window origin

	// Below threshold: no alert.
	for i := 0; i < MissingEdgeAlertThreshold; i++ {
		s.OnMissingEdge("/tmp/proj", "dep", "blocker")
	}
	if len(filer.titles) != 0 {
		t.Fatalf("alert filed below threshold: %d", len(filer.titles))
	}

	// Breach threshold: one alert + bead.
	s.OnMissingEdge("/tmp/proj", "dep", "blocker")
	if len(filer.titles) != 1 {
		t.Fatalf("want 1 alert bead, got %d", len(filer.titles))
	}
	alerts := 0
	for _, ev := range sink.events {
		if ev.Type == "dag_missing_edge_rate_alert" {
			alerts++
		}
	}
	if alerts != 1 {
		t.Fatalf("want 1 alert event, got %d", alerts)
	}

	// Within cooldown: no second alert.
	current = base.Add(10 * time.Minute)
	s.OnMissingEdge("/tmp/proj", "dep", "blocker")
	if len(filer.titles) != 1 {
		t.Fatalf("alert cooldown violated: %d", len(filer.titles))
	}

	// Roll the window: hourly summary emitted, count reset.
	current = base.Add(2 * missingEdgeWindow)
	s.HourlyCheck()
	summaries := 0
	for _, ev := range sink.events {
		if ev.Type == "dag_missing_edge_rate" {
			summaries++
			if ev.Fields["count"] == 0 {
				t.Fatalf("summary carried zero count: %+v", ev)
			}
		}
	}
	if summaries != 1 {
		t.Fatalf("want 1 hourly summary, got %d", summaries)
	}

	// After reset: below threshold again, no alert.
	current = current.Add(time.Second)
	s.OnMissingEdge("/tmp/proj", "dep", "blocker")
	if len(filer.titles) != 1 {
		t.Fatalf("alert after window reset: %d", len(filer.titles))
	}
}

// TestSensor_NilSafe pins fail-open behavior.
func TestSensor_NilSafe(t *testing.T) {
	var s *MissingEdgeSensor
	s.OnMissingEdge("/p", "a", "b") // must not panic
	s.HourlyCheck()
}

// TestSensor_HourlyCheckNoEventOnZero pins: zero-count windows emit nothing.
func TestSensor_HourlyCheckNoEventOnZero(t *testing.T) {
	base := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	s, sink, _ := testSensor(t)
	s.clock = func() time.Time { return base.Add(2 * missingEdgeWindow) }
	s.HourlyCheck()
	if len(sink.events) != 0 {
		t.Fatalf("want 0 events for empty window, got %d", len(sink.events))
	}
}

// TestDAGBuild_PrunesTerminalAndMissingEdges is the AC regression test:
// a chain REFACTOR <- GREEN <- SYNTAX (SYNTAX closed early) must not wedge
// REFACTOR dispatch, and edges to missing blockers must be pruned with a
// sensor event, not logged-and-kept.
func TestDAGBuild_PrunesTerminalAndMissingEdges(t *testing.T) {
	base := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	s, sink, _ := testSensor(t)
	s.clock = func() time.Time { return base }

	// mockBeadsClient keys beads by ID string; ListBeads filters by
	// bead.Project == filepath.Base(projectPath).
	syntax := types.Bead{ID: "synt-1", Project: "proj", Status: types.StatusClosed}
	green := types.Bead{ID: "gren-2", Project: "proj", Status: types.StatusOpen, Dependencies: []types.BeadID{"synt-1"}}
	refactor := types.Bead{ID: "refa-3", Project: "proj", Status: types.StatusOpen, Dependencies: []types.BeadID{"gren-2", "gone-4"}}

	client := newMockBeadsClient()
	client.beads["synt-1"] = syntax
	client.beads["gren-2"] = green
	client.beads["refa-3"] = refactor

	proj := types.Project{Name: "proj", Path: "/tmp/proj", Type: types.RepoTypeSingle}
	dag := NewDAG()
	b := NewDAGBuilder(NewErrorLogger(nil))
	b.Sensor = s
	if err := b.Build(dag, client, []types.Project{proj}, map[string]bool{}); err != nil {
		t.Fatalf("Build: %v", err)
	}

	ready := map[string]bool{}
	for _, n := range dag.GetReadyNodes() {
		ready[n.Bead.ID.String()] = true
	}
	if ready["refa-3"] {
		t.Fatalf("REFACTOR ready while GREEN still open — incorrect")
	}
	if !ready["gren-2"] {
		t.Fatalf("GREEN wedged by closed SYNTAX (terminal edge not pruned); ready=%v", ready)
	}

	// Simulate GREEN closing next tick: the next Build must dispatch REFACTOR
	// (no permanent wedge from the stale edges).
	client.beads["gren-2"] = types.Bead{ID: "gren-2", Project: "proj", Status: types.StatusClosed, Dependencies: []types.BeadID{"synt-1"}}
	dag2 := NewDAG()
	if err := b.Build(dag2, client, []types.Project{proj}, map[string]bool{}); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	ready2 := map[string]bool{}
	for _, n := range dag2.GetReadyNodes() {
		ready2[n.Bead.ID.String()] = true
	}
	if !ready2["refa-3"] {
		t.Fatalf("REFACTOR wedged after GREEN closed; ready=%v", ready2)
	}

	// Edge REFACTOR->gone-4 must be pruned; the REFACTOR->GREEN edge kept.
	deps, err := dag.GetDependencies("refa-3")
	if err != nil || len(deps) != 1 || deps[0] != "gren-2" {
		t.Fatalf("missing edge not pruned / live edge lost: deps=%v err=%v", deps, err)
	}
	// Missing-edge event recorded via the sensor.
	found := false
	for _, ev := range sink.events {
		if ev.Type == "dag_missing_edge" && ev.Bead == "refa-3" && ev.Fields["blocker"] == "gone-4" {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing-edge event not recorded: %+v", sink.events)
	}
}
