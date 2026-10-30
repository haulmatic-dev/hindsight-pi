package covevidence

import (
	"os"
	"strings"
	"testing"
)

// covevidenceEnforceEnv gates the RED behavior tests: by default they skip
// loudly (daemon gate stays green pre-GREEN); with COVEVIDENCE_ENFORCE=1 they
// fail, proving RED detects the missing contract.
const covevidenceEnforceEnv = "COVEVIDENCE_ENFORCE"

// requireImpl skips while the RED stub is in place; under COVEVIDENCE_ENFORCE=1
// it fails instead, so the RED phase is provable.
func requireImpl(t *testing.T) {
	t.Helper()
	if covevidenceImplemented {
		return
	}
	if os.Getenv(covevidenceEnforceEnv) == "1" {
		t.Fatalf("RED: covevidence contract not yet implemented (%s=1 set)", covevidenceEnforceEnv)
	}
	t.Skip("RED: covevidence contract not yet implemented")
}

// lcovThreeFiles is a real-shaped lcov excerpt with 3 SF blocks: one full
// record (functions + branches + lines), one lines-only, one zero-hit.
const lcovThreeFiles = `TN:
SF:src/app.ts
FN:10,(anonymous_1)
FNDA:3,(anonymous_1)
FNF:1
FNH:1
BRDA:20,0,0,1
BRDA:20,0,1,0
BRF:2
BRH:1
DA:10,1
DA:20,0
DA:30,4
LF:3
LH:2
end_of_record
SF:src/util.ts
DA:1,5
DA:2,0
LF:2
LH:1
end_of_record
SF:src/zero.ts
DA:1,0
LF:1
LH:0
end_of_record
`

func mustParseLcov(t *testing.T, input string) (map[string]FileCoverage, ParseStats) {
	t.Helper()
	files, stats, err := ParseLcovWithStats(strings.NewReader(input))
	if err != nil {
		t.Fatalf("ParseLcovWithStats returned error: %v", err)
	}
	return files, stats
}

func TestLcov_ParseThreeSFBlocks(t *testing.T) {
	requireImpl(t)
	files, stats := mustParseLcov(t, lcovThreeFiles)

	if stats.MalformedLines != 0 {
		t.Fatalf("clean fixture reported %d malformed lines, want 0", stats.MalformedLines)
	}
	if len(files) != 3 {
		t.Fatalf("parsed %d files, want 3: %v", len(files), files)
	}

	app, ok := files["src/app.ts"]
	if !ok {
		t.Fatalf("src/app.ts missing from result: %v", files)
	}
	wantFuncs := CoverageMetric{Found: 1, Hit: 1} // count(FN)=1, count(FNDA>0)=1
	if app.Functions != wantFuncs {
		t.Fatalf("src/app.ts Functions = %+v, want %+v", app.Functions, wantFuncs)
	}
	wantBranches := CoverageMetric{Found: 2, Hit: 1} // count(BRDA)=2, count(BRH)=1
	if app.Branches != wantBranches {
		t.Fatalf("src/app.ts Branches = %+v, want %+v", app.Branches, wantBranches)
	}
	wantLines := CoverageMetric{Found: 3, Hit: 2} // LF=3, LH=2 (from DA map)
	if app.Lines != wantLines {
		t.Fatalf("src/app.ts Lines = %+v, want %+v", app.Lines, wantLines)
	}
	if app.Statements != app.Lines {
		t.Fatalf("src/app.ts Statements = %+v, must alias Lines %+v", app.Statements, app.Lines)
	}

	util, ok := files["src/util.ts"]
	if !ok {
		t.Fatalf("src/util.ts missing from result: %v", files)
	}
	if util.Lines != (CoverageMetric{Found: 2, Hit: 1}) {
		t.Fatalf("src/util.ts Lines = %+v, want {2 1}", util.Lines)
	}

	zero, ok := files["src/zero.ts"]
	if !ok {
		t.Fatalf("src/zero.ts missing from result: %v", files)
	}
	if zero.Lines != (CoverageMetric{Found: 1, Hit: 0}) {
		t.Fatalf("src/zero.ts Lines = %+v, want {1 0}", zero.Lines)
	}
}

func TestLcov_ZeroHitFileYieldsPctZero(t *testing.T) {
	requireImpl(t)
	files, _ := mustParseLcov(t, lcovThreeFiles)

	zero := files["src/zero.ts"]
	if got := zero.Lines.Pct(); got != 0 {
		t.Fatalf("zero-hit file Lines.Pct() = %v, want 0", got)
	}
	if got := zero.Statements.Pct(); got != 0 {
		t.Fatalf("zero-hit file Statements.Pct() = %v, want 0", got)
	}
}

