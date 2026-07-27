package core

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// gapReport mirrors the on-disk schema emitted by hydra-compliance-analyzer.
// It is duplicated here (rather than imported) because the analyzer lives in
// a separate `main` package under tools/.
type gapReport struct {
	Passed  bool   `json:"passed"`
	Gaps    []gap  `json:"gaps"`
	Summary string `json:"summary,omitempty"`
}

type gap struct {
	ID            string   `json:"id"`
	Category      string   `json:"category"`
	Severity      string   `json:"severity"`
	Description   string   `json:"description"`
	PRDRef        string   `json:"prd_ref,omitempty"`
	TargetFiles   []string `json:"target_files,omitempty"`
	SuggestedBead string   `json:"suggested_bead,omitempty"`
}

// adjudication is the persisted verdict for a gap fingerprint.
type adjudication struct {
	Verdict    string  `json:"verdict"` // real | false_positive | ambiguous
	Confidence float64 `json:"confidence"`
	Reasoning  string  `json:"reasoning,omitempty"`
	GapID      string  `json:"gap_id,omitempty"`
	Cycle      int     `json:"cycle"`
}

// gapFingerprint identifies a gap across cycles independent of its per-run
// ID, so a verdict can suppress the same gap when it is re-detected.
func gapFingerprint(g gap) string {
	files := append([]string{}, g.TargetFiles...)
	sort.Strings(files)
	key := strings.ToLower(strings.TrimSpace(g.Category)) + "|" +
		strings.ToLower(strings.TrimSpace(g.Description)) + "|" +
		strings.ToLower(strings.TrimSpace(g.PRDRef)) + "|" +
		strings.Join(files, ",")
	sum := sha1.Sum([]byte(key))
	return fmt.Sprintf("%x", sum[:8])
}

func (cm *ComplianceCycleManager) adjudicationsPath() string {
	return filepath.Join(cm.config.ProjectDir, ".hydra", "gap-adjudications.json")
}

func (cm *ComplianceCycleManager) loadAdjudications() map[string]adjudication {
	m := map[string]adjudication{}
	data, err := os.ReadFile(cm.adjudicationsPath())
	if err != nil {
		return m
	}
	_ = json.Unmarshal(data, &m)
	return m
}

func (cm *ComplianceCycleManager) saveAdjudications(m map[string]adjudication) {
	path := cm.adjudicationsPath()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		log.Printf("[Adjudicator] mkdir %s: %v", filepath.Dir(path), err)
		return
	}
	data, _ := json.MarshalIndent(m, "", "  ")
	if err := os.WriteFile(path, data, 0644); err != nil {
		log.Printf("[Adjudicator] write %s: %v", path, err)
	}
}

// adjudicateGaps reads the gap report, adjudicates each gap (skipping ones
// already ruled false_positive), and writes a filtered report containing
// only REAL gaps. It returns that report's path and the surviving count.
// halted is true when the surviving set is identical to the previous
// cycle's (no progress → stop instead of re-decomposing the same gaps).
func (cm *ComplianceCycleManager) adjudicateGaps(gapReportPath string) (filteredPath string, survivors int, halted bool, err error) {
	data, err := os.ReadFile(gapReportPath)
	if err != nil {
		return "", 0, false, err
	}
	var report gapReport
	if err := json.Unmarshal(data, &report); err != nil {
		return "", 0, false, err
	}
	if len(report.Gaps) == 0 {
		return gapReportPath, 0, false, nil
	}

	adj := cm.loadAdjudications()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()

	var real []gap
	survSet := map[string]bool{}
	for _, g := range report.Gaps {
		fp := gapFingerprint(g)
		if prev, ok := adj[fp]; ok && prev.Verdict == "false_positive" {
			log.Printf("[Adjudicator] gap %s (%s) already ruled false_positive — suppressing", g.ID, fp)
			continue
		}

		v := cm.adjudicateGap(ctx, g)
		adj[fp] = adjudication{Verdict: v.Verdict, Confidence: v.Confidence, Reasoning: v.Reasoning, GapID: g.ID, Cycle: cm.cycles}

		switch v.Verdict {
		case "false_positive":
			log.Printf("[Adjudicator] gap %s → FALSE_POSITIVE (conf %.2f): %s", g.ID, v.Confidence, v.Reasoning)
		case "ambiguous":
			log.Printf("[Adjudicator] gap %s → AMBIGUOUS: %s", g.ID, v.Reasoning)
			cm.notifyAmbiguous(g, v)
		default: // "real" or anything unexpected → treat as real (conservative)
			real = append(real, g)
			survSet[fp] = true
		}
	}
	cm.saveAdjudications(adj)

	// Progress-delta: an identical surviving set to last cycle means the
	// previous round changed nothing — halt rather than loop.
	if len(survSet) > 0 && sameStringSet(survSet, cm.prevSurviving) {
		return "", len(real), true, nil
	}
	cm.prevSurviving = survSet

	if len(real) == 0 {
		return "", 0, false, nil
	}

	report.Gaps = real
	report.Passed = false
	filteredPath = filepath.Join(cm.config.ProjectDir, "gaps", fmt.Sprintf("gap-report-cycle-%d-adjudicated.json", cm.cycles))
	out, _ := json.MarshalIndent(report, "", "  ")
	if err := os.WriteFile(filteredPath, out, 0644); err != nil {
		return "", 0, false, err
	}
	return filteredPath, len(real), false, nil
}

