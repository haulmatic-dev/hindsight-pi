package validation

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/haulmatic-dev/hydra/pkg/types"

	"github.com/haulmatic-dev/hydra/internal/beadfiles"
)

// beadEvidenceImplemented reports whether BeadEvidence and
// ValidateTDDChainEvidence carry real behavior. The contract tests consult it
// (via the BEAD_EVIDENCE_ENFORCE gate in the test helpers) to decide whether
// to skip loudly while the RED phase pins the contract. GREEN flips this to
// true and implements both entry points per
// docs/PRDs/validator-truth-prd-20261022.md FR-003.
const beadEvidenceImplemented = true

// Evidence is the per-bead commit/dirty evidence collected from the repo.
// Files holds repo-relative paths touched by every commit whose message
// contains the whole bead id, plus — when includeDirty was set — the dirty
// set of the repo (excluding .beads/). BlobIDs maps committed paths to their
// git blob ids. Fingerprint is sha256 over the sorted `path:blob-id` pairs.
type Evidence struct {
	Files       []string
	BlobIDs     map[string]string
	Fingerprint string
}

// ChainEvidenceInput carries the RED/GREEN pair, their located flags, their
// Evidence, and the repo root used to read file contents for assertion and
// stub checks.
type ChainEvidenceInput struct {
	RedBead       types.Bead
	GreenBead     types.Bead
	RedLocated    bool
	GreenLocated  bool
	RedEvidence   Evidence
	GreenEvidence Evidence
	RepoRoot      string
}

// TDDResult is the result of evidence-based chain validation. It aliases the
// existing TDDValidationResult so the close-path consumers stay compatible.
type TDDResult = TDDValidationResult

// wholeWordBeadIDRe compiles the whole-word bead-id matcher: the id must be
// followed by a non-alphanumeric byte or end of input, so `hydra-ab12` never
// matches inside `hydra-ab123`. The identical shape is pinned by
// TestBeadEvidence_WholeWordRegexShape.
func wholeWordBeadIDRe(beadID string) *regexp.Regexp {
	return regexp.MustCompile(regexp.QuoteMeta(beadID) + `([^[:alnum:]]|$)`)
}

// gitOutput runs one read-only git command inside repoRoot and returns
// stdout. Errors are always returned to the caller, which decides fail-open.
func gitOutput(repoRoot string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = repoRoot
	out, err := cmd.Output()
	return string(out), err
}

// isGitWorkTree reports whether repoRoot is inside a git work tree.
func isGitWorkTree(repoRoot string) bool {
	out, err := gitOutput(repoRoot, "rev-parse", "--is-inside-work-tree")
	return err == nil && strings.TrimSpace(out) == "true"
}

// BeadEvidence collects the evidence for one bead id inside repoRoot.
//
// A commit is evidence for the bead when its full message matches the bead id
// whole-word (subject `(hydra-ab12)` form or body `Bead: hydra-ab12` form).
// The union of files touched by those commits is evidence.Files; blob ids are
// resolved for committed paths from a single `ls-tree` of HEAD. With
// includeDirty the working-tree dirty set (modified + untracked, `-uall`,
// excluding .beads/) is appended. Everything fails open: a non-repo dir, a
// repo with no commits, or any git fault yields empty evidence and a nil
// error — evidence is advisory input to the chain checks, never a hard gate.
//
// Cost is O(1) subprocess spawns per call regardless of how many commits
// match: one rev-parse probe, one `log --all` scan, one batched
// `diff-tree --stdin` over every matching commit, one `ls-tree -r` blob map,
// and one `status` when includeDirty is set.
func BeadEvidence(repoRoot, beadID string, includeDirty bool) (Evidence, error) {
	ev := Evidence{BlobIDs: map[string]string{}}
	if strings.TrimSpace(repoRoot) == "" || strings.TrimSpace(beadID) == "" {
		return ev, nil
	}
	if !isGitWorkTree(repoRoot) {
		return ev, nil
	}
	re := wholeWordBeadIDRe(beadID)

	// One log sweep over every ref: <hash>\0<full message>\x1e per commit.
	committed := map[string]bool{}
	out, err := gitOutput(repoRoot, "log", "--all", "--format=%H%x00%B%x1e")
	if err == nil {
		var hashes []string
		for _, rec := range strings.Split(out, "\x1e") {
			rec = strings.TrimLeft(rec, "\n")
			hash, body, ok := strings.Cut(rec, "\x00")
			if !ok {
				continue
			}
			if re.MatchString(body) {
				hashes = append(hashes, hash)
			}
		}
		if len(hashes) > 0 {
			if files, derr := gitFilesForCommits(repoRoot, hashes); derr == nil {
				for f := range files {
					committed[f] = true
				}
			}
		}
	}

	files := make([]string, 0, len(committed))
	for f := range committed {
		files = append(files, f)
	}

	// Blob ids for committed paths from a single tree read; dirty paths have
	// no blob id (they are not committed), so they contribute only paths.
	if len(committed) > 0 {
		blobs := gitBlobMap(repoRoot)
		for f := range committed {
			if id, ok := blobs[f]; ok {
				ev.BlobIDs[f] = id
			}
		}
	}

	if includeDirty {
		for _, p := range gitDirtyPaths(repoRoot) {
			if !committed[p] {
				files = append(files, p)
			}
		}
	}

	sort.Strings(files)
	ev.Files = files
	ev.Fingerprint = evidenceFingerprint(ev)
	return ev, nil
}

