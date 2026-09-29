package core

import (
	"fmt"
	"log"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	quarantinePrefix = "hydra/quarantine/"
	day              = 24 * time.Hour
)

// DrainQuarantineBranches scans a project's hydra/quarantine/* branches and
// deletes the ones that are safe to remove. It is deterministic and never uses
// an LLM. dryRun reports what would be deleted without deleting.
//
// Rules (in order, all require the branch to be at least 7 days old):
//  1. Branch is fully merged into the working branch -> delete (fully-merged).
//  2. Diff vs working branch touches only files whose content is identical on
//     both sides -> delete (identical-content).
//  3. Branch is older than 14 days and adds only *.spec.ts / *_test.go files
//     that already exist on the working branch -> delete (superseded-tests).
//
// Everything else is kept. A summary line is logged once per run.
func DrainQuarantineBranches(projectPath string, dryRun bool) (deleted []string, kept int, err error) {
	if _, err := gitOut(projectPath, "rev-parse", "--git-dir"); err != nil {
		return nil, 0, nil // not a git repo, no-op
	}

	working, err := gitOut(projectPath, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return nil, 0, fmt.Errorf("working branch: %w", err)
	}
	working = strings.TrimSpace(working)

	out, err := gitOut(projectPath, "for-each-ref",
		"--format=%(refname:short) %(committerdate:unix)",
		"refs/heads/"+quarantinePrefix+"*")
	if err != nil {
		return nil, 0, nil // no matching refs is fine
	}

	now := time.Now()
	lines := strings.Split(strings.TrimSpace(out), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}
		branch := parts[0]
		committerUnix, _ := strconv.ParseInt(parts[1], 10, 64)
		age := now.Sub(time.Unix(committerUnix, 0))

		if age < 7*day {
			kept++
			continue // in-flight work, never touch
		}

		reason, ok := classifyQuarantineBranch(projectPath, working, branch, age)
		if !ok {
			kept++
			continue
		}

		log.Printf("[QuarantineDrain] deleted %s (%s)", branch, reason)
		if !dryRun {
			if _, err := gitOut(projectPath, "branch", "-D", branch); err != nil {
				log.Printf("[QuarantineDrain] delete %s failed: %v", branch, err)
				kept++
				continue
			}
		}
		deleted = append(deleted, branch)
	}

	mode := ""
	if dryRun {
		mode = " dry-run"
	}
	log.Printf("[QuarantineDrain] project=%s kept=%d deleted=%d%s", projectPath, kept, len(deleted), mode)
	return deleted, kept, nil
}

func classifyQuarantineBranch(projectPath, working, branch string, age time.Duration) (string, bool) {
	if isAncestor(projectPath, branch, working) {
		return "fully-merged", true
	}

	if diffTouchesOnlyIdenticalContent(projectPath, working, branch) {
		return "identical-content", true
	}

	if age >= 14*day && diffAddsOnlySupersededTests(projectPath, working, branch) {
		return "superseded-tests", true
	}

	return "", false
}

func isAncestor(projectPath, maybeAncestor, descendant string) bool {
	_, err := gitOut(projectPath, "merge-base", "--is-ancestor", maybeAncestor, descendant)
	return err == nil
}

// diffTouchesOnlyIdenticalContent reports whether the changes introduced by
// branch (relative to the working branch's merge base) touch only files whose
// blob content is the same on both branches.
func diffTouchesOnlyIdenticalContent(projectPath, working, branch string) bool {
	files, err := diffNameStatus(projectPath, working, branch)
	if err != nil || len(files) == 0 {
		return false
	}
	for _, f := range files {
		if f.status == "D" {
			return false // deletions are never "identical content"
		}
		left, lerr := blobContent(projectPath, working, f.path)
		right, rerr := blobContent(projectPath, branch, f.path)
		if lerr != nil || rerr != nil || !equalBytes(left, right) {
			return false
		}
	}
	return true
}

// diffAddsOnlySupersededTests reports whether branch adds only *.spec.ts or
// *_test.go files that already exist on the working branch.
func diffAddsOnlySupersededTests(projectPath, working, branch string) bool {
	files, err := diffNameStatus(projectPath, working, branch)
	if err != nil || len(files) == 0 {
		return false
	}
	for _, f := range files {
		if f.status != "A" {
			return false
		}
		if !strings.HasSuffix(f.path, ".spec.ts") && !strings.HasSuffix(f.path, "_test.go") {
			return false
		}
		if _, err := blobContent(projectPath, working, f.path); err != nil {
			return false // test file does not already exist on working branch
		}
	}
	return true
}

type diffEntry struct {
	status string
	path   string
}

func diffNameStatus(projectPath, working, branch string) ([]diffEntry, error) {
	out, err := gitOut(projectPath, "diff", "--name-status", working+"..."+branch)
	if err != nil {
		return nil, err
	}
	var entries []diffEntry
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Lines are "<status>\t<path>" or rename lines with two paths.
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) != 2 {
			continue
		}
		status := strings.TrimSpace(parts[0])
		path := strings.TrimSpace(parts[1])
		if status == "" || path == "" {
			continue
		}
		// Normalize rename status (R123 -> R).
		if len(status) > 0 && status[0] == 'R' {
			status = "R"
		}
		entries = append(entries, diffEntry{status: status, path: path})
	}
	return entries, nil
}

func blobContent(projectPath, branch, path string) ([]byte, error) {
	out, err := gitOut(projectPath, "show", branch+":"+path)
	return []byte(out), err
}

func equalBytes(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// QuarantineBranchExists reports whether a hydra/quarantine/* branch exists.
func QuarantineBranchExists(projectPath, key string) bool {
	return branchExists(projectPath, quarantinePrefix+key)
}

// QuarantineBranchPath returns the ref path for a quarantine branch.
func QuarantineBranchPath(key string) string {
	return filepath.Join(quarantinePrefix, key)
}

// QuarantineBranch is one hydra/quarantine/* branch with its tip commit age.
type QuarantineBranch struct {
	Name      string    `json:"name"`
	Committed time.Time `json:"committed"`
}

// ListQuarantineBranches returns the project's hydra/quarantine/* branches,
// newest commit last. Read-only; used by the operator API (FR-033). Not a git
// repo or no matching refs: empty list, nil error.
func ListQuarantineBranches(projectPath string) ([]QuarantineBranch, error) {
	if _, err := gitOut(projectPath, "rev-parse", "--git-dir"); err != nil {
		return nil, nil // not a git repo, no-op
	}
	out, err := gitOut(projectPath, "for-each-ref",
		"--format=%(refname:short) %(committerdate:unix)",
		"refs/heads/"+quarantinePrefix+"*")
	if err != nil {
		return nil, nil // no matching refs is fine
	}
	var branches []QuarantineBranch
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		parts := strings.Fields(strings.TrimSpace(line))
		if len(parts) < 2 {
			continue
		}
		unix, _ := strconv.ParseInt(parts[1], 10, 64)
		branches = append(branches, QuarantineBranch{
			Name:      parts[0],
			Committed: time.Unix(unix, 0).UTC(),
		})
	}
	return branches, nil
}
