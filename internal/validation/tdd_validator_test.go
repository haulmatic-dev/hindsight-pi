package validation

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// TestTDDValidator_* pins the evidence-based chain checks contract
// (ValidateTDDChainEvidence). Same RED gate as the BeadEvidence suite.
//
// Contract (PRD validator-truth FR-003):
//   - RED passes when its evidence contains >=1 beadfiles.IsTestFile file
//     with a meaningful assertion (case-sensitive patterns).
//   - GREEN passes when its evidence contains >=1 non-test source file (not
//     IsTestFile, not *.md, not under docs/, not CHANGELOG*) that is not a
//     stub.
//   - Declared files (beadfiles.Resolve) only scope the stub check and name
//     sibling tests; a declared non-test file is never checked as a test.
//   - Declared files present and no evidence -> one gap naming the bead id
//     and saying `no test file in bead evidence` (RED) or `no implementation
//     file in bead evidence` (GREEN).
//   - No declared files and no evidence -> no gap. Unlocated side -> no gap.

func chainBead(id string, declaredFiles []string) types.Bead {
	b := types.Bead{ID: types.BeadID(id), Title: "T: " + id, Metadata: map[string]string{}}
	if len(declaredFiles) > 0 {
		b.Metadata["files"] = strings.Join(declaredFiles, ",")
	}
	return b
}

func redGapCount(r TDDResult) int {
	n := 0
	for _, g := range r.Gaps {
		if g.Type == "red" {
			n++
		}
	}
	return n
}

func greenGapCount(r TDDResult) int {
	n := 0
	for _, g := range r.Gaps {
		if g.Type == "green" {
			n++
		}
	}
	return n
}