// adjudicateGap asks a high-IQ model to rule a single gap real vs
// false-positive via a two-sided prompt. It invokes pi directly in text mode
// (prompt on stdin, verdict on stdout) — the same proven path workers use —
// rather than the RPC connector, which returned not-Success in the daemon
// context and forced every gap to "real". On any failure it still returns
// "real" so a genuine gap is never silently dropped when the adjudicator breaks.
// normalizeThinking canonicalises a configured reasoning-effort value. A
// disabled level is NEVER emitted: the Kimi endpoint silently reroutes K3 and
// K2.7 requests to the older K2.6 model when thinking is turned off (verified
// against the official model docs, 2026-07-27), so asking for "none" would
// quietly downgrade the model instead of saving anything. "low" is the
// weakest level Hydra will send.
func normalizeThinking(level string) string {
	l := strings.ToLower(strings.TrimSpace(level))
	if l == "none" || l == "off" || l == "disabled" {
		return "low"
	}
	return l
}

// adjudicatorArgs builds the pi argv for an adjudication call. Extracted so the
// model/thinking overrides are testable without spawning pi.
func adjudicatorArgs(provider, model, thinking string) []string {
	args := []string{"--no-session"}
	if provider != "" {
		args = append(args, "--provider", provider)
	}
	if model != "" {
		args = append(args, "--model", model)
	}
	if t := normalizeThinking(thinking); t != "" {
		args = append(args, "--thinking-level", t)
	}
	return args
}

func (cm *ComplianceCycleManager) adjudicateGap(ctx context.Context, g gap) adjudication {
	core := cm.overseer.config.Agents.Pi
	model := core.Model
	if cm.config.AdjudicatorModel != "" {
		model = cm.config.AdjudicatorModel
	}

	piPath := os.Getenv("HYDRA_PI_PATH")
	if piPath == "" {
		piPath = "pi"
	}
	args := adjudicatorArgs(core.Provider, model, cm.config.AdjudicatorThinking)

	c := exec.CommandContext(ctx, piPath, args...)
	c.Dir = cm.config.ProjectDir
	c.Env = os.Environ() // inherit the daemon env (provider API keys)
	c.Stdin = strings.NewReader(buildAdjudicationPrompt(g))
	var stdout, stderr bytes.Buffer
	c.Stdout = &stdout
	c.Stderr = &stderr

	if err := c.Run(); err != nil {
		tail := strings.TrimSpace(stderr.String())
		if len(tail) > 200 {
			tail = tail[len(tail)-200:]
		}
		log.Printf("[Adjudicator] pi failed for gap %s: %v — treating as real (stderr: %s)", g.ID, err, tail)
		return adjudication{Verdict: "real", GapID: g.ID}
	}

	v := parseAdjudication(stdout.String())
	v.GapID = g.ID
	return v
}

// buildAdjudicationPrompt builds the two-sided (argue-real then argue-false)
// judgment prompt. The model must end with a single JSON verdict line.
func buildAdjudicationPrompt(g gap) string {
	var b strings.Builder
	b.WriteString("You are adjudicating whether a compliance GAP flagged by an automated detector is REAL (needs a code change) or a FALSE POSITIVE, for a source-to-Go migration.\n\n")
	b.WriteString("GAP:\n")
	b.WriteString("- id: " + g.ID + "\n")
	b.WriteString("- category: " + g.Category + "\n")
	b.WriteString("- severity: " + g.Severity + "\n")
	b.WriteString("- description: " + g.Description + "\n")
	if g.PRDRef != "" {
		b.WriteString("- prd_ref: " + g.PRDRef + "\n")
	}
	if len(g.TargetFiles) > 0 {
		b.WriteString("- target_files: " + strings.Join(g.TargetFiles, ", ") + "\n")
	}
	b.WriteString("\nDo BOTH, then decide:\n")
	b.WriteString("1. Argue the strongest case that the gap is REAL and requires a change. Cite file:line on BOTH the source and Go sides — compare the SAME layer/symbol.\n")
	b.WriteString("2. Argue the strongest case that it is a FALSE POSITIVE (already satisfied at HEAD, or a cross-layer detector misfire).\n")
	b.WriteString("3. Decide. Prefer REAL when uncertain. Choose false_positive ONLY with concrete same-layer evidence that the Go code already satisfies it; choose ambiguous if a human must decide.\n\n")
	b.WriteString("End your reply with ONE JSON object on the final line and nothing after it:\n")
	b.WriteString(`{"verdict":"real|false_positive|ambiguous","confidence":0.0,"reasoning":"one sentence"}` + "\n")
	return b.String()
}

// parseAdjudication extracts the final JSON verdict object from model
// output. On any parse failure it returns "real" (conservative).
func parseAdjudication(out string) adjudication {
	start := strings.LastIndex(out, "{")
	end := strings.LastIndex(out, "}")
	if start >= 0 && end > start {
		var v adjudication
		if err := json.Unmarshal([]byte(out[start:end+1]), &v); err == nil && v.Verdict != "" {
			v.Verdict = strings.ToLower(strings.TrimSpace(v.Verdict))
			return v
		}
	}
	log.Printf("[Adjudicator] could not parse verdict from output — treating as real")
	return adjudication{Verdict: "real"}
}

// notifyAmbiguous surfaces a gap that needs a human decision. Uses the same
// log-based notification path as notifyApproved/notifyMaxCyclesReached.
func (cm *ComplianceCycleManager) notifyAmbiguous(g gap, v adjudication) {
	log.Printf("[ComplianceCycle] HUMAN REVIEW NEEDED: gap %s is AMBIGUOUS (%s). Not decomposing; awaiting human. // TODO: Slack/email", g.ID, v.Reasoning)
}

func sameStringSet(a, b map[string]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if !b[k] {
			return false
		}
	}
	return true
}
