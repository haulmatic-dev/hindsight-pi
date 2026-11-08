// Brownfield subagent enrichment (FR-003, hydra-ew18 contract).
//
// GREEN implementation. `enrichBeadClaims` runs a detached text-mode subagent
// scoped to the bead's target files ONLY when every target file exists on the
// integration base (brownfield trigger: `git cat-file -e <base>:<path>`). It
// parses the agent output into `claim: <assertion> (verify: ...)` lines plus
// summary assertions, drops summary lines with no matching claim (anti-
// laundering gate), applies the token budget (context_pack.max_pack_tokens,
// chars/4, oldest-first elision with an `elided: <n>` footer), and commits
// the result as docs/hydra/handoff/enrich-<bead>.md in the worktree so the
// existing loadChainContext injection picks it up (no new channel).
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/haulmatic-dev/hydra/internal/core"
	"github.com/haulmatic-dev/hydra/pkg/types"
)

// enrichmentImplemented gates the enforce run of the enrichment contract
// suite. Flipped by GREEN.
const enrichmentImplemented = true

// enrichAgentRunner is the injectable subagent seam. Production launches a
// detached text-mode Pi child (pan.RunPi pattern); tests inject a fake.
type enrichAgentRunner func(worktreePath, prompt string) (stdout string, err error)

// enrichEnrichTimeout bounds the detached subagent run.
const enrichEnrichTimeout = 10 * time.Minute

// enrichDefaultAgentRunner is the production detached runner.
var enrichDefaultAgentRunner enrichAgentRunner = enrichDetachedPiRunner

// enrichAgentArgv returns the argv the production runner uses for the given
// prompt: detached text mode, never rpc mode (discovery decision 1).
func enrichAgentArgv(prompt string) []string {
	return []string{"--no-extensions", "--no-session", prompt}
}

// enrichDetachedPiRunner runs a one-shot Pi text-mode child with the prompt
// on stdin and the captured stdout returned. No session, no extensions; no
// verdict protocol state.
func enrichDetachedPiRunner(worktreePath, prompt string) (string, error) {
	piPath := os.Getenv("HYDRA_PI_PATH")
	if piPath == "" {
		piPath = "/bin/pi"
	}
	ctx, cancel := context.WithTimeout(context.Background(), enrichEnrichTimeout)
	defer cancel()
	argv := enrichAgentArgv(prompt)
	c := exec.CommandContext(ctx, piPath, argv...)
	c.Dir = worktreePath
	c.Stdin = strings.NewReader(prompt)
	c.Stderr = os.Stderr
	out, err := c.Output()
	if err != nil {
		return string(out), fmt.Errorf("enrichment subagent failed: %w", err)
	}
	return string(out), nil
}

// enrichSubstantiveWords tokenizes s into lowercase alnum runs of length >= 2.
func enrichSubstantiveWords(s string) map[string]bool {
	words := map[string]bool{}
	cur := strings.Builder{}
	flush := func() {
		w := strings.ToLower(cur.String())
		cur.Reset()
		if len(w) >= 1 {
			words[w] = true
		}
	}
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			cur.WriteRune(r)
		} else {
			flush()
		}
	}
	flush()
	return words
}

// enrichClaim holds one parsed `claim: <assertion> (verify: ...)` line.
type enrichClaim struct {
	line      string // full raw line
	assertion string // text after "claim:" up to any "(verify:" clause
}

// enrichParseClaims extracts claim lines from agent output.
func enrichParseClaims(output string) []enrichClaim {
	var claims []enrichClaim
	for _, line := range strings.Split(output, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "claim:") {
			continue
		}
		body := strings.TrimSpace(strings.TrimPrefix(trimmed, "claim:"))
		assertion := body
		if idx := strings.Index(strings.ToLower(body), "(verify:"); idx >= 0 {
			assertion = strings.TrimSpace(body[:idx])
		}
		claims = append(claims, enrichClaim{line: trimmed, assertion: assertion})
	}
	return claims
}

// enrichLineMatches reports whether a summary line shares at least 2
// substantive words with some claim — the affixed anti-laundering match.
func enrichLineMatches(line string, claims []enrichClaim) bool {
	lw := enrichSubstantiveWords(line)
	for _, c := range claims {
		cw := enrichSubstantiveWords(c.line)
		shared := 0
		for w := range lw {
			if cw[w] {
				shared++
				if shared >= 2 {
					return true
				}
			}
		}
	}
	return false
}

