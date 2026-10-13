package core

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/haulmatic-dev/hydra/internal/pan"
	"github.com/haulmatic-dev/hydra/pkg/types"
)

func TestFilePanBeadCapAndLabel(t *testing.T) {
	o := &Overseer{beadsClient: newMockBeadsClient()}
	path := "/tmp/fakeproj"

	// Empty project: first filing succeeds and carries the pan-filed label.
	if err := o.filePanBead(path, "bead-src", "Wedge found", "details"); err != nil {
		t.Fatalf("first file-bead: %v", err)
	}
	beads, _ := o.beadsClient.ListBeads(path, "")
	if len(beads) != 1 || !slices.Contains(beads[0].Labels, pan.FiledLabel) {
		t.Fatalf("bead not filed with %s label: %+v", pan.FiledLabel, beads)
	}

	// Cap: fill to MaxOpenFiledBeadsPerProject, next filing must refuse.
	for i := 1; i < pan.MaxOpenFiledBeadsPerProject; i++ {
		if err := o.filePanBead(path, "bead-src", "Wedge found more", "details"); err != nil {
			t.Fatalf("file-bead %d: %v", i, err)
		}
	}
	err := o.filePanBead(path, "bead-src", "Yet another wedge", "details")
	if err == nil || !strings.Contains(err.Error(), "cap reached") {
		t.Fatalf("expected cap error, got %v", err)
	}

	// Closing one filed bead frees a slot.
	beads, _ = o.beadsClient.ListBeads(path, "")
	if err := o.beadsClient.UpdateBeadStatus(path, beads[0].ID, types.StatusClosed); err != nil {
		t.Fatalf("close filed bead: %v", err)
	}
	if err := o.filePanBead(path, "bead-src", "Post-close wedge", "details"); err != nil {
		t.Fatalf("file-bead after close: %v", err)
	}
}

func TestFilePanBead_DuplicateOpenTitleSkipped(t *testing.T) {
	o := &Overseer{beadsClient: newMockBeadsClient()}
	path := t.TempDir()
	if err := o.filePanBead(path, "bead-src", "Publisher wedge (1h stale)", "details"); err != nil {
		t.Fatalf("first filing failed: %v", err)
	}
	// Same normalized title (volatile age differs) → ErrFileBeadDuplicate.
	err := o.filePanBead(path, "bead-src-2", "Publisher wedge (2h stale)", "details")
	if !errors.Is(err, pan.ErrFileBeadDuplicate) {
		t.Fatalf("want ErrFileBeadDuplicate, got %v", err)
	}
	// Different title still files (cap is 2).
	if err := o.filePanBead(path, "bead-src-3", "Different finding", "details"); err != nil {
		t.Fatalf("different title should file: %v", err)
	}
}

func TestFilePanBead_CapCountsInProgress(t *testing.T) {
	o := &Overseer{beadsClient: newMockBeadsClient()}
	path := t.TempDir()

	// Fill the cap.
	names := []string{"alpha", "beta", "gamma", "delta", "epsilon", "zeta", "eta", "theta"}
	for i := 0; i < pan.MaxOpenFiledBeadsPerProject; i++ {
		if err := o.filePanBead(path, "bead-src", fmt.Sprintf("Wedge %s: a distinct root cause", names[i]), "details"); err != nil {
			t.Fatalf("file-bead %d: %v", i, err)
		}
	}
	// Claim one filed bead: it goes in_progress. The cap must still hold —
	// claimed beads keep occupying a slot until terminal.
	beads, _ := o.beadsClient.ListBeads(path, "")
	if err := o.beadsClient.UpdateBeadStatus(path, beads[0].ID, types.StatusInProgress); err != nil {
		t.Fatalf("claim filed bead: %v", err)
	}
	err := o.filePanBead(path, "bead-src", "Bypass attempt", "details")
	if err == nil || !strings.Contains(err.Error(), "cap reached") {
		t.Fatalf("cap must count in_progress filed beads, got %v", err)
	}

	// Close (terminal) one — frees the slot.
	if err := o.beadsClient.UpdateBeadStatus(path, beads[0].ID, types.StatusClosed); err != nil {
		t.Fatalf("close filed bead: %v", err)
	}
	if err := o.filePanBead(path, "bead-src", "Bypass attempt 2", "details"); err != nil {
		t.Fatalf("terminal close must free slot: %v", err)
	}
}

