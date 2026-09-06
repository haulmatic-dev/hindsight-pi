package core

import (
	"strings"
	"testing"
)

func TestNormalizeThinking(t *testing.T) {
	cases := map[string]string{
		"max":      "max",
		"HIGH":     "high",
		" low ":    "low",
		"medium":   "medium", // endpoint maps medium→high; not rewritten client-side
		"none":     "low",    // never disable: the endpoint would reroute to K2.6
		"off":      "low",
		"disabled": "low",
		"":         "",
	}
	for in, want := range cases {
		if got := normalizeThinking(in); got != want {
			t.Errorf("normalizeThinking(%q) = %q, want %q", in, got, want)
		}
	}
}

func hasConsecutive(args []string, a, b string) bool {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == a && args[i+1] == b {
			return true
		}
	}
	return false
}

func TestAdjudicatorArgs_ThinkingOverride(t *testing.T) {
	args := adjudicatorArgs("kimi-coding", "k3-256k", "max", "hydra-adjud-b1")
	if !hasConsecutive(args, "--model", "k3-256k") {
		t.Errorf("missing model override in %v", args)
	}
	if !hasConsecutive(args, "--thinking-level", "max") {
		t.Errorf("missing thinking override in %v", args)
	}
}

// Unset thinking must leave the argv byte-identical to the previous behaviour.
func TestAdjudicatorArgs_UnsetOmitsFlag(t *testing.T) {
	args := adjudicatorArgs("kimi-coding", "kimi-for-coding", "", "hydra-adjud-b1")
	for _, a := range args {
		if a == "--thinking-level" {
			t.Fatalf("--thinking-level must be absent when unconfigured: %v", args)
		}
	}
	if strings.Join(args, " ") != "--session-id hydra-adjud-b1 --provider kimi-coding --model kimi-for-coding" {
		t.Errorf("unexpected argv: %v", args)
	}
}

// A disabled level must be coerced to "low", never emitted as none/off.
func TestAdjudicatorArgs_NeverDisablesThinking(t *testing.T) {
	for _, level := range []string{"none", "off", "disabled"} {
		args := adjudicatorArgs("kimi-coding", "k3-256k", level, "hydra-adjud-b1")
		if !hasConsecutive(args, "--thinking-level", "low") {
			t.Errorf("level %q should coerce to low, got %v", level, args)
		}
	}
}

func TestAdjudicatorArgs_EmptyProviderAndModelOmitted(t *testing.T) {
	args := adjudicatorArgs("", "", "high", "hydra-adjud-b1")
	if strings.Join(args, " ") != "--session-id hydra-adjud-b1 --thinking-level high" {
		t.Errorf("unexpected argv: %v", args)
	}
}

// The adjudicator must run in a named per-bead-purpose session, not
// --no-session: naming keeps the transcript resumable and the prompt-cache
// prefix warm; per-bead scoping prevents verdict bias from prior
// adjudications. (Cache hardening, 2026-09-06.)
func TestAdjudicatorArgs_NamedSession(t *testing.T) {
	args := adjudicatorArgs("", "", "", "hydra-adjud-srv-x-1")
	if !hasConsecutive(args, "--session-id", "hydra-adjud-srv-x-1") {
		t.Errorf("missing named session in %v", args)
	}
	for _, a := range args {
		if a == "--no-session" {
			t.Fatalf("--no-session must be gone once sessions are named: %v", args)
		}
	}
}

func TestAdjudicatorSessionID(t *testing.T) {
	if got := adjudicatorSessionID(gap{SuggestedBead: "srv-smart-engine-abc"}); got != "hydra-adjud-srv-smart-engine-abc" {
		t.Errorf("suggested bead: got %q", got)
	}
	if got := adjudicatorSessionID(gap{ID: "gap-7"}); got != "hydra-adjud-gap-7" {
		t.Errorf("gap id fallback: got %q", got)
	}
	if got := adjudicatorSessionID(gap{ID: "a/b c"}); got != "hydra-adjud-a-b-c" {
		t.Errorf("sanitise: got %q", got)
	}
}