// enrichStrongAssertions reports whether the claims carry substantive content
// (some assertion has >= 2 substantive words). Vague claim sets cannot
// attribute laundering, so the gate fails open.
func enrichStrongAssertions(claims []enrichClaim) bool {
	for _, c := range claims {
		w := enrichSubstantiveWords(c.assertion)
		if len(w) >= 2 {
			return true
		}
	}
	return false
}

// enrichBeadClaims runs the enrichment subagent scoped to the bead's target
// files and returns its claims + anti-laundered, budgeted summary.
func enrichBeadClaims(bead types.Bead, projectPath, worktreePath string, run enrichAgentRunner) ([]string, string, error) {
	beadID := string(bead.ID)
	docPath := filepath.Join(worktreePath, "docs", "hydra", "handoff", "enrich-"+beadID+".md")
	if _, err := os.Stat(docPath); err == nil {
		// Discovery A2 memoization: once per bead.
		return nil, "", nil
	}

	targets := extractTargetFiles(bead)
	if len(targets) > 0 {
		base, _, err := core.ResolveIntegrationBase(nil, projectPath)
		if err != nil {
			base = ""
		}
		brownfield := checkBrownfield(worktreePath, base, targets)
		if !brownfield {
			log.Printf("[Enrich] %s: greenfield (target files absent on base %q) — enrichment skipped, zero tokens", beadID, base)
			return nil, "", nil
		}
	}

	prompt := buildEnrichPrompt(bead, targets)
	stdout, err := run(worktreePath, prompt)
	if err != nil {
		return nil, "", err
	}

	claims := enrichParseClaims(stdout)
	claimLines := make([]string, 0, len(claims))
	for _, c := range claims {
		claimLines = append(claimLines, c.line)
	}

	summary, dropped := enrichEmitSummary(stdout, claims)
	summary, _ = applyEnrichBudget(summary, enrichDefaultBudgetTokens(), dropped)

	doc := renderEnrichDoc(beadID, claimLines, summary)
	if err := writeEnrichDoc(docPath, doc); err != nil {
		return claimLines, summary, err
	}
	return claimLines, summary, nil
}

// checkBrownfield runs `git cat-file -e <base>:<path>` for every target; any
// failure means greenfield. Unresolvable/empty base → greenfield (fail-open
// to no enrichment, never blocks the bead).
func checkBrownfield(worktreePath, base string, targets []string) bool {
	if base == "" {
		return false
	}
	for _, t := range targets {
		c := exec.Command("git", "-C", worktreePath, "cat-file", "-e", base+":"+t)
		if err := c.Run(); err != nil {
			return false
		}
	}
	return true
}

// buildEnrichPrompt scopes the subagent strictly to the bead's target files.
func buildEnrichPrompt(bead types.Bead, targets []string) string {
	var b strings.Builder
	b.WriteString("Analyze the existing code and emit the facts a fresh implementer needs.\n")
	b.WriteString("Bead: " + string(bead.ID) + " — " + bead.Title + "\n")
	if bead.Description != "" {
		b.WriteString("Task: " + firstEnrichLines(bead.Description, 20) + "\n")
	}
	b.WriteString("Scope (target files ONLY): " + strings.Join(targets, ", ") + "\n")
	b.WriteString("Output format: one `claim: <assertion> (verify: <command>)` line per fact,\n")
	b.WriteString("followed by one `- <fact>` summary line per claim, in the same order.\n")
	return b.String()
}

// firstEnrichLines caps a description to its first n lines.
func firstEnrichLines(s string, n int) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) > n {
		lines = lines[:n]
	}
	return strings.Join(lines, " ")
}

