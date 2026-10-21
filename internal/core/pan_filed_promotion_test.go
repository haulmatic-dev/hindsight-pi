package core

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

func TestIsCodeChangeProposal(t *testing.T) {
	cases := []struct {
		title, desc string
		want        bool
	}{
		{"Stale worker claim wedges bead after retry exhaustion", "Investigate.\nhydra code change suggested: reset the claim.", true},
		{"Orphaned spawn registration wedges bead", "HYDRA CODE CHANGE SUGGESTED in scheduler.go", true},
		{"Fleet spawn-freeze observed", "28 br-ready, 3 spawnable, nothing spawned for 15m — human triage needed.", false},
		{"P0 campaign anomaly", "orphaned in_progress beads", false},
		{"Code Change Suggested", "", true},
	}
	for _, tc := range cases {
		if got := isCodeChangeProposal(tc.title, tc.desc); got != tc.want {
			t.Errorf("isCodeChangeProposal(%q, %q) = %v, want %v", tc.title, tc.desc, got, tc.want)
		}
	}
}

func promoterFixture(t *testing.T) (*PanFiledPromoter, *mockBeadsClient, string) {
	t.Helper()
	m := newMockBeadsClient()
	path := t.TempDir()
	p := NewPanFiledPromoter(m, func() []types.Project {
		return []types.Project{{Name: "proj", Path: path}}
	}, nil)
	return p, m, path
}

func TestPanFiledPromoter_MarkerPromoted(t *testing.T) {
	p, m, path := promoterFixture(t)
	addBeadFixture(t, m, path, types.Bead{
		ID: "proj-ukf", Title: "Stale worker claim wedges bead",
		Description: "hydra code change suggested",
		Type:        "task", Status: types.StatusOpen,
		Labels: []string{panFiledLabel},
	})
	p.scan()
	bead, err := m.GetBead(path, "", "proj-ukf")
	if err != nil {
		t.Fatalf("get bead: %v", err)
	}
	if hasBeadLabel(bead.Labels, panFiledLabel) {
		t.Fatalf("pan-filed label not stripped: %v", bead.Labels)
	}
}

func TestPanFiledPromoter_NoMarkerStays(t *testing.T) {
	p, m, path := promoterFixture(t)
	addBeadFixture(t, m, path, types.Bead{
		ID: "proj-anom", Title: "P0 campaign anomaly",
		Description: "orphaned in_progress claims",
		Type:        "task", Status: types.StatusOpen,
		Labels: []string{panFiledLabel},
	})
	p.scan()
	bead, _ := m.GetBead(path, "", "proj-anom")
	if !hasBeadLabel(bead.Labels, panFiledLabel) {
		t.Fatalf("human-triage filing must keep pan-filed label: %v", bead.Labels)
	}
}

func TestPanFiledPromoter_NonTaskTypeStays(t *testing.T) {
	p, m, path := promoterFixture(t)
	addBeadFixture(t, m, path, types.Bead{
		ID: "proj-epic", Title: "wedge", Description: "code change suggested",
		Type: "epic", Status: types.StatusOpen,
		Labels: []string{panFiledLabel},
	})
	p.scan()
	bead, _ := m.GetBead(path, "", "proj-epic")
	if !hasBeadLabel(bead.Labels, panFiledLabel) {
		t.Fatalf("non-task bead must not be promoted: %v", bead.Labels)
	}
}

func TestPanFiledPromoter_TerminalAndUnlabeledSkipped(t *testing.T) {
	p, m, path := promoterFixture(t)
	addBeadFixture(t, m, path, types.Bead{
		ID: "proj-closed", Title: "old wedge", Description: "code change suggested",
		Type: "task", Status: types.StatusClosed,
		Labels: []string{panFiledLabel},
	})
	addBeadFixture(t, m, path, types.Bead{
		ID: "proj-plain", Title: "regular work", Description: "code change suggested",
		Type: "task", Status: types.StatusOpen,
	})
	p.scan()
	if bead, _ := m.GetBead(path, "", "proj-closed"); !hasBeadLabel(bead.Labels, panFiledLabel) {
		t.Fatalf("terminal bead must be untouched")
	}
	if bead, _ := m.GetBead(path, "", "proj-plain"); hasBeadLabel(bead.Labels, panFiledLabel) {
		t.Fatalf("unlabeled bead must be untouched")
	}
}

func TestPanFiledPromoter_EmitsPromotionEvent(t *testing.T) {
	m := newMockBeadsClient()
	path := t.TempDir()
	sink := NewEventSink(t.TempDir())
	p := NewPanFiledPromoter(m, func() []types.Project {
		return []types.Project{{Name: "proj", Path: path}}
	}, sink)
	addBeadFixture(t, m, path, types.Bead{
		ID: "proj-5kx", Title: "Orphaned spawn registration wedges bead",
		Description: "hydra code change suggested",
		Type:        "task", Status: types.StatusOpen,
		Labels: []string{panFiledLabel},
	})
	p.scan()
	var found bool
	for _, ev := range readSinkEvents(t, sink.centralDir) {
		if ev.Type == "pan_filed_promoted" && string(ev.Bead) == "proj-5kx" {
			found = true
		}
	}
	if !found {
		t.Fatalf("pan_filed_promoted event not emitted")
	}
}

func TestPanFiledPromoter_StartStop(t *testing.T) {
	p, m, path := promoterFixture(t)
	addBeadFixture(t, m, path, types.Bead{
		ID: "proj-live", Title: "wedge", Description: "code change suggested",
		Type: "task", Status: types.StatusOpen,
		Labels: []string{panFiledLabel},
	})
	p.Start()
	time.Sleep(200 * time.Millisecond)
	p.Stop()
	p.Stop() // idempotent
	bead, _ := m.GetBead(path, "", "proj-live")
	if hasBeadLabel(bead.Labels, panFiledLabel) {
		t.Fatalf("Start loop did not promote")
	}
}

func TestPanFiledPromoter_NilInert(t *testing.T) {
	p := NewPanFiledPromoter(nil, nil, nil)
	p.Start()
	p.Stop() // must not panic
}

func TestLogContainerTypeSkip_NeverSilent(t *testing.T) {
	out := captureLogOutput(t, func() {
		logContainerTypeSkip(types.Bead{ID: "proj-gate", Type: "approval-gate"})
	})
	if !strings.Contains(out, "Skipping container-type bead in retry path") {
		t.Fatalf("container-type skip must log: %q", out)
	}
	if !strings.Contains(out, "proj-gate") {
		t.Fatalf("skip log must name the bead: %q", out)
	}
}

// addBeadFixture sets bead.Project so the mock's ListBeads project filter
// (filepath.Base(projectPath)) admits the bead.
func addBeadFixture(t *testing.T, m *mockBeadsClient, path string, bead types.Bead) {
	t.Helper()
	bead.Project = filepath.Base(path)
	m.addBead(bead)
}
