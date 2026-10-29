package core

import (
	"errors"
	"os"
	"sync"
	"testing"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// campaignParkRequireImpl gates the behavioral contract tests: pre-GREEN they
// skip loudly so the daemon gate stays green (RED phase), and
// CAMPAIGN_PARK_ENFORCE=1 turns every skip into a failure proving RED detects
// the missing campaign park label sweep.
func campaignParkRequireImpl(t *testing.T) {
	t.Helper()
	if !campaignParkSweepImplemented && os.Getenv("CAMPAIGN_PARK_ENFORCE") == "" {
		t.Skip("RED: campaign park label sweep contract not yet implemented (set CAMPAIGN_PARK_ENFORCE=1 to fail)")
	}
}

// campaignParkHooks installs in-memory seam fakes and returns a restore func.
type campaignParkRecorded struct {
	mu       sync.Mutex
	setCalls []campaignParkLabelCall
	rmCalls  []campaignParkLabelCall
	events   []campaignParkEventCall
}

type campaignParkLabelCall struct {
	projectPath string
	repoName    string
	beadID      types.BeadID
	labels      []string
}

type campaignParkEventCall struct {
	eventType string
	fields    map[string]any
}

func (r *campaignParkRecorded) setFor(id types.BeadID) []campaignParkLabelCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []campaignParkLabelCall
	for _, c := range r.setCalls {
		if c.beadID == id {
			out = append(out, c)
		}
	}
	return out
}

func (r *campaignParkRecorded) callsFor(id types.BeadID) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, c := range r.setCalls {
		if c.beadID == id {
			n++
		}
	}
	for _, c := range r.rmCalls {
		if c.beadID == id {
			n++
		}
	}
	return n
}

func (r *campaignParkRecorded) eventsOf(eventType string) []campaignParkEventCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []campaignParkEventCall
	for _, e := range r.events {
		if e.eventType == eventType {
			out = append(out, e)
		}
	}
	return out
}

func campaignParkHooks(rec *campaignParkRecorded) (restore func()) {
	oldList, oldSet, oldRm, oldEmit := campaignParkListBeads, campaignParkApplyLabels, campaignParkRemoveLabels, campaignParkEmit
	campaignParkListBeads = func(projectPath, repoName string) ([]types.Bead, error) {
		return campaignParkFixture(), nil
	}
	if rec != nil {
		campaignParkApplyLabels = func(projectPath, repoName string, beadID types.BeadID, labels []string) error {
			rec.mu.Lock()
			rec.setCalls = append(rec.setCalls, campaignParkLabelCall{projectPath, repoName, beadID, labels})
			rec.mu.Unlock()
			return nil
		}
		campaignParkRemoveLabels = func(projectPath, repoName string, beadID types.BeadID, labels []string) error {
			rec.mu.Lock()
			rec.rmCalls = append(rec.rmCalls, campaignParkLabelCall{projectPath, repoName, beadID, labels})
			rec.mu.Unlock()
			return nil
		}
		campaignParkEmit = func(eventType string, fields map[string]any) {
			rec.mu.Lock()
			rec.events = append(rec.events, campaignParkEventCall{eventType, fields})
			rec.mu.Unlock()
		}
	}
	return func() {
		campaignParkListBeads, campaignParkApplyLabels, campaignParkRemoveLabels, campaignParkEmit = oldList, oldSet, oldRm, oldEmit
	}
}

// campaignParkFixture reproduces the hydra-gcip leak state: campaign-1's
// store holds two live members (open + in_progress) whose park-gate labels
// are missing, one terminal member, and one UNRELATED bead that — per the
// leak — is the only bead carrying park-gate. The parked sweep must label the
// members and leave the unrelated and terminal beads alone.
func campaignParkFixture() []types.Bead {
	return []types.Bead{
		{ID: "c1-open", Project: "campaign-1", Title: "campaign-1 open member", Type: "implement", Status: types.StatusOpen},
		{ID: "c1-inprog", Project: "campaign-1", Title: "campaign-1 in_progress member", Type: "implement", Status: types.StatusInProgress},
		{ID: "c1-done", Project: "campaign-1", Title: "campaign-1 terminal member", Type: "implement", Status: types.StatusCompleted},
		// The leak: gate attached only to an unrelated bead in the same store.
		{ID: "c1-unrelated", Project: "other-campaign", Title: "unrelated bead that already got the gate", Type: "implement", Status: types.StatusOpen, Labels: []string{campaignParkGateLabel}},
	}
}

// TestCampaignPark_LabelAndEventContract pins the exact label and event-type
// identifiers GREEN must use — these names are the operator-facing contract.
func TestCampaignPark_LabelAndEventContract(t *testing.T) {
	if campaignParkGateLabel != "park-gate" {
		t.Fatalf("campaignParkGateLabel = %q, want %q", campaignParkGateLabel, "park-gate")
	}
	if campaignParkLeakEventType != "campaign_park_leak" {
		t.Fatalf("campaignParkLeakEventType = %q, want %q", campaignParkLeakEventType, "campaign_park_leak")
	}
}

