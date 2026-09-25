package core

import (
	"bufio"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
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