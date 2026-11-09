package main

import (
	"strings"
	"testing"
)

func TestBuildRetainContent_VerdictPreferred(t *testing.T) {
	result := &ExecutionResult{
		Output: strings.Repeat("raw turn text noise ", 500),
		Verdict: &Verdict{
			Done:         true,
			ACMet:        []string{"tests pass"},
			FilesChanged: []string{"internal/registry/registry.go"},
			Verification: []VerificationEntry{{Cmd: "go test ./internal/registry/", Result: "ok"}},
		},
	}
	content := buildRetainContent(result)
	if !strings.Contains(content, "HYDRA_VERDICT") || !strings.Contains(content, "internal/registry/registry.go") {
		t.Errorf("verdict path must carry the structured summary, got: %q", content[:200])
	}
	if strings.Contains(content, "raw turn text noise") {
		t.Errorf("verdict path must NOT include raw output")
	}
	if len(content) > retainVerdictCap+16 {
		t.Errorf("verdict content exceeds cap: %d", len(content))
	}
}

func TestBuildRetainContent_AbandonVerdictKept(t *testing.T) {
	result := &ExecutionResult{
		Output:  "partial work",
		Verdict: &Verdict{Done: false, Summary: "implemented 2 of 3, blocked on API"},
	}
	content := buildRetainContent(result)
	if !strings.Contains(content, "blocked on API") {
		t.Errorf("abandon summary is exactly the memory worth keeping, got: %q", content)
	}
}

func TestBuildRetainContent_FallbackSanitizes(t *testing.T) {
	sig := `"thinkingSignature":"` + strings.Repeat("QUJD", 400) + `"`
	raw := "work happened\n" + sig + "\nmore work"
	result := &ExecutionResult{Output: raw}
	content := buildRetainContent(result)
	if strings.Contains(content, strings.Repeat("QUJD", 10)) {
		t.Errorf("thinkingSignature blob must be stripped")
	}
	if !strings.Contains(content, "work happened") || !strings.Contains(content, "more work") {
		t.Errorf("real text must survive sanitizing, got: %q", content)
	}
}

func TestBuildRetainContent_FallbackTailBiasedCap(t *testing.T) {
	head := strings.Repeat("H", 3000)
	tail := "FAIL: TestRegistry expected 1 got 2"
	result := &ExecutionResult{Output: head + strings.Repeat("filler\n", 2000) + tail}
	content := buildRetainContent(result)
	// commits block (~300-600B) + capped sanitized output
	if len(content) > retainSanitizedCap+64+len(collectFixCommits()) {
		t.Errorf("sanitized content exceeds cap: %d", len(content))
	}
	if !strings.HasSuffix(content, tail) {
		t.Errorf("failure evidence at the tail must survive, tail is: %q", content[len(content)-60:])
	}
}

func TestBuildRetainContent_NilAndEmpty(t *testing.T) {
	if got := buildRetainContent(nil); got != "" {
		t.Errorf("nil result = empty content, got %q", got)
	}
	// Non-nil empty result in a git repo still carries the FIX_COMMITS block
	// (fix-retention hook, now leading); outside git it's empty — both fine.
	got := buildRetainContent(&ExecutionResult{})
	if got != "" && !strings.HasPrefix(got, "FIX_COMMITS") {
		t.Errorf("empty result = commits-leading or empty, got %q", got[:min(len(got), 30)])
	}
}
