package main

import (
	"encoding/json"
	"fmt"
	"strings"
)

// verdictNudgeCap is how many times the worker re-prompts a live pi session
// that ended its turn without a valid done/abandon verdict before giving up
// (and letting Pan decide). PRD worker-park-and-resume Part 3.
const verdictNudgeCap = 3

// Verdict is the structured closure block every worker prompt demands as the
// agent's final output (HYDRA_VERDICT). Pi is never treated as done without
// one; a hard kill is reserved for hung processes and is a system failure.
// VerificationEntry is one command+result pair the agent ran to prove its
// acceptance criteria.
type VerificationEntry struct {
	Cmd    string `json:"cmd"`
	Result string `json:"result"`
}

type Verdict struct {
	Done         bool                `json:"done"`
	ACMet        []string            `json:"ac_met"`
	ACUnmet      []string            `json:"ac_unmet"`
	FilesChanged []string            `json:"files_changed"`
	Verification []VerificationEntry `json:"verification"`
	Summary      string              `json:"summary"`
	// HollowGreen marks a done verdict whose verification output actually
	// shows suites that crashed at load/import time. Set by normalizeVerdict;
	// such a verdict is downgraded to done=false before acceptance (hollow
	// green: ajantha-srv-telematics RED bead closed done:true while its
	// suites died with "Cannot find module ../e2e-harness").
	HollowGreen bool `json:"hollow_green,omitempty"`
}

// loadCrashSignatures are output fragments that prove a test suite failed to
// even load — these are not expected-failure assertions, they mean no test
// ran. Matched case-insensitively against verdict verification results.
var loadCrashSignatures = []string{
	"cannot find module",
	"test suite failed to run",
	"syntaxerror",
}

// loadCrashEvidence returns the signatures found in the verdict's
// verification entries (empty when none). Checks result text only — cmd is
// the command name, not evidence.
func loadCrashEvidence(v *Verdict) []string {
	if v == nil {
		return nil
	}
	var found []string
	for _, e := range v.Verification {
		lowered := strings.ToLower(e.Result)
		for _, sig := range loadCrashSignatures {
			if strings.Contains(lowered, sig) {
				found = append(found, sig)
			}
		}
	}
	return found
}

// normalizeVerdict rejects hollow-green closures: a done verdict whose
// verification output contains import/load errors proves suites crashed at
// load time, so the acceptance criteria are NOT met regardless of any exit 0
// the agent quoted. The verdict is downgraded to done=false with the load
// errors recorded as unmet ACs, so the nudge loop (and any downstream
// retention) sees the real state instead of the hollow pass.
func normalizeVerdict(v *Verdict) *Verdict {
	if v == nil || !v.Done {
		return v
	}
	evidence := loadCrashEvidence(v)
	if len(evidence) == 0 {
		return v
	}
	v.Done = false
	v.HollowGreen = true
	v.ACUnmet = append(v.ACUnmet,
		"verification contains load errors (suites failed to import/run): "+strings.Join(evidence, ", "))
	if v.Summary == "" {
		v.Summary = "verdict downgraded: quoted verification output shows suite-level load crashes, not passing tests"
	}
	return v
}

const verdictMarker = "HYDRA_VERDICT:"

// parseVerdict extracts the LAST HYDRA_VERDICT block from the agent's final
// assistant text. Caller guarantees the text is assistant-authored (turn
// output), never tool results or file contents — the spoofing guard lives in
// that contract. Malformed JSON is ignored: an agent that cannot emit valid
// JSON has not closed its task.
func parseVerdict(text string) *Verdict {
	idx := strings.LastIndex(text, verdictMarker)
	if idx < 0 {
		return nil
	}
	rest := strings.TrimSpace(text[idx+len(verdictMarker):])

	// The verdict is the JSON object immediately following the marker;
	// prose may follow the object.
	depth := 0
	end := -1
	inString := false
	escaped := false
	for i := 0; i < len(rest); i++ {
		ch := rest[i]
		if escaped {
			escaped = false
			continue
		}
		if ch == '\\' && inString {
			escaped = true
			continue
		}
		if ch == '"' {
			inString = !inString
			continue
		}
		if inString {
			continue
		}
		switch ch {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				end = i + 1
			}
		}
		if end > 0 {
			break
		}
	}
	if end <= 0 {
		return nil
	}

	var v Verdict
	if err := json.Unmarshal([]byte(rest[:end]), &v); err != nil {
		return nil
	}
	return &v
}

// decideTurnEnd decides what happens when a pi turn reaches agent_end:
// nudge the live session again, or let the turn close. A done verdict with
// no unmet ACs and an abandon verdict are both valid closures — only they
// end the turn cleanly.
func decideTurnEnd(v *Verdict, nudges, cap int) (nudge bool, reason string) {
	if v != nil && v.HollowGreen {
		return true, "hollow-green verdict: verification output shows suites that failed to load (import errors), which is not a written test passing or expected-failing — fix the suites or mark the task not done"
	}
	if v != nil && (v.Done && len(v.ACUnmet) == 0 || !v.Done && v.Summary != "") {
		return false, ""
	}
	if nudges >= cap {
		return false, ""
	}
	if v == nil {
		return true, "verdict missing — emit HYDRA_VERDICT or continue working"
	}
	if v.Done && len(v.ACUnmet) > 0 {
		return true, fmt.Sprintf("verdict claims done but acceptance criteria unmet: %s — complete them or mark the task not done",
			strings.Join(v.ACUnmet, ", "))
	}
	// done=false without a summary is no closure either.
	return true, "verdict incomplete — if the task is not done, say what remains in summary"
}

// buildNudgePrompt renders the re-prompt sent into the live session.
// turnCount is the prompt number in this bead session (1 = initial prompt,
// 2+ = nudges). If turnCount lands on the configured cadence, a non-binding
// turn-budget reminder is appended to the tail.
func buildNudgePrompt(reason string, turnCount int, piConfig PiConfig) string {
	base := fmt.Sprintf(`Your turn ended but the task is not closed: %s.

Continue working. When the task is genuinely complete — required file edits
exist on disk and verification commands ran — end your final message with:

HYDRA_VERDICT: {"done": true, "ac_met": [...], "ac_unmet": [], "files_changed": [...], "verification": [{"cmd": "...", "result": "..."}]}

If you cannot complete it, end with:

HYDRA_VERDICT: {"done": false, "summary": "what was done and what remains"}`, reason)

	cadence := piConfig.reminderCadence()
	if turnCount > 0 && cadence > 0 && turnCount%cadence == 0 {
		budget := piConfig.turnBudget()
		return base + fmt.Sprintf("\n\nReminder: you have used %d of ~%d turns. This is a pacing hint, not a hard cap; keep working if you are making progress.", turnCount, budget)
	}
	return base
}
