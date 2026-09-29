package core

import (
	"strings"
	"slices"
	"testing"

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

