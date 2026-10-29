package core

import (
	"sync"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// Campaign-wide park via label sweep. When a campaign is parked (Pan
// park-campaign sets the durable project_parks flag), the flag only gates the
// scheduler spawn decision — beads already sitting in the DAG, the retry
// queue, or mid-flight keep leaking work (the hydra-gcip leak: the only
// park-gate label in the store sat on an unrelated bead while campaign
// members kept spawning). This sweep makes the park durable at the bead
// level: every non-terminal member of the parked campaign's store gets the
// park-gate label attached, every gate that consults labels (DAG navigator,
// discovery filter, retry queue) then sees the park, and an in_progress
// member emits exactly one campaign_park_leak event so the leak is countable
// from the event stream. Closing the PARK-GATE (unpark sweep) removes the
// labels and restores readiness.

// campaignParkGateLabel is the PARK-GATE label attached to every non-terminal
// member of a parked campaign. Its presence must block DAG readiness; its
// absence restores it.
const campaignParkGateLabel = "park-gate"

// campaignParkLeakEventType is the typed event emitted exactly once per
// in_progress member when the parked sweep finds it mid-flight.
const campaignParkLeakEventType = "campaign_park_leak"

// campaignParkSweepImplemented is false: the sweep body is an inert RED stub
// and the contract tests in campaign_park_test.go skip loudly (set
// CAMPAIGN_PARK_ENFORCE=1 to fail them instead).
const campaignParkSweepImplemented = false

// Production wiring seams: package-level function vars so tests can override
// the store/event plumbing wholesale (same pattern as staleFilingListBeads).
// NewOverseer installs the beadsClient-backed implementations; a nil seam
// makes the sweep fail open at its caller.
var (
	// campaignParkListBeads lists the campaign's bead store (repoName "" = single).
	campaignParkListBeads func(projectPath, repoName string) ([]types.Bead, error)
	// campaignParkApplyLabels attaches labels to a member bead.
	campaignParkApplyLabels func(projectPath, repoName string, beadID types.BeadID, labels []string) error
	// campaignParkRemoveLabels removes labels from a member bead.
	campaignParkRemoveLabels func(projectPath, repoName string, beadID types.BeadID, labels []string) error
	// campaignParkEmit emits a typed event (campaign_park_leak) to the stream.
	campaignParkEmit func(eventType string, fields map[string]any)
)

// Once-per-bead leak-event ledger: an in_progress member must emit exactly
// one campaign_park_leak even when the parked sweep runs on every tick.
var (
	campaignParkLeakMu    sync.Mutex
	campaignParkLeakNoted = map[string]bool{}
)

// resetCampaignParkLeakLedger clears the leak ledger (test seam).
func resetCampaignParkLeakLedger() {
	campaignParkLeakMu.Lock()
	defer campaignParkLeakMu.Unlock()
	campaignParkLeakNoted = map[string]bool{}
}

// sweepCampaignParkLabels attaches (parked=true) or removes (parked=false)
// the park-gate label on every non-terminal member of the campaign keyed by
// key, returning the number of members whose park-gate state changed.
// Membership is bead.Project == key: a campaign-2 bead that merely depends on
// a campaign-1 member is NOT a member and is never labelled by this sweep.
// RED stub: inert, no store reads or writes.
func sweepCampaignParkLabels(projectPath, repoName, key string, parked bool) (int, error) {
	return 0, nil // RED: campaign park label sweep not yet implemented
}

// campaignParkGateActive reports whether the park-gate label is present.
func campaignParkGateActive(labels []string) bool {
	for _, l := range labels {
		if l == campaignParkGateLabel {
			return true
		}
	}
	return false
}

// campaignParkMember reports whether bead belongs to the campaign keyed by
// key. The only membership signal is bead.Project: dependency edges into the
// campaign do not confer membership (a downstream bead of another campaign
// must keep running when the upstream campaign parks).
func campaignParkMember(bead types.Bead, key string) bool {
	return bead.Project == key
}
