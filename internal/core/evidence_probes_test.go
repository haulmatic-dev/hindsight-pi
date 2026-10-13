package core

import (
	"os"
	"testing"

	"github.com/haulmatic-dev/hydra/internal/hindsight"
)

func TestEvidenceProbes_NilRegistryNoActivity(t *testing.T) {
	o := &Overseer{}
	if o.recentWorkerActivityProbe()("demo") {
		t.Fatalf("nil registry must report no activity")
	}
}

func TestEvidenceProbes_NilHindsightClientZero(t *testing.T) {
	o := &Overseer{}
	n, err := o.hindsightMemoriesProbe()("demo")
	if err != nil || n != 0 {
		t.Fatalf("nil client = %d, %v; want 0, nil", n, err)
	}
}

type stubEvidenceHindsight struct {
	HindsightClient
	resp *hindsight.SearchResponse
	err  error
}

func (s stubEvidenceHindsight) IsEnabled() bool { return true }

func (s stubEvidenceHindsight) Search(query, beadType string, limit int) (*hindsight.SearchResponse, error) {
	return s.resp, s.err
}

func TestEvidenceProbes_MemoriesCounted(t *testing.T) {
	o := &Overseer{hindsightClient: stubEvidenceHindsight{resp: &hindsight.SearchResponse{
		Memories: []hindsight.Memory{{}, {}}, Results: []hindsight.Memory{{}},
	}}}
	n, err := o.hindsightMemoriesProbe()("demo")
	if err != nil || n != 3 {
		t.Fatalf("count = %d, %v; want 3, nil", n, err)
	}
}

func TestEvidenceProbes_SearchErrorPropagates(t *testing.T) {
	o := &Overseer{hindsightClient: stubEvidenceHindsight{err: os.ErrPermission}}
	if _, err := o.hindsightMemoriesProbe()("demo"); err == nil {
		t.Fatalf("search error must propagate for fail-open handling")
	}
}

func TestEvidenceProbes_CountFallsBackToRespCount(t *testing.T) {
	o := &Overseer{hindsightClient: stubEvidenceHindsight{resp: &hindsight.SearchResponse{Count: 4}}}
	n, err := o.hindsightMemoriesProbe()("demo")
	if err != nil || n != 4 {
		t.Fatalf("count = %d, %v; want 4, nil", n, err)
	}
}
