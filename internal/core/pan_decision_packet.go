package core

import (
	"time"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// Spec-infeasible decision packets (close-path-truth PRD FR-004, RED phase).
//
// A bead whose close-gate rejections keep repeating with the SAME
// fingerprint (gate-fp-<fp8>-N with N >= decisionPacketMinRejections), that
// has had no infra-class rejection since, and whose latest recorded worker
// verdict was done:false, is not making progress no matter how many judge
// rounds it burns: the gate is structurally unsatisfiable for this spec
// (jest coverage thresholds, missing tooling, unverifiable acceptance
// wording). Such a bead is labeled spec-infeasible and one decision_request
// event carrying the evidence (verification command, rejection fingerprint,
// last verbatim gate output truncated to 4 KB, measurements parsed from that
// output) is filed for a human. A human decision (a spec-decided:<choice>
// label, or any status change) removes the candidacy, and the
// clear-escalation repair refuses while a packet is pending.
//
// RED-phase compile-only stub: every body is inert and
// decisionPacketImplemented is false, so the contract tests in
// pan_decision_packet_test.go skip loudly (PAN_DECISION_PACKET_ENFORCE=1
// makes them fail) and the daemon lint gate stays green. GREEN implements
// decisionPacketCheck hung off the stale-filing sweep body
// (pan_stale_filing.go; the sweep tick runs once per project) and flips
// decisionPacketImplemented to true.

// decisionPacketImplemented gates the contract tests: false while only this
// stub exists; GREEN flips it to true alongside the real implementation.
const decisionPacketImplemented = false

const (
	// specInfeasibleLabel marks a bead whose spec the close gates cannot
	// verify — awaiting a human spec decision.
	specInfeasibleLabel = "spec-infeasible"
	// specDecidedLabelPrefix prefixes the human decision label
	// ("spec-decided:<choice>") that clears spec-infeasible candidacy.
	specDecidedLabelPrefix = "spec-decided:"
	// decisionRequestEventType is the typed event filed for a human decision.
	decisionRequestEventType = "decision_request"
	// decisionPacketMaxGateOutputBytes caps the verbatim gate output carried
	// in the decision_request payload.
	decisionPacketMaxGateOutputBytes = 4096
	// decisionPacketMinRejections is the minimum same-fingerprint rejection
	// count (the N in gate-fp-<fp8>-N) for a bead to be spec-infeasible.
	decisionPacketMinRejections = 3
)

// Decision-packet probe seams. Production wiring consults the durable
// verdict log (ExtractRecordedVerdict over workersLogDir) and the last
// close-gate rejection output; tests override both. A nil seam means the
// probe is unknown (no verdict recorded / no output captured).
var (
	// decisionPacketVerdict returns the latest recorded worker verdict text
	// for the bead ("" when none).
	decisionPacketVerdict func(beadID string) string
	// decisionPacketGateOutput returns the most recent verbatim close-gate
	// rejection output for the bead ("" when none).
	decisionPacketGateOutput func(beadID string) string
)

// gateFingerprintRejectionCount returns the durable rejection count for the
// bead: the highest N among its gate-fp-<fp8>-N labels. A bare
// gate-fp-<fp8> label (written by trackVerificationFingerprint before the
// counted form exists) counts as 1 round, not 0.
func gateFingerprintRejectionCount(labels []string) int {
	return 0
}

// hasInfraRequeueLabel reports whether the bead carries an infra-class
// rejection marker (infra-reject-N / infra-requeue-N). Infra-class failures
// are environmental, not spec-infeasible; a bead with such a marker never
// files a decision packet.
func hasInfraRequeueLabel(labels []string) bool {
	return false
}

// clearEscalationRefusalFor returns the refusal message the clear-escalation
// repair handler must surface for this bead, or "" when the repair may
// proceed. A pending spec decision (spec-infeasible without
// spec-decided:<x>) must not be wiped by an automated escalation reset.
func clearEscalationRefusalFor(b types.Bead) string {
	return ""
}

// decisionPacketMeasurements extracts deterministic measurements from the
// verbatim gate output for the decision_request payload (for jest: the
// "All files" coverage row and the per-path breach count). Nil when nothing
// measurable is present.
func decisionPacketMeasurements(cmd, output string) map[string]string {
	return nil
}

// decisionPacketCheck evaluates one bead for spec-infeasibility during a
// stale-filing sweep tick: it labels the bead spec-infeasible and files
// exactly one decision_request event per bead when the packet criteria
// hold. Inert RED stub — GREEN implements the body.
func (o *Overseer) decisionPacketCheck(project types.Project, b types.Bead, now time.Time) {
}
