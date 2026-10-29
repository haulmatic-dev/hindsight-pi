package core

import (
	"strings"

	"github.com/haulmatic-dev/hydra/internal/beadfiles"
	"github.com/haulmatic-dev/hydra/pkg/types"
)

// beadTargetFiles resolves a bead's declared target files through the shared
// resolver (internal/beadfiles.Resolve) and applies one-directional
// sibling-test expansion: for each resolved impl file x.go, the sibling test
// file x_test.go is also in scope. A bead that declares only test files gains
// NO impl-file scope — such a diff stays empty and belongs to the GREEN bead
// of the chain.
//
// This replaces the bead-local parser (metadata files/target_files + a
// "Files:" description line) that used to live in bead_closer.go: both the
// validators and the close path now share one implementation, so the two
// lists can no longer drift. Metadata precedence ("files" then
// "target_files"), the fenced Context JSON block and the description
// "Files:"/"**Target Files:**" lines are the resolver's contract.
func beadTargetFiles(bead *types.Bead) []string {
	if bead == nil {
		return nil
	}
	return siblingTestExpansion(beadfiles.Resolve(*bead))
}

// siblingTestExpansion appends, without duplicates and in declaration order,
// the Go sibling test file (x_test.go) of every resolved impl file (.go, not
// itself a test). Test files and non-Go files pass through unexpanded; a
// nil/empty input yields nil so callers keep their no-targets semantics.
func siblingTestExpansion(files []string) []string {
	if len(files) == 0 {
		return nil
	}
	out := make([]string, 0, len(files))
	seen := make(map[string]bool, len(files))
	for _, f := range files {
		if !seen[f] {
			seen[f] = true
			out = append(out, f)
		}
		if !beadfiles.IsTestFile(f) {
			if sib := siblingTestPath(f); sib != "" && !seen[sib] {
				seen[sib] = true
				out = append(out, sib)
			}
		}
	}
	return out
}

// siblingTestPath returns x_test.go for the impl file x.go, or "" for
// non-Go paths and Go test files.
func siblingTestPath(path string) string {
	if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
		return ""
	}
	return strings.TrimSuffix(path, ".go") + "_test.go"
}
