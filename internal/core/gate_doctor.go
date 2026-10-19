package core

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// GateDoctor is the pi-based triage of verification RED (feature
// "Gate-doctor: pi-based triage of verification RED (subsumes hydra-4nwg)").
//
// On a verification-command RED (gate-dir routing fix already landed), a
// cheap LLM pass reads the RED output + bead context and classifies the
// failure family, then the doctor takes ONE bounded action:
//
//	environment → allowed repair (npm ci / reinstall), rerun gate once
//	code-broken → file fix-bead carrying the exact RED output
//	stale-gate  → comment evidence + escalate; NEVER edits the gate
//	flaky       → one rerun then escalate
//	uncertain   → escalate, fail-closed
//
// BOUNDS (adversarial): the action allowlist only. GateDoctor may NOT edit
// tests/gates, force-close, or reset its own attempt counters. It is terminal
// per bead per attempt (no gate-doctor loops). Wrong-classification worst
// case lands in the existing escalation path.
type GateDoctor struct {
	// PiRunner invokes the cheap LLM classifier (same shape as triage.PiRunner).
	PiRunner func(ctx context.Context, cfg GateDoctorConfig, prompt string) (string, error)
	// beadsClient is used to file the fix-bead (code-broken branch only).
	BeadsClient BeadsClient
	// events is the structured decision stream; nil = no events.
	Events *EventSink
	// Config is the doctor's cheap-model identity.
	Config GateDoctorConfig
	// worktreePath is the path where the RED was observed (used for reruns).
	WorktreePath string
}

// GateDoctorConfig is the classifier's pi identity (cheap model: GLM class).
type GateDoctorConfig struct {
	Provider   string
	Model      string
	Thinking   string
	APIKeyName string
	PiPath     string
}

// GateFailureClass is the failure family classification of a verification RED.
type GateFailureClass string

// The five failure families. Fail-closed: any parse failure or invalid output
// classifies as uncertain.
const (
	GateClassEnvironment GateFailureClass = "environment"
	GateClassCodeBroken  GateFailureClass = "code-broken"
	GateClassStaleGate   GateFailureClass = "stale-gate"
	GateClassFlaky       GateFailureClass = "flaky"
	GateClassUncertain   GateFailureClass = "uncertain"
)

// GateDoctorAction is the bounded action the doctor takes after classification.
type GateDoctorAction struct {
	Class GateFailureClass
	// Rerun is true when the doctor is allowed to rerun the gate once
	// (environment / flaky). The caller must still enforce the one-rerun cap.
	Rerun bool
	// FileFixBead is true when a fix-bead must be filed with the exact RED
	// output (code-broken branch only).
	FileFixBead bool
	// Comment is the evidence comment text for stale-gate / escalate paths.
	Comment string
	// Escalate is true when the doctor must label + escalate to a human.
	Escalate bool
}

// DefaultGateDoctorConfig returns the production cheap-model identity for the
// classifier (mirrors the gap adjudicator's GLM-class cheap tier).
func DefaultGateDoctorConfig() GateDoctorConfig {
	return GateDoctorConfig{
		Provider:   "openai",
		Model:      "glm-4.5-air",
		Thinking:   "off",
		APIKeyName: "ANTHROPIC_API_KEY",
		PiPath:     "/opt/dev-tools/bin/pi",
	}
}

// GateDoctorPrompt renders the classification prompt for the cheap LLM.
func GateDoctorPrompt(beadTitle, gateName string, exitCode int, summary string) string {
	var b strings.Builder
	b.WriteString(`You are a verification gate doctor. A CI/test gate exited non-zero (RED).
Classify the failure family and return STRICT JSON only:

{"class":"environment"|"code-broken"|"stale-gate"|"flaky"|"uncertain","reasoning":"..."}

- environment: missing dep/node_modules/token/permission — repairable, safe to rerun once.
- code-broken: real code/test failure — file a fix-bead with the RED output.
- stale-gate: gate itself is outdated (wrong path/obsolete assertion) — NEVER edit the gate; comment + escalate.
- flaky: transient timing/ordering failure — rerun once, then escalate.
- uncertain: cannot confidently classify — escalate (fail-closed).

`)
	fmt.Fprintf(&b, "Bead: %s\nGate: %s\nExit code: %d\n\nRED output:\n%s\n",
		beadTitle, gateName, exitCode, summary)
	return b.String()
}

