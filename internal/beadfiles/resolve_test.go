package beadfiles

import (
	"os"
	"reflect"
	"testing"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// requireImpl skips the contract tests loudly while the RED-phase stub is in
// place. BEADFILES_ENFORCE=1 disables the skip so the real assertions run
// against the stub and fail for the missing behavior.
func requireImpl(t *testing.T) {
	t.Helper()
	if resolveImplemented {
		return
	}
	if os.Getenv("BEADFILES_ENFORCE") == "1" {
		return
	}
	t.Skip("RED: beadfiles resolver contract not yet implemented")
}

// hydra367dDescription mirrors the live hydra-367d description shape: a
// "## Context" heading, a fenced json block carrying target.files, then
// trailing instruction and gate-log text (which itself contains braces).
const hydra367dDescription = "RED: Write tests for Validator truth\n\n" +
	"## Context\n\n" +
	"```json\n" +
	"{\n" +
	"  \"task\": {\"title\": \"Validator truth\", \"phase\": \"red\"},\n" +
	"  \"target\": {\"files\": [\"internal/core/service_starter.go\"], \"functions\": []},\n" +
	"  \"criteria\": {\"parent\": [\"a\"], \"phase_specific\": [\"b\"]}\n" +
	"}\n" +
	"```\n\n" +
	"## Instructions\n\nWrite failing tests.\n\n" +
	"## Gate failure\n\n" +
	"FAIL internal/core {unexpected} [build failed]\n" +
	"```json\n{\"files\": [\"must/not/be/used.go\"]}\n```\n"

func bead(desc string, meta map[string]string) types.Bead {
	return types.Bead{Description: desc, Metadata: meta}
}

func assertFiles(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) == 0 && len(want) == 0 {
		return
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Resolve = %#v, want %#v", got, want)
	}
}

func TestResolve_MetadataFilesOnly(t *testing.T) {
	requireImpl(t)
	got := Resolve(bead("", map[string]string{"files": "a/x.go, b/y.go"}))
	assertFiles(t, got, []string{"a/x.go", "b/y.go"})
}

func TestResolve_MetadataTargetFilesFallback(t *testing.T) {
	requireImpl(t)
	got := Resolve(bead("", map[string]string{"files": "  ", "target_files": "c/z.go"}))
	assertFiles(t, got, []string{"c/z.go"})
}

func TestResolve_MetadataBeatsDescription(t *testing.T) {
	requireImpl(t)
	got := Resolve(bead(hydra367dDescription+"\nFiles: other.go\n", map[string]string{"files": "meta.go"}))
	assertFiles(t, got, []string{"meta.go"})
}

func TestResolve_DescriptionJSONOnlyHydra367dShape(t *testing.T) {
	requireImpl(t)
	got := Resolve(bead(hydra367dDescription, nil))
	assertFiles(t, got, []string{"internal/core/service_starter.go"})
}

func TestResolve_JSONOnlyFromFirstBlockAfterContextHeading(t *testing.T) {
	requireImpl(t)
	desc := "```json\n{\"target\": {\"files\": [\"before/heading.go\"]}}\n```\n\n" +
		"## Context\n\nprose\n\n" +
		"```json\n{\"target\": {\"files\": [\"first/block.go\"]}}\n```\n\n" +
		"```json\n{\"target\": {\"files\": [\"second/block.go\"]}}\n```\n"
	assertFiles(t, Resolve(bead(desc, nil)), []string{"first/block.go"})
}

func TestResolve_EmptyFilesArrayIsEmpty(t *testing.T) {
	requireImpl(t)
	desc := "## Context\n\n```json\n{\"target\": {\"files\": [], \"functions\": []}}\n```\n"
	got := Resolve(bead(desc, nil))
	if len(got) != 0 {
		t.Fatalf("Resolve = %#v, want empty", got)
	}
}

func TestResolve_TargetFilesBacktickedLine(t *testing.T) {
	requireImpl(t)
	desc := "**Type:** fr\n**Target Files:** `internal/beadfiles/resolve.go`, `internal/beadfiles/resolve_test.go`\n**Test Command:** `go test`\n"
	assertFiles(t, Resolve(bead(desc, nil)),
		[]string{"internal/beadfiles/resolve.go", "internal/beadfiles/resolve_test.go"})
}

func TestResolve_FilesLineSeparatorsAndTBD(t *testing.T) {
	requireImpl(t)
	assertFiles(t, Resolve(bead("Files: a.go; b.go TBD", nil)), []string{"a.go", "b.go"})
	assertFiles(t, Resolve(bead("intro\nfiles:\ta.go,b.go;c.go d.go", nil)),
		[]string{"a.go", "b.go", "c.go", "d.go"})
}

func TestResolve_SymbolSuffixStripped(t *testing.T) {
	requireImpl(t)
	assertFiles(t, Resolve(bead("", map[string]string{"files": "a/b.go:Func, c/d.go"})),
		[]string{"a/b.go", "c/d.go"})
	desc := "## Context\n\n```json\n{\"target\": {\"files\": [\"a/b.go:Func\"]}}\n```\n"
	assertFiles(t, Resolve(bead(desc, nil)), []string{"a/b.go"})
}

func TestResolve_MalformedJSONFallsThroughToNextSource(t *testing.T) {
	requireImpl(t)
	desc := "## Context\n\n```json\n{\"target\": {\"files\": [\"broken.go\"\n```\n\nFiles: next/source.go\n"
	assertFiles(t, Resolve(bead(desc, nil)), []string{"next/source.go"})

	// Malformed JSON with no further source is absent, never a panic.
	only := "## Context\n\n```json\n{not json\n```\n"
	if got := Resolve(bead(only, nil)); len(got) != 0 {
		t.Fatalf("Resolve = %#v, want empty", got)
	}
}

func TestResolve_DedupesPreservingOrder(t *testing.T) {
	requireImpl(t)
	got := Resolve(bead("", map[string]string{"files": "b.go, a.go, b.go, a.go:Sym, c.go"}))
	assertFiles(t, got, []string{"b.go", "a.go", "c.go"})
}

func TestResolve_NoSourcesIsEmpty(t *testing.T) {
	requireImpl(t)
	if got := Resolve(types.Bead{}); len(got) != 0 {
		t.Fatalf("Resolve(zero bead) = %#v, want empty", got)
	}
	if got := Resolve(bead("just prose, no declared files", nil)); len(got) != 0 {
		t.Fatalf("Resolve(prose) = %#v, want empty", got)
	}
}

func TestIsTestFile_Positives(t *testing.T) {
	requireImpl(t)
	for _, p := range []string{
		"foo_test.go", "internal/core/foo_test.go",
		"web/a.spec.ts", "web/a.test.ts",
		"web/a.spec.js", "web/a.test.js",
		"tests/test_foo.py", "test_foo.py",
		"pkg/foo_test.py",
	} {
		t.Run(p, func(t *testing.T) {
			if !IsTestFile(p) {
				t.Fatalf("IsTestFile(%q) = false, want true", p)
			}
		})
	}
}

func TestIsTestFile_NegativePerLanguage(t *testing.T) {
	requireImpl(t)
	for _, p := range []string{
		"internal/core/foo.go", "internal/core/testutil.go", // go
		"web/a.ts", "web/spec.ts", // typescript
		"web/a.js", "web/test.js", // javascript
		"pkg/foo.py", "pkg/testing.py", "pkg/latest_foo.py", // python
		"README.md", "",
	} {
		t.Run(p, func(t *testing.T) {
			if IsTestFile(p) {
				t.Fatalf("IsTestFile(%q) = true, want false", p)
			}
		})
	}
}
