package core

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

func openBeads(n int, project string) []types.Bead {
	beads := make([]types.Bead, 0, n)
	for i := 0; i < n; i++ {
		beads = append(beads, types.Bead{
			ID:      types.BeadID(string(project) + "-" + string(rune('a'+i))),
			Project: project,
			Type:    "task",
			Status:  types.StatusOpen,
		})
	}
	return beads
}

func guardTestProject(t *testing.T) (types.Project, string) {
	t.Helper()
	path := t.TempDir()
	if err := os.MkdirAll(filepath.Join(path, ".beads", ".br_history"), 0o755); err != nil {
		t.Fatal(err)
	}
	return types.Project{Name: "proj", Path: path}, path
}

func writeSnapshot(t *testing.T, histDir, name, content string) string {
	t.Helper()
	p := filepath.Join(histDir, name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	// Stagger modtimes so newest-by-mtime is deterministic.
	future := time.Now().Add(time.Hour)
	for i := 0; i < len(name); i++ {
		future = future.Add(time.Millisecond)
	}
	_ = os.Chtimes(p, future, future)
	return p
}

func readEventsOfType(t *testing.T, sink *EventSink, evType string) []Event {
	t.Helper()
	events, err := ReadEvents(filepath.Join(sink.centralDir, time.Now().UTC().Format("2006-01-02")+".jsonl"))
	if err != nil {
		t.Fatalf("ReadEvents: %v", err)
	}
	var out []Event
	for _, ev := range events {
		if ev.Type == evType {
			out = append(out, ev)
		}
	}
	return out
}

func TestStoreGuard_WipeToZeroBlocksProject(t *testing.T) {
	project, _ := guardTestProject(t)
	g := NewBeadStoreGuard(func(string) (int, error) { return 0, errors.New("not used") })

	g.Check(project, openBeads(5, "proj"))
	if g.IsBlocked(project.Path) {
		t.Fatal("project blocked after first (baseline) tick — should only record baseline")
	}
	g.Check(project, nil) // wipe
	if !g.IsBlocked(project.Path) {
		t.Fatal("project NOT blocked after open count 5 -> 0 in one tick")
	}
	// Stays blocked on subsequent ticks (no re-trigger, no silent unblock).
	g.Check(project, openBeads(5, "proj"))
	if !g.IsBlocked(project.Path) {
		t.Fatal("project unblocked by a later tick while store unresolved — fail-closed violated")
	}
}

func TestStoreGuard_DropOver80PercentBlocks(t *testing.T) {
	project, _ := guardTestProject(t)
	g := NewBeadStoreGuard(func(string) (int, error) { return 0, errors.New("not used") })

	g.Check(project, openBeads(10, "proj"))
	g.Check(project, openBeads(1, "proj")) // 10 -> 1 = 90% drop
	if !g.IsBlocked(project.Path) {
		t.Fatal("project NOT blocked after 90% one-tick drop")
	}
}

func TestStoreGuard_GradualClosesNoSignal(t *testing.T) {
	project, _ := guardTestProject(t)
	g := NewBeadStoreGuard(func(string) (int, error) { return 0, errors.New("not used") })

	g.Check(project, openBeads(10, "proj"))
	for n := 9; n >= 5; n-- {
		g.Check(project, openBeads(n, "proj"))
	}
	if g.IsBlocked(project.Path) {
		t.Fatal("blocked after legitimate gradual closes 10 -> 5 over five ticks")
	}
}

func TestStoreGuard_ListFailureAfterBaselineBlocks(t *testing.T) {
	project, _ := guardTestProject(t)
	g := NewBeadStoreGuard(func(string) (int, error) { return 0, errors.New("not used") })

	g.Check(project, openBeads(7, "proj"))
	g.NoteListFailure(project, errors.New("br list: exit status 7"))
	if !g.IsBlocked(project.Path) {
		t.Fatal("project NOT blocked after list failure with healthy baseline (issues.jsonl deleted case)")
	}
}

func TestStoreGuard_AutoRestoreFromSnapshotVerifiesEffect(t *testing.T) {
	project, path := guardTestProject(t)
	histDir := filepath.Join(path, ".beads", ".br_history")
	snapshotContent := `{"id":"proj-a","title":"a","status":"open","type":"task"}`
	writeSnapshot(t, histDir, "issues.20260911_091150_1.jsonl", snapshotContent)

	// The verify step (re-list after restore) sees the restored count.
	g := NewBeadStoreGuard(func(string) (int, error) { return 47, nil })
	sink := NewEventSink(t.TempDir())
	g.SetEventSink(sink)

	g.Check(project, openBeads(47, "proj"))
	g.Check(project, nil) // wipe -> auto-restore fires

	if g.IsBlocked(project.Path) {
		t.Fatal("project still blocked after verified auto-restore")
	}
	restored, err := os.ReadFile(filepath.Join(path, ".beads", "issues.jsonl"))
	if err != nil {
		t.Fatalf("issues.jsonl not restored: %v", err)
	}
	if string(restored) != snapshotContent {
		t.Fatalf("restored content mismatch:\n got: %q\nwant: %q", restored, snapshotContent)
	}
	if evs := readEventsOfType(t, sink, "bead_store_restored"); len(evs) != 1 {
		t.Fatalf("expected 1 bead_store_restored event, got %d", len(evs))
	}
}

func TestStoreGuard_AutoRestoreFailsWithoutSnapshot_ThenExplicitRestoreUnblocks(t *testing.T) {
	project, path := guardTestProject(t) // .br_history exists but empty
	sink := NewEventSink(t.TempDir())

	// Verify step fails until a snapshot exists (fail-open: stay blocked).
	listOK := false
	g := NewBeadStoreGuard(func(string) (int, error) {
		if !listOK {
			return 0, errors.New("br list: no such file")
		}
		return 47, nil
	})
	g.SetEventSink(sink)

	g.Check(project, openBeads(47, "proj"))
	g.Check(project, nil) // wipe -> auto-restore attempted, no snapshot -> fails
	if !g.IsBlocked(project.Path) {
		t.Fatal("project unblocked despite failed restore — fail-open violated")
	}
	if evs := readEventsOfType(t, sink, "bead_store_restore_failed"); len(evs) != 1 {
		t.Fatalf("expected 1 bead_store_restore_failed event, got %d", len(evs))
	}
	if evs := readEventsOfType(t, sink, "bead_store_suspect"); len(evs) != 1 {
		t.Fatalf("expected 1 bead_store_suspect event, got %d", len(evs))
	}

	// Pan repair-bead-store path: snapshot appears, explicit Restore verifies.
	writeSnapshot(t, filepath.Join(path, ".beads", ".br_history"),
		"issues.20260911_091200_2.jsonl", `{"id":"proj-a","status":"open"}`)
	listOK = true
	if err := g.Restore(path); err != nil {
		t.Fatalf("explicit Restore failed: %v", err)
	}
	if g.IsBlocked(project.Path) {
		t.Fatal("project still blocked after verified explicit restore")
	}
}

func TestStoreGuard_RestoreRejectsUnrecoveredCount(t *testing.T) {
	project, path := guardTestProject(t)
	writeSnapshot(t, filepath.Join(path, ".beads", ".br_history"),
		"issues.20260911_091150_1.jsonl", `{"id":"proj-a","status":"open"}`)

	// Verify step keeps seeing 0: restore must NOT unblock.
	g := NewBeadStoreGuard(func(string) (int, error) { return 0, nil })
	g.Check(project, openBeads(30, "proj"))
	g.Check(project, nil)
	if err := g.Restore(path); err == nil {
		t.Fatal("Restore succeeded despite open count 0 — effect not verified")
	}
	if !g.IsBlocked(project.Path) {
		t.Fatal("project unblocked despite unrecovered count")
	}
}

func TestStoreGuard_PicksNewestSnapshot(t *testing.T) {
	project, path := guardTestProject(t)
	histDir := filepath.Join(path, ".beads", ".br_history")
	writeSnapshot(t, histDir, "issues.20260910_000000_1.jsonl", "OLD")
	writeSnapshot(t, histDir, "issues.20260911_091150_9.jsonl", "NEW")

	g := NewBeadStoreGuard(func(string) (int, error) { return 5, nil })
	g.Check(project, openBeads(5, "proj"))
	g.Check(project, nil)

	restored, err := os.ReadFile(filepath.Join(path, ".beads", "issues.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if string(restored) != "NEW" {
		t.Fatalf("restored %q, want newest snapshot NEW", restored)
	}
}

func TestFindFromBR_StoreGuardExcludesBlockedProject(t *testing.T) {
	project, _ := guardTestProject(t)
	mock := newMockBeadsClient()
	mock.beads["proj-a"] = types.Bead{ID: "proj-a", Project: "proj", Type: "task", Status: types.StatusOpen}
	mock.beads["other-1"] = types.Bead{ID: "other-1", Project: "other", Type: "task", Status: types.StatusOpen}

	other := types.Project{Name: "other", Path: filepath.Join(t.TempDir(), "other")}

	var mu sync.RWMutex
	d := NewBeadDiscovery(mock, nil, []types.Project{project, other}, map[string]bool{}, nil, &mu)

	// Fail the auto-restore verify so the project stays blocked; then
	// confirm discovery excludes it (fail-closed) while "other" schedules.
	g := NewBeadStoreGuard(func(string) (int, error) { return 0, errors.New("br list: corrupt") })
	d.SetStoreGuard(g)

	g.Check(project, openBeads(9, "proj"))
	g.Check(project, nil)
	if !g.IsBlocked(project.Path) {
		t.Fatal("setup: project not blocked")
	}

	beads, err := d.findFromBR(map[string]bool{})
	if err != nil {
		t.Fatalf("findFromBR: %v", err)
	}
	if len(beads) != 1 || beads[0].ID != "other-1" {
		t.Fatalf("expected only other-1 (blocked project excluded), got %+v", beads)
	}
}

func TestStoreGuard_EventStreamInformed(t *testing.T) {
	project, _ := guardTestProject(t)
	g := NewBeadStoreGuard(func(string) (int, error) { return 0, errors.New("verify fails") })
	sink := NewEventSink(t.TempDir())
	g.SetEventSink(sink)

	g.Check(project, openBeads(4, "proj"))
	g.Check(project, nil)

	suspects := readEventsOfType(t, sink, "bead_store_suspect")
	if len(suspects) != 1 {
		t.Fatalf("expected 1 bead_store_suspect, got %d", len(suspects))
	}
	if suspects[0].Project != "proj" || suspects[0].Reason != "open_count_drop" {
		t.Fatalf("suspect event malformed: %+v", suspects[0])
	}
	if suspects[0].Fields["previous"] != float64(4) || suspects[0].Fields["current"] != float64(0) {
		t.Fatalf("suspect event fields missing counts: %+v", suspects[0].Fields)
	}
}

func TestStoreGuard_ListFailureEmitsStoreErrorField(t *testing.T) {
	project, _ := guardTestProject(t)
	g := NewBeadStoreGuard(func(string) (int, error) { return 0, errors.New("verify fails") })
	sink := NewEventSink(t.TempDir())
	g.SetEventSink(sink)

	g.Check(project, openBeads(4, "proj"))
	g.NoteListFailure(project, errors.New("br list --json failed: exit status 7"))

	suspects := readEventsOfType(t, sink, "bead_store_suspect")
	if len(suspects) != 1 {
		t.Fatalf("expected 1 suspect event, got %d", len(suspects))
	}
	if !strings.Contains(suspects[0].Fields["list_error"].(string), "exit status 7") {
		t.Fatalf("list_error field missing exact error: %+v", suspects[0].Fields)
	}
}
