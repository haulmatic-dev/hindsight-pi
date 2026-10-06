package core

import (
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/haulmatic-dev/hydra/internal/pan"
	"github.com/haulmatic-dev/hydra/internal/registry"
	"github.com/haulmatic-dev/hydra/pkg/types"
)

const premiseTestPath = "/tmp/srv-jobfile"

func premiseSourceBead(t *testing.T, tw *testOverseerWithMocks, status types.BeadStatus) {
	t.Helper()
	tw.mockBeads.beads["srv-jobfile-5my"] = types.Bead{
		ID: "srv-jobfile-5my", Project: "srv-jobfile", Status: status, Type: "task",
	}
}

func premiseBeadCount(t *testing.T, o *Overseer) int {
	t.Helper()
	beads, err := o.beadsClient.ListBeads(premiseTestPath, "")
	if err != nil {
		t.Fatalf("ListBeads: %v", err)
	}
	n := 0
	for _, b := range beads {
		if slices.Contains(b.Labels, pan.FiledLabel) {
			n++
		}
	}
	return n
}

// TestFilePanBead_PremiseStale_SourceResolved: a wedge filing whose judgment
// source bead has since gone terminal is refused before creation (jm2 class:
// filed on a stale "wedged" premise).
func TestFilePanBead_PremiseStale_SourceResolved(t *testing.T) {
	tw := setupTestOverseer(t)
	o := tw.overseer
	premiseSourceBead(t, tw, types.StatusClosed)

	err := o.filePanBead(premiseTestPath, "srv-jobfile-5my", "integration-exhausted hard-park with no worktree", "details")
	if !errors.Is(err, pan.ErrFileBeadPremiseStale) {
		t.Fatalf("want ErrFileBeadPremiseStale, got %v", err)
	}
	if n := premiseBeadCount(t, o); n != 0 {
		t.Fatalf("no bead should be created, have %d", n)
	}
}

// TestFilePanBead_PremiseStale_WorkerLive: a live registry worker on the
// source bead means work is mid-flight, not a silent wedge.
func TestFilePanBead_PremiseStale_WorkerLive(t *testing.T) {
	tw := setupTestOverseer(t)
	o := tw.overseer
	premiseSourceBead(t, tw, types.StatusInProgress)

	st, err := registry.Open(":memory:")
	if err != nil {
		t.Fatalf("registry.Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	o.registryStore = st
	if err := st.UpsertWorker(registry.WorkerRecord{
		BeadID: "srv-jobfile-5my", PID: 1, Project: "srv-jobfile", State: registry.StateWorking,
		StartedAt: time.Now(), LastHeartbeat: time.Now(),
	}); err != nil {
		t.Fatalf("UpsertWorker: %v", err)
	}

	if err := o.filePanBead(premiseTestPath, "srv-jobfile-5my", "orphaned in_progress claim", "details"); !errors.Is(err, pan.ErrFileBeadPremiseStale) {
		t.Fatalf("want ErrFileBeadPremiseStale, got %v", err)
	}
	if n := premiseBeadCount(t, o); n != 0 {
		t.Fatalf("no bead should be created, have %d", n)
	}
}

// TestFilePanBead_PremiseStale_DigestStale: a filing built on a digest older
// than the staleness horizon is refused, pseudo candidates included.
func TestFilePanBead_PremiseStale_DigestStale(t *testing.T) {
	tw := setupTestOverseer(t)
	o := tw.overseer
	o.panDigestAt.Store(time.Now().Add(-20 * time.Minute).UnixNano())

	if err := o.filePanBead(premiseTestPath, "srv-jobfile-campaign-anomaly", "P0 campaign anomaly", "details"); !errors.Is(err, pan.ErrFileBeadPremiseStale) {
		t.Fatalf("want ErrFileBeadPremiseStale, got %v", err)
	}
	if n := premiseBeadCount(t, o); n != 0 {
		t.Fatalf("no bead should be created, have %d", n)
	}
}

// TestFilePanBead_FreshDigestFiles: fresh digest + open source bead → filing
// proceeds (no over-blocking of genuine wedges).
func TestFilePanBead_FreshDigestFiles(t *testing.T) {
	tw := setupTestOverseer(t)
	o := tw.overseer
	premiseSourceBead(t, tw, types.StatusInProgress)
	o.panDigestAt.Store(time.Now().UnixNano())

	if err := o.filePanBead(premiseTestPath, "srv-jobfile-5my", "genuine wedge", "details"); err != nil {
		t.Fatalf("filing should proceed: %v", err)
	}
	if n := premiseBeadCount(t, o); n != 1 {
		t.Fatalf("exactly one filed bead expected, have %d", n)
	}
}
