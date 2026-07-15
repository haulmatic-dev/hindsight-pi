package main

import (
	"errors"
	"strings"
	"testing"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// TestBuildPiPrompt_ReflexionAndCass verifies the prompt carries the previous
// attempt's failure and injected CASS rules — and omits both sections when
// there is nothing to say.
func TestBuildPiPrompt_ReflexionAndCass(t *testing.T) {
	bead := types.Bead{Type: "task", Title: "Implement X", Description: "desc"}

	plain := buildPiPrompt(bead, nil, promptExtras{})
	if strings.Contains(plain, "PREVIOUS ATTEMPT FAILED") || strings.Contains(plain, "playbook") {
		t.Error("empty extras must add no sections")
	}

	enriched := buildPiPrompt(bead, nil, promptExtras{
		PrevFailure: "exit code 1. undefined: scopeViolations",
		CassBullets: []string{"never place worktrees in the project parent dir"},
	})
	if !strings.Contains(enriched, "PREVIOUS ATTEMPT FAILED: exit code 1. undefined: scopeViolations") {
		t.Error("prompt must carry the previous failure")
	}
	if !strings.Contains(enriched, "Do not repeat the same approach") {
		t.Error("prompt must instruct to diagnose, not repeat")
	}
	if !strings.Contains(enriched, "never place worktrees in the project parent dir") {
		t.Error("prompt must carry CASS bullets")
	}
}

func TestSummarizeFailure(t *testing.T) {
	s := summarizeFailure(&ExecutionResult{ExitCode: 2, Error: "build failed: undefined: Foo"}, errors.New("exit status 2"))
	for _, want := range []string{"exit code 2", "undefined: Foo", "exit status 2"} {
		if !strings.Contains(s, want) {
			t.Errorf("summary missing %q: %s", want, s)
		}
	}

	// Falls back to Output when Error is empty; long tails truncated from the front.
	long := strings.Repeat("x", 500) + "TAIL"
	s2 := summarizeFailure(&ExecutionResult{ExitCode: 1, Output: long}, nil)
	if !strings.Contains(s2, "TAIL") {
		t.Error("summary must keep the tail of long output")
	}
	if len(s2) > 500 {
		t.Errorf("summary too long: %d", len(s2))
	}

	if got := summarizeFailure(nil, nil); got != "" {
		t.Errorf("nil/nil should be empty, got %q", got)
	}
}

func TestParseCASSBullets(t *testing.T) {
	// Real cm shape (verified live): data.relevantBullets, scores ~0.4 for
	// healthy young rules — the noise floor is 0.3, not 0.7.
	out := []byte(`{"success":true,"data":{"relevantBullets":[
		{"content":"rule one","finalScore":0.4},
		{"content":"noise","finalScore":0.1},
		{"content":"dead rule","finalScore":0.9,"deprecated":true},
		{"content":"rule two","finalScore":0.35},
		{"content":"rule three","finalScore":0.8},
		{"content":"rule four (over cap)","finalScore":0.99}
	]}}`)
	got := parseCASSBullets(out)
	if len(got) != 3 {
		t.Fatalf("expected 3 bullets (floor-gated, deprecated-skipped, capped), got %d: %v", len(got), got)
	}
	for _, g := range got {
		if g == "noise" || g == "dead rule" {
			t.Errorf("filtered bullet leaked: %q", g)
		}
	}

	if parseCASSBullets([]byte("not json")) != nil {
		t.Error("garbage input must yield nil")
	}
	if parseCASSBullets([]byte(`{"data":{"relevantBullets":[]}}`)) != nil {
		t.Error("empty bullets must yield nil")
	}
}
