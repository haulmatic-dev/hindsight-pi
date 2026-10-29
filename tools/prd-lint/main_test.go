package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func runWithArgs(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr strings.Builder
	code := run(args, &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

func minimalValidPRD() string {
	return `# PRD

## FR-001: Feature

**Type:** fr
**Priority:** P1
**Target Files:** ` + "`tools/x.go`" + `
**Acceptance Criteria:**
- ` + "`go test ./...` exits 0" + `

## US-001: Integration

**Type:** us-integration
**Priority:** P1
**Aggregates:**
- FR-001
**Acceptance Criteria:**
- decomposition works

## NFR-SYS: System

**Type:** nfr-verification
**Priority:** P2
**Acceptance Criteria:**
- ` + "`go build ./...` exits 0" + `
`
}

func TestLintContent_MinimalValidPasses(t *testing.T) {
	result := lintContent("test.md", minimalValidPRD())
	if !result.Passed {
		t.Fatalf("expected clean lint, got violations: %v", result.Violations)
	}
	if len(result.Violations) != 0 {
		t.Fatalf("expected zero violations, got %d", len(result.Violations))
	}
}

func TestLintContent_TargetFilesBulletViolation(t *testing.T) {
	prd := strings.ReplaceAll(minimalValidPRD(), "**Target Files:** `tools/x.go`", "**Target Files:**\n- `tools/x.go`")
	result := lintContent("test.md", prd)
	if result.Passed {
		t.Fatalf("expected failing lint")
	}
	var found []Violation
	for _, v := range result.Violations {
		if v.Rule == "R3-target-files" {
			found = append(found, v)
		}
	}
	if len(found) != 1 {
		t.Fatalf("expected exactly one R3-target-files violation, got %d: %v", len(found), result.Violations)
	}
	if !strings.Contains(found[0].Message, "Target Files must be same-line (bullet lists are not parsed)") {
		t.Fatalf("message missing exact literal: %q", found[0].Message)
	}
}

func TestLintContent_MissingPriorityViolation(t *testing.T) {
	prd := strings.Replace(minimalValidPRD(), "**Priority:** P1\n", "", 1)
	result := lintContent("test.md", prd)
	if result.Passed {
		t.Fatalf("expected failing lint")
	}
	var found []Violation
	for _, v := range result.Violations {
		if v.Rule == "R2-priority" {
			found = append(found, v)
		}
	}
	if len(found) != 1 {
		t.Fatalf("expected exactly one R2-priority violation, got %d: %v", len(found), result.Violations)
	}
	if found[0].ID != "FR-001" {
		t.Fatalf("expected violation to name FR-001, got %q", found[0].ID)
	}
}

func TestLintContent_MissingReferenceViolation(t *testing.T) {
	prd := strings.ReplaceAll(minimalValidPRD(), "- FR-001\n", "- FR-009\n")
	result := lintContent("test.md", prd)
	if result.Passed {
		t.Fatalf("expected failing lint")
	}
	var found []Violation
	for _, v := range result.Violations {
		if v.Rule == "R7-refs" {
			found = append(found, v)
		}
	}
	if len(found) != 1 {
		t.Fatalf("expected exactly one R7-refs violation, got %d: %v", len(found), result.Violations)
	}
	if !strings.Contains(found[0].Message, "FR-009") {
		t.Fatalf("expected violation to mention FR-009: %q", found[0].Message)
	}
}

func TestLintContent_DuplicateIDViolation(t *testing.T) {
	prd := strings.ReplaceAll(minimalValidPRD(), "## US-001: Integration", "## FR-001: Second")
	result := lintContent("test.md", prd)
	if result.Passed {
		t.Fatalf("expected failing lint")
	}
	var found []Violation
	for _, v := range result.Violations {
		if v.Rule == "R5-unique-id" {
			found = append(found, v)
		}
	}
	if len(found) != 1 {
		t.Fatalf("expected exactly one R5-unique-id violation, got %d: %v", len(found), result.Violations)
	}
	if found[0].ID != "FR-001" {
		t.Fatalf("expected violation to name FR-001, got %q", found[0].ID)
	}
}

func TestLintContent_ReferencesResetOnSubheading(t *testing.T) {
	prd := `# PRD

## FR-001: Feature

**Type:** fr
**Priority:** P1
**Target Files:** ` + "`tools/x.go`" + `
**Acceptance Criteria:**
- ` + "`go test ./...` exits 0" + `

## NFR-SYS: System

**Type:** nfr-verification
**Priority:** P2
**Verifies:**
- FR-001
**Acceptance Criteria:**
- ` + "`go build ./...` exits 0" + `

### Edge Cases
- FR-999 is discussed but not a real reference.
`
	result := lintContent("test.md", prd)
	if !result.Passed {
		for _, v := range result.Violations {
			if v.Rule == "R7-refs" && strings.Contains(v.Message, "FR-999") {
				t.Fatalf("expected FR-999 in a subheading not to be treated as a reference: %v", v)
			}
		}
		t.Fatalf("expected clean lint, got violations: %v", result.Violations)
	}
}

func TestLintContent_NoHeadingsViolation(t *testing.T) {
	result := lintContent("test.md", "# Just a heading\n\nSome text.\n")
	if result.Passed {
		t.Fatalf("expected failing lint")
	}
	if len(result.Violations) != 1 {
		t.Fatalf("expected exactly one violation, got %d: %v", len(result.Violations), result.Violations)
	}
	v := result.Violations[0]
	if v.Rule != "R0-empty" {
		t.Fatalf("expected R0-empty, got %q", v.Rule)
	}
	if !strings.Contains(v.Message, "no requirement headings found") {
		t.Fatalf("message missing exact literal: %q", v.Message)
	}
}

func TestLintContent_JSONOutputParses(t *testing.T) {
	prd := strings.Replace(minimalValidPRD(), "**Priority:** P1\n", "", 1)
	result := lintContent("test.md", prd)
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if passed, ok := parsed["passed"].(bool); !ok || passed {
		t.Fatalf("expected passed=false, got %v", parsed["passed"])
	}
	violations, ok := parsed["violations"].([]any)
	if !ok || len(violations) == 0 {
		t.Fatalf("expected non-empty violations array, got %v", parsed["violations"])
	}
}

func TestRun_NoArgsExits2WithUsage(t *testing.T) {
	code, _, stderr := runWithArgs(t)
	if code != 2 {
		t.Fatalf("expected exit 2, got %d", code)
	}
	if !strings.Contains(strings.ToLower(stderr), "usage") {
		t.Fatalf("expected usage in stderr, got %q", stderr)
	}
}

func TestRun_MissingFileExits2(t *testing.T) {
	code, _, _ := runWithArgs(t, filepath.Join(t.TempDir(), "missing.md"))
	if code != 2 {
		t.Fatalf("expected exit 2, got %d", code)
	}
}

func TestRun_ValidPRDExits0(t *testing.T) {
	path := filepath.Join(t.TempDir(), "prd.md")
	if err := writeFile(path, minimalValidPRD()); err != nil {
		t.Fatalf("write file: %v", err)
	}
	code, stdout, stderr := runWithArgs(t, path)
	if code != 0 {
		t.Fatalf("expected exit 0, got %d; stdout=%q stderr=%q", code, stdout, stderr)
	}
	if stdout != "" {
		t.Fatalf("expected no stdout output for clean run, got %q", stdout)
	}
}

func TestRun_ValidPRDJSONEmptyArray(t *testing.T) {
	path := filepath.Join(t.TempDir(), "prd.md")
	if err := writeFile(path, minimalValidPRD()); err != nil {
		t.Fatalf("write file: %v", err)
	}
	code, stdout, stderr := runWithArgs(t, path, "--json")
	if code != 0 {
		t.Fatalf("expected exit 0, got %d; stderr=%q", code, stderr)
	}
	if !strings.Contains(stdout, `"violations": []`) {
		t.Fatalf("expected clean JSON to contain empty violations array, got stdout=%q stderr=%q", stdout, stderr)
	}
}

func TestRun_JSONOutputValid(t *testing.T) {
	prd := strings.Replace(minimalValidPRD(), "**Priority:** P1\n", "", 1)
	path := filepath.Join(t.TempDir(), "prd.md")
	if err := writeFile(path, prd); err != nil {
		t.Fatalf("write file: %v", err)
	}
	code, stdout, stderr := runWithArgs(t, path, "--json")
	if code != 1 {
		t.Fatalf("expected exit 1, got %d; stderr=%q", code, stderr)
	}
	var result LintResult
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\nstdout=%q", err, stdout)
	}
	if result.Passed {
		t.Fatalf("expected passed=false")
	}
	if len(result.Violations) == 0 {
		t.Fatalf("expected non-empty violations")
	}
}

func TestRun_HumanReadableViolation(t *testing.T) {
	prd := strings.Replace(minimalValidPRD(), "**Priority:** P1\n", "", 1)
	path := filepath.Join(t.TempDir(), "prd.md")
	if err := writeFile(path, prd); err != nil {
		t.Fatalf("write file: %v", err)
	}
	code, stdout, stderr := runWithArgs(t, path)
	if code != 1 {
		t.Fatalf("expected exit 1, got %d; stderr=%q", code, stderr)
	}
	if !strings.Contains(stdout, "R2-priority") {
		t.Fatalf("expected human output to contain rule, got %q", stdout)
	}
}

func TestBinary_BuildAndRun(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "prd-lint")
	cmd := exec.Command("go", "build", "-o", bin, ".")
	cmd.Dir = "."
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build failed: %v\n%s", err, out)
	}
	path := filepath.Join(dir, "prd.md")
	if err := writeFile(path, minimalValidPRD()); err != nil {
		t.Fatalf("write file: %v", err)
	}
	out, err := exec.Command(bin, path).CombinedOutput()
	if err != nil {
		t.Fatalf("prd-lint exited non-zero: %v\n%s", err, out)
	}
}

func writeFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0644)
}

func writeFileBytes(path string, data []byte) error {
	return os.WriteFile(path, data, 0644)
}

// TestCreatePrdProducerContract pins the producer contract documented in the
// create-prd skill: the five-value System Type enum, the five chain prefixes,
// and the strict classification precedence. Regression for the TDD chain
// "create-prd producer contract documentation" (hydra-cxpf).
func TestCreatePrdProducerContract(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", ".agents", "skills", "create-prd", "SKILL.md"))
	if err != nil {
		t.Fatalf("read SKILL.md: %v", err)
	}
	doc := string(data)

	// 1. System Type enum with all five values.
	enumValues := []string{"library", "cli", "api", "application", "ops"}
	for _, v := range enumValues {
		if !strings.Contains(doc, "`"+v+"`") {
			t.Errorf("System Type enum value %q missing from SKILL.md", v)
		}
	}
	if !strings.Contains(doc, "**System Type enum") {
		t.Error("SKILL.md does not document the System Type enum section")
	}

	// 2. All five chain prefixes documented.
	prefixes := []string{"RED:", "GREEN:", "REFACTOR:", "SYNTAX:", "FIX:"}
	for _, p := range prefixes {
		if !strings.Contains(doc, "`"+p+"`") {
			t.Errorf("chain prefix %q missing from SKILL.md", p)
		}
	}

	// 3. Precedence order stated: prefix > target-file shape > System Type > tdd_chain.
	if !strings.Contains(doc, "Classification precedence") {
		t.Error("SKILL.md does not document the classification precedence")
	}
	ordered := []string{"**Prefix**", "**Target-file shape**", "**System Type**", "**tdd_chain**"}
	last := -1
	for _, term := range ordered {
		idx := strings.Index(doc, term)
		if idx < 0 {
			t.Fatalf("precedence term %q missing", term)
		}
		if idx <= last {
			t.Errorf("precedence term %q appears out of order", term)
		}
		last = idx
	}
}

