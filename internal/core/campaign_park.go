package core

import (
	"log"
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

// campaignParkSweepImplemented is true: the GREEN implementation below makes
// the contract tests in campaign_park_test.go authoritative.
const campaignParkSweepImplemented = true

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
// key, returning the number of members whose park-gate label state the sweep
// wrote (attach on parked, strip on unpark). Membership is bead.Project ==
// key: a campaign-2 bead that merely depends on a campaign-1 member is NOT a
// member and is never labelled by this sweep. Terminal members (completed,
// closed, failed) are skipped in both directions. The parked sweep is
// idempotent: members already carrying the gate are not rewritten; an
// in_progress member emits exactly one campaign_park_leak per bead lifetime
// (once-per-bead ledger). A nil list seam or listing error is returned to
// the caller, which fails open (never gates the pipeline on sweep health).
func sweepCampaignParkLabels(projectPath, repoName, key string, parked bool) (int, error) {
	if campaignParkListBeads == nil {
		// No store plumbing installed: fail open, nothing to sweep.
		return 0, nil
	}
	beads, err := campaignParkListBeads(projectPath, repoName)
	if err != nil {
		return 0, err
	}
	changed := 0
	for _, bead := range beads {
		if !campaignParkMember(bead, key) || bead.Status.IsTerminal() {
			continue
		}
		if parked {
			if campaignParkGateActive(bead.Labels) {
				continue // already gated: idempotent re-sweep
			}
			if campaignParkApplyLabels != nil {
				if err := campaignParkApplyLabels(projectPath, repoName, bead.ID, []string{campaignParkGateLabel}); err != nil {
					return changed, err
				}
			}
			changed++
			if bead.Status == types.StatusInProgress {
				campaignParkNoteLeak(bead)
			}
			continue
		}
		// Unpark: strip the gate from every non-terminal member so readiness
		// is restored even when the listing no longer shows the label (the
		// removal itself is idempotent at the store level).
		if campaignParkRemoveLabels != nil {
			if err := campaignParkRemoveLabels(projectPath, repoName, bead.ID, []string{campaignParkGateLabel}); err != nil {
				return changed, err
			}
		}
		changed++
	}
	return changed, nil
}

// campaignParkNoteLeak emits campaign_park_leak for an in_progress member at
// most once per bead (ledger), so repeated sweep ticks stay countable.
func campaignParkNoteLeak(bead types.Bead) {
	campaignParkLeakMu.Lock()
	defer campaignParkLeakMu.Unlock()
	id := string(bead.ID)
	if campaignParkLeakNoted[id] {
		return
	}
	campaignParkLeakNoted[id] = true
	if campaignParkEmit != nil {
		campaignParkEmit(campaignParkLeakEventType, map[string]any{
			"bead":    id,
			"project": bead.Project,
		})
	}
}

// wireCampaignParkSweep installs the beadsClient-backed implementations on
// the package-level seams (same pattern as wireStaleFilingProbes). Safe to
// call repeatedly (single daemon per process); tests override afterwards.
func wireCampaignParkSweep(o *Overseer) {
	if o.beadsClient == nil {
		return
	}
	client := o.beadsClient
	campaignParkListBeads = client.ListBeads
	campaignParkApplyLabels = client.UpdateBeadLabels
	campaignParkRemoveLabels = client.RemoveBeadLabels
	campaignParkEmit = func(eventType string, fields map[string]any) {
		if o.eventSink == nil {
			return
		}
		o.eventSink.Emit(Event{Type: eventType, Fields: fields})
	}
}

// campaignParkSweepBead runs the parked label sweep for bead's campaign,
// failing open: a sweep error is logged and never blocks the pipeline. Called
// at the campaignParked consult points so the park becomes durable at the
// bead level (hydra-gcip: the flag alone kept DAG members spawning).
func (o *Overseer) campaignParkSweepBead(bead *types.Bead) {
	projectPath := o.beadAnalyzer.FindProjectPath(bead)
	if projectPath == "" {
		return
	}
	n, err := sweepCampaignParkLabels(projectPath, "", bead.Project, true)
	if err != nil {
		log.Printf("⚠️  Campaign park sweep failed for %s (fail open): %v", bead.Project, err)
		return
	}
	if n > 0 {
		log.Printf("⏸️  Campaign park sweep labelled %d member(s) of %s", n, bead.Project)
	}
}

// campaignParkGateActive reports whether the park-gate label is present
// (exact match; labels are machine-written slugs).
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
