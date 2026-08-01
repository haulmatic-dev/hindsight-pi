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
	Done          bool                `json:"done"`
	ACMet         []string            `json:"ac_met"`
	ACUnmet       []string            `json:"ac_unmet"`
	FilesChanged  []string            `json:"files_changed"`
	Verification  []VerificationEntry `json:"verification"`
	Summary       string              `json:"summary"`
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
func buildNudgePrompt(reason string) string {
	return fmt.Sprintf(`Your turn ended but the task is not closed: %s.

Continue working. When the task is genuinely complete — required file edits
exist on disk and verification commands ran — end your final message with:

HYDRA_VERDICT: {"done": true, "ac_met": [...], "ac_unmet": [], "files_changed": [...], "verification": [{"cmd": "...", "result": "..."}]}

If you cannot complete it, end with:

HYDRA_VERDICT: {"done": false, "summary": "what was done and what remains"}`, reason)
}
