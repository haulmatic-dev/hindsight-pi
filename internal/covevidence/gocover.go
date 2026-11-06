package covevidence

import (
	"bufio"
	"os"
	"strconv"
	"strings"
)

// ParseGoCover converts a `go test -coverprofile` file (mode: set, atomic, or
// count) into per-file coverage: statements Hit = Σ NumStmt over blocks with
// Count>0, Found = Σ NumStmt over all blocks (never sum raw execution
// counts); branches and functions report Found=0 so Pct is skipped by callers
// when Found==0. A missing or unreadable profile returns an error with a nil
// map. Malformed block lines are skipped, never fatal.
func ParseGoCover(profilePath string) (map[string]FileCoverage, error) {
	f, err := os.Open(profilePath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	acc := make(map[string]*goCoverAccum)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "mode:") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 3 {
			continue
		}
		locIdx := strings.LastIndexByte(fields[0], ':')
		if locIdx < 0 {
			continue
		}
		path := fields[0][:locIdx]
		numStmt, err1 := strconv.Atoi(fields[1])
		if err1 != nil || numStmt < 0 {
			continue
		}
		exec, err2 := strconv.Atoi(fields[2])
		if err2 != nil {
			continue
		}
		a := acc[path]
		if a == nil {
			a = &goCoverAccum{path: path}
			acc[path] = a
		}
		a.found += numStmt
		if exec > 0 {
			a.hit += numStmt
		}
	}
	out := make(map[string]FileCoverage, len(acc))
	for path, a := range acc {
		m := CoverageMetric{Found: a.found, Hit: a.hit}
		out[path] = FileCoverage{Path: path, Statements: m, Lines: m}
	}
	return out, nil
}

type goCoverAccum struct {
	path       string
	found, hit int
}
