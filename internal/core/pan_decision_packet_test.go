package core

import (
	"os"
	"testing"
	"time"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// RED-phase contract tests for FR-004 "Spec-infeasible decision packets"
// (close-path-truth PRD, docs/PRDs/close-path-truth-prd-20261023.md).
//
// Contract pinned here (driven through the stale-filing sweep tick, which
// GREEN must hang decisionPacketCheck off):
//   - 3 same-fingerprint `work` rejections (durable gate-fp-<fp8>-3 label) +
//     a recorded done:false verdict → the bead is labeled spec-infeasible
//     and exactly ONE decision_request event files whose payload carries the
//     verification command, the rejection fingerprint, the verbatim gate
//     output (≤ 4 KB) and ≥ 1 measurement parsed from it. A second sweep
//     tick does not duplicate the packet.
//   - The pending packet excludes the bead from clear-escalation candidates
//     with the exact refusal "clear-escalation: <id> is spec-infeasible;
//     awaiting spec-decided".
//   - Infra-class rejection markers (infra-reject-N / infra-requeue-N) never
//     trigger the packet.
//   - Fewer than 3 matching fingerprints (or the bare uncounted
//     gate-fp-<fp8> label) → untouched.
//   - A human decision label spec-decided:<x> removes the spec-infeasible
//     candidacy (no packet, no refusal).

// decisionPacketJestOutput is a realistic jest coverage-gate failure the
// measurements probe must be able to read (the "All files" row and the
// per-path breach).
const decisionPacketJestOutput = `PASS src/x/a.spec.ts
---------------------------|---------|----------|---------|---------|-------------------
File                       | % Stmts | % Branch | % Funcs | % Lines | Uncovered Line #s
---------------------------|---------|----------|---------|---------|-------------------
All files                  |   62.5  |   40.0   |   55.5  |   63.1  |
 src/deep/util.ts          |   48.2  |   25.0   |   40.0  |   50.0  | 12-45
---------------------------|---------|----------|---------|---------|-------------------
Test Suites: 1 failed, 1 passed, 2 total`

// decisionPacketDoneFalseVerdict is the shape the durable verdict log
// records for a self-reported failed close.
const decisionPacketDoneFalseVerdict = `HYDRA_VERDICT: {"done": false, "summary": "coverage gate still failing"}`

// decisionPacketRequireImpl skips loudly (or fails under
// PAN_DECISION_PACKET_ENFORCE=1) while the GREEN implementation is absent,
// mirroring the STALE_FILING_ENFORCE / BEAD_CLAIMS RED gates.
func decisionPacketRequireImpl(t *testing.T) {
	t.Helper()
	if !decisionPacketImplemented && os.Getenv("PAN_DECISION_PACKET_ENFORCE") == "" {
		t.Skip("RED: spec-infeasible decision packet contract not yet implemented (set PAN_DECISION_PACKET_ENFORCE=1 to fail)")
	}
}

// decisionPacketFixture builds a stuck work bead: in_progress, carrying
// durable same-fingerprint rejection labels, declaring its verification
// command in metadata (the machine-written contract source).
func decisionPacketFixture(t *testing.T, id string, labels []string) types.Bead {
	t.Helper()
	return types.Bead{
		ID:       types.BeadID(id),
		Title:    "GREEN: implement coverage gate",
		Project:  "fakeproj-dp",
		Status:   types.StatusInProgress,
		Labels:   labels,
		Metadata: map[string]string{"test_command": "npm test"},
	}
}

func decisionPacketEnvironment(t *testing.T) (*Overseer, *mockBeadsClient) {
	t.Helper()
	o := &Overseer{
		beadsClient: newMockBeadsClient(),
		projects:    []types.Project{{Name: "fakeproj-dp", Path: "/tmp/fakeproj-dp", Type: types.RepoTypeSingle}},
	}
	o.eventSink = NewEventSink(t.TempDir())
	return o, o.beadsClient.(*mockBeadsClient)
}

// decisionPacketHooks injects the verdict + gate-output probe seams and
// returns a restore func.
func decisionPacketHooks(verdict, output string) (restore func()) {
	oldV, oldO := decisionPacketVerdict, decisionPacketGateOutput
	decisionPacketVerdict = func(string) string { return verdict }
	decisionPacketGateOutput = func(string) string { return output }
	return func() { decisionPacketVerdict, decisionPacketGateOutput = oldV, oldO }
}

func decisionPacketHasLabel(b types.Bead, label string) bool {
	for _, l := range b.Labels {
		if l == label {
			return true
		}
	}
	return false
}

// AC: 3 same-fingerprint `work` rejections + a done:false verdict →
// spec-infeasible label, ONE decision_request event whose payload contains
// the command and ≥ 1 measurement; the pending packet excludes the bead
// from clear-escalation candidates; a second sweep tick does not duplicate.
func TestDecisionPacket_ThreeWorkRejectionsFilePacket(t *testing.T) {
	decisionPacketRequireImpl(t)
	defer decisionPacketHooks(decisionPacketDoneFalseVerdict, decisionPacketJestOutput)()
	now := time.Now()
	o, mock := decisionPacketEnvironment(t)
	mock.addBead(decisionPacketFixture(t, "proj-dp1", []string{"gate-fp-1a2b3c4d-3"}))

	o.sweepStalePanFilings(now)

	labeled := mock.beads["proj-dp1"]
	if !decisionPacketHasLabel(labeled, specInfeasibleLabel) {
		t.Fatalf("bead must carry %q after 3 same-fingerprint work rejections + done:false verdict; labels: %v",
			specInfeasibleLabel, labeled.Labels)
	}
	evs := readEventsOfType(t, o.eventSink, decisionRequestEventType)
	if len(evs) != 1 {
		t.Fatalf("expected exactly one decision_request event, got %d: %+v", len(evs), evs)
	}
	ev := evs[0]
	if ev.Bead != "proj-dp1" {
		t.Errorf("event must name the bead, got %q", ev.Bead)
	}
	if cmd, _ := ev.Fields["command"].(string); cmd != "npm test" {
		t.Errorf("payload must carry the verification command, got %v", ev.Fields["command"])
	}
	if fp, _ := ev.Fields["fingerprint"].(string); fp != "1a2b3c4d" {
		t.Errorf("payload must carry the rejection fingerprint, got %v", ev.Fields["fingerprint"])
	}
	if out, _ := ev.Fields["gate_output"].(string); len(out) == 0 || len(out) > decisionPacketMaxGateOutputBytes {
		t.Errorf("payload must carry verbatim gate output capped at %d bytes, got %d bytes",
			decisionPacketMaxGateOutputBytes, len(out))
	}
	ms, ok := ev.Fields["measurements"].(map[string]string)
	if !ok || len(ms) < 1 {
		t.Errorf("payload must carry ≥1 measurement parsed from the gate output, got %T %v",
			ev.Fields["measurements"], ev.Fields["measurements"])
	}
	// Exactly-once: a second sweep tick must not duplicate the packet.
	o.sweepStalePanFilings(now.Add(time.Minute))
	if evs := readEventsOfType(t, o.eventSink, decisionRequestEventType); len(evs) != 1 {
		t.Fatalf("second sweep tick must not duplicate decision_request, got %d", len(evs))
	}
	// Pending packet excludes the bead from clear-escalation candidates.
	if refusal := clearEscalationRefusalFor(mock.beads["proj-dp1"]); refusal != "clear-escalation: proj-dp1 is spec-infeasible; awaiting spec-decided" {
		t.Fatalf("pending packet must refuse clear-escalation with the exact message, got %q", refusal)
	}
}

// AC: infra-class rejections never trigger the packet — an infra-reject /
// infra-requeue marker disqualifies the bead even with ≥ 3 same-fingerprint
// work rejections and a done:false verdict.
func TestDecisionPacket_InfraRejectionsNeverTrigger(t *testing.T) {
	decisionPacketRequireImpl(t)
	defer decisionPacketHooks(decisionPacketDoneFalseVerdict, decisionPacketJestOutput)()
	now := time.Now()
	o, mock := decisionPacketEnvironment(t)
	mock.addBead(decisionPacketFixture(t, "proj-dp2", []string{"gate-fp-1a2b3c4d-3", "infra-reject-2"}))
	mock.addBead(decisionPacketFixture(t, "proj-dp3", []string{"gate-fp-9f8e7d6c-4", "infra-requeue-1"}))

	o.sweepStalePanFilings(now)

	for _, id := range []string{"proj-dp2", "proj-dp3"} {
		if decisionPacketHasLabel(mock.beads[id], specInfeasibleLabel) {
			t.Errorf("%s must NOT be labeled %q while an infra-class rejection marker is present",
				id, specInfeasibleLabel)
		}
	}
	if evs := readEventsOfType(t, o.eventSink, decisionRequestEventType); len(evs) != 0 {
		t.Errorf("infra-class beads must file no decision_request, got %+v", evs)
	}
}

// AC: fewer than 3 matching fingerprints → untouched. The bare uncounted
// gate-fp-<fp8> label (the pre-count form) counts as 1 round and never
// qualifies on its own.
func TestDecisionPacket_FewerThanThreeRejectionsUntouched(t *testing.T) {
	decisionPacketRequireImpl(t)
	defer decisionPacketHooks(decisionPacketDoneFalseVerdict, decisionPacketJestOutput)()
	now := time.Now()
	o, mock := decisionPacketEnvironment(t)
	mock.addBead(decisionPacketFixture(t, "proj-dp4", []string{"gate-fp-1a2b3c4d-2"}))
	mock.addBead(decisionPacketFixture(t, "proj-dp5", []string{"gate-fp-1a2b3c4d"}))

	o.sweepStalePanFilings(now)

	for _, id := range []string{"proj-dp4", "proj-dp5"} {
		if decisionPacketHasLabel(mock.beads[id], specInfeasibleLabel) {
			t.Errorf("%s must stay untouched below %d rejections", id, decisionPacketMinRejections)
		}
	}
	if evs := readEventsOfType(t, o.eventSink, decisionRequestEventType); len(evs) != 0 {
		t.Errorf("below-threshold beads must file no decision_request, got %+v", evs)
	}
}

// AC: labeling spec-decided:<x> removes spec-infeasible candidacy — no
// packet, no label churn, and clear-escalation may proceed.
func TestDecisionPacket_SpecDecidedLabelClearsCandidacy(t *testing.T) {
	decisionPacketRequireImpl(t)
	defer decisionPacketHooks(decisionPacketDoneFalseVerdict, decisionPacketJestOutput)()
	now := time.Now()
	o, mock := decisionPacketEnvironment(t)
	mock.addBead(decisionPacketFixture(t, "proj-dp6", []string{"gate-fp-1a2b3c4d-3", "spec-decided:add-coverage-tests"}))

	o.sweepStalePanFilings(now)

	if decisionPacketHasLabel(mock.beads["proj-dp6"], specInfeasibleLabel) {
		t.Errorf("spec-decided:<x> must remove %q candidacy", specInfeasibleLabel)
	}
	if evs := readEventsOfType(t, o.eventSink, decisionRequestEventType); len(evs) != 0 {
		t.Errorf("decided bead must file no decision_request, got %+v", evs)
	}
	if refusal := clearEscalationRefusalFor(mock.beads["proj-dp6"]); refusal != "" {
		t.Errorf("decided bead must not block clear-escalation, got %q", refusal)
	}
}

// Ungated guardrail: the counted-label contract builds on the existing
// fingerprint label prefix; GREEN changing it silently breaks packet parsing
// and every already-labeled bead.
func TestDecisionPacket_LabelPrefixContract(t *testing.T) {
	if gateFingerprintLabelPrefix != "gate-fp-" {
		t.Fatalf("gate-fp label prefix changed: %q — decision packet parsing and all existing labels depend on it",
			gateFingerprintLabelPrefix)
	}
}