func TestFilePanBead_DuplicateInProgressTitleSkipped(t *testing.T) {
	o := &Overseer{beadsClient: newMockBeadsClient()}
	path := t.TempDir()
	if err := o.filePanBead(path, "bead-src", "Publisher wedge (1h stale)", "details"); err != nil {
		t.Fatalf("first filing failed: %v", err)
	}
	beads, _ := o.beadsClient.ListBeads(path, "")
	if err := o.beadsClient.UpdateBeadStatus(path, beads[0].ID, types.StatusInProgress); err != nil {
		t.Fatalf("claim filed bead: %v", err)
	}
	// Same normalized title on a claimed (in_progress) bead is still a twin.
	err := o.filePanBead(path, "bead-src-2", "Publisher wedge (2h stale)", "details")
	if !errors.Is(err, pan.ErrFileBeadDuplicate) {
		t.Fatalf("want ErrFileBeadDuplicate for in_progress twin, got %v", err)
	}
}

func TestFilePanBead_SignatureWindowDedupe(t *testing.T) {
	o := &Overseer{beadsClient: newMockBeadsClient()}
	path := "/tmp/fakeproj"
	// Wedge filings whose titles differ only by volatile noise (cardinal
	// counts, bead IDs, paths, timestamps) share one signature.
	v1 := "PROJECT-WEDGE: 17 beads blocked by \"spawn failed\" (bead hydra-ee5r)"
	v2 := "PROJECT-WEDGE: 23 beads blocked by \"spawn failed\" (bead hydra-x0k2)"
	if err := o.filePanBead(path, "bead-src", v1, "details"); err != nil {
		t.Fatalf("first wedge filing: %v", err)
	}
	if beads, _ := o.beadsClient.ListBeads(path, ""); len(beads) != 1 {
		t.Fatalf("beads = %d, want 1 (identical signature dedupes)", len(beads))
	}
	err := o.filePanBead(path, "bead-src-2", v2, "details")
	if !errors.Is(err, pan.ErrFileBeadDuplicate) {
		t.Fatalf("want ErrFileBeadDuplicate for variant title, got %v", err)
	}
	// Even after the wedge bead closes, the window still blocks refiling.
	beads, _ := o.beadsClient.ListBeads(path, "")
	if err := o.beadsClient.UpdateBeadStatus(path, beads[0].ID, types.StatusClosed); err != nil {
		t.Fatalf("close wedge bead: %v", err)
	}
	err = o.filePanBead(path, "bead-src-3", v2, "details")
	if !errors.Is(err, pan.ErrFileBeadDuplicate) {
		t.Fatalf("want ErrFileBeadDuplicate within window after close, got %v", err)
	}
	// After the window expires the finding may file again.
	panFileSigTTL = -time.Second
	defer func() { panFileSigTTL = time.Hour }()
	if err := o.filePanBead(path, "bead-src-4", v2, "details"); err != nil {
		t.Fatalf("filing after window expiry: %v", err)
	}
	// A genuinely different finding files immediately.
	if err := o.filePanBead(path, "bead-src-5", "Disk watermark breached", "details"); err != nil {
		t.Fatalf("different finding should file: %v", err)
	}
}

func TestFilePanBead_FilerErrorLeavesWindowOpen(t *testing.T) {
	mc := newMockBeadsClient()
	mc.listErrByPath = map[string]error{"/bad": errors.New("list boom")}
	o := &Overseer{beadsClient: mc}
	path := "/tmp/fakeproj"
	// Filer error (list failure) happens after the window check but before
	// create: the signature must NOT be recorded, so a retry can file.
	if err := o.filePanBead("/bad", "bead-src", "Wedge A", "details"); err == nil {
		t.Fatalf("expected filer error for broken project")
	}
	if err := o.filePanBead(path, "bead-src", "Wedge A", "details"); err != nil {
		t.Fatalf("retry after filer error must file: %v", err)
	}
}
