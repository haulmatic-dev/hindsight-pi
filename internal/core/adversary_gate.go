package core

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// Adversarial close gate (bounded veto). After the deterministic close gates
// pass, a pi-backed reviewer scores the diff against the bead's acceptance
// claims. Ship condition: zero BLOCKER findings AND score >= min_score.
// NIT findings never block. Rejections ride the standard verified-close
// rounds (Validation.MaxRounds via rejectClose); a verifier that produces
// NEW findings every round trips the churn guard and the close ships —
// an unconstrained adversary is an infinite nitpick generator.
//
// Verifier failures (pi error, unparseable verdict, timeout) FAIL OPEN with
// a loud log + adversary_error event: optional tooling must never wedge the
// close pipeline.
//
// See docs/architecture/shepherd.md ("Adversarial close gate").

type adversaryFinding struct {
	Class    string `json:"class"`    // "blocker" | "nit"
	Summary  string `json:"summary"`  // one line
	Citation string `json:"citation"` // acceptance criterion or reproducible evidence
}

type adversaryVerdict struct {
	Score    float64            `json:"score"`
	Findings []adversaryFinding `json:"findings"`
	Notes    string             `json:"notes"`
}

// churn memory: beadID -> signature of the blocker set from the previous
// round. Process-local like the publisher waiting marks; a daemon restart
// simply re-verifies once.
var (
	adversaryChurnMu sync.Mutex
	adversaryChurn   = map[string]string{}
)

// Test seams: the pi runner and diff collector are package vars so tests can
// drive the gate without a model or a git repo.
var (
	adversaryRunner = runAdversaryPi
	adversaryDiffFn = adversaryDiffEvidence
)

// adversaryBlockerSignature hashes the sorted blocker set so "same findings
// again" (worker not converging) is distinguishable from "new findings every
// round" (adversary manufacturing work).
func adversaryBlockerSignature(findings []adversaryFinding) string {
	parts := make([]string, 0, len(findings))
	for _, f := range findings {
		parts = append(parts, strings.ToLower(strings.TrimSpace(f.Class))+"|"+strings.ToLower(strings.TrimSpace(f.Summary)))
	}
	sort.Strings(parts)
	sum := sha1.Sum([]byte(strings.Join(parts, "\n")))
	return hex.EncodeToString(sum[:])
}

