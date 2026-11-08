package core

// Integration tests for the tool-health degradation drill (parent AC:
// "go test ./internal/core/ -run 'TestToolHealth' -count=1" exits 0).
//
// Per parent AC, the event sink and the bead-filing dedup store are NOT
// mocked: the tests drive the real EventSink (reading back the central JSONL
// day file) and the real file-backed ToolHealthDedupStore. Only the BeadsClient
// is a stub (probe-borne errors are injected by stubbed probe functions).

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// toolHealthTestBeads is a minimal BeadsClient recording CreateBead calls; the
// real bead store is not required because only the dedup store must be real.
type toolHealthTestBeads struct {
	created []types.CreateBeadRequest
	ids     []string
	next    int
}

func (b *toolHealthTestBeads) ListBeads(projectPath, repoName string) ([]types.Bead, error) {
	return nil, nil
}
func (b *toolHealthTestBeads) GetBead(projectPath, repoName string, beadID types.BeadID) (*types.Bead, error) {
	return nil, fmt.Errorf("not found")
}
func (b *toolHealthTestBeads) CloseBead(projectPath, repoName string, beadID types.BeadID, reason string) error {
	return nil
}
func (b *toolHealthTestBeads) GetReadyBeads(projectName, projectPath, repoName string) ([]types.Bead, error) {
	return nil, nil
}
func (b *toolHealthTestBeads) UpdateBeadStatus(projectPath string, beadID types.BeadID, status types.BeadStatus) error {
	return nil
}
func (b *toolHealthTestBeads) CreateBead(projectPath string, req types.CreateBeadRequest) (types.BeadID, error) {
	b.created = append(b.created, req)
	b.next++
	id := types.BeadID(fmt.Sprintf("p1-tool-%d", b.next))
	b.ids = append(b.ids, string(id))
	return id, nil
}
func (b *toolHealthTestBeads) UpdateBeadLabels(projectPath, repoName string, beadID types.BeadID, labels []string) error {
	return nil
}
func (b *toolHealthTestBeads) RemoveBeadLabels(projectPath, repoName string, beadID types.BeadID, labels []string) error {
	return nil
}
func (b *toolHealthTestBeads) UpdateBeadDescription(projectPath, repoName string, beadID types.BeadID, description string) error {
	return nil
}
func (b *toolHealthTestBeads) UpdateBeadAgentContext(projectPath, repoName string, beadID types.BeadID, ctxJSON string) error {
	return nil
}
func (b *toolHealthTestBeads) AddDependency(projectPath, repoName string, beadID, dependsOn types.BeadID) error {
	return nil
}

// readToolHealthEvents drains the real sink's central JSONL day file.
func readToolHealthEvents(t *testing.T, dir string) []Event {
	t.Helper()
	path := filepath.Join(dir, "2026-07-29.jsonl")
	evs, err := ReadEvents(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatalf("ReadEvents(%s): %v", path, err)
	}
	return evs
}

// countTypes returns the number of events of each of the given types.
func countTypes(evs []Event, types ...string) map[string]int {
	out := map[string]int{}
	for _, ev := range evs {
		for _, ty := range types {
			if ev.Type == ty {
				out[ty]++
			}
		}
	}
	return out
}

// toolHealthFixture builds a monitor wired to a real EventSink and a real
// file-backed dedup store in temp dirs.
func toolHealthFixture(t *testing.T) (*ToolHealthMonitor, *toolHealthTestBeads, string) {
	t.Helper()
	eventsDir := t.TempDir()
	sink := NewEventSink(eventsDir)
	sink.now = func() time.Time { return time.Date(2026, 7, 29, 10, 30, 0, 0, time.UTC) }
	beads := &toolHealthTestBeads{}
	dedupPath := filepath.Join(t.TempDir(), "tool-health-dedup.json")
	m := NewToolHealthMonitor(beads, "/tmp/proj", sink, NewToolHealthDedupStore(dedupPath))
	return m, beads, eventsDir
}

