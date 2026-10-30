package covevidence

// ParseGoCover converts a `go test -coverprofile` file (mode: set, atomic, or
// count) into per-file coverage: statements Hit = Σ NumStmt over blocks with
// Count>0, Found = Σ NumStmt over all blocks (never sum raw execution
// counts); branches and functions report Found=0 so Pct is skipped by callers
// when Found==0. A missing or unreadable profile returns an error.
//
// RED stub: inert (never reads the path, never errors).
func ParseGoCover(profilePath string) (map[string]FileCoverage, error) {
	return nil, nil
}