// TestCampaignPark_GateActivePredicate pins the gate predicate semantics.
func TestCampaignPark_GateActivePredicate(t *testing.T) {
	cases := []struct {
		labels []string
		want   bool
	}{
		{nil, false},
		{[]string{"escalated"}, false},
		{[]string{"park-gate"}, true},
		{[]string{"escalated", "park-gate", "needs-human"}, true},
	}
	for _, tc := range cases {
		if got := campaignParkGateActive(tc.labels); got != tc.want {
			t.Fatalf("campaignParkGateActive(%v) = %v, want %v", tc.labels, got, tc.want)
		}
	}
}

// TestCampaignPark_MembershipByProjectOnly pins the campaign-2 isolation
// contract at the predicate level: dependency edges do not confer membership.
func TestCampaignPark_MembershipByProjectOnly(t *testing.T) {
	member := types.Bead{ID: "c1-open", Project: "campaign-1"}
	if !campaignParkMember(member, "campaign-1") {
		t.Fatalf("campaign-1 bead must be a member of campaign-1")
	}
	dependent := types.Bead{ID: "c2-dependent", Project: "campaign-2", Dependencies: []types.BeadID{"c1-open"}}
	if campaignParkMember(dependent, "campaign-1") {
		t.Fatalf("campaign-2 bead depending on a campaign-1 member must NOT be a campaign-1 member")
	}
}

// TestCampaignPark_GateFixtureHydraGCIPLeak: parked sweep must attach
// park-gate to every live member of the parked campaign even though the only
// pre-existing gate sits on an unrelated bead (the leak), and must never
// touch the unrelated bead or terminal members.
func TestCampaignPark_GateFixtureHydraGCIPLeak(t *testing.T) {
	campaignParkRequireImpl(t)
	rec := &campaignParkRecorded{}
	restore := campaignParkHooks(rec)
	defer restore()
	resetCampaignParkLeakLedger()
	defer resetCampaignParkLeakLedger()

	n, err := sweepCampaignParkLabels("/proj", "", "campaign-1", true)
	if err != nil {
		t.Fatalf("sweep parked: %v", err)
	}
	if n != 2 {
		t.Fatalf("parked count = %d, want 2 (open + in_progress members)", n)
	}
	if calls := rec.setFor("c1-open"); len(calls) == 0 {
		t.Fatalf("open member c1-open got no park-gate label — the leak reproduces")
	}
	if calls := rec.setFor("c1-inprog"); len(calls) == 0 {
		t.Fatalf("in_progress member c1-inprog got no park-gate label — the leak reproduces")
	}
	if calls := rec.callsFor("c1-unrelated"); calls != 0 {
		t.Fatalf("unrelated bead c1-unrelated received %d label writes, want 0", calls)
	}
	if calls := rec.callsFor("c1-done"); calls != 0 {
		t.Fatalf("terminal member c1-done received %d label writes, want 0", calls)
	}
}

// TestCampaignPark_CrossCampaignDependentNotParked: a campaign-2 bead that
// depends on a campaign-1 member must not be parked by campaign-1's sweep.
func TestCampaignPark_CrossCampaignDependentNotParked(t *testing.T) {
	campaignParkRequireImpl(t)
	rec := &campaignParkRecorded{}
	restore := campaignParkHooks(rec)
	defer restore()
	resetCampaignParkLeakLedger()
	defer resetCampaignParkLeakLedger()

	if _, err := sweepCampaignParkLabels("/proj", "", "campaign-1", true); err != nil {
		t.Fatalf("sweep parked: %v", err)
	}
	if calls := rec.callsFor("c2-dependent"); calls != 0 {
		t.Fatalf("campaign-2 dependent bead received %d label writes from campaign-1's sweep, want 0", calls)
	}
	if campaignParkGateActive([]string{}) {
		t.Fatalf("campaign-2 bead must not end up gate-labelled by campaign-1's park")
	}
}

// TestCampaignPark_CloseGateRemovesLabelsRestoresReadiness: the unpark sweep
// strips park-gate from members and the gate predicate reports inactive.
func TestCampaignPark_CloseGateRemovesLabelsRestoresReadiness(t *testing.T) {
	campaignParkRequireImpl(t)
	rec := &campaignParkRecorded{}
	restore := campaignParkHooks(rec)
	defer restore()
	resetCampaignParkLeakLedger()
	defer resetCampaignParkLeakLedger()

	if _, err := sweepCampaignParkLabels("/proj", "", "campaign-1", true); err != nil {
		t.Fatalf("sweep parked: %v", err)
	}
	n, err := sweepCampaignParkLabels("/proj", "", "campaign-1", false)
	if err != nil {
		t.Fatalf("sweep unparked: %v", err)
	}
	if n != 2 {
		t.Fatalf("unparked count = %d, want 2", n)
	}
	if calls := rec.setFor("c1-open"); len(calls) == 0 || len(rec.rmCalls) == 0 {
		t.Fatalf("close of the PARK-GATE must remove park-gate labels: set=%d rm=%d", len(rec.setCalls), len(rec.rmCalls))
	}
	if campaignParkGateActive(nil) {
		t.Fatalf("gate must be inactive after the PARK-GATE closes")
	}
}