func TestLcov_MalformedLineSkippedAndCounted(t *testing.T) {
	requireImpl(t)
	// Inject garbage records: an unknown token, a malformed DA line, and a
	// truncated SF record. Parsing must stay non-fatal and skip+count each.
	noisy := strings.Replace(lcovThreeFiles,
		"SF:src/util.ts\n",
		"NOTARECORD\nDA:notanumber,7\nSF\nSF:src/util.ts\n", 1)

	files, stats := mustParseLcov(t, noisy)

	if stats.MalformedLines != 3 {
		t.Fatalf("MalformedLines = %d, want 3", stats.MalformedLines)
	}
	// The parsed evidence must be identical to the clean fixture parse.
	clean, _ := mustParseLcov(t, lcovThreeFiles)
	for path, want := range clean {
		got, ok := files[path]
		if !ok {
			t.Fatalf("file %s missing after garbage injection: %v", path, files)
		}
		if got != want {
			t.Fatalf("file %s = %+v, want %+v (garbage lines must not affect parsing)", path, got, want)
		}
	}
	if len(files) != len(clean) {
		t.Fatalf("parsed %d files, want %d", len(files), len(clean))
	}
}

func TestLcov_DuplicateSFMergesByDAUnion(t *testing.T) {
	requireImpl(t)
	// Two SF blocks for the same file: line 1 covered in both (union counts it
	// once, never sums), line 2 covered only in the first, line 3 only in the
	// second. LH/LF are recomputed from the unioned DA map.
	dup := `SF:a.go
DA:1,1
DA:2,1
LF:2
LH:2
end_of_record
SF:a.go
DA:1,1
DA:3,0
LF:2
LH:1
end_of_record
`

	files, _ := mustParseLcov(t, dup)

	got, ok := files["a.go"]
	if !ok {
		t.Fatalf("a.go missing from result: %v", files)
	}
	want := CoverageMetric{Found: 3, Hit: 2}
	if got.Lines != want {
		t.Fatalf("a.go Lines = %+v, want %+v (DA union, not LH/LF sum 4/3)", got.Lines, want)
	}
	if got.Statements != want {
		t.Fatalf("a.go Statements = %+v, want %+v", got.Statements, want)
	}
}

func matchTestFiles() map[string]FileCoverage {
	return map[string]FileCoverage{
		"src/a.ts":      {Path: "src/a.ts"},
		"src/x/a.ts":    {Path: "src/x/a.ts"},
		"src/x/b.go":    {Path: "src/x/b.go"},
		"src/x/note.md": {Path: "src/x/note.md"},
		"app/main.go":   {Path: "app/main.go"},
	}
}

func keysOf(m map[string]FileCoverage) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

func TestLcov_Match_ExactPath(t *testing.T) {
	requireImpl(t)
	got := Match([]string{"src/a.ts"}, matchTestFiles())
	if len(got) != 1 {
		t.Fatalf("exact match returned %d files %v, want exactly 1", len(got), keysOf(got))
	}
	if _, ok := got["src/a.ts"]; !ok {
		t.Fatalf("exact match missing src/a.ts: %v", keysOf(got))
	}
}

func TestLcov_Match_DirPrefix(t *testing.T) {
	requireImpl(t)
	got := Match([]string{"src/"}, matchTestFiles())
	if len(got) != 4 {
		t.Fatalf("dir prefix returned %d files %v, want 4 (everything under src/)", len(got), keysOf(got))
	}
	if _, ok := got["app/main.go"]; ok {
		t.Fatalf("dir prefix matched outside the directory: %v", keysOf(got))
	}
}

func TestLcov_Match_GlobstarMatchesDirectAndNested(t *testing.T) {
	requireImpl(t)
	got := Match([]string{"src/**"}, matchTestFiles())
	// ** = zero or more directories: matches src/a.ts AND src/x/a.ts (and the
	// rest of src/), never app/main.go.
	for _, want := range []string{"src/a.ts", "src/x/a.ts"} {
		if _, ok := got[want]; !ok {
			t.Fatalf("src/** must match %s; got %v", want, keysOf(got))
		}
	}
	if len(got) != 4 {
		t.Fatalf("src/** returned %d files %v, want 4", len(got), keysOf(got))
	}
	if _, ok := got["app/main.go"]; ok {
		t.Fatalf("src/** matched outside the directory: %v", keysOf(got))
	}
}

func TestLcov_Match_NestedGlobOnlyTS(t *testing.T) {
	requireImpl(t)
	got := Match([]string{"src/**/*.ts"}, matchTestFiles())
	if len(got) != 2 {
		t.Fatalf("src/**/*.ts returned %d files %v, want 2", len(got), keysOf(got))
	}
	for _, want := range []string{"src/a.ts", "src/x/a.ts"} {
		if _, ok := got[want]; !ok {
			t.Fatalf("src/**/*.ts must match %s; got %v", want, keysOf(got))
		}
	}
	for _, absent := range []string{"src/x/b.go", "src/x/note.md", "app/main.go"} {
		if _, ok := got[absent]; ok {
			t.Fatalf("src/**/*.ts must not match %s; got %v", absent, keysOf(got))
		}
	}
}

func TestLcov_Match_UnmatchedAbsent(t *testing.T) {
	requireImpl(t)
	got := Match([]string{"missing/**", "nope.ts"}, matchTestFiles())
	if len(got) != 0 {
		t.Fatalf("unmatched targets returned %v, want an empty result (absent, not zero-valued)", keysOf(got))
	}
}