// gitFilesForCommits resolves the union of files touched by the given
// commits in ONE batched `diff-tree --stdin` call (--root so initial
// commits contribute too). Input is newline-separated commit ids; output is
// NUL-terminated repo-relative paths.
func gitFilesForCommits(repoRoot string, hashes []string) (map[string]bool, error) {
	cmd := exec.Command("git", "diff-tree", "--stdin", "--no-commit-id", "--name-only", "-r", "--root", "-z")
	cmd.Dir = repoRoot
	cmd.Stdin = strings.NewReader(strings.Join(hashes, "\n") + "\n")
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	files := map[string]bool{}
	for _, p := range strings.Split(string(out), "\x00") {
		if p != "" {
			files[p] = true
		}
	}
	return files, nil
}

// gitBlobMap returns path → blob id for the whole HEAD tree in one
// `ls-tree -r -z` call. Missing HEAD (no commits yet) yields an empty map.
func gitBlobMap(repoRoot string) map[string]string {
	m := map[string]string{}
	out, err := gitOutput(repoRoot, "ls-tree", "-r", "-z", "HEAD")
	if err != nil {
		return m
	}
	for _, entry := range strings.Split(out, "\x00") {
		tab := strings.IndexByte(entry, '\t')
		if tab < 0 {
			continue
		}
		meta := strings.Fields(entry[:tab])
		if len(meta) == 3 {
			m[entry[tab+1:]] = meta[2]
		}
	}
	return m
}

// gitDirtyPaths lists the working-tree dirty set (modified, staged and
// untracked, `-uall` so files inside new untracked directories appear
// individually), excluding .beads/. Porcelain -z renames carry the old path
// as a second NUL-separated record, which is skipped.
func gitDirtyPaths(repoRoot string) []string {
	out, err := gitOutput(repoRoot, "status", "--porcelain=v1", "-z", "-uall")
	if err != nil {
		return nil
	}
	entries := strings.Split(out, "\x00")
	paths := make([]string, 0, len(entries))
	for i := 0; i < len(entries); i++ {
		e := entries[i]
		if len(e) < 4 {
			continue
		}
		p := e[3:]
		if e[0] == 'R' || e[0] == 'C' {
			i++ // skip the paired old-path record
		}
		if p == ".beads" || strings.HasPrefix(p, ".beads/") {
			continue
		}
		paths = append(paths, p)
	}
	return paths
}

// evidenceFingerprint derives the deterministic sha256 fingerprint over the
// sorted `path:blob-id` pairs (dirty paths have an empty blob id). Committed
// content changes move the blob id and therefore the fingerprint.
func evidenceFingerprint(ev Evidence) string {
	entries := make([]string, 0, len(ev.Files))
	for _, f := range ev.Files {
		entries = append(entries, f+":"+ev.BlobIDs[f])
	}
	sort.Strings(entries)
	sum := sha256.Sum256([]byte(strings.Join(entries, "\n")))
	return hex.EncodeToString(sum[:])
}