// verifyAdversarial runs the semantic close gate. Returns nil to ship, or the
// rejectClose error to abort the close (reopen/escalate semantics preserved).
func (c *BeadCloser) verifyAdversarial(bead *types.Bead, projectPath, repoName, repoPath, base string) error {
	if c.config == nil {
		return nil
	}
	cfg := agentVerifierWithDefaults(c.config.Validation.AgentVerifier)
	if !cfg.Enabled {
		return nil
	}
	if !adversaryClassGated(string(bead.Type), cfg.Classes) {
		return nil
	}
	beadID := string(bead.ID)

	diffStat, diff, err := adversaryDiffFn(repoPath, base, cfg.MaxDiffBytes)
	if err != nil {
		log.Printf("[Adversary] %s: diff evidence failed (%v) — fail-open, close proceeds", beadID, err)
		c.events.EmitRepo(projectPath, Event{Type: "adversary_error", Project: repoName, Bead: beadID,
			Reason: "diff-evidence", Detail: "fail-open: " + err.Error()})
		return nil
	}

	prompt := buildAdversaryPrompt(bead, diffStat, diff)
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(cfg.TimeoutSeconds)*time.Second)
	defer cancel()
	out, err := adversaryRunner(ctx, cfg, prompt, repoPath)
	if err != nil {
		log.Printf("[Adversary] %s: verifier error (%v) — fail-open, close proceeds", beadID, err)
		c.events.EmitRepo(projectPath, Event{Type: "adversary_error", Project: repoName, Bead: beadID,
			Reason: "verifier", Detail: "fail-open: " + err.Error()})
		return nil
	}
	verdict, err := parseAdversaryVerdict(out)
	if err != nil {
		log.Printf("[Adversary] %s: unparseable verdict (%v) — fail-open, close proceeds", beadID, err)
		c.events.EmitRepo(projectPath, Event{Type: "adversary_error", Project: repoName, Bead: beadID,
			Reason: "parse", Detail: "fail-open: " + err.Error()})
		return nil
	}

	var blockers, nits []adversaryFinding
	for _, f := range verdict.Findings {
		if strings.EqualFold(strings.TrimSpace(f.Class), "blocker") {
			blockers = append(blockers, f)
		} else {
			nits = append(nits, f)
		}
	}

	// Churn tripwire: on a retry round (the bead has a recorded signature),
	// a DIFFERENT blocker set means the adversary manufactures new findings
	// each round — the infinite-nitpick signature. Ship with the dissent
	// recorded instead of burning the remaining rounds.
	sig := adversaryBlockerSignature(blockers)
	adversaryChurnMu.Lock()
	prevSig, seen := adversaryChurn[beadID]
	adversaryChurn[beadID] = sig
	adversaryChurnMu.Unlock()
	if seen && len(blockers) > 0 && prevSig != "" && prevSig != sig {
		log.Printf("[Adversary] %s: churn tripwire — new blocker set on retry round; shipping with findings recorded", beadID)
		c.events.EmitRepo(projectPath, Event{Type: "adversary_churn", Project: repoName, Bead: beadID,
			Reason: "new-findings-per-round",
			Detail: fmt.Sprintf("verifier produced a different blocker set across rounds; shipping. blockers=%d nits=%d score=%.2f", len(blockers), len(nits), verdict.Score),
			Fields: map[string]any{"blockers": len(blockers), "nits": len(nits), "score": verdict.Score}})
		c.labelClosedWithFindings(bead, projectPath, repoName)
		return nil
	}

	if len(blockers) == 0 && verdict.Score >= cfg.MinScore {
		c.events.EmitRepo(projectPath, Event{Type: "agent_verdict", Project: repoName, Bead: beadID,
			Reason: "pass",
			Detail: fmt.Sprintf("adversary pass: score=%.2f nits=%d", verdict.Score, len(nits)),
			Fields: map[string]any{"score": verdict.Score, "blockers": 0, "nits": len(nits)}})
		log.Printf("[Adversary] %s: PASS score=%.2f nits=%d", beadID, verdict.Score, len(nits))
		if len(nits) > 0 {
			c.labelClosedWithFindings(bead, projectPath, repoName)
		}
		return nil
	}

	// Reject: feedback must reach the worker or the rounds are blind — the
	// blocker findings are appended to the bead description (fail-open when
	// the update fails), then the standard reject path reopens/escalates.
	feedback := adversaryFeedbackText(verdict, blockers, cfg.MinScore)
	c.events.EmitRepo(projectPath, Event{Type: "agent_verdict", Project: repoName, Bead: beadID,
		Reason: "reject",
		Detail: fmt.Sprintf("adversary reject: score=%.2f blockers=%d nits=%d", verdict.Score, len(blockers), len(nits)),
		Fields: map[string]any{"score": verdict.Score, "blockers": len(blockers), "nits": len(nits)}})
	log.Printf("[Adversary] %s: REJECT score=%.2f blockers=%d nits=%d", beadID, verdict.Score, len(blockers), len(nits))
	if c.beadsClient != nil && bead.Description != "" {
		newDesc := bead.Description + "\n\n## Adversary close-gate feedback (round " +
			fmt.Sprintf("%d", c.validationRetryCount(bead)+1) + ")\n" + feedback
		if err := c.beadsClient.UpdateBeadDescription(projectPath, repoName, bead.ID, newDesc); err != nil {
			log.Printf("[Adversary] %s: feedback injection failed (worker will retry blind): %v", beadID, err)
		}
	}
	return c.rejectClose(bead, projectPath, repoName, "adversary-blocker")
}

func adversaryClassGated(beadType string, classes []string) bool {
	t := strings.ToLower(strings.TrimSpace(beadType))
	for _, cl := range classes {
		if strings.EqualFold(strings.TrimSpace(cl), t) {
			return true
		}
	}
	return false
}

// labelClosedWithFindings marks a ship-with-dissent close; label add is
// fail-open (the close itself is already decided).
func (c *BeadCloser) labelClosedWithFindings(bead *types.Bead, projectPath, repoName string) {
	if c.beadsClient == nil {
		return
	}
	if err := c.beadsClient.UpdateBeadLabels(projectPath, repoName, bead.ID, []string{"closed-with-findings"}); err != nil {
		log.Printf("[Adversary] %s: closed-with-findings label failed: %v", string(bead.ID), err)
	}
}

// adversaryDiffEvidence collects the diff against the integration base,
// capped at maxBytes of patch content (stat is always complete).
func adversaryDiffEvidence(repoPath, base string, maxBytes int) (stat, patch string, err error) {
	statOut, err := exec.Command("git", "-C", repoPath, "diff", "--stat", "origin/"+base+"...HEAD").Output()
	if err != nil {
		return "", "", fmt.Errorf("diff --stat: %w", err)
	}
	patchOut, err := exec.Command("git", "-C", repoPath, "diff", "origin/"+base+"...HEAD").Output()
	if err != nil {
		return "", "", fmt.Errorf("diff: %w", err)
	}
	patch = string(patchOut)
	if len(patch) > maxBytes {
		patch = patch[:maxBytes] + "\n[... diff truncated at byte cap ...]"
	}
	return string(statOut), patch, nil
}

