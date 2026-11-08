// Package covevidence extracts per-file coverage evidence from the coverage
// reports verification commands already produce (lcov tracefiles, go
// coverprofiles). Standard library only.
//
// FR-001 (docs/PRDs/coverage-truth-prd-20261028.md): malformed lines are
// skipped and counted, never fatal; a metric with Found==0 reports Pct 0 and
// callers skip it.
package covevidence

import (
	"bufio"
	"io"
	"path/filepath"
	"strconv"
	"strings"
)

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

// covevidenceImplemented is the RED gate flag; the real parsers are landed.
const covevidenceImplemented = true

// lcovRecord accumulates one SF block's detail records before finalization.
type lcovRecord struct {
	path  string
	fns   map[string]bool // fn name -> seen (FN)
	fnHit map[string]bool // fn name -> seen with FNDA>0
	brs   map[string]bool // "line,block,branch" -> seen (BRDA)
	brHit map[string]bool // same key -> taken > 0
	lns   map[string]bool // line -> seen (DA)
	lnHit map[string]bool // line -> seen with count > 0
	// summary-only fallbacks in case the tracefile carries summaries without
	// detail records (LF/LH, FNF/FNH, BRF/BRH).
	sumLines, sumLinesHit, sumFns, sumFnsHit, sumBrs, sumBrsHit int
	sawLine, sawFn, sawBrDetail                                 bool
}

func newLcovRecord(path string) *lcovRecord {
	return &lcovRecord{
		path:  path,
		fns:   map[string]bool{},
		fnHit: map[string]bool{},
		brs:   map[string]bool{},
		brHit: map[string]bool{},
		lns:   map[string]bool{},
		lnHit: map[string]bool{},
	}
}

// finalize renders the accumulated detail records as FileCoverage, recomputing
// LH/LF from the unioned DA map and falling back to the record's own summary
// lines only when the corresponding detail records never appeared.
func (r *lcovRecord) finalize() FileCoverage {
	fc := FileCoverage{
		Path:       r.path,
		Statements: CoverageMetric{Found: len(r.lns), Hit: count(r.lnHit)},
	}
	fc.Lines = fc.Statements
	if !r.sawLine && (r.sumLines > 0 || r.sumLinesHit > 0) {
		fc.Lines = CoverageMetric{Found: r.sumLines, Hit: r.sumLinesHit}
		fc.Statements = fc.Lines
	}
	fc.Functions = CoverageMetric{Found: len(r.fns), Hit: count(r.fnHit)}
	if !r.sawFn && (r.sumFns > 0 || r.sumFnsHit > 0) {
		fc.Functions = CoverageMetric{Found: r.sumFns, Hit: r.sumFnsHit}
	}
	fc.Branches = CoverageMetric{Found: len(r.brs), Hit: count(r.brHit)}
	if !r.sawBrDetail && (r.sumBrs > 0 || r.sumBrsHit > 0) {
		fc.Branches = CoverageMetric{Found: r.sumBrs, Hit: r.sumBrsHit}
	}
	return fc
}

func count(m map[string]bool) int {
	n := 0
	for _, v := range m {
		if v {
			n++
		}
	}
	return n
}