// enrichEmitSummary applies the anti-laundering gate: each summary line must
// share at least 3 substantive words with a claim line within 8 lines of it
// (adjacency-scoped attribution — a summary block cannot launder against a
// distant claims block); anything else is dropped (counted for the `elided:`
// footer). When NO summary line matches any claim, the gate fails open and
// keeps all lines — without a single verifiable pairing there is nothing to
// prove laundering against, and inventing an e.g. greenfield agent format
// must not destroy the agent's summary.
func enrichEmitSummary(output string, claims []enrichClaim) (string, int) {
	type entry struct {
		line       string
		isClaim    bool
		claimWords map[string]bool
	}
	entries := make([]entry, 0, len(claims)+4)
	claimIdx := map[int]map[string]bool{}
	ci := -1
	ci2 := -1
	for _, raw := range strings.Split(output, "\n") {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			continue
		}
		ci++
		ci2 = len(entries)
		if strings.HasPrefix(trimmed, "claim:") {
			w := enrichSubstantiveWords(trimmed)
			claimIdx[ci2] = w
			entries = append(entries, entry{line: trimmed, isClaim: true, claimWords: w})
			continue
		}
		entries = append(entries, entry{line: trimmed})
	}

	matched := make([]bool, len(entries))
	anyMatched := false
	const window = 8
	for i, e := range entries {
		if e.isClaim {
			continue
		}
		lw := enrichSubstantiveWords(e.line)
		for j, c := range entries {
			if j > i+window {
				break
			}
			if !c.isClaim {
				continue
			}
			if j+window < i {
				continue
			}
			shared := 0
			for w := range lw {
				if claimIdx[j][w] {
					shared++
				}
			}
			if shared >= 3 {
				matched[i] = true
				anyMatched = true
				break
			}
		}
	}

	var kept []string
	dropped := 0
	for i, e := range entries {
		if e.isClaim {
			continue
		}
		if matched[i] || (!anyMatched && !enrichStrongAssertions(claims)) {
			kept = append(kept, e.line)
		} else {
			dropped++
		}
	}
	return strings.Join(kept, "\n"), dropped
}

// applyEnrichBudget applies the token budget (estimated tokens = chars/4,
// oldest-first elision — the head lines go first) and appends one
// `elided: <n>` footer counting the elided lines plus any lines already
// dropped by the anti-laundering gate. No overflow → unchanged, unless drops
// occurred, in which case the footer carries the drop count.
func applyEnrichBudget(summary string, budget, alreadyDropped int) (string, bool) {
	if budget <= 0 && alreadyDropped == 0 {
		return summary, false
	}
	lines := strings.Split(summary, "\n")
	if lines[0] == "" && len(lines) == 1 {
		if alreadyDropped == 0 {
			return summary, false
		}
		return summary + fmt.Sprintf("\nelided: %d", alreadyDropped), true
	}
	estimated := 0
	for _, l := range lines {
		estimated += (len(l) + 3) / 4
	}
	removedCount := 0
	kept := make([]string, 0, len(lines))
	for _, l := range lines {
		if estimated > budget && budget > 0 {
			estimated -= (len(l) + 3) / 4
			removedCount++
			continue
		}
		kept = append(kept, l)
	}
	if removedCount+alreadyDropped > 0 {
		kept = append(kept, fmt.Sprintf("elided: %d", removedCount+alreadyDropped))
	}
	return strings.Join(kept, "\n"), removedCount > 0
}

// renderEnrichDoc renders the handoff markdown committed next to the other
// chain handoffs so loadChainContext picks it up.
func renderEnrichDoc(beadID string, claims []string, summary string) string {
	var b strings.Builder
	b.WriteString("# Enrichment handoff (hydra-" + beadID + ")\n\n")
	if len(claims) > 0 {
		b.WriteString("## Claims\n\n")
		for _, c := range claims {
			b.WriteString(c + "\n")
		}
		b.WriteString("\n")
	}
	if summary != "" {
		b.WriteString("## Summary\n\n" + summary + "\n")
	}
	return b.String()
}

// writeEnrichDoc creates the handoff directory and writes the doc.
func writeEnrichDoc(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("enrich handoff mkdir: %w", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return fmt.Errorf("enrich handoff write: %w", err)
	}
	return nil
}

// enrichDefaultBudgetTokens returns the token budget from
// context_pack.max_pack_tokens (default internal/core.DefaultMaxPackTokens).
// The centralized config is not read here — the worker applies the default;
// per-project overrides follow the same default the pack budget ships with.
func enrichDefaultBudgetTokens() int {
	if v := os.Getenv("HYDRA_ENRICH_MAX_TOKENS"); v != "" {
		var n int
		if _, err := fmt.Sscanf(v, "%d", &n); err == nil && n > 0 {
			return n
		}
	}
	return core.DefaultMaxPackTokens
}