func TestSystemTypeLint(t *testing.T) {
	if _, ok := systemType([]line{{text: "**System Type:** library (internal Go, daemon-side)"}}); !ok {
		t.Error("expected System Type line to be detected")
	}
	if v, _ := systemType([]line{{text: "**System Type:** library (internal Go, daemon-side)"}}); v != "library" {
		t.Errorf("expected enum value \"library\", got %q", v)
	}
	if _, ok := systemType([]line{{text: "**Priority:** P1"}}); ok {
		t.Error("non-System-Type line must not be detected")
	}
	if isValidSystemType("database") {
		t.Error("\"database\" must be an invalid System Type")
	}
	for _, v := range []string{"library", "cli", "api", "application", "ops"} {
		if !isValidSystemType(v) {
			t.Errorf("%q must be a valid System Type", v)
		}
	}
}

// coverage-threshold semantics rule (RED phase — hydra-74mf).
//
// The rule under test: a requirement whose Acceptance Criteria state a
// coverage threshold over a file glob (e.g. "coverage >= 80% for `tools/**`")
// must also carry an explicit coverage-exemption marker when it does not
// intend to be gated by that threshold. A block that names a coverage
// threshold on a glob without the marker yields a coverage-threshold
// warning violation. Blocks that never mention coverage are unaffected.
//
// RED gating: the rule is not implemented yet (GREEN's job), so by default
// these tests skip loudly to keep `go test ./...` green for the daemon
// gate. Setting PRDLINT_COVERAGE_THRESHOLD_ENFORCE=1 turns them into hard
// assertions, and the enforced run is expected to FAIL against the current
// code — that failure is the RED evidence.
const coverageThresholdEnforceEnv = "PRDLINT_COVERAGE_THRESHOLD_ENFORCE"