// merge unions another record for the same path into r (duplicated SF blocks
// must union per-line DA state, never sum LH/LF across blocks).
func (r *lcovRecord) merge(o *lcovRecord) {
	for k := range o.fns {
		r.fns[k] = true
	}
	for k, v := range o.fnHit {
		if v {
			r.fnHit[k] = true
		}
	}
	for k := range o.brs {
		r.brs[k] = true
	}
	for k, v := range o.brHit {
		if v {
			r.brHit[k] = true
		}
	}
	for k := range o.lns {
		r.lns[k] = true
	}
	for k, v := range o.lnHit {
		if v {
			r.lnHit[k] = true
		}
	}
	r.sumLines = maxInt(r.sumLines, o.sumLines)
	r.sumLinesHit = maxInt(r.sumLinesHit, o.sumLinesHit)
	r.sumFns = maxInt(r.sumFns, o.sumFns)
	r.sumFnsHit = maxInt(r.sumFnsHit, o.sumFnsHit)
	r.sumBrs = maxInt(r.sumBrs, o.sumBrs)
	r.sumBrsHit = maxInt(r.sumBrsHit, o.sumBrsHit)
	r.sawLine = r.sawLine || o.sawLine
	r.sawFn = r.sawFn || o.sawFn
	r.sawBrDetail = r.sawBrDetail || o.sawBrDetail
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// ParseLcov parses an lcov tracefile (SF/FN/FNDA/FNF/BRDA/BRF/BRH/DA/LH/LF
// records) into per-file coverage. Malformed lines are skipped and counted,
// never fatal. Duplicate SF blocks merge by unioning the per-line DA map and
// recomputing LH/LF from the union (never summing LH/LF across blocks).
// Functions: Found=count(FN), Hit=count(FNDA>0). Branches: Found=count(BRDA),
// Hit=count(BRH). Statements alias Lines (DA/LH/LF).
func ParseLcov(r io.Reader) (map[string]FileCoverage, error) {
	files, _, err := ParseLcovWithStats(r)
	return files, err
}

// ParseLcovWithStats is ParseLcov plus the non-fatal skip accounting
// (ParseStats.MalformedLines). ParseLcov delegates to it.
func ParseLcovWithStats(r io.Reader) (map[string]FileCoverage, ParseStats, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	stats := ParseStats{}
	out := make(map[string]FileCoverage)
	open := make(map[string]*lcovRecord)
	var cur *lcovRecord

	flush := func() {
		if cur == nil {
			return
		}
		rec := open[cur.path]
		if rec != nil {
			rec.merge(cur)
			cur = rec
		}
		open[cur.path] = cur
		cur = nil
	}
	for sc.Scan() {
		line := sc.Text()
		if line == "" {
			continue
		}
		switch {
		case line == "end_of_record":
			flush()
		case strings.HasPrefix(line, "SF:"):
			flush()
			cur = newLcovRecord(line[3:])
		case strings.HasPrefix(line, "TN:"):
			// tracefile/test-case metadata: nothing to accumulate.
		case strings.HasPrefix(line, "FN:"):
			if cur == nil {
				stats.MalformedLines++
				continue
			}
			cur.sawFn = true
			if _, name, ok := fnName(line[3:]); ok {
				cur.fns[name] = true
			} else {
				stats.MalformedLines++
			}
		case strings.HasPrefix(line, "FNDA:"):
			if cur == nil {
				stats.MalformedLines++
				continue
			}
			cnt, rest, ok := splitCount(line[5:])
			if !ok {
				stats.MalformedLines++
				continue
			}
			if rest == "" {
				stats.MalformedLines++
				continue
			}
			cur.fnHit[rest] = cnt > 0
		case strings.HasPrefix(line, "BRDA:"):
			if cur == nil {
				stats.MalformedLines++
				continue
			}
			cur.sawBrDetail = true
			key, taken, ok := brdaKey(line[5:])
			if !ok {
				stats.MalformedLines++
				continue
			}
			cur.brs[key] = true
			cur.brHit[key] = taken > 0
		case strings.HasPrefix(line, "DA:"):
			if cur == nil {
				stats.MalformedLines++
				continue
			}
			cur.sawLine = true
			lno, cnt, ok := parseDA(line[3:])
			if !ok {
				stats.MalformedLines++
				continue
			}
			key := strconv.Itoa(lno)
			cur.lns[key] = true
			cur.lnHit[key] = cur.lnHit[key] || cnt > 0
		case strings.HasPrefix(line, "LH:"):
			if cur == nil {
				stats.MalformedLines++
				continue
			}
			if n, err := strconv.Atoi(line[3:]); err == nil {
				cur.sumLinesHit = n
			} else {
				stats.MalformedLines++
			}
		case strings.HasPrefix(line, "LF:"):
			if cur == nil {
				stats.MalformedLines++
				continue
			}
			if n, err := strconv.Atoi(line[3:]); err == nil {
				cur.sumLines = n
			} else {
				stats.MalformedLines++
			}
		case strings.HasPrefix(line, "FNH:"):
			if cur == nil {
				stats.MalformedLines++
				continue
			}
			if n, err := strconv.Atoi(line[4:]); err == nil {
				cur.sumFnsHit = n
			} else {
				stats.MalformedLines++
			}
		case strings.HasPrefix(line, "FNF:"):
			if cur == nil {
				stats.MalformedLines++
				continue
			}
			if n, err := strconv.Atoi(line[4:]); err == nil {
				cur.sumFns = n
			} else {
				stats.MalformedLines++
			}
		case strings.HasPrefix(line, "BRH:"):
			if cur == nil {
				stats.MalformedLines++
				continue
			}
			if n, err := strconv.Atoi(line[4:]); err == nil {
				cur.sumBrsHit = n
			} else {
				stats.MalformedLines++
			}
		case strings.HasPrefix(line, "BRF:"):
			if cur == nil {
				stats.MalformedLines++
				continue
			}
			if n, err := strconv.Atoi(line[4:]); err == nil {
				cur.sumBrs = n
			} else {
				stats.MalformedLines++
			}
		default:
			stats.MalformedLines++
		}
	}
	flush()
	for path, rec := range open {
		out[path] = rec.finalize()
	}
	return out, stats, nil
}

// fnName splits "line,name" and returns the function name. The line number is
// validated but unused.
func fnName(s string) (int, string, bool) {
	i := strings.IndexByte(s, ',')
	if i < 0 {
		return 0, "", false
	}
	if _, err := strconv.Atoi(s[:i]); err != nil {
		return 0, "", false
	}
	name := strings.TrimSpace(s[i+1:])
	if name == "" {
		return 0, "", false
	}
	return 0, name, true
}

// splitCount parses a leading decimal count followed by a comma-delimited
// remainder ("count,rest").
func splitCount(s string) (int, string, bool) {
	i := strings.IndexByte(s, ',')
	if i < 0 {
		if n, err := strconv.Atoi(strings.TrimSpace(s)); err == nil {
			return n, "", true
		}
		return 0, "", false
	}
	n, err := strconv.Atoi(strings.TrimSpace(s[:i]))
	if err != nil {
		return 0, "", false
	}
	return n, strings.TrimSpace(s[i+1:]), true
}

// brdaKey builds the branch-identity key from "line,block,branch,taken" and
// validates the taken field; the key is order-stable per branch instance.
func brdaKey(s string) (string, int, bool) {
	i := strings.LastIndexByte(s, ',')
	if i < 0 {
		return "", 0, false
	}
	takenStr := strings.TrimSpace(s[i+1:])
	body := strings.TrimSpace(s[:i])
	parts := strings.Split(body, ",")
	if len(parts) < 3 {
		return "", 0, false
	}
	line, block, branch := "", "", ""
	// lcov orders fields line,block,branch (branch may itself contain commas
	// only in exotic generators; the 3rd field is enough identity here).
	line, block, branch = parts[0], parts[1], strings.Join(parts[2:], ",")
	if _, err := strconv.Atoi(strings.TrimSpace(line)); err != nil {
		return "", 0, false
	}
	taken := 0
	if takenStr != "-" {
		n, err := strconv.Atoi(takenStr)
		if err != nil {
			return "", 0, false
		}
		taken = n
	}
	return line + "," + block + "," + branch, taken, true
}

// parseDA parses "line,hit".
func parseDA(s string) (int, int, bool) {
	parts := strings.Split(s, ",")
	if len(parts) != 2 {
		return 0, 0, false
	}
	lno, err1 := strconv.Atoi(strings.TrimSpace(parts[0]))
	cnt, err2 := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err1 != nil || err2 != nil {
		return 0, 0, false
	}
	return lno, cnt, true
}

// Match filters files to those matching the target list. Targets match as
// exact paths, `dir/` prefixes (every file under dir), or `dir/**`(/glob)
// patterns: the `**` means zero or more directories (so `src/**` matches both
// `src/a.ts` and `src/x/a.ts`, and `src/**/*.ts` matches nested .ts only).
// Unmatched targets contribute nothing: a file absent from the result is
// unmatched, never zero-valued.
func Match(targets []string, files map[string]FileCoverage) map[string]FileCoverage {
	out := make(map[string]FileCoverage)
	for _, tgt := range targets {
		var matcher func(string) bool
		switch {
		case tgt == "":
			continue
		case strings.HasSuffix(tgt, "/"):
			matcher = func(p string) bool { return strings.HasPrefix(p, tgt) }
		case strings.Contains(tgt, "**"):
			i := strings.Index(tgt, "**")
			base := tgt[:i]
			pattern := "**" + tgt[i+2:]
			if !strings.HasSuffix(base, "/") {
				matcher = func(p string) bool { return p == base || matchGlob(p, pattern, base) }
			} else {
				matcher = func(p string) bool {
					tail := strings.TrimPrefix(p, base)
					if tail == p {
						// base not a prefix; tail==p means no match
						return false
					}
					return matchGlob(tail, pattern, "")
				}
			}
		default:
			matcher = func(p string) bool { return p == tgt }
		}
		for path, fc := range files {
			if matcher(path) {
				out[path] = fc
			}
		}
	}
	return out
}

// matchGlob matches the remaining pattern with '**' meaning zero or more
// directories against the (already base-stripped) path.
func matchGlob(path, pattern, base string) bool {
	path = strings.TrimPrefix(path, base)
	ps := strings.Split(pattern, "/")
	ts := strings.Split(path, "/")
	var m func(i, j int) bool
	m = func(i, j int) bool {
		for {
			if j == len(ps) {
				return i == len(ts)
			}
			if ps[j] == "**" {
				for k := i; k <= len(ts); k++ {
					if m(k, j+1) {
						return true
					}
				}
				return false
			}
			if i == len(ts) {
				return false
			}
			ok, err := filepath.Match(ps[j], ts[i])
			if err != nil || !ok {
				return false
			}
			i++
			j++
		}
	}
	return m(0, 0)
}
