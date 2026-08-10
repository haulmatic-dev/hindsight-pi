package core

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// writeJSONL writes raw lines into a project's .beads/issues.jsonl.
func writeJSONL(t *testing.T, lines ...string) (projectPath string) {
	t.Helper()
	dir := t.TempDir()
	beads := filepath.Join(dir, ".beads")
	if err := os.MkdirAll(beads, 0o755); err != nil {
		t.Fatal(err)
	}
	content := ""
	for _, l := range lines {
		content += l + "\n"
	}
	if err := os.WriteFile(filepath.Join(beads, "issues.jsonl"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func readOne(t *testing.T, line string) types.Bead {
	t.Helper()
	dir := writeJSONL(t, line)
	r := NewBeadFileReader([]types.Project{{Name: "p", Path: dir, Type: types.RepoTypeSingle}})
	beads, err := r.ReadFile(filepath.Join(dir, ".beads", "issues.jsonl"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if len(beads) != 1 {
		t.Fatalf("expected 1 bead, got %d", len(beads))
	}
	return beads[0]
}

// TestBeadFileReader_ParsesObjectDependencies is the regression test for the
// defect that disabled the whole chain layer: `br` writes dependencies as
// objects, the parser only accepted strings, so every bead looked
// dependency-free. ChainRegistrar then classified all 1450 beads as roots and
// registered 0 chains, so the completion callback — quarantine, integration,
// PRD validation, pattern extraction — never fired at all.
func TestBeadFileReader_ParsesObjectDependencies(t *testing.T) {
	// Exactly the shape br writes into issues.jsonl.
	line := `{"id":"hydra-code-335","title":"Fix: watchdog","status":"closed",` +
		`"dependencies":[{"issue_id":"hydra-code-335","depends_on_id":"hydra-code-elj",` +
		`"type":"blocks","created_by":"hydra"}]}`

	bead := readOne(t, line)

	if len(bead.Dependencies) != 1 {
		t.Fatalf("expected 1 dependency, got %d — object-shaped dependencies are being dropped", len(bead.Dependencies))
	}
	if got := string(bead.Dependencies[0]); got != "hydra-code-elj" {
		t.Errorf("dependency = %q, want the depends_on_id (hydra-code-elj); got the wrong direction?", got)
	}
}

// TestBeadFileReader_ParsesBrShowDependencyShape: `br show --json` uses `id`
// rather than `depends_on_id` for the same relationship. One parser has to
// handle both or GetBead-sourced beads lose their edges.
func TestBeadFileReader_ParsesBrShowDependencyShape(t *testing.T) {
	line := `{"id":"a-1","title":"t","status":"open",` +
		`"dependencies":[{"id":"a-2","title":"other","status":"open","dependency_type":"blocks"}]}`

	bead := readOne(t, line)

	if len(bead.Dependencies) != 1 || string(bead.Dependencies[0]) != "a-2" {
		t.Fatalf("expected dependency a-2 from the br-show shape, got %v", bead.Dependencies)
	}
}

// TestBeadFileReader_StillParsesStringDependencies keeps the older shape working
// — the fix must add a case, not replace one.
func TestBeadFileReader_StillParsesStringDependencies(t *testing.T) {
	line := `{"id":"a-1","title":"t","status":"open","dependencies":["a-2","a-3"]}`

	bead := readOne(t, line)

	if len(bead.Dependencies) != 2 {
		t.Fatalf("expected 2 string dependencies, got %v", bead.Dependencies)
	}
}

// TestBeadFileReader_MalformedDependenciesAreSkipped: a junk entry must not
// panic or invent an empty BeadID, which would create a bogus graph edge.
func TestBeadFileReader_MalformedDependenciesAreSkipped(t *testing.T) {
	line := `{"id":"a-1","title":"t","status":"open",` +
		`"dependencies":["",{"type":"blocks"},{"depends_on_id":""},42,null,{"depends_on_id":"a-9"}]}`

	bead := readOne(t, line)

	if len(bead.Dependencies) != 1 || string(bead.Dependencies[0]) != "a-9" {
		t.Fatalf("expected only the one valid dependency a-9, got %v", bead.Dependencies)
	}
}

// TestBeadFileReader_NoDependenciesStaysEmpty: a genuine root must still read as
// a root, or everything becomes a chain member.
func TestBeadFileReader_NoDependenciesStaysEmpty(t *testing.T) {
	for _, line := range []string{
		`{"id":"a-1","title":"t","status":"open"}`,
		`{"id":"a-1","title":"t","status":"open","dependencies":[]}`,
		`{"id":"a-1","title":"t","status":"open","dependencies":null}`,
	} {
		bead := readOne(t, line)
		if len(bead.Dependencies) != 0 {
			t.Errorf("expected no dependencies for %s, got %v", line, bead.Dependencies)
		}
	}
}