func TestTDDValidator_RedEvidenceTestFilePasses(t *testing.T) {
	beadEvidenceRequireImpl(t)
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "pkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	content := "package pkg\n\nimport \"testing\"\n\nfunc TestA(t *testing.T) {\n\tt.Run(\"sub\", func(t *testing.T) {})\n}\n"
	if err := os.WriteFile(filepath.Join(root, "pkg/a_test.go"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	red := chainBead("hydra-ab12", nil) // empty metadata, no declared files
	in := ChainEvidenceInput{
		RedBead: red, RedLocated: true, RedEvidence: Evidence{Files: []string{"pkg/a_test.go"}},
		RepoRoot: root,
	}
	res := ValidateTDDChainEvidence(in)
	if !res.RedPassed {
		t.Fatalf("RED with evidence test file containing t.Run( must pass; gaps: %+v", res.Gaps)
	}
	if n := redGapCount(res); n != 0 {
		t.Fatalf("want zero red gaps, got %d: %+v", n, res.Gaps)
	}
}

func TestTDDValidator_RedDeclaredFileNoEvidenceOneGap(t *testing.T) {
	beadEvidenceRequireImpl(t)
	red := chainBead("hydra-cd34", []string{"internal/core/service_starter.go"})
	in := ChainEvidenceInput{
		RedBead: red, RedLocated: true, RedEvidence: Evidence{},
		RepoRoot: t.TempDir(),
	}
	res := ValidateTDDChainEvidence(in)
	if res.RedPassed {
		t.Fatal("RED with declared files and no evidence must not pass")
	}
	if n := redGapCount(res); n != 1 {
		t.Fatalf("want exactly one red gap, got %d: %+v", n, res.Gaps)
	}
	gap := ""
	for _, g := range res.Gaps {
		if g.Type == "red" {
			gap = g.Description
		}
	}
	if !strings.Contains(gap, "hydra-cd34") || !strings.Contains(gap, "no test file in bead evidence") {
		t.Fatalf("red gap must name bead id and say `no test file in bead evidence`; got %q", gap)
	}
}

func TestTDDValidator_NoEvidenceNoDeclaredNoGaps(t *testing.T) {
	beadEvidenceRequireImpl(t)
	red := chainBead("hydra-ee56", nil)
	in := ChainEvidenceInput{
		RedBead: red, RedLocated: true, RedEvidence: Evidence{},
		RepoRoot: t.TempDir(),
	}
	res := ValidateTDDChainEvidence(in)
	if n := redGapCount(res); n != 0 {
		t.Fatalf("no declared files + no evidence must yield zero gaps, got %d: %+v", n, res.Gaps)
	}
}

func TestTDDValidator_UnlocatedSideNoGaps(t *testing.T) {
	beadEvidenceRequireImpl(t)
	red := chainBead("hydra-ff78", []string{"internal/core/service_starter.go"})
	in := ChainEvidenceInput{
		RedBead: red, RedLocated: false, RedEvidence: Evidence{},
		RepoRoot: t.TempDir(),
	}
	res := ValidateTDDChainEvidence(in)
	if n := redGapCount(res); n != 0 {
		t.Fatalf("unlocated side must produce no gap, got %d: %+v", n, res.Gaps)
	}
}

func TestTDDValidator_GreenDocsOnlyEvidenceOneGap(t *testing.T) {
	beadEvidenceRequireImpl(t)
	green := chainBead("hydra-gg90", nil)
	in := ChainEvidenceInput{
		GreenBead: green, GreenLocated: true,
		GreenEvidence: Evidence{Files: []string{"CHANGELOG.md", "docs/x.md"}},
		RepoRoot:      t.TempDir(),
	}
	res := ValidateTDDChainEvidence(in)
	if res.GreenPassed {
		t.Fatal("GREEN whose evidence is only CHANGELOG.md and docs/x.md must not pass")
	}
	if n := greenGapCount(res); n != 1 {
		t.Fatalf("want exactly one green gap, got %d: %+v", n, res.Gaps)
	}
}

func TestTDDValidator_GreenEvidenceImplPasses(t *testing.T) {
	beadEvidenceRequireImpl(t)
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "pkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	impl := "package pkg\n\nfunc Add(a, b int) int {\n\tif a < 0 {\n\t\ta = -a\n\t}\n\treturn a + b\n}\n"
	if err := os.WriteFile(filepath.Join(root, "pkg/calc.go"), []byte(impl), 0o644); err != nil {
		t.Fatal(err)
	}
	green := chainBead("hydra-hh02", nil)
	in := ChainEvidenceInput{
		GreenBead: green, GreenLocated: true,
		GreenEvidence: Evidence{Files: []string{"pkg/calc.go"}},
		RepoRoot:      root,
	}
	res := ValidateTDDChainEvidence(in)
	if !res.GreenPassed {
		t.Fatalf("GREEN with real implementation evidence must pass; gaps: %+v", res.Gaps)
	}
	if n := greenGapCount(res); n != 0 {
		t.Fatalf("want zero green gaps, got %d: %+v", n, res.Gaps)
	}
}

func TestTDDValidator_GreenDeclaredNoEvidenceOneGap(t *testing.T) {
	beadEvidenceRequireImpl(t)
	green := chainBead("hydra-ii14", []string{"internal/validation/validator.go"})
	in := ChainEvidenceInput{
		GreenBead: green, GreenLocated: true, GreenEvidence: Evidence{},
		RepoRoot: t.TempDir(),
	}
	res := ValidateTDDChainEvidence(in)
	if n := greenGapCount(res); n != 1 {
		t.Fatalf("want exactly one green gap, got %d: %+v", n, res.Gaps)
	}
	gap := ""
	for _, g := range res.Gaps {
		if g.Type == "green" {
			gap = g.Description
		}
	}
	if !strings.Contains(gap, "hydra-ii14") || !strings.Contains(gap, "no implementation file in bead evidence") {
		t.Fatalf("green gap must name bead id and say `no implementation file in bead evidence`; got %q", gap)
	}
}

func TestTDDValidator_ImportTestingOnlyNotMeaningful(t *testing.T) {
	beadEvidenceRequireImpl(t)
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "pkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "pkg/empty_test.go"), []byte("package pkg\n\nimport \"testing\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	v := NewTDDValidator(DefaultValidationConfig())
	if v.hasMeaningfulAssertions("package pkg\n\nimport \"testing\"\n") {
		t.Fatal(`a Go file containing only import "testing" is not a meaningful test`)
	}
}

func TestTDDValidator_AssertionPatternsCaseSensitive(t *testing.T) {
	beadEvidenceRequireImpl(t)
	v := NewTDDValidator(DefaultValidationConfig())
	positive := []string{
		"func TestX(t *testing.T) {}",
		"t.Run(\"a\", func(t *testing.T) {})",
		"t.Error(\"boom\")",
		"t.Fatal(\"nope\")",
		"assert.Equal(t, 1, 1)",
		"require.NoError(t, err)",
		"expect(1).toBe(1)",
		"describe(\"x\", () => {})",
		"def test_add(): pass",
	}
	for _, c := range positive {
		if !v.hasMeaningfulAssertions(c) {
			t.Fatalf("case-sensitive pattern must match: %q", c)
		}
	}
	negative := []string{
		"// T.RUN( uppercase must not match",
		"ASSERT.Equal(t, 1, 1)",
		"EXPECT(1).toBe(1)",
		"package pkg\nimport \"testing\"",
	}
	for _, c := range negative {
		if v.hasMeaningfulAssertions(c) {
			t.Fatalf("uppercase/mere-import must NOT match: %q", c)
		}
	}
}
