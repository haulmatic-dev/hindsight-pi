package skillinducer

import (
	"encoding/json"
	"fmt"
	"strings"
)

// induction is the structured verdict the LLM must return for one execution
// transcript.
type induction struct {
	HasSkill    bool     `json:"has_skill"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Triggers    []string `json:"triggers"`
	Body        string   `json:"body_markdown"`
	Reason      string   `json:"reason"`
}

const systemPrompt = `You extract reusable engineering procedures from autonomous coding-agent execution transcripts.

A procedure is worth keeping as a skill ONLY if ALL hold:
- it is a repeatable technique: commands, sequencing, gotchas, verification steps
- it generalizes beyond this one repository (fleet-wide applicability)
- it is not obvious from a tool's help text

Repo-specific workarounds, one-off fixes, and narrative summaries are NOT skills.
If the transcript shows no such procedure, return {"has_skill": false}.

Respond with ONLY a JSON object — no prose, no markdown fences:
{
  "has_skill": true,
  "name": "kebab-case-skill-name",
  "description": "One or two sentences: what this skill does and when to use it.",
  "triggers": ["short phrases that should make an agent load this skill"],
  "body_markdown": "The skill body in markdown: steps, commands, gotchas. Concrete and terse.",
  "reason": ""
}
When has_skill is false, set reason to one line explaining why and leave other fields empty.`

// buildUserPrompt renders the execution metadata plus a truncated transcript.
// The tail gets 3/4 of the budget: outcomes, verification, and close-out
// reasoning — the parts that carry the reusable procedure — live at the end.
func buildUserPrompt(rec *ExecutionRecord, maxChars int) string {
	transcript := rec.Output
	if len(transcript) > maxChars {
		head := maxChars / 4
		tail := maxChars - head
		transcript = transcript[:head] + "\n\n[... transcript truncated ...]\n\n" + transcript[len(transcript)-tail:]
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Execution metadata:\n")
	fmt.Fprintf(&b, "- bead_id: %s\n", rec.BeadID)
	fmt.Fprintf(&b, "- agent_type: %s\n", rec.AgentType)
	fmt.Fprintf(&b, "- turns: %d\n", rec.Turns)
	fmt.Fprintf(&b, "- transcript_chars: %d (excerpted to fit budget)\n", len(rec.Output))
	fmt.Fprintf(&b, "\nTranscript:\n<<<\n%s\n>>>\n", transcript)
	return b.String()
}

// parseInduction extracts the JSON verdict from an LLM response, tolerating
// markdown fences and surrounding prose.
func parseInduction(resp string) (*induction, error) {
	resp = strings.TrimSpace(resp)
	if resp == "" {
		return nil, fmt.Errorf("empty LLM response")
	}
	// Strip a fenced block if present.
	if i := strings.Index(resp, "```"); i >= 0 {
		rest := resp[i+len("```"):]
		if j := strings.Index(rest, "```"); j >= 0 {
			resp = rest[:j]
		}
		resp = strings.TrimSpace(resp)
		resp = strings.TrimPrefix(resp, "json")
		resp = strings.TrimSpace(resp)
	}
	// Fall back to the outermost JSON object.
	start := strings.Index(resp, "{")
	end := strings.LastIndex(resp, "}")
	if start < 0 || end < start {
		return nil, fmt.Errorf("no JSON object in response")
	}
	var ind induction
	if err := json.Unmarshal([]byte(resp[start:end+1]), &ind); err != nil {
		return nil, fmt.Errorf("parse induction JSON: %w", err)
	}
	return &ind, nil
}
