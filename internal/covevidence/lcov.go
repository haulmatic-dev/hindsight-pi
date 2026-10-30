// Package covevidence extracts per-file coverage evidence from the coverage
// reports verification commands already produce (lcov tracefiles, go
// coverprofiles). Standard library only.
//
// RED PHASE (FR-001, docs/PRDs/coverage-truth-prd-20261028.md): the parser
// functions in this package are compile-only contract stubs. The pinned types
// below are declared so `go vet` passes and the contract tests compile; every
// extraction behaves inertly (nil/zero results) until GREEN implements the
// real parsing. Behavior tests skip loudly unless COVEVIDENCE_ENFORCE=1 is
// set, mirroring the established RED gates in this repo (BEAD_CLAIMS_ENFORCE,
// MIRROR_HEALTH_ENFORCE, BEADS_MIRROR_AUDIT_ENFORCE).
package covevidence

import "io"

// CoverageMetric is one found/hit pair (statements, branches, functions, or
// lines). Pct is only meaningful when Found>0; it returns 0 when Found==0 and
// callers skip a metric whose Found is 0 (PRD FR-001).
type CoverageMetric struct {
	Found int
	Hit   int
}

// Pct returns Hit/Found*100, or 0 when Found==0.
func (m CoverageMetric) Pct() float64 {
	if m.Found == 0 {
		return 0
	}
	return float64(m.Hit) / float64(m.Found) * 100
}

// FileCoverage is the per-file coverage evidence extracted from a report.
// Statements alias Lines (both derive from lcov DA/LH/LF or go-cover NumStmt),
// so they always report identical numbers by definition.
type FileCoverage struct {
	Path       string
	Statements CoverageMetric
	Branches   CoverageMetric
	Functions  CoverageMetric
	Lines      CoverageMetric
}

// ParseStats reports non-fatal conditions observed while parsing a report.
// Malformed lines are skipped with a count, never fatal (PRD FR-001).
type ParseStats struct {
	MalformedLines int
}

// covevidenceImplemented is the RED gate flag. GREEN flips it to true when the
// real parsers land.
const covevidenceImplemented = false

// ParseLcov parses an lcov tracefile (SF/FN/FNDA/FNF/BRDA/BRF/BRH/DA/LH/LF
// records) into per-file coverage. Malformed lines are skipped and counted,
// never fatal. Duplicate SF blocks merge by unioning the per-line DA map and
// recomputing LH/LF from the union (never summing LH/LF across blocks).
// Functions: Found=count(FN), Hit=count(FNDA>0). Branches: Found=count(BRDA),
// Hit=count(BRH). Statements alias Lines (DA/LH/LF).
//
// RED stub: inert.
func ParseLcov(r io.Reader) (map[string]FileCoverage, error) {
	return nil, nil
}

// ParseLcovWithStats is ParseLcov plus the non-fatal skip accounting
// (ParseStats.MalformedLines). ParseLcov delegates to it.
//
// RED stub: inert.
func ParseLcovWithStats(r io.Reader) (map[string]FileCoverage, ParseStats, error) {
	return nil, ParseStats{}, nil
}

// Match filters files to those matching the target list. Targets match as
// exact paths, `dir/` prefixes (every file under dir), or `dir/**`(/glob)
// patterns: strip the `dir/**/` prefix and accept every file under dir whose
// remaining path matches the remainder pattern segment-by-segment via
// filepath.Match, where `**` means zero or more directories (so `src/**`
// matches both `src/a.ts` and `src/x/a.ts`, and `src/**/*.ts` matches nested
// .ts only). Unmatched targets contribute nothing: a file absent from the
// result is unmatched, never zero-valued.
//
// RED stub: inert.
func Match(targets []string, files map[string]FileCoverage) map[string]FileCoverage {
	return nil
}
