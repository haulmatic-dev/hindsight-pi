package core

import (
	"strings"
	"testing"
)

// Parser registry: deterministic extraction of gate verdicts from raw command
// output. Deterministic extraction covers the common case with zero LLM
// tokens; ambiguous output escalates instead of guessing.

func TestGateParser_ExitCode(t *testing.T) {
	p, ok := GateParserFor("exit-code")
	if !ok {
		t.Fatal("exit-code parser must be registered")
	}
	if v := p.Parse("anything", 0, 0); !v.Passed {
		t.Errorf("exit 0 must pass, got %+v", v)
	}
	v := p.Parse("boom", 2, 0)
	if v.Passed {
		t.Errorf("exit 2 must fail")
	}
	if v.Summary == "" {
		t.Errorf("failure summary must not be empty")
	}
}

func TestGateParser_JestCoverage(t *testing.T) {
	p, ok := GateParserFor("jest-coverage")
	if !ok {
		t.Fatal("jest-coverage parser must be registered")
	}

	passLog := `
Test Suites: 41 passed, 41 total
Tests:       182 passed, 182 total
All files | 85.34 | 90.1 | 88.0 | 85.2 |
`
	v := p.Parse(passLog, 0, 80)
	if !v.Passed {
		t.Errorf("85.34%% >= 80 with exit 0 must pass, got %+v", v)
	}

	failCov := `
FAIL src/modules/x/x.service.spec.ts
All files | 72.5 | 80.0 | 70.0 | 72.1 |
`
	v = p.Parse(failCov, 0, 80)
	if v.Passed {
		t.Errorf("72.5%% < 80 must fail")
	}
	if !containsAll(v.Summary, "72.5", "x.service.spec.ts") {
		t.Errorf("summary must name coverage and failing suite, got %q", v.Summary)
	}

	// Coverage table absent (runner exploded before reporting) -> fail-closed.
	v = p.Parse("jest: command not found", 127, 80)
	if v.Passed {
		t.Errorf("unparseable output must fail closed")
	}
}

func TestGateParser_UnknownFailsClosed(t *testing.T) {
	if _, ok := GateParserFor("definitely-not-a-parser"); ok {
		t.Errorf("unknown parser must not resolve — fail closed, never silently pass")
	}
}

func containsAll(s string, subs ...string) bool {
	for _, sub := range subs {
		if !strings.Contains(s, sub) {
			return false
		}
	}
	return true
}
