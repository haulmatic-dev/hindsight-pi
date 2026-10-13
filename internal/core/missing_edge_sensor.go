package core

import (
	"fmt"
	"path/filepath"
	"sync"
	"time"
)

// MissingEdgeAlertThreshold is the per-hour missing-edge rate above which the
// sensor raises an alert (typed event + alert bead). 2026-10-02 saw 23,201
// "Dependency not found" events in one day (~967/hr at peak) before edge
// pruning landed; a healthy rate after pruning should be near zero.
const MissingEdgeAlertThreshold = 100

// missingEdgeWindow is the rate-measurement window (hourly, per AC).
const missingEdgeWindow = time.Hour

// missingEdgeAlertCooldown bounds alert noise: at most one alert bead per
// window even when the rate stays over threshold.
const missingEdgeAlertCooldown = time.Hour

// missingEdgeEventSink is the narrow event surface the sensor needs; *EventSink
// satisfies it.
type missingEdgeEventSink interface {
	EmitRepo(repoPath string, ev Event) Event
}

// MissingEdgeSensor tracks the hourly rate of DAG missing-edge prunes and
// raises a typed event + alert bead when the rate breaches the threshold.
// It is fail-open: nil sensor / nil sink / filer errors never affect the DAG
// build. Not safe for use before Start of the daemon lifecycle; guarded by a
// mutex for concurrent Build ticks.
type MissingEdgeSensor struct {
	mu sync.Mutex

	windowStart time.Time
	count       int

	lastAlert time.Time

	sink  missingEdgeEventSink
	filer func(projectPath, title, description string) error

	lastProjectPath string

	// clock is overridable for tests.
	clock func() time.Time
}

// NewMissingEdgeSensor wires the sensor to the daemon event sink and the
// pan bead filer (both optional).
func NewMissingEdgeSensor(sink missingEdgeEventSink, filer func(projectPath, title, description string) error) *MissingEdgeSensor {
	return &MissingEdgeSensor{
		sink:        sink,
		filer:       filer,
		windowStart: time.Now().UTC(),
		clock:       func() time.Time { return time.Now().UTC() },
	}
}

func (s *MissingEdgeSensor) now() time.Time {
	if s.clock != nil {
		return s.clock()
	}
	return time.Now().UTC()
}

// OnMissingEdge records one pruned missing edge (dependent depends on a
// blocker absent from the DAG build) and evaluates the hourly rate.
func (s *MissingEdgeSensor) OnMissingEdge(projectPath, dependent, blocker string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.now()
	s.rollWindowLocked(now)

	s.count++
	if projectPath != "" {
		s.lastProjectPath = projectPath
	}

	if s.sink != nil {
		s.sink.EmitRepo(projectPath, Event{
			Type:    "dag_missing_edge",
			Project: filepath.Base(projectPath),
			Bead:    dependent,
			Reason:  "blocker_absent",
			Detail:  fmt.Sprintf("dependency edge pruned: %s -> %s (blocker not in DAG build)", dependent, blocker),
			Fields:  map[string]any{"blocker": blocker},
		})
	}

	s.maybeAlertLocked(now, dependent, blocker)
}

// HourlyCheck closes any expired window, emitting the hourly rate event. It is
// called once per DAG build; a build tick with zero missing edges still rolls
// the window so the hourly cadence holds.
func (s *MissingEdgeSensor) HourlyCheck() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rollWindowLocked(s.now())
}

// rollWindowLocked emits the hourly summary and resets when the window
// expired. Caller holds s.mu.
func (s *MissingEdgeSensor) rollWindowLocked(now time.Time) {
	if now.Sub(s.windowStart) < missingEdgeWindow {
		return
	}
	if s.sink != nil && s.count > 0 {
		s.sink.EmitRepo(s.lastProjectPath, Event{
			Type:    "dag_missing_edge_rate",
			Project: filepath.Base(s.lastProjectPath),
			Reason:  "hourly_summary",
			Detail:  fmt.Sprintf("missing-edge prunes in the last hour: %d (threshold %d)", s.count, MissingEdgeAlertThreshold),
			Fields: map[string]any{
				"count":     s.count,
				"window":    "1h",
				"threshold": MissingEdgeAlertThreshold,
			},
		})
	}
	s.windowStart = now
	s.count = 0
}

// maybeAlertLocked raises the alert event and files an alert bead when the
// current window's rate breaches the threshold and the cooldown elapsed.
// Caller holds s.mu.
func (s *MissingEdgeSensor) maybeAlertLocked(now time.Time, dependent, blocker string) {
	if s.count <= MissingEdgeAlertThreshold {
		return
	}
	if !s.lastAlert.IsZero() && now.Sub(s.lastAlert) < missingEdgeAlertCooldown {
		return
	}
	s.lastAlert = now

	projectPath := s.lastProjectPath
	if s.sink != nil {
		s.sink.EmitRepo(projectPath, Event{
			Type:    "dag_missing_edge_rate_alert",
			Project: filepath.Base(projectPath),
			Bead:    dependent,
			Reason:  "rate_threshold",
			Detail: fmt.Sprintf("missing-edge rate %d/hr exceeds threshold %d/hr — chains are being severed repeatedly (latest: %s -> %s)",
				s.count, MissingEdgeAlertThreshold, dependent, blocker),
			Fields: map[string]any{"count": s.count, "threshold": MissingEdgeAlertThreshold, "blocker": blocker},
		})
	}
	if s.filer != nil {
		_ = s.filer(projectPath, fmt.Sprintf("DAG missing-edge rate alert: %d/hr (threshold %d)", s.count, MissingEdgeAlertThreshold),
			fmt.Sprintf("Sensor observed %d pruned missing dependency edges in the current hour window (threshold %d). "+
				"Latest edge: %s -> %s. Investigate out-of-order closes severing chains; edges are pruned so dispatch is not wedged, but the blocker beads are missing from the DAG build.", s.count, MissingEdgeAlertThreshold, dependent, blocker))
	}
}
