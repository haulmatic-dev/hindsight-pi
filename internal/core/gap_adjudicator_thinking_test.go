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
	args := adjudicatorArgs("kimi-coding", "k3-256k", "max")
	if !hasConsecutive(args, "--model", "k3-256k") {
		t.Errorf("missing model override in %v", args)
	}
	if !hasConsecutive(args, "--thinking-level", "max") {
		t.Errorf("missing thinking override in %v", args)
	}
}

// Unset thinking must leave the argv byte-identical to the previous behaviour.
func TestAdjudicatorArgs_UnsetOmitsFlag(t *testing.T) {
	args := adjudicatorArgs("kimi-coding", "kimi-for-coding", "")
	for _, a := range args {
		if a == "--thinking-level" {
			t.Fatalf("--thinking-level must be absent when unconfigured: %v", args)
		}
	}
	if strings.Join(args, " ") != "--no-session --provider kimi-coding --model kimi-for-coding" {
		t.Errorf("unexpected argv: %v", args)
	}
}

// A disabled level must be coerced to "low", never emitted as none/off.
func TestAdjudicatorArgs_NeverDisablesThinking(t *testing.T) {
	for _, level := range []string{"none", "off", "disabled"} {
		args := adjudicatorArgs("kimi-coding", "k3-256k", level)
		if !hasConsecutive(args, "--thinking-level", "low") {
			t.Errorf("level %q should coerce to low, got %v", level, args)
		}
	}
}

func TestAdjudicatorArgs_EmptyProviderAndModelOmitted(t *testing.T) {
	args := adjudicatorArgs("", "", "high")
	if strings.Join(args, " ") != "--no-session --thinking-level high" {
		t.Errorf("unexpected argv: %v", args)
	}
}
