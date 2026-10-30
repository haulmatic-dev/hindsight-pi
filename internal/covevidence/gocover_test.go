package covevidence

import (
	"os"
	"path/filepath"
	"testing"
)

// goCoverSetTwoFiles is a real-shaped `go test -coverprofile` excerpt in
// mode: set with two files: a.go has a covered block (2 stmts) and an uncovered
// block (3 stmts); b.go has one covered block (1 stmt).
const goCoverSetTwoFiles = `mode: set
example.com/m/pkg/a.go:10.20,12.3 2 1
example.com/m/pkg/a.go:14.1,16.2 3 0
example.com/m/pkg/b.go:1.1,2.2 1 1
`

func writeProfile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "cover.prof")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writing profile fixture: %v", err)
	}
	return path
}

func TestGoCover_ModeSetTwoFiles(t *testing.T) {
	requireImpl(t)
	files, err := ParseGoCover(writeProfile(t, goCoverSetTwoFiles))
	if err != nil {
		t.Fatalf("ParseGoCover returned error: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("parsed %d files, want 2: %v", len(files), files)
	}

	a, ok := files["example.com/m/pkg/a.go"]
	if !ok {
		t.Fatalf("a.go missing from result: %v", files)
	}
	// Hit = Σ NumStmt over blocks with Count>0 = 2; Found = Σ NumStmt over all
	// blocks = 2+3 = 5 (never sum raw execution counts).
	if a.Statements != (CoverageMetric{Found: 5, Hit: 2}) {
		t.Fatalf("a.go Statements = %+v, want {Found:5 Hit:2}", a.Statements)
	}
	if a.Statements != a.Lines {
		t.Fatalf("a.go Statements = %+v, must alias Lines %+v", a.Statements, a.Lines)
	}

	b, ok := files["example.com/m/pkg/b.go"]
	if !ok {
		t.Fatalf("b.go missing from result: %v", files)
	}
	if b.Statements != (CoverageMetric{Found: 1, Hit: 1}) {
		t.Fatalf("b.go Statements = %+v, want {Found:1 Hit:1}", b.Statements)
	}
}

func TestGoCover_ModeCountWeightsByNumStmt(t *testing.T) {
	requireImpl(t)
	// mode: count: the first block executed 100 times but carries NumStmt=4;
	// Hit must weight by NumStmt over Count>0 blocks (4), never sum the raw
	// execution counts (100) or mix them (104/106).
	profile := `mode: count
p/c.go:1.1,2.2 4 100
p/c.go:3.1,4.2 2 0
`
	files, err := ParseGoCover(writeProfile(t, profile))
	if err != nil {
		t.Fatalf("ParseGoCover returned error: %v", err)
	}
	c, ok := files["p/c.go"]
	if !ok {
		t.Fatalf("c.go missing from result: %v", files)
	}
	if c.Statements != (CoverageMetric{Found: 6, Hit: 4}) {
		t.Fatalf("c.go Statements = %+v, want {Found:6 Hit:4} (NumStmt-weighted, execution counts never summed)", c.Statements)
	}
}

func TestGoCover_MissingFileReturnsError(t *testing.T) {
	requireImpl(t)
	missing := filepath.Join(t.TempDir(), "absent.prof")
	files, err := ParseGoCover(missing)
	if err == nil {
		t.Fatalf("ParseGoCover(%q) returned nil error, want an error for a missing profile", missing)
	}
	if files != nil {
		t.Fatalf("ParseGoCover(%q) returned files %v alongside an error; want nil", missing, files)
	}
}

func TestGoCover_BranchesFunctionsFoundZero(t *testing.T) {
	requireImpl(t)
	// Go cover profiles carry no branch/function data: Branches/Functions must
	// report Found=0 so callers skip their Pct instead of dividing by zero.
	files, err := ParseGoCover(writeProfile(t, goCoverSetTwoFiles))
	if err != nil {
		t.Fatalf("ParseGoCover returned error: %v", err)
	}
	a := files["example.com/m/pkg/a.go"]
	if a.Branches.Found != 0 {
		t.Fatalf("a.go Branches.Found = %d, want 0 (go cover has no branch data)", a.Branches.Found)
	}
	if a.Functions.Found != 0 {
		t.Fatalf("a.go Functions.Found = %d, want 0 (go cover has no function data)", a.Functions.Found)
	}
	if got := a.Branches.Pct(); got != 0 {
		t.Fatalf("a.go Branches.Pct() = %v, want 0 when Found==0", got)
	}
}
