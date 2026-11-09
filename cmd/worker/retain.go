package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/haulmatic-dev/hydra/internal/degrade"
)

// Retain-content policy (Sink A, PRD verdict memory): the extraction LLM
// (Hindsight's MiniMax) must see signal, not stream noise.
//
//   - A parsed HYDRA_VERDICT is the distilled outcome — done, acceptance
//     criteria, files changed, verification commands. Retaining it instead of
//     the raw turn text raises memory quality AND cuts extraction input from
//     tens of KB to ~200 tokens.
//   - Without a verdict (crashed/failed runs), the output is sanitized:
//     thinkingSignature blobs and base64 runs stripped, tail-biased cap —
//     failure evidence (errors, failing tests) lives at the tail.

const (
	retainVerdictCap   = 2048 // verdict path: structured summary cap
	retainSanitizedCap = 4096 // fallback path: sanitized output cap
	base64RunMinLength = 120  // strip base64-ish runs at least this long
)

var (
	thinkingSignatureRe = regexp.MustCompile(`"thinkingSignature"\s*:\s*"[^"]*"`)
	base64RunRe         = regexp.MustCompile(`[A-Za-z0-9+/]{` + fmt.Sprint(base64RunMinLength) + `,}={0,2}`)
)

// recordRetainAttempt / recordRetainSuccess feed the FR-032 fail-open
// degradation counters: retention is fail-open (errors logged and swallowed),
// so a permanently dead Hindsight would otherwise be invisible.
func recordRetainAttempt() { degrade.RecordAttempt("hindsight-retain") }
func recordRetainSuccess() { degrade.RecordSuccess("hindsight-retain") }

// buildRetainContent decides what the worker stores in Hindsight for a run.
// Verdict path when a verdict was parsed; sanitized-output fallback otherwise.
func buildRetainContent(result *ExecutionResult) string {
	if result == nil {
		return ""
	}
	var b strings.Builder
	// Fix-retention hook LEADS with the commits this worker landed so
	// extraction derives "fixed by <sha>" corrective facts automatically —
	// the stale-as-current asymmetry (incidents retained, fixes not) dies
	// here. Leading also preserves the failure-evidence-at-tail invariant
	// of sanitizeForRetain below.
	b.WriteString(strings.TrimLeft(collectFixCommits(), "\n"))
	if result.Verdict != nil {
		if v, err := json.Marshal(result.Verdict); err == nil {
			content := "HYDRA_VERDICT: " + string(v)
			if len(content) > retainVerdictCap {
				content = content[:retainVerdictCap]
			}
			b.WriteString(content)
		} else {
			b.WriteString(sanitizeForRetain(result.Output))
		}
	} else {
		b.WriteString(sanitizeForRetain(result.Output))
	}
	return b.String()
}

// collectFixCommits lists the most recent commits from the worktree the
// worker executed in (cwd). Fail-open: no git, no commits, or a dirty
// mid-rebase state returns "" — retention must never fail on this.
func collectFixCommits() string {
	out, err := execGitLog()
	if err != nil || strings.TrimSpace(out) == "" {
		return ""
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) > 10 {
		lines = lines[:10]
	}
	var b strings.Builder
	b.WriteString("\n\nFIX_COMMITS:\n")
	for _, l := range lines {
		b.WriteString("- " + strings.TrimSpace(l) + "\n")
	}
	return b.String()
}

func execGitLog() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "log", "--oneline", "--no-decorate", "-n", "10")
	cmd.Dir = "."
	out, err := cmd.Output()
	return string(out), err
}

// sanitizeForRetain strips extraction-hostile noise from raw output:
// thinkingSignature values (1-2KB base64 each, hundreds per run), long
// base64 runs, and excess blank lines; then caps tail-biased because errors
// and failure causes accumulate at the end.
func sanitizeForRetain(output string) string {
	out := thinkingSignatureRe.ReplaceAllString(output, `"thinkingSignature":"…"`)
	out = base64RunRe.ReplaceAllString(out, "…")
	out = collapseBlankLines(out)
	if len(out) > retainSanitizedCap {
		head := retainSanitizedCap / 4
		tail := retainSanitizedCap - head
		out = out[:head] + "\n…[truncated]…\n" + out[len(out)-tail:]
	}
	return out
}

// collapseBlankLines reduces 3+ consecutive newlines to 2.
func collapseBlankLines(s string) string {
	for strings.Contains(s, "\n\n\n") {
		s = strings.ReplaceAll(s, "\n\n\n", "\n\n")
	}
	return s
}