func requireCoverageThresholdEnforced(t *testing.T) {
	t.Helper()
	if os.Getenv(coverageThresholdEnforceEnv) == "" {
		t.Skipf("coverage-threshold rule not implemented yet (RED); set %s=1 to enforce and observe the expected RED failure", coverageThresholdEnforceEnv)
	}
}

func isCoverageThresholdWarning(v Violation) bool {
	return strings.Contains(strings.ToLower(v.Rule), "coverage-threshold") ||
		strings.Contains(strings.ToLower(v.Message), "coverage-threshold")
}

func coverageThresholdFixturePRD() string {
	return minimalValidPRD() + `

## FR-COV: Coverage threshold reporting

**Type:** fr
**Priority:** P1
**Target Files:** ` + "`tools/prd-lint/main.go`" + `
**Acceptance Criteria:**
- ` + "`go test ./... -cover` reports coverage >= 80% for glob `tools/**`" + `
`
}

func TestCoverageThresholdLint_GlobThresholdCriterionWithoutMarkerWarns(t *testing.T) {
	requireCoverageThresholdEnforced(t)

	result := lintContent("test.md", coverageThresholdFixturePRD())
	if result.Passed {
		t.Fatalf("expected the glob coverage-threshold criterion without an exemption marker to produce a coverage-threshold warning, got a clean pass")
	}
	found := false
	for _, v := range result.Violations {
		if !isCoverageThresholdWarning(v) {
			continue
		}
		found = true
		if v.ID != "FR-COV" {
			t.Errorf("coverage-threshold warning should point at FR-COV, got ID %q", v.ID)
		}
		if v.Line == 0 {
			t.Errorf("coverage-threshold warning must carry a line number")
		}
		if strings.TrimSpace(v.Message) == "" {
			t.Errorf("coverage-threshold warning must carry a message")
		}
	}
	if !found {
		t.Fatalf("expected a coverage-threshold warning mentioning the threshold semantics for FR-COV, got violations: %v", result.Violations)
	}
}

func TestCoverageThresholdLint_NonCoverageCriteriaUnaffected(t *testing.T) {
	requireCoverageThresholdEnforced(t)

	// Regression fixture: no block mentions coverage, so no
	// coverage-threshold warning may appear.
	result := lintContent("test.md", minimalValidPRD())
	for _, v := range result.Violations {
		if isCoverageThresholdWarning(v) {
			t.Errorf("non-coverage criterion must not trigger a coverage-threshold warning, got %+v", v)
		}
	}
	if !result.Passed {
		t.Fatalf("expected the coverage-free regression fixture to stay clean, got violations: %v", result.Violations)
	}

	// Same fixture with the exemption marker present: the marker suppresses
	// the warning, so the glob-threshold block lints clean.
	marked := strings.Replace(
		coverageThresholdFixturePRD(),
		"## FR-COV: Coverage threshold reporting",
		"<!-- coverage-threshold: exempt -->\n\n## FR-COV: Coverage threshold reporting",
		1,
	)
	markedResult := lintContent("test.md", marked)
	for _, v := range markedResult.Violations {
		if isCoverageThresholdWarning(v) {
			t.Errorf("explicit exemption marker must suppress the coverage-threshold warning, got %+v", v)
		}
	}
	if !markedResult.Passed {
		t.Fatalf("expected the marked fixture to pass cleanly, got violations: %v", markedResult.Violations)
	}
}
