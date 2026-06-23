package validation

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

type fakeGateBeadsClient struct {
	beads map[string]types.Bead
}

func (f *fakeGateBeadsClient) GetBead(projectPath, repoName string, beadID types.BeadID) (*types.Bead, error) {
	if b, ok := f.beads[string(beadID)]; ok {
		return &b, nil
	}
	return nil, nil
}

func TestIsComplianceGate(t *testing.T) {
	tests := []struct {
		labels []string
		want   bool
	}{
		{[]string{"compliance-gate", "blocks-release"}, true},
		{[]string{"prd-contract"}, true},
		{[]string{"blocks-release"}, true},
		{[]string{"fr", "p1"}, false},
		{nil, false},
	}
	for _, tc := range tests {
		got := IsComplianceGate(types.Bead{Labels: tc.labels})
		if got != tc.want {
			t.Errorf("IsComplianceGate(%v) = %v, want %v", tc.labels, got, tc.want)
		}
	}
}

func TestValidateGate_NoGaps(t *testing.T) {
	dir := t.TempDir()
	realImpl := `package foo
type S struct{}
func (s *S) DoWork(x int) int { return x + 1 }
`
	if err := os.WriteFile(filepath.Join(dir, "good.go"), []byte(realImpl), 0o644); err != nil {
		t.Fatal(err)
	}
	gate := types.Bead{
		ID:     "gate-1",
		Title:  "COMPLIANCE GATE",
		Labels: []string{"compliance-gate"},
		Metadata: map[string]string{
			"target_files": "good.go",
		},
		Project: "test",
	}
	dep := types.Bead{
		ID:     "dep-1",
		Status: types.StatusClosed,
		Metadata: map[string]string{
			"target_files":  "good.go",
			"close_reason":  "completed",
		},
	}

	gv := NewComplianceGateValidator()
	// Provide a client so deps are inspected.
	client := &fakeGateBeadsClient{beads: map[string]types.Bead{string(dep.ID): dep}}
	gvOK := gv
	_ = client
	gaps, err := gvOK.ValidateGate(context.Background(), gate, []types.Bead{dep}, dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(gaps) != 0 {
		t.Errorf("expected no gaps, got %d: %v", len(gaps), gaps)
	}
}

func TestValidateGate_DetectsUpstreamStub(t *testing.T) {
	dir := t.TempDir()
	// Write a stub file in the same dir (gate's target_files) and
	// another stub for the upstream dep's target_files.
	stubFile := `package foo
type S struct{}
// Real implementation comes in the follow-up bead.
func (s *S) DoNothing(_ interface{}, id string) (string, error) {
	_ = id
	return "", nil
}
`
	if err := os.WriteFile(filepath.Join(dir, "stub.go"), []byte(stubFile), 0o644); err != nil {
		t.Fatal(err)
	}

	gate := types.Bead{
		ID:     "gate-1",
		Labels: []string{"compliance-gate"},
		Metadata: map[string]string{
			"target_files": "stub.go",
		},
	}
	dep := types.Bead{
		ID:     "dep-1",
		Status: types.StatusClosed,
		Metadata: map[string]string{
			"target_files": "stub.go",
		},
	}

	gv := NewComplianceGateValidator()
	gaps, err := gv.ValidateGate(context.Background(), gate, []types.Bead{dep}, dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(gaps) == 0 {
		t.Fatal("expected at least one gap for upstream stub")
	}
	foundStub := false
	for _, g := range gaps {
		if g.Type == "stub" {
			foundStub = true
		}
	}
	if !foundStub {
		t.Errorf("expected a 'stub' gap, got %+v", gaps)
	}
}

func TestValidateGate_DetectsFailedClosure(t *testing.T) {
	dir := t.TempDir()
	realImpl := `package foo
type S struct{}
func (s *S) DoWork(x int) int { return x + 1 }
`
	if err := os.WriteFile(filepath.Join(dir, "good.go"), []byte(realImpl), 0o644); err != nil {
		t.Fatal(err)
	}
	gate := types.Bead{
		ID:     "gate-1",
		Labels: []string{"compliance-gate"},
		Metadata: map[string]string{
			"target_files": "good.go",
		},
	}
	dep := types.Bead{
		ID:     "dep-1",
		Status: types.StatusClosed,
		Metadata: map[string]string{
			"target_files": "good.go",
			"close_reason": "failed",
		},
	}

	gv := NewComplianceGateValidator()
	gaps, _ := gv.ValidateGate(context.Background(), gate, []types.Bead{dep}, dir)
	found := false
	for _, g := range gaps {
		if g.Type == "failed-closure" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected a 'failed-closure' gap, got %+v", gaps)
	}
}

func TestValidateGate_RequiresTargetFiles(t *testing.T) {
	gate := types.Bead{
		ID:       "gate-1",
		Labels:   []string{"compliance-gate"},
		Metadata: map[string]string{},
	}
	gv := NewComplianceGateValidator()
	gaps, _ := gv.ValidateGate(context.Background(), gate, nil, t.TempDir())
	found := false
	for _, g := range gaps {
		if g.Type == "missing-file" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected a 'missing-file' gap when gate has no target_files, got %+v", gaps)
	}
}

func TestFormatGaps(t *testing.T) {
	gaps := []ComplianceGap{
		{Type: "stub", Description: "in foo.go"},
		{Type: "failed-closure", Description: "dep closed as failed"},
	}
	out := FormatGaps(gaps)
	if len(out) < 30 {
		t.Errorf("FormatGaps output too short: %q", out)
	}
}