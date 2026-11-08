package core

import (
	"bufio"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// Accepted-empty reasons: a worker that legitimately closed with no diff on
// the bead's declared targets may self-report WHY in its HYDRA_VERDICT
// summary. Only these tokens are honoured — an empty diff with no accepted
// reason still reopens the bead (rejectClose), never spawns a fix bead.
const (
	acceptedEmptyDocsOnly          = "docs-only"
	acceptedEmptyChainDeferredRoot = "chain-deferred-to-root"
	acceptedEmptySuperseded        = "superseded"
)

// emptyGateVerdict is the daemon-side mirror of the worker's HYDRA_VERDICT
// closure block (cmd/worker/verdict.go). Only the fields the diff gate needs.
type emptyGateVerdict struct {
	Done    bool   `json:"done"`
	Summary string `json:"summary"`
}

// docExtensions are file suffixes that count as documentation for the
// "docs-only" accepted-empty reason.
var docExtensions = map[string]bool{
	".md": true, ".mdx": true, ".rst": true, ".adoc": true, ".txt": true,
}

// isDocPath reports whether a repo-relative path is documentation-only.
func isDocPath(p string) bool {
	base := strings.ToLower(filepath.Base(p))
	ext := filepath.Ext(base)
	if docExtensions[ext] {
		return true
	}
	parts := strings.Split(strings.ToLower(filepath.ToSlash(p)), "/")
	return len(parts) > 1 && parts[0] == "docs"
}

// allDocsOnly reports whether every changed path is documentation.
func allDocsOnly(changed []string) bool {
	if len(changed) == 0 {
		return false
	}
	for _, f := range changed {
		if !isDocPath(f) {
			return false
		}
	}
	return true
}

// extractEmptyGateVerdict reads the newest worker log for the bead and
// parses the LAST HYDRA_VERDICT line. Fail-open: any error (no log dir, no
// log file, unparseable JSON) returns a zero verdict — the diff gate then
// behaves exactly as before this gate existed.
func extractEmptyGateVerdict(logDir, beadID string) *emptyGateVerdict {
	matches, err := filepath.Glob(filepath.Join(logDir, safeBeadID(beadID)+"-*.log"))
	if err != nil || len(matches) == 0 {
		return nil
	}
	sort.Slice(matches, func(i, j int) bool {
		fi, errI := os.Stat(matches[i])
		fj, errJ := os.Stat(matches[j])
		if errI != nil || errJ != nil {
			return matches[i] > matches[j]
		}
		return fi.ModTime().After(fj.ModTime())
	})
	f, err := os.Open(matches[0])
	if err != nil {
		return nil
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	last := ""
	for scanner.Scan() {
		line := scanner.Text()
		if idx := strings.Index(line, "HYDRA_VERDICT:"); idx >= 0 {
			last = line[idx+len("HYDRA_VERDICT:"):]
		}
	}
	if last == "" {
		return nil
	}
	var v emptyGateVerdict
	if err := json.Unmarshal([]byte(strings.TrimSpace(last)), &v); err != nil {
		return nil
	}
	return &v
}

// acceptedEmptyReason returns the accepted-empty reason the worker
// self-reported, or "" when none applies. Reasons are matched as tokens in
// the verdict summary; the verdict must be done:true or the reason is not
// honoured (a failed run claiming docs-only is a contradiction).
func acceptedEmptyReason(v *emptyGateVerdict, changed []string) string {
	if v == nil || !v.Done {
		return ""
	}
	summary := strings.ToLower(v.Summary)
	switch {
	case strings.Contains(summary, acceptedEmptyDocsOnly) && allDocsOnly(changed):
		return acceptedEmptyDocsOnly
	case strings.Contains(summary, acceptedEmptyChainDeferredRoot) && len(changed) == 0:
		return acceptedEmptyChainDeferredRoot
	case strings.Contains(summary, acceptedEmptySuperseded) && len(changed) == 0:
		return acceptedEmptySuperseded
	}
	return ""
}

// emitEmptyDiffCompleted is the mechanical backstop (thok-class false
// closure, 2026-10-01): fires when a bead whose worker claimed a done
// verdict is about to be rejected because the branch diff is empty or
// touches none of the declared target files. Fail-open: nil verdict or nil
// event sink emits nothing. The close itself is still rejected by the
// caller — this only makes the false closure observable in the event
// stream.
func (c *BeadCloser) emitEmptyDiffCompleted(bead *types.Bead, repoName, repoPath string, changed, targets []string) {
	if c.events == nil {
		return
	}
	v := extractEmptyGateVerdict(workersLogDir(), bead.ID.String())
	if v == nil || !v.Done {
		return
	}
	empty := len(changed) == 0 || (len(targets) > 0 && !hasTargetChanges(changed, targets))
	if !empty {
		return
	}
	c.events.EmitRepo(repoPath, Event{Type: "empty_diff_completed", Project: repoName,
		Bead: string(bead.ID), Reason: "empty-diff-with-done-verdict",
		Detail: "worker claimed done but the branch diff is empty or misses every declared target file — likely false closure"})
}

// tryAcceptEmptyClose consults the worker's HYDRA_VERDICT for an
// accepted-empty reason when the diff gate would otherwise reject. Returns
// true (and logs/annotates) when the close may proceed despite the empty
// target diff; false means the caller must reject as before.
func (c *BeadCloser) tryAcceptEmptyClose(bead *types.Bead, repoName, changedRepoPath string, changed []string) bool {
	v := extractEmptyGateVerdict(workersLogDir(), bead.ID.String())
	reason := acceptedEmptyReason(v, changed)
	if reason == "" {
		return false
	}
	// E1 (reliability review v2, 2026-11-09): superseded and
	// chain-deferred-to-root were accepted on the worker's word alone —
	// an agent could close its own bead by typing the token. Now the
	// summary must NAME a bead, and the daemon verifies it:
	//   superseded             → the named superseder is CLOSED (its work
	//                            landed or was adjudicated);
	//   chain-deferred-to-root → the named chain root EXISTS and is OPEN
	//                            (someone still owns the deferred work).
	// Unverifiable or absent references reject the close as before.
	if reason == acceptedEmptySuperseded || reason == acceptedEmptyChainDeferredRoot {
		if !c.verifyEmptyCloseReference(bead, repoName, v.Summary, reason) {
			log.Printf("[VerifiedClose] Bead %s empty-diff reason %q FAILED reference verification — rejecting", bead.ID, reason)
			return false
		}
	}
	log.Printf("[VerifiedClose] Bead %s empty diff accepted via self-reported reason %q", bead.ID, reason)
	c.logCloseEvent(bead, "accepted", "empty-diff/"+reason, c.validationRetryCount(bead))
	AppendActivity(changedRepoPath, string(bead.ID), "close-accepted: empty diff with accepted reason "+reason)
	if c.events != nil {
		c.events.EmitRepo(changedRepoPath, Event{Type: "close-accepted-empty-reason", Project: repoName,
			Bead: string(bead.ID), Reason: reason,
			Detail: "verified-close accepted empty diff: worker verdict self-reported accepted reason " + reason})
	}
	return true
}

// beadRefPrefix strips the trailing suffix from a bead ID to get its
// project (or project/repo) prefix: "hydra-q1nxj" → "hydra",
// "srv-telematics/fe-ab12" → "srv-telematics/fe". Suffixes are 3-6 chars
// (fix_cause.go beadIDTokenRe documents 3-5; 6 tolerated).
func beadRefPrefix(id types.BeadID) string {
	s := string(id)
	if i := strings.LastIndexAny(s, "-/"); i > 0 && i >= len(s)-7 && i < len(s)-1 {
		if suffix := s[i+1:]; isBeadSuffix(suffix) {
			return s[:i]
		}
	}
	return ""
}

func isBeadSuffix(s string) bool {
	if len(s) < 3 || len(s) > 6 {
		return false
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9') {
			return false
		}
	}
	return true
}

// beadRefPatternFor builds the reference pattern for a given project prefix
// (e.g. "srv-telematics/fe-(ab12)" — quoted prefix + 3-6 char suffix).
// Anchoring on the bead's OWN prefix avoids both English-word collisions
// and the hydra-only assumption that broke every other project (review
// round 1 blocker).
func beadRefPatternFor(prefix string) *regexp.Regexp {
	return regexp.MustCompile(regexp.QuoteMeta(prefix) + `-([a-z0-9]{3,6})\b`)
}

// verifyEmptyCloseReference checks the bead NAMED in an accepted empty-close
// summary. The reference must use the SAME project prefix as the closing
// bead (derived from its own ID — fleet projects differ, hydra- only was a
// review-round-1 blocker). Returns true when the reference resolves and
// satisfies the reason's contract; false rejects the close (fail-closed —
// an unresolvable claim is exactly the trust problem E1 exists to close).
func (c *BeadCloser) verifyEmptyCloseReference(bead *types.Bead, repoName, summary, reason string) bool {
	prefix := beadRefPrefix(bead.ID)
	if prefix == "" {
		return false // cannot derive a reference namespace — reject
	}
	m := beadRefPatternFor(prefix).FindStringSubmatch(strings.ToLower(summary))
	if m == nil {
		return false // token without a same-project bead reference
	}
	suffix := m[1]
	if suffix == strings.ToLower(strings.TrimPrefix(strings.ToLower(string(bead.ID)), strings.ToLower(prefix)+"-")) {
		return false // naming ITSELF is not a reference
	}
	if c.projectResolver == nil {
		return false
	}
	projectPath := c.projectResolver.ResolveProjectPath(bead)
	if projectPath == "" {
		return false
	}
	if c.beadsClient == nil {
		return false
	}
	beads, err := c.beadsClient.ListBeads(projectPath, repoName)
	if err != nil {
		return false
	}
	// Exact-length suffix match first (wrong-bead tiebreak: two beads may
	// share a short tail, e.g. -9zz9 and -x9zz9); HasSuffix fallback for
	// any remaining shape.
	var ref *types.Bead
	for i := range beads {
		id := strings.ToLower(string(beads[i].ID))
		if strings.HasSuffix(id, "-"+suffix) && len(suffix) == len(id)-1-strings.LastIndexAny(id, "-/") {
			ref = &beads[i]
			break
		}
	}
	if ref == nil {
		for i := range beads {
			if strings.HasSuffix(strings.ToLower(string(beads[i].ID)), "-"+suffix) {
				ref = &beads[i]
				break
			}
		}
	}
	if ref == nil {
		return false
	}
	switch reason {
	case acceptedEmptySuperseded:
		return ref.Status.IsTerminal() // superseder's work concluded
	case acceptedEmptyChainDeferredRoot:
		// The root owns the deferred work while open or in_progress (a
		// root mid-run still owns it; strict-open rejected live roots).
		return ref.Status == types.StatusOpen || ref.Status == types.StatusInProgress
	}
	return false
}