func adversaryFeedbackText(v *adversaryVerdict, blockers []adversaryFinding, minScore float64) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Adversarial close review REJECTED this close (score %.2f, threshold %.2f).\n", v.Score, minScore)
	if len(blockers) > 0 {
		b.WriteString("BLOCKER findings (each cites an acceptance criterion or reproducible defect):\n")
		for i, f := range blockers {
			fmt.Fprintf(&b, "%d. %s\n   evidence: %s\n", i+1, f.Summary, f.Citation)
		}
	} else {
		fmt.Fprintf(&b, "No blocker findings, but score %.2f is below the ship threshold %.2f — strengthen the work against the acceptance criteria.\n", v.Score, minScore)
	}
	if v.Notes != "" {
		b.WriteString("reviewer notes: " + v.Notes + "\n")
	}
	b.WriteString("Address the blockers (or raise the score), re-run the verification commands, and close again.")
	return b.String()
}

// buildAdversaryPrompt — the anti-nitpick rubric is the heart of the gate:
// every BLOCKER must cite a violated acceptance criterion or a reproducible
// defect; anything uncited is a NIT and cannot block. The reviewer is told
// it is falsifying claims, not improving code.
func buildAdversaryPrompt(bead *types.Bead, diffStat, diff string) string {
	return `You are an adversarial close reviewer for an autonomous coding daemon. A worker claims the bead below is complete. Your job is to FALSIFY that claim against the acceptance criteria — nothing else.

Rules (strict):
1. Every BLOCKER finding must cite the acceptance criterion it violates or a reproducible defect (a check that fails, a promised file/behavior absent from the diff). If you cannot cite one, the finding is a NIT.
2. You are NOT invited to improve the code. Style preferences, speculative edge cases, hypothetical refactors, and "could be better" are NITs and never block.
3. Score the work 0.0-1.0: 1.0 = every acceptance criterion demonstrably met by the diff; lower = unmet or unprovable claims.
4. Reply with ONLY a JSON object, no prose, no markdown fences:
{"score": <float>, "findings": [{"class": "blocker"|"nit", "summary": "<one line>", "citation": "<criterion or evidence>"}], "notes": "<one line>"}

BEAD
title: ` + bead.Title + `
type: ` + string(bead.Type) + `
description (contains the acceptance criteria):
` + bead.Description + `

DIFF STAT vs integration base:
` + diffStat + `
DIFF (possibly truncated):
` + diff + `
`
}

// runAdversaryPi invokes pi headless, mirroring the Pan runner (prompt on
// stdin, --no-extensions, optional provider/model identity).
func runAdversaryPi(ctx context.Context, cfg AgentVerifierConfig, prompt, dir string) (string, error) {
	args := []string{"--no-extensions"}
	if cfg.Provider != "" {
		args = append(args, "--provider", cfg.Provider)
	}
	if cfg.Model != "" {
		args = append(args, "--model", cfg.Model)
	}
	piPath := cfg.PiPath
	if piPath == "" {
		piPath = "pi"
	}
	if env := os.Getenv("HYDRA_PI_PATH"); env != "" {
		piPath = env
	}
	cmd := exec.CommandContext(ctx, piPath, args...)
	cmd.Dir = dir
	cmd.Stdin = strings.NewReader(prompt)
	cmd.Env = os.Environ()
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("pi: %w: %s", err, truncateStr(string(out), 400))
	}
	return string(out), nil
}

// parseAdversaryVerdict extracts the JSON verdict — tolerant of the model
// wrapping it in prose or a fenced block (same lesson as the worker fence
// extractor): try the whole output, then the last {...} region.
func parseAdversaryVerdict(out string) (*adversaryVerdict, error) {
	trimmed := strings.TrimSpace(out)
	var v adversaryVerdict
	if err := json.Unmarshal([]byte(trimmed), &v); err == nil {
		return &v, nil
	}
	start := strings.Index(trimmed, "{")
	end := strings.LastIndex(trimmed, "}")
	if start < 0 || end <= start {
		return nil, fmt.Errorf("no JSON object in verifier output (first 200 chars): %s", truncateStr(trimmed, 200))
	}
	if err := json.Unmarshal([]byte(trimmed[start:end+1]), &v); err != nil {
		return nil, fmt.Errorf("verdict JSON invalid: %w", err)
	}
	return &v, nil
}

func truncateStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