// ParseGateDoctorClass extracts the classification from LLM output,
// tolerating markdown code fences. Unknown/missing values fail closed as
// GateClassUncertain.
func ParseGateDoctorClass(out string) (GateFailureClass, string) {
	s := strings.TrimSpace(out)
	if i := strings.Index(s, "{"); i >= 0 {
		if j := strings.LastIndex(s, "}"); j > i {
			s = s[i : j+1]
		}
	}
	// Tolerant extraction: look for the class field even without strict JSON.
	class := GateClassUncertain
	reasoning := ""
	for _, k := range []struct {
		token string
		class GateFailureClass
	}{
		{"\"environment\"", GateClassEnvironment},
		{"\"code-broken\"", GateClassCodeBroken},
		{"\"stale-gate\"", GateClassStaleGate},
		{"\"flaky\"", GateClassFlaky},
		{"\"uncertain\"", GateClassUncertain},
	} {
		if strings.Contains(s, k.token) {
			class = k.class
			break
		}
	}
	if i := strings.Index(s, "\"reasoning\""); i >= 0 {
		rest := s[i:]
		if j := strings.Index(rest, ":"); j >= 0 {
			rest = rest[j+1:]
			end := strings.LastIndex(rest, "\"")
			if end > 0 {
				trimmed := strings.TrimSpace(rest[:end])
				reasoning = strings.Trim(trimmed, "\" \n")
			}
		}
	}
	return class, reasoning
}

// Triage classifies the RED and returns the bounded action. It never mutates
// bead state itself; the caller (BeadCloser) enforces the action.
func (d *GateDoctor) Triage(ctx context.Context, beadID, beadTitle, gateName string, exitCode int, summary string) GateDoctorAction {
	if d == nil || d.PiRunner == nil {
		return GateDoctorAction{Class: GateClassUncertain, Escalate: true, Comment: "gate-doctor missing (fail-closed)"}
	}
	prompt := GateDoctorPrompt(beadTitle, gateName, exitCode, summary)
	out, err := d.PiRunner(ctx, d.Config, prompt)
	if err != nil {
		log.Printf("[GateDoctor] %s: pi fault (fail-closed uncertain): %v", beadID, err)
		return GateDoctorAction{Class: GateClassUncertain, Escalate: true, Comment: fmt.Sprintf("gate-doctor pi fault: %v", err)}
	}
	class, reasoning := ParseGateDoctorClass(out)
	if d.Events != nil {
		d.Events.EmitRepo("", Event{Type: "gate_doctor_classified", Project: "", Bead: beadID, Reason: string(class),
			Fields: map[string]any{"gate": gateName, "exit_code": exitCode, "reasoning": reasoning}})
	}
	switch class {
	case GateClassEnvironment:
		return GateDoctorAction{Class: class, Rerun: true, Comment: reasoning}
	case GateClassCodeBroken:
		return GateDoctorAction{Class: class, FileFixBead: true, Comment: reasoning}
	case GateClassStaleGate:
		return GateDoctorAction{Class: class, Escalate: true, Comment: reasoning}
	case GateClassFlaky:
		return GateDoctorAction{Class: class, Rerun: true, Comment: reasoning}
	default:
		return GateDoctorAction{Class: GateClassUncertain, Escalate: true, Comment: reasoning}
	}
}

// FileFixBead files a fix bead carrying the exact RED output (code-broken branch).
// This is the only bead-writing path the doctor is allowed to take.
func (d *GateDoctor) FileFixBead(projectPath, repoName, beadID, gateName string, exitCode int, summary string) {
	if d == nil || d.BeadsClient == nil {
		return
	}
	desc := fmt.Sprintf("Fix-bead filed by gate-doctor (code-broken classification).\nOriginal bead: %s\nGate: %s\nExit: %d\n\nExact RED output:\n%s",
		beadID, gateName, exitCode, summary)
	if len(desc) > 6000 {
		desc = desc[:6000]
	}
	title := fmt.Sprintf("gate-doctor fix: %s gate RED (code-broken)", gateName)
	req := types.CreateBeadRequest{
		Title:       title,
		Description: desc,
		Type:        "implement",
		Priority:    1,
	}
	if _, err := d.BeadsClient.CreateBead(projectPath, req); err != nil {
		log.Printf("[GateDoctor] %s: failed to file fix-bead: %v", beadID, err)
	} else {
		log.Printf("[GateDoctor] %s: filed fix-bead for code-broken %s", beadID, gateName)
	}
}
