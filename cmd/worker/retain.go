package main

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

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
	if result.Verdict != nil {
		if b, err := json.Marshal(result.Verdict); err == nil {
			content := "HYDRA_VERDICT: " + string(b)
			if len(content) > retainVerdictCap {
				content = content[:retainVerdictCap]
			}
			return content
		}
	}
	return sanitizeForRetain(result.Output)
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
