package core

import (
	"path/filepath"
	"testing"

	"github.com/haulmatic-dev/hydra/internal/registry"
	"github.com/haulmatic-dev/hydra/pkg/types"
)

// newParkTestOverseer wires an overseer with a real (temp-file) registry store
// and a single multi-user project so campaignParked exercises its true path:
// FindProjectPath -> projectKey -> ProjectParkInfo.
func newParkTestOverseer(t *testing.T) *Overseer {
	t.Helper()
	_, o := newTestScheduler(t, 5, GovernorConfig{})

	store, err := registry.Open(filepath.Join(t.TempDir(), "registry.db"))
	if err != nil {
		t.Fatalf("registry.Open: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	o.registryStore = store

	o.beadAnalyzer = NewBeadAnalyzer([]types.Project{
		{Name: "ajantha", Path: "/home/ajantha/projects", Type: types.RepoTypeMulti},
	})
	return o
}

// TestCampaignParked_ParkedProject locks the park lookup itself: a parked
// project key must suppress the bead. The 2026-09-21 telematics incident showed
// parks written to the registry while spawns proceeded — this test pins the
// store-facing half of that contract.
func TestCampaignParked_ParkedProject(t *testing.T) {
	o := newParkTestOverseer(t)
	bead := types.Bead{ID: types.BeadID("ajantha-srv-telematics-x1"), Project: "ajantha", Repo: "srv-telematics", Status: types.StatusOpen}

	if err := o.registryStore.SetProjectPark("ajantha/srv-telematics", true, "test"); err != nil {
		t.Fatalf("SetProjectPark: %v", err)
	}
	if !o.campaignParked(bead) {
		t.Error("parked project reported not parked — park rows are invisible to the scheduler")
	}
}

func TestCampaignParked_UnparkedProject(t *testing.T) {
	o := newParkTestOverseer(t)
	bead := types.Bead{ID: types.BeadID("ajantha-srv-telematics-x1"), Project: "ajantha", Repo: "srv-telematics", Status: types.StatusOpen}

	if o.campaignParked(bead) {
		t.Error("never-parked project reported parked")
	}

	if err := o.registryStore.SetProjectPark("ajantha/srv-telematics", true, "test"); err != nil {
		t.Fatalf("SetProjectPark: %v", err)
	}
	if err := o.registryStore.SetProjectPark("ajantha/srv-telematics", false, ""); err != nil {
		t.Fatalf("SetProjectPark(false): %v", err)
	}
	if o.campaignParked(bead) {
		t.Error("unparked project still reported parked")
	}
}

// TestCampaignParked_NilRegistryFailsOpen: no registry must never block work.
func TestCampaignParked_NilRegistryFailsOpen(t *testing.T) {
	_, o := newTestScheduler(t, 5, GovernorConfig{})
	o.registryStore = nil
	bead := types.Bead{ID: types.BeadID("ajantha-srv-telematics-x1"), Project: "ajantha", Repo: "srv-telematics", Status: types.StatusOpen}
	if o.campaignParked(bead) {
		t.Error("nil registry reported parked — fail-open violated")
	}
}
