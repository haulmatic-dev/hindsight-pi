package core

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// GateVerdict is the deterministic outcome of parsing a gate's raw output.
// Summary is bounded (~2KB cap applied by the runner) and is the only part
// that ever reaches an LLM prompt; raw logs are referenced by path.
type GateVerdict struct {
	Passed  bool
	Summary string
}

// GateParser extracts a verdict from raw gate output. Parsers are
// deterministic, stack-specific, and versioned with hydra — the finite
// registry is the stack-knowledge boundary between daemon and config.
type GateParser interface {
	Parse(output string, exitCode int, threshold int) GateVerdict
}

var gateParsers = map[string]GateParser{
	"exit-code":     exitCodeParser{},
	"jest-coverage": jestCoverageParser{},
}

// GateParserFor resolves a parser by config name. Unknown names fail closed:
// a gate that cannot parse its output must never silently pass.
func GateParserFor(name string) (GateParser, bool) {
	p, ok := gateParsers[name]
	return p, ok
}

// exitCodeParser trusts the process exit code only. For lint/build-style
// gates where the tool's exit status is the contract.
type exitCodeParser struct{}

func (exitCodeParser) Parse(output string, exitCode int, _ int) GateVerdict {
	if exitCode == 0 {
		return GateVerdict{Passed: true, Summary: "exit 0"}
	}
	tail := tailLines(output, 20)
	return GateVerdict{Passed: false, Summary: fmt.Sprintf("exit %d\n%s", exitCode, tail)}
}

// jestCoverageParser reads jest's coverage text summary ("All files | 85.3 |
// ...") plus FAIL suite lines. Passing requires exit 0 AND line coverage at
// or above the threshold. A missing coverage line is fail-closed: the runner
// exploded before producing evidence.
type jestCoverageParser struct{}

var (
	jestCoverageLine = regexp.MustCompile(`(?m)^All files\s*\|\s*([0-9]+(?:\.[0-9]+)?)`)
	jestFailLine     = regexp.MustCompile(`(?m)^FAIL\s+(\S+)`)
)

func (jestCoverageParser) Parse(output string, exitCode int, threshold int) GateVerdict {
	var sb strings.Builder

	m := jestCoverageLine.FindStringSubmatch(output)
	if m == nil {
		sb.WriteString("coverage line not found (no 'All files |' row)")
	} else {
		cov, _ := strconv.ParseFloat(m[1], 64)
		fmt.Fprintf(&sb, "coverage %.2f%% (threshold %d%%)", cov, threshold)
		if cov < float64(threshold) {
			sb.WriteString(" — BELOW THRESHOLD")
		}
	}

	suites := jestFailLine.FindAllStringSubmatch(output, 10)
	for _, s := range suites {
		sb.WriteString("\nFAIL ")
		sb.WriteString(s[1])
	}

	passed := exitCode == 0 && m != nil && !strings.Contains(sb.String(), "BELOW THRESHOLD") && len(suites) == 0
	if !passed && sb.Len() == 0 {
		sb.WriteString(fmt.Sprintf("exit %d", exitCode))
	}
	return GateVerdict{Passed: passed, Summary: sb.String()}
}

func tailLines(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}