// ValidateTDDChainEvidence validates a RED/GREEN pair against their per-bead
// evidence (PRD validator-truth FR-003):
//
//   - RED passes when its evidence contains ≥1 beadfiles.IsTestFile file with
//     a meaningful assertion (case-sensitive patterns).
//   - GREEN passes when its evidence contains ≥1 non-test source file (not
//     IsTestFile, not *.md, not under docs/, not CHANGELOG*) that is not a
//     stub.
//   - Declared files (beadfiles.Resolve — the single shared parser) only
//     scope the stub check and name sibling tests; a declared non-test file
//     is never checked as a test.
//   - Declared files present and no evidence → one gap naming the bead id
//     and saying `no test file in bead evidence` (RED) or `no
//     implementation file in bead evidence` (GREEN).
//   - No declared files and no evidence → no gap. Unlocated side → no gap.
func ValidateTDDChainEvidence(in ChainEvidenceInput) TDDResult {
	result := TDDResult{Passed: true, RedPassed: true, GreenPassed: true, Gaps: []TDDGap{}}
	v := NewTDDValidator(DefaultValidationConfig())
	if in.RedLocated {
		gaps := evidenceRedGaps(v, in)
		result.Gaps = append(result.Gaps, gaps...)
		if len(gaps) > 0 {
			result.RedPassed = false
			result.Passed = false
		}
	}
	if in.GreenLocated {
		gaps := evidenceGreenGaps(in)
		result.Gaps = append(result.Gaps, gaps...)
		if len(gaps) > 0 {
			result.GreenPassed = false
			result.Passed = false
		}
	}
	return result
}

// evidenceRedGaps checks the RED side. The no-gap escape holds when the
// evidence is empty AND no files are declared (vacuous, fail-open); evidence
// that exists but carries no passing test file is a real gap even without
// declared files.
func evidenceRedGaps(v *TDDValidator, in ChainEvidenceInput) []TDDGap {
	for _, f := range in.RedEvidence.Files {
		if !beadfiles.IsTestFile(f) {
			continue
		}
		content, err := os.ReadFile(filepath.Join(in.RepoRoot, f))
		if err != nil {
			continue
		}
		if v.hasMeaningfulAssertions(string(content)) {
			return nil
		}
	}
	if len(in.RedEvidence.Files) == 0 && len(beadfiles.Resolve(in.RedBead)) == 0 {
		return nil
	}
	return []TDDGap{{
		Type:        "red",
		Description: fmt.Sprintf("RED bead %s: no test file in bead evidence", in.RedBead.ID),
		Severity:    SeverityError,
	}}
}

// isDocsEvidence reports whether a path is documentation-shaped and can
// never count as implementation evidence: any *.md, anything under docs/,
// and CHANGELOG* files.
func isDocsEvidence(f string) bool {
	lower := strings.ToLower(f)
	if strings.HasSuffix(lower, ".md") || strings.HasPrefix(lower, "docs/") {
		return true
	}
	base := strings.ToLower(filepath.Base(f))
	return strings.HasPrefix(base, "changelog")
}

// evidenceGreenGaps checks the GREEN side. A candidate is a non-test,
// non-docs evidence file that is a real implementation: for Go files the
// shared DetectStubs scanner must find nothing, for other files a non-empty
// file is enough (there is no stub parser for arbitrary languages).
func evidenceGreenGaps(in ChainEvidenceInput) []TDDGap {
	for _, f := range in.GreenEvidence.Files {
		if beadfiles.IsTestFile(f) || isDocsEvidence(f) {
			continue
		}
		full := filepath.Join(in.RepoRoot, f)
		if strings.HasSuffix(f, ".go") {
			if len(DetectStubs(full)) > 0 {
				continue
			}
		} else if info, err := os.Stat(full); err != nil || info.Size() == 0 {
			continue
		}
		return nil
	}
	if len(in.GreenEvidence.Files) == 0 && len(beadfiles.Resolve(in.GreenBead)) == 0 {
		return nil
	}
	return []TDDGap{{
		Type:        "green",
		Description: fmt.Sprintf("GREEN bead %s: no implementation file in bead evidence", in.GreenBead.ID),
		Severity:    SeverityError,
	}}
}