// TestToolHealthDegradationDrill is the end-to-end drill: a stubbed probe
// returning an error produces a tool_degraded event and exactly one fix bead;
// repeated failing probes produce no duplicate bead (durable dedup); restoring
// the probe produces a tool_recovered event and clears the dedup entry so the
// next degradation episode files a fresh fix bead.
func TestToolHealthDegradationDrill(t *testing.T) {
	m, beads, eventsDir := toolHealthFixture(t)

	fail := false
	m.RegisterProbe("bv", func() error {
		if fail {
			return errors.New("connection refused")
		}
		return nil
	})

	// Probe 1: healthy -> no events, no beads.
	m.RunProbes()
	if got := countTypes(readToolHealthEvents(t, eventsDir), "tool_degraded", "tool_recovered"); len(got) != 0 {
		t.Fatalf("healthy probe emitted events: %v", got)
	}
	if len(beads.created) != 0 {
		t.Fatalf("healthy probe filed beads: %d", len(beads.created))
	}

	// Probe 2: failure -> tool_degraded + exactly one fix bead.
	fail = true
	m.RunProbes()
	evs := readToolHealthEvents(t, eventsDir)
	got := countTypes(evs, "tool_degraded", "tool_recovered")
	if got["tool_degraded"] != 1 || got["tool_recovered"] != 0 {
		t.Fatalf("after first failure: %v, want exactly one tool_degraded", got)
	}
	if len(beads.created) != 1 {
		t.Fatalf("after first failure: %d fix beads filed, want exactly 1", len(beads.created))
	}
	if beads.created[0].Title != "tool-health: bv probe failing — investigate and repair" {
		t.Errorf("fix bead title = %q", beads.created[0].Title)
	}
	if !hasLabel(beads.created[0].Labels, "tool-degraded") {
		t.Errorf("fix bead labels = %v, want tool-degraded", beads.created[0].Labels)
	}

	// Probe 3: still failing -> no duplicate tool_degraded, no second bead.
	m.RunProbes()
	m.RunProbes()
	evs = readToolHealthEvents(t, eventsDir)
	got = countTypes(evs, "tool_degraded", "tool_recovered")
	if got["tool_degraded"] != 1 {
		t.Fatalf("still-failing probes emitted %d tool_degraded, want 1 (deduped)", got["tool_degraded"])
	}
	if len(beads.created) != 1 {
		t.Fatalf("still-failing probes filed %d fix beads, want exactly 1", len(beads.created))
	}

	// Probe 4: recovery -> tool_recovered, dedup entry cleared.
	fail = false
	m.RunProbes()
	evs = readToolHealthEvents(t, eventsDir)
	got = countTypes(evs, "tool_degraded", "tool_recovered")
	if got["tool_recovered"] != 1 {
		t.Fatalf("recovery emitted %d tool_recovered, want exactly 1", got["tool_recovered"])
	}
	has, err := m.dedup.Has("bv")
	if err != nil || has {
		t.Fatalf("dedup entry after recovery: has=%v err=%v, want cleared", has, err)
	}

	// Probe 5: fresh degradation episode -> second (deduped-then-new) fix bead.
	fail = true
	m.RunProbes()
	if len(beads.created) != 2 {
		t.Fatalf("second episode filed %d fix beads total, want 2", len(beads.created))
	}
	got = countTypes(readToolHealthEvents(t, eventsDir), "tool_degraded")
	if got["tool_degraded"] != 2 {
		t.Fatalf("second episode emitted %d tool_degraded, want 2", got["tool_degraded"])
	}
}

// TestToolHealthDedupStorePersistence pins the durable dedup store: the entry
// survives a new store instance over the same path (daemon restart semantics).
func TestToolHealthDedupStorePersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dedup.json")
	s1 := NewToolHealthDedupStore(path)
	if err := s1.Add("br", "p1-abc"); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if has, _ := s1.Has("br"); !has {
		t.Fatal("Has right after Add: false")
	}
	// New instance over the same path = post-restart state.
	s2 := NewToolHealthDedupStore(path)
	has, err := s2.Has("br")
	if err != nil || !has {
		t.Fatalf("durable Has after restart: has=%v err=%v", has, err)
	}
	if err := s2.Remove("br"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if has, _ := s2.Has("br"); has {
		t.Fatal("Has after Remove: true")
	}
	// Missing file is a clean empty store.
	s3 := NewToolHealthDedupStore(filepath.Join(t.TempDir(), "absent.json"))
	if has, err := s3.Has("br"); has || err != nil {
		t.Fatalf("missing-file Has: has=%v err=%v, want false/nil", has, err)
	}
}

// TestToolHealthFailOpen pins the fail-open contract: a CreateBead failure must
// not panic the monitor and the degradation event must still be emitted.
func TestToolHealthFailOpen(t *testing.T) {
	eventsDir := t.TempDir()
	sink := NewEventSink(eventsDir)
	sink.now = func() time.Time { return time.Date(2026, 7, 29, 10, 30, 0, 0, time.UTC) }
	dedup := NewToolHealthDedupStore(filepath.Join(t.TempDir(), "d.json"))
	m := NewToolHealthMonitor(&failCreateBeads{}, "/tmp/proj", sink, dedup)
	m.RegisterProbe("mcp", func() error { return errors.New("boom") })
	m.RunProbes() // must not panic despite bead filing failure
	got := countTypes(readToolHealthEvents(t, eventsDir), "tool_degraded")
	if got["tool_degraded"] != 1 {
		t.Fatalf("tool_degraded count = %d, want 1 (event must survive filing failure)", got["tool_degraded"])
	}
}

// failCreateBeads is a BeadsClient whose CreateBead always fails.
type failCreateBeads struct{ toolHealthTestBeads }

func (failCreateBeads) CreateBead(projectPath string, req types.CreateBeadRequest) (types.BeadID, error) {
	return "", errors.New("store unavailable")
}
