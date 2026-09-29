package triage

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// PiRunner invokes the LLM judge. Same invocation shape as pan.RunPi:
// headless pi, prompt on stdin, --no-extensions (self-contained JSON-in/
// JSON-out prompt; extension load measured 48k vs 12k tokens).
type PiRunner func(ctx context.Context, cfg JudgeConfig, prompt string) (string, error)

// JudgeConfig is the judge's pi identity (cheap model: GLM class).
type JudgeConfig struct {
	Provider   string
	Model      string
	Thinking   string
	APIKeyName string
	PiPath     string
}

// DefaultPiRunner is the production judge runner (mirrors pan.RunPi).
func DefaultPiRunner(ctx context.Context, cfg JudgeConfig, prompt string) (string, error) {
	return runPi(ctx, cfg, prompt)
}

// JudgePrompt renders the group for the LLM and demands strict JSON.
func JudgePrompt(g Group) string {
	var b strings.Builder
	b.WriteString(`You are a PR triage advisor for a software team. You are given a group of open pull requests that all modify at least one shared file, so their merges conflict.

For the group decide:
1. merge_order: PR numbers in the recommended merge order (merge the most complete/foundational first).
2. duplicates: PRs that implement the same underlying change as another PR in the group and should be considered for close-as-duplicate, each with the PR it duplicates and a one-line reason.
3. needs_human: PRs where the decision genuinely needs human judgment, each with a one-line reason.
4. reasoning: one short paragraph of rationale.

Respond with STRICT JSON only, no prose outside the JSON:
{"merge_order":[<numbers>],"duplicates":[{"pr":<n>,"of":<n>,"reason":"..."}],"needs_human":[{"pr":<n>,"reason":"..."}],"reasoning":"..."}

Never recommend auto-merging or auto-closing in reasoning: output is posted as advice for a human Tier 2 merger.

`)
	b.WriteString(fmt.Sprintf("Shared files: %s\n\n", strings.Join(g.Files, ", ")))
	for _, p := range g.PRs {
		b.WriteString(fmt.Sprintf("PR #%d: %s\n  branch: %s\n  files: %s\n\n",
			p.Number, p.Title, p.Branch, strings.Join(p.Files, ", ")))
	}
	return b.String()
}

// ParseRecommendation extracts the JSON recommendation from model output,
// tolerating markdown code fences.
func ParseRecommendation(out string) (Recommendation, error) {
	s := strings.TrimSpace(out)
	if i := strings.Index(s, "{"); i >= 0 {
		if j := strings.LastIndex(s, "}"); j > i {
			s = s[i : j+1]
		}
	}
	var r Recommendation
	if err := json.Unmarshal([]byte(s), &r); err != nil {
		return Recommendation{}, fmt.Errorf("parse judge output: %w", err)
	}
	return r, nil
}
