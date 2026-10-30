package core

import (
	"log"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// Spec-infeasible decision packets (close-path-truth PRD FR-004).
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
// decisionPacketCheck is hung off the stale-filing sweep body
// (pan_stale_filing.go; the sweep tick runs once per project) so the packet
// is evaluated once per tick regardless of the pan spawn-probe gate.

// decisionPacketImplemented gates the contract tests; true since GREEN.
const decisionPacketImplemented = true

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

// infraRejectionPrefixes are the infra-class rejection markers (written by
// the infra gate and the requeue budget): a bead carrying either is failing
// for environmental reasons, never for spec reasons.
var infraRejectionPrefixes = []string{"infra-reject-", "infra-requeue-"}

// Decision-packet probe seams. Production wiring consults the durable
// verdict log (ExtractRecordedVerdict over workersLogDir) and the last
// close-gate rejection output; tests override both. A nil seam means the
// probe is unknown (no verdict recorded / no output captured).
var (
	decisionPacketRequestedMu sync.Mutex
	// decisionPacketRequested is the in-process exactly-once ledger: a bead
	// files at most one decision_request event per daemon lifetime even if
	// the durable label write fails behind it.
	decisionPacketRequested = map[string]struct{}{}
	// decisionPacketVerdict returns the latest recorded worker verdict text
	// for the bead ("" when none). Wired from ExtractRecordedVerdict over
	// the workers log dir.
	decisionPacketVerdict func(beadID string) string
	// decisionPacketGateOutput returns the most recent verbatim close-gate
	// rejection output for the bead ("" when none).
	decisionPacketGateOutput func(beadID string) string
)

// gateFingerprintCountedRe parses the durable counted rejection label form
// gate-fp-<fp8>-<N> written by trackVerificationFingerprint.
var gateFingerprintCountedRe = regexp.MustCompile(`^gate-fp-([0-9a-f]{8})-(\d+)$`)

// gateFingerprintRejectionCount returns the durable rejection count for the
// bead: the highest N among its gate-fp-<fp8>-N labels. A bare
// gate-fp-<fp8> label (written before the counted form existed) counts as
// 1 round, not 0.
func gateFingerprintRejectionCount(labels []string) int {
	max := 0
	for _, l := range labels {
		if m := gateFingerprintCountedRe.FindStringSubmatch(l); m != nil {
			if n, err := strconv.Atoi(m[2]); err == nil && n > max {
				max = n
			}
		}
	}
	return max
}

// dominantGateFingerprint returns the fp8 of the most-rejected fingerprint
// on the bead (the counted label with the highest N), or "" when none.
func dominantGateFingerprint(labels []string) string {
	best, bestN := "", 0
	for _, l := range labels {
		if m := gateFingerprintCountedRe.FindStringSubmatch(l); m != nil {
			if n, err := strconv.Atoi(m[2]); err == nil && n > bestN {
				bestN, best = n, m[1]
			}
		}
	}
	return best
}

// hasInfraRequeueLabel reports whether the bead carries an infra-class
// rejection marker (infra-reject-N / infra-requeue-N). Infra-class failures
// are environmental, not spec-infeasible; a bead with such a marker never
// files a decision packet.
func hasInfraRequeueLabel(labels []string) bool {
	for _, l := range labels {
		for _, p := range infraRejectionPrefixes {
			if strings.HasPrefix(l, p) {
				return true
			}
		}
	}
	return false
}

// hasSpecDecidedLabel reports whether a human decision label
// (spec-decided:<choice>) is present.
func hasSpecDecidedLabel(labels []string) bool {
	for _, l := range labels {
		if strings.HasPrefix(l, specDecidedLabelPrefix) {
			return true
		}
	}
	return false
}

// verdictSaysDoneFalse reports whether the recorded verdict text contains a
// `"done": false` field (whitespace-tolerant).
var verdictDoneFalseRe = regexp.MustCompile(`"done"\s*:\s*false`)

func verdictSaysDoneFalse(verdict string) bool {
	return verdictDoneFalseRe.MatchString(verdict)
}

// clearEscalationRefusalFor returns the refusal message the clear-escalation
// repair handler must surface for this bead, or "" when the repair may
// proceed. A pending spec decision (spec-infeasible without
// spec-decided:<x>) must not be wiped by an automated escalation reset.
func clearEscalationRefusalFor(b types.Bead) string {
	if !hasBeadLabel(b.Labels, specInfeasibleLabel) || hasSpecDecidedLabel(b.Labels) {
		return ""
	}
	return "clear-escalation: " + string(b.ID) + " is spec-infeasible; awaiting spec-decided"
}

// decisionPacketMeasurements extracts deterministic measurements from the
// verbatim gate output for the decision_request payload (for jest: the
// "All files" coverage row and the count of per-path rows, i.e. the paths
// breaching the gate). Nil when nothing measurable is present.
func decisionPacketMeasurements(cmd, output string) map[string]string {
	_ = cmd
	if output == "" {
		return nil
	}
	ms := map[string]string{}
	breaching := 0
	for _, line := range strings.Split(output, "\n") {
		if !strings.Contains(line, "|") {
			continue
		}
		cells := strings.Split(line, "|")
		if len(cells) < 2 {
			continue
		}
		name := strings.TrimSpace(cells[0])
		if name == "" || strings.HasPrefix(name, "-") || name == "File" || name == "Uncovered Line #s" {
			continue
		}
		vals := make([]string, 0, len(cells)-1)
		for _, c := range cells[1:] {
			if v := strings.TrimSpace(c); v != "" && v != "Uncovered Line #s" {
				vals = append(vals, v)
			}
		}
		if len(vals) == 0 {
			continue
		}
		if name == "All files" {
			ms["all_files"] = strings.Join(vals, " ")
		} else {
			breaching++
		}
	}
	if breaching > 0 {
		ms["breaching_paths"] = strconv.Itoa(breaching)
	}
	if len(ms) == 0 {
		return nil
	}
	return ms
}

// decisionPacketCheck evaluates one bead for spec-infeasibility during a
// stale-filing sweep tick: when the packet criteria hold it labels the bead
// spec-infeasible and files exactly one decision_request event. Fail-open:
// every probe and the label write degrade to "no packet" without breaking
// the sweep.
func (o *Overseer) decisionPacketCheck(project types.Project, b types.Bead, now time.Time) {
	if o.beadsClient == nil || o.eventSink == nil {
		return
	}
	_ = now
	// Any status change is a human/automation decision of another kind: it
	// ends the candidacy without a packet.
	if b.Status.IsTerminal() {
		return
	}
	// Infra-class rejections mean the environment, not the spec, is failing.
	if hasInfraRequeueLabel(b.Labels) {
		return
	}
	// A human decision already recorded.
	if hasSpecDecidedLabel(b.Labels) {
		return
	}
	// Durability threshold: the same gate rejection signature N times.
	if gateFingerprintRejectionCount(b.Labels) < decisionPacketMinRejections {
		return
	}
	// Already filed (durable exactly-once marker).
	if hasBeadLabel(b.Labels, specInfeasibleLabel) {
		return
	}
	// The worker itself reported it cannot satisfy the spec.
	verdict := ""
	if decisionPacketVerdict != nil {
		verdict = decisionPacketVerdict(string(b.ID))
	}
	if !verdictSaysDoneFalse(verdict) {
		return
	}
	fp := dominantGateFingerprint(b.Labels)
	if fp == "" {
		return
	}
	output := ""
	if decisionPacketGateOutput != nil {
		output = decisionPacketGateOutput(string(b.ID))
	}
	if len(output) > decisionPacketMaxGateOutputBytes {
		output = output[:decisionPacketMaxGateOutputBytes]
	}
	cmd, _ := verificationCommandFor(&b)
	fields := map[string]any{
		"command":     cmd,
		"fingerprint": fp,
		"gate_output": output,
	}
	if ms := decisionPacketMeasurements(cmd, output); ms != nil {
		fields["measurements"] = ms
	}

	decisionPacketRequestedMu.Lock()
	_, filed := decisionPacketRequested[string(b.ID)]
	if !filed {
		decisionPacketRequested[string(b.ID)] = struct{}{}
	}
	decisionPacketRequestedMu.Unlock()
	if filed {
		return
	}

	o.eventSink.EmitRepo(project.Path, Event{
		Type:    decisionRequestEventType,
		Project: project.Name,
		Bead:    string(b.ID),
		Fields:  fields,
	})
	if err := o.beadsClient.UpdateBeadLabels(project.Path, "", b.ID, []string{specInfeasibleLabel}); err != nil {
		log.Printf("[DecisionPacket] label %s on %s failed (continuing; event already filed): %v",
			specInfeasibleLabel, b.ID, err)
	}
}
