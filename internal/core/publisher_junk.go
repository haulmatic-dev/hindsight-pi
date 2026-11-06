package core

// Publish-path junk guard — RED-phase inert stub (bead hydra-ai97h RED chain).
//
// The publish path (PublishRepo and the close-path publish helpers) must
// refuse to push branches whose committed diff contains junk-signature
// paths (junksig.IsJunkPath), fail closed with a bounded, visible exit:
//
//   - one typed publish_junk_blocked event naming the offending paths
//     verbatim per blocked publish attempt;
//   - a per-project consecutive-block counter: after publishJunkMaxBlocks
//     consecutive blocks the guard reports the escalated reason and the
//     publish loop stops re-attempting (bounded retry, no churn);
//   - junk-free branches (e.g. only thread-cache.service.ts changes) are
//     never blocked;
//   - once the junk is removed from the branch, the counter resets and
//     the next publish attempt proceeds normally.
//
// Every git probe failure is fail-open (a broken probe cannot block a
// healthy publish).

const (
	// publishJunkReason is the distinct skip reason for a junk-diff branch.
	publishJunkReason = "publish-junk"
	// publishJunkEscalatedReason is reported after publishJunkMaxBlocks
	// consecutive blocks — the publish loop must stop retrying.
	publishJunkEscalatedReason = "publish-junk-escalated"
	// publishJunkBlockedEvent is the typed event for one visible block.
	publishJunkBlockedEvent = "publish_junk_blocked"
	// publishJunkMaxBlocks caps consecutive guard blocks per project
	// before the escalated (stop-retrying) reason takes over.
	publishJunkMaxBlocks = 3
)

// publishJunkImplemented shifts the RED-phase contract tests. GREEN: real
// diff-probe + counter implementation below.
const publishJunkImplemented = false

// publishJunkDiffProbe is the seam for the guard's junk-path diff probe
// (production: committed diff of the working branch against origin/<branch>,
// classified by junksig.IsJunkPath). Overridden in tests.
var publishJunkDiffProbe = defaultPublishJunkDiffProbe

// defaultPublishJunkDiffProbe returns the junk paths found in the branch
// diff. RED stub: inert, always empty (fail-open).
func defaultPublishJunkDiffProbe(projectPath string) ([]string, error) {
	return nil, nil
}

// publishJunkGuard evaluates the junk guard for one publish attempt in the
// given project directory. blocked=false means the publish may proceed;
// blocked=true means the publish must be rejected with the returned reason
// (publishJunkReason or publishJunkEscalatedReason once the consecutive
// block cap is exceeded). RED stub: always allow.
func publishJunkGuard(projectPath string) (blocked bool, reason string) {
	return false, ""
}

// resetPublishJunkBlocks clears the consecutive-block counter for the
// project — called on a successful/clean publish so junk fixed upstream
// gets a fresh guard budget. RED stub: no-op.
func resetPublishJunkBlocks(projectPath string) {}
