package core

import (
	"testing"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// newFixClusterCloser builds a BeadCloser (config nil = clustering enabled)
// with a mock beads client.
func newFixClusterCloser(t *testing.T, seed types.Bead) (*BeadCloser, *mockBeadsClient) {
	t.Helper()
	mock := newMockBeadsClient()
	mock.beads[string(seed.ID)] = seed
	return &BeadCloser{beadsClient: mock}, mock
}

// TestFailureSignature_ScrubsBeadIDs: two beads failing for the same reason
// must produce the same signature regardless of which bead it happened to.
func TestFailureSignature_ScrubsBeadIDs(t *testing.T) {
	a := FailureSignature("worker-exit", "exit status 1 for bead hydra-5az8: commit failed on hydra/wt-hydra-5az8")
	b := FailureSignature("worker-exit", "exit status 1 for bead pmgh-9f3c: commit failed on pmgh/wt-pmgh-9f3c")
	if a != b {
		t.Errorf("signatures differ:\n%q\n%q", a, b)
	}
	if a == FailureSignature("worker-exit", "entirely different failure") {
		t.Error("different failures must not share a signature")
	}
	// Operation is part of the key: integrate failures and worker exits cluster apart.
	if a == FailureSignature("integrate", "exit status 1 for bead hydra-5az8: commit failed on hydra/wt-hydra-5az8") {
		t.Error("operation must be part of the signature")
	}
}

// TestSignatureLabel_DurableAndStable: same signature -> same label, so
// clustering and release survive daemon restarts.
func TestSignatureLabel_DurableAndStable(t *testing.T) {
	sig := FailureSignature("integrate", "conflict in docs/discovery.md")
	l1, l2 := SignatureLabel(sig), SignatureLabel(sig)
	if l1 != l2 {
		t.Errorf("label unstable: %q vs %q", l1, l2)
	}
	if len(l1) < 7 || l1[:7] != "fix-sig" {
		t.Errorf("label = %q, want a fix-sig- prefix", l1)
	}
}

// TestCreateFixBead_SameSignatureParksAgainstMeta: one signature = one meta
// fix bead; the second failure is parked (labelled) instead of spawning
// another per-bead fix bead.
func TestCreateFixBead_SameSignatureParksAgainstMeta(t *testing.T) {
	orig1 := types.Bead{ID: "proj-a1", Title: "GREEN: Implement the reader", Priority: 2}
	closer, mock := newFixClusterCloser(t, orig1)

	if err := closer.createFixBeadForFailedClose(&orig1, "/tmp/proj", "", string(orig1.ID)); err != nil {
		t.Fatalf("first failure: %v", err)
	}
	fixes := countFixBeads(mock)
	if len(fixes) != 1 {
		t.Fatalf("first failure should create exactly 1 meta fix bead, got %d", len(fixes))
	}
	if len(fixes[0].Labels) == 0 || !hasBeadLabel(fixes[0].Labels, "auto-fix") {
		t.Fatalf("meta fix bead lost its labels: %v", fixes[0].Labels)
	}
	var sigLabel string
	for _, l := range fixes[0].Labels {
		if len(l) > 8 && l[:8] == "fix-sig-" {
			sigLabel = l
		}
	}
	if sigLabel == "" {
		t.Fatalf("meta fix bead must carry a fix-sig- label, has %v", fixes[0].Labels)
	}

	// Second failure with the same (empty-log -> identical) signature.
	orig2 := types.Bead{ID: "proj-b2", Title: "GREEN: Implement the writer", Priority: 2}
	mock.beads[string(orig2.ID)] = orig2
	if err := closer.createFixBeadForFailedClose(&orig2, "/tmp/proj", "", string(orig2.ID)); err != nil {
		t.Fatalf("second failure: %v", err)
	}
	if fixes := countFixBeads(mock); len(fixes) != 1 {
		t.Fatalf("same signature must not spawn a second fix bead, got %d", len(fixes))
	}
	parked := mock.beads[string(orig2.ID)]
	if !hasBeadLabel(parked.Labels, fixClusteredLabel) || !hasBeadLabel(parked.Labels, sigLabel) {
		t.Errorf("parked bead labels = %v, want %s + %s", parked.Labels, fixClusteredLabel, sigLabel)
	}

	// Third failure parks too; still one meta bead.
	orig3 := types.Bead{ID: "proj-c3", Title: "GREEN: Implement the index", Priority: 2}
	mock.beads[string(orig3.ID)] = orig3
	if err := closer.createFixBeadForFailedClose(&orig3, "/tmp/proj", "", string(orig3.ID)); err != nil {
		t.Fatalf("third failure: %v", err)
	}
	if fixes := countFixBeads(mock); len(fixes) != 1 {
		t.Fatalf("still want exactly 1 meta fix bead, got %d", len(fixes))
	}
}

// TestCreateFixBead_DifferentSignatureCreatesSeparateMeta: distinct error
// classes get their own meta bead — clustering must not bury unrelated gaps.
func TestCreateFixBead_DifferentSignatureCreatesSeparateMeta(t *testing.T) {
	orig1 := types.Bead{ID: "proj-d1", Title: "RED: Write tests for the writer", Priority: 2}
	closer, mock := newFixClusterCloser(t, orig1)
	if err := closer.createFixBeadForFailedClose(&orig1, "/tmp/projA", "", string(orig1.ID)); err != nil {
		t.Fatalf("first failure: %v", err)
	}
	// A different cause text (read from the mock's log dir — empty here, so
	// seed a signature label directly on the existing meta bead to force a
	// different signature match attempt).
	for id, b := range mock.beads {
		if b.Type == "fix" {
			b.Labels = append(b.Labels, SignatureLabel(FailureSignature("integrate", "conflict in docs/discovery.md")))
			mock.beads[id] = b
		}
	}
	orig2 := types.Bead{ID: "proj-d2", Title: "RED: Write tests for the reader", Priority: 2}
	mock.beads[string(orig2.ID)] = orig2
	// Same empty-log signature as the first failure -> parked, not new bead.
	if err := closer.createFixBeadForFailedClose(&orig2, "/tmp/projA", "", string(orig2.ID)); err != nil {
		t.Fatalf("second failure: %v", err)
	}
	if fixes := countFixBeads(mock); len(fixes) != 1 {
		t.Fatalf("identical signature must park, got %d fix beads", len(fixes))
	}
}

// TestCreateFixBead_ClusteringDisabledLegacy: disable_fix_signature_clustering
// restores the legacy per-bead fix bead.
func TestCreateFixBead_ClusteringDisabledLegacy(t *testing.T) {
	cfg := &Config{}
	cfg.Validation.DisableFixSignatureClustering = true
	orig1 := types.Bead{ID: "proj-e1", Title: "GREEN: Implement the reader", Priority: 2}
	mock := newMockBeadsClient()
	mock.beads[string(orig1.ID)] = orig1
	closer := &BeadCloser{beadsClient: mock, config: cfg}

	if err := closer.createFixBeadForFailedClose(&orig1, "/tmp/proj", "", string(orig1.ID)); err != nil {
		t.Fatalf("first failure: %v", err)
	}
	orig2 := types.Bead{ID: "proj-e2", Title: "GREEN: Implement the writer", Priority: 2}
	mock.beads[string(orig2.ID)] = orig2
	if err := closer.createFixBeadForFailedClose(&orig2, "/tmp/proj", "", string(orig2.ID)); err != nil {
		t.Fatalf("second failure: %v", err)
	}
	if fixes := countFixBeads(mock); len(fixes) != 2 {
		t.Errorf("clustering disabled: expected 2 per-bead fix beads, got %d", len(fixes))
	}
}

// TestReleaseParkedBeads_OnMetaClose: closing the meta fix bead clears the
// fix-clustered label from every parked original (PRD FR-020).
func TestReleaseParkedBeads_OnMetaClose(t *testing.T) {
	projectPath := "/tmp/proj-release-test/proj" // base must equal bead.Project for the mock
	meta := types.Bead{ID: "proj-fix-1", Project: "proj", Title: "Fix: GREEN: Implement the reader", Type: "fix",
		Status: types.StatusOpen, Labels: []string{"auto-fix", "fix-sig-ab12cd34"}}
	parked1 := types.Bead{ID: "proj-a1", Project: "proj", Title: "GREEN: Implement the reader", Type: "implement",
		Status: types.StatusFailed, Labels: []string{"closed-failed", "fix-clustered", "fix-sig-ab12cd34"}}
	parked2 := types.Bead{ID: "proj-b2", Project: "proj", Title: "GREEN: Implement the writer", Type: "implement",
		Status: types.StatusOpen, Labels: []string{"closed-failed", "fix-clustered", "fix-sig-ab12cd34"}}
	unrelated := types.Bead{ID: "proj-c3", Project: "proj", Title: "GREEN: Implement the index", Type: "implement",
		Status: types.StatusOpen, Labels: []string{"closed-failed"}}

	mock := newMockBeadsClient()
	for _, b := range []types.Bead{meta, parked1, parked2, unrelated} {
		mock.beads[string(b.ID)] = b
	}
	closer := &BeadCloser{beadsClient: mock}

	metaClosed := meta
	closer.releaseParkedBeads(projectPath, "", &metaClosed)

	for _, id := range []string{"proj-a1", "proj-b2"} {
		if b := mock.beads[id]; hasBeadLabel(b.Labels, fixClusteredLabel) {
			t.Errorf("%s still parked after meta close: %v", id, b.Labels)
		}
		if b := mock.beads[id]; !hasBeadLabel(b.Labels, "fix-sig-ab12cd34") {
			t.Errorf("%s lost its signature label: %v", id, b.Labels)
		}
		if b := mock.beads[id]; b.Status == types.StatusFailed {
			t.Errorf("%s released but still failed; want reopened", id)
		}
	}
	if b := mock.beads["proj-c3"]; len(b.Labels) != 1 {
		t.Errorf("unrelated bead touched: %v", b.Labels)
	}
}

// TestCloseBead_MetaCloseReleasesClustered: through Close on the completed
// path — a meta fix bead closing releases its parked originals.
func TestCloseBead_MetaCloseReleasesClustered(t *testing.T) {
	projectPath := t.TempDir()
	base := "projclose"
	mock := newMockBeadsClient()
	meta := types.Bead{ID: types.BeadID(base + "-fix-1"), Project: base, Title: "Fix: GREEN: Implement the reader", Type: "fix",
		Status: types.StatusInProgress, Labels: []string{"auto-fix", "fix-sig-ab12cd34"}}
	parked := types.Bead{ID: types.BeadID(base + "-a1"), Project: base, Title: "GREEN: Implement the reader", Type: "implement",
		Status: types.StatusFailed, Labels: []string{"closed-failed", "fix-clustered", "fix-sig-ab12cd34"}}
	for _, b := range []types.Bead{meta, parked} {
		mock.beads[string(b.ID)] = b
	}
	// The mock lists beads by filepath.Base(projectPath); point the project at
	// a temp dir whose base matches the beads' Project field.
	realBase := "projclose"
	_ = realBase
	projectPath = t.TempDir() + "/projclose"
	resolver := setupTestProjectResolver(t, "projclose", projectPath)
	closer := NewBeadCloser(mock, NewIntelligenceHub(IntelligenceConfig{}), resolver, nil, nil)

	if err := closer.Close(&types.Worker{BeadID: types.BeadID("projclose-fix-1"), ProjectPath: projectPath}, true, 0); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if b := mock.beads["projclose-a1"]; hasBeadLabel(b.Labels, fixClusteredLabel) {
		t.Errorf("parked bead not released after meta close: %v", b.Labels)
	}
}