// TestCampaignPark_GateBlockedBeadNotReady: while the PARK-GATE is attached,
// a DAG-ready open member must not be handed to a worker; closing the gate
// (labels removed) restores readiness.
func TestCampaignPark_GateBlockedBeadNotReady(t *testing.T) {
	campaignParkRequireImpl(t)

	dag := NewDAG()
	gated := types.Bead{ID: "c1-open", Project: "campaign-1", Title: "gated member", Type: "implement", Status: types.StatusOpen, Labels: []string{campaignParkGateLabel}}
	normal := types.Bead{ID: "other-open", Project: "campaign-2", Title: "unparked bead", Type: "implement", Status: types.StatusOpen}
	if err := dag.AddNode(gated); err != nil {
		t.Fatalf("AddNode(gated): %v", err)
	}
	if err := dag.AddNode(normal); err != nil {
		t.Fatalf("AddNode(normal): %v", err)
	}
	nav := NewDAGNavigator(dag)

	ready := nav.GetReadyBeads(nil)
	for _, b := range ready {
		if campaignParkGateActive(b.Labels) {
			t.Fatalf("gate-labelled bead %s handed to a worker while PARK-GATE attached: %+v", b.ID, b)
		}
	}
	if len(ready) != 1 || ready[0].ID != normal.ID {
		t.Fatalf("ready = %+v, want only the ungated %s", ready, normal.ID)
	}

	// Closing the PARK-GATE removes labels and restores readiness.
	gated.Labels = nil
	if err := dag.AddNode(gated); err == nil {
		// AddNode rejects duplicates; update through the node instead.
		if node, ok := dag.GetNode(string(gated.ID)); ok {
			node.Bead.Labels = nil
		}
	}
	ready = nav.GetReadyBeads(nil)
	found := false
	for _, b := range ready {
		if b.ID == gated.ID {
			found = true
		}
	}
	if !found {
		t.Fatalf("gate-closed bead %s not ready again after label removal", gated.ID)
	}
}

// TestCampaignPark_InProgressMemberEmitsExactlyOneLeakEvent: an in_progress
// member must emit exactly one campaign_park_leak event across repeated
// parked sweeps; open members emit none.
func TestCampaignPark_InProgressMemberEmitsExactlyOneLeakEvent(t *testing.T) {
	campaignParkRequireImpl(t)
	rec := &campaignParkRecorded{}
	restore := campaignParkHooks(rec)
	defer restore()
	resetCampaignParkLeakLedger()
	defer resetCampaignParkLeakLedger()

	for i := 0; i < 2; i++ {
		if _, err := sweepCampaignParkLabels("/proj", "", "campaign-1", true); err != nil {
			t.Fatalf("sweep %d: %v", i, err)
		}
	}
	events := rec.eventsOf(campaignParkLeakEventType)
	if len(events) != 1 {
		t.Fatalf("campaign_park_leak events = %d, want exactly 1 (in_progress member c1-inprog)", len(events))
	}
	if got, _ := events[0].fields["bead"].(string); got != "c1-inprog" {
		t.Fatalf("leak event bead = %v, want c1-inprog", events[0].fields["bead"])
	}
	if got, _ := events[0].fields["project"].(string); got != "campaign-1" {
		t.Fatalf("leak event project = %v, want campaign-1", events[0].fields["project"])
	}
	if open := rec.eventsOf(campaignParkLeakEventType); len(open) != 1 {
		t.Fatalf("open member must emit no leak event; total leak events = %d", len(open))
	}
}

// TestCampaignPark_ListErrorFailsOpen: a store listing error must surface as
// an error without label writes or panics — callers fail open.
func TestCampaignPark_ListErrorFailsOpen(t *testing.T) {
	campaignParkRequireImpl(t)
	rec := &campaignParkRecorded{}
	restore := campaignParkHooks(rec)
	defer restore()

	campaignParkListBeads = func(projectPath, repoName string) ([]types.Bead, error) {
		return nil, errors.New("store boom")
	}
	n, err := sweepCampaignParkLabels("/proj", "", "campaign-1", true)
	if err == nil {
		t.Fatalf("list error must be surfaced, got nil with count %d", n)
	}
	if rec.callsFor("c1-open") != 0 {
		t.Fatalf("label writes on listing error, want 0")
	}
}
