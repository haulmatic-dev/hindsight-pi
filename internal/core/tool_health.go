package core

// Tool-health monitoring (degradation drill).
//
// The daemon depends on a set of sidecar tools (br, bv, provider APIs). When a
// tool starts failing silently the overseer keeps scheduling work against it
// and nobody notices until beads pile up. The monitor runs a registry of named
// probes and turns the health transitions into typed events:
//
//   - a probe that fails for the first time emits `tool_degraded` and files
//     exactly one fix bead per degradation episode (deduped durably);
//   - a previously degraded probe that passes again emits `tool_recovered`.
//
// Everything is fail-open: probe, event and filing errors never break the
// caller's tick.

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// ToolHealthProbe is one named liveness check for a daemon-side tool. Check
// returns a non-nil error when the tool is considered unhealthy.
type ToolHealthProbe struct {
	// Name is the stable tool identifier used in events, bead titles and the
	// dedup store (e.g. "br", "bv", "gptcache").
	Name string
	// Check reports the tool's health; a non-nil error means degraded.
	Check func() error
}

// ToolHealthDedupStore is the durable dedup store backing tool-degraded fix
// bead filing. It records, per tool, the fix bead filed for the current
// degradation episode so repeated failing probes cannot pile up duplicate
// beads, even across a daemon restart. The backing file is a JSON map of
// tool name -> fix bead ID.
type ToolHealthDedupStore struct {
	path string
	mu   sync.Mutex
}

// NewToolHealthDedupStore builds a dedup store persisted at path.
func NewToolHealthDedupStore(path string) *ToolHealthDedupStore {
	return &ToolHealthDedupStore{path: path}
}

// load reads the store; a missing file is an empty (nil) map, not an error.
func (s *ToolHealthDedupStore) load() (map[string]string, error) {
	if s == nil || s.path == "" {
		return nil, nil
	}
	raw, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var m map[string]string
	if len(raw) == 0 {
		return nil, nil
	}
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("tool-health dedup store corrupt at %s: %w", s.path, err)
	}
	return m, nil
}

// save atomically writes the store (write temp + rename).
func (s *ToolHealthDedupStore) save(m map[string]string) error {
	if s == nil || s.path == "" {
		return nil
	}
	raw, err := json.Marshal(m)
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// Has reports whether a fix bead is already filed for tool.
func (s *ToolHealthDedupStore) Has(tool string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, err := s.load()
	if err != nil {
		return false, err
	}
	_, ok := m[tool]
	return ok, nil
}

// Add records the fix bead filed for tool's degradation episode.
func (s *ToolHealthDedupStore) Add(tool, beadID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, err := s.load()
	if err != nil {
		return err
	}
	if m == nil {
		m = map[string]string{}
	}
	m[tool] = beadID
	return s.save(m)
}

// Remove clears the entry for tool (called on recovery so the next degradation
// episode files a fresh fix bead).
func (s *ToolHealthDedupStore) Remove(tool string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, err := s.load()
	if err != nil {
		return err
	}
	if _, ok := m[tool]; !ok {
		return nil
	}
	delete(m, tool)
	return s.save(m)
}

// ToolHealthMonitor runs a registry of tool probes and turns health
// transitions into typed events and deduped fix beads.
type ToolHealthMonitor struct {
	beads       BeadsClient
	projectPath string
	events      *EventSink
	dedup       *ToolHealthDedupStore

	probes   map[string]func() error
	degraded map[string]bool
	mu       sync.Mutex
}

// NewToolHealthMonitor builds a monitor. beads/projectPath are where fix beads
// are filed (both may be empty to observe-only), events is the daemon event
// sink (nil-safe) and dedup is the durable fix-bead dedup store.
func NewToolHealthMonitor(beads BeadsClient, projectPath string, events *EventSink, dedup *ToolHealthDedupStore) *ToolHealthMonitor {
	return &ToolHealthMonitor{
		beads:       beads,
		projectPath: projectPath,
		events:      events,
		dedup:       dedup,
		probes:      map[string]func() error{},
		degraded:    map[string]bool{},
	}
}

// RegisterProbe adds (or replaces) a named probe.
func (m *ToolHealthMonitor) RegisterProbe(name string, check func() error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.probes[name] = check
}

// RunProbes executes every registered probe once and applies the degraded /
// recovered transition logic to each result.
func (m *ToolHealthMonitor) RunProbes() {
	m.mu.Lock()
	probes := make(map[string]func() error, len(m.probes))
	for name, check := range m.probes {
		probes[name] = check
	}
	m.mu.Unlock()
	for name, check := range probes {
		m.runProbe(name, check)
	}
}

// runProbe applies the transition logic for one probe result. Transitions:
// healthy->degraded emits tool_degraded + files a fix bead (deduped);
// degraded->healthy emits tool_recovered and clears the dedup entry.
func (m *ToolHealthMonitor) runProbe(name string, check func() error) {
	err := check()

	m.mu.Lock()
	wasDegraded := m.degraded[name]
	if err != nil {
		m.degraded[name] = true
	} else {
		delete(m.degraded, name)
	}
	m.mu.Unlock()

	switch {
	case err != nil && !wasDegraded:
		m.events.Emit(Event{
			Type:   "tool_degraded",
			Reason: "probe_failed",
			Detail: fmt.Sprintf("tool %q degraded: probe failed: %v", name, err),
			Fields: map[string]any{"tool": name, "error": err.Error()},
		})
		m.fileFixBead(name, err)
	case err == nil && wasDegraded:
		m.events.Emit(Event{
			Type:   "tool_recovered",
			Reason: "probe_ok",
			Detail: fmt.Sprintf("tool %q recovered: probe healthy again", name),
			Fields: map[string]any{"tool": name},
		})
		// Recovery closes the episode: clear the dedup entry so the next
		// degradation files a fresh fix bead.
		if m.dedup != nil {
			_ = m.dedup.Remove(name)
		}
	}
}

// fileFixBead files exactly one fix bead for the tool's degradation episode,
// deduped through the durable store. Filing problems are fail-open: the
// tool_degraded event has already been emitted.
func (m *ToolHealthMonitor) fileFixBead(name string, probeErr error) {
	if m.dedup == nil {
		return
	}
	has, err := m.dedup.Has(name)
	if err != nil {
		return // cannot verify dedup; fail-open, do not risk duplicates
	}
	if has {
		return
	}
	if m.beads == nil || m.projectPath == "" {
		return
	}
	title := fmt.Sprintf("tool-health: %s probe failing — investigate and repair", name)
	id, err := m.beads.CreateBead(m.projectPath, types.CreateBeadRequest{
		Title: title,
		Description: fmt.Sprintf(
			"Automatically filed by the tool-health monitor: the %q probe is failing (%v).\n\nRecovery is automatic — the monitor emits tool_recovered when the probe passes again; this bead tracks investigation of the underlying failure.", name, probeErr),
		Type:     "task",
		Priority: 2,
		Labels:   []string{"tool-degraded"},
	})
	if err != nil {
		return
	}
	m.events.Emit(Event{
		Type:   "fix_created",
		Reason: "tool_degraded",
		Bead:   string(id),
		Detail: fmt.Sprintf("fix bead filed for degraded tool %q", name),
		Fields: map[string]any{"tool": name},
	})
	// Durable dedup: record AFTER the bead exists so a crash between create
	// and record files a duplicate rather than silently losing the fix.
	_ = m.dedup.Add(name, string(id))
}
