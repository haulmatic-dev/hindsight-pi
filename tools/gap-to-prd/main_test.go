package main

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "update golden file")

func loadFixture(t *testing.T) GapReport {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "gap-report.json"))
	if err != nil {
		t.Fatalf("reading fixture: %v", err)
	}
	var report GapReport
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatalf("parsing fixture: %v", err)
	}
	return report
}

func stripGeneratedLine(s string) string {
	var lines []string
	for _, line := range strings.Split(s, "\n") {
		if strings.HasPrefix(line, "**Generated:**") {
			continue
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

func TestGenerateMiniPRD_CriticalBehavior(t *testing.T) {
	report := GapReport{
		PRDPath: "docs/PRDs/sample.md",
		Cycles:  1,
		Summary: "test",
		Gaps: []Gap{
			{
				ID:             "G1",
				Category:       "behavior",
				Severity:       "critical",
				Description:    "Drift",
				SourceEvidence: "src",
				GoEvidence:     "go",
				PRDRef:         "PRD §1",
				TargetFiles:    []string{"internal/core/foo.go"},
				SuggestedBead:  "FIX",
			},
		},
	}
	prd := generateMiniPRD(report)
	required := []string{
		"**Type:** fr",
		"**Priority:** P0",
		"**Target Files:** `internal/core/foo.go`",
		"**Test Command:** `go test ./...`",
		"**Build Command:** `go build ./...`",
	}
	for _, line := range required {
		if !strings.Contains(prd, line) {
			t.Errorf("missing required line: %q\noutput:\n%s", line, prd)
		}
	}
}

func TestGenerateMiniPRD_WarningPriority(t *testing.T) {
	report := GapReport{
		Gaps: []Gap{
			{
				ID:          "W1",
				Category:    "behavior",
				Severity:    "warning",
				TargetFiles: []string{"foo.go"},
			},
		},
	}
	prd := generateMiniPRD(report)
	if !strings.Contains(prd, "**Priority:** P1") {
		t.Errorf("expected P1 for warning severity\noutput:\n%s", prd)
	}
}

func TestGenerateMiniPRD_InlineTargetFilesRegex(t *testing.T) {
	report := GapReport{
		Gaps: []Gap{
			{
				ID:          "G2",
				Category:    "behavior",
				Severity:    "warning",
				TargetFiles: []string{"a.go", "b.go"},
			},
		},
	}
	prd := generateMiniPRD(report)
	re := regexp.MustCompile(`(?i)^\*{0,2}target files:?\*{0,2}\s+(.+)`)
	found := false
	for _, line := range strings.Split(prd, "\n") {
		if re.MatchString(line) {
			found = true
			match := re.FindStringSubmatch(line)[1]
			if match != "`a.go`, `b.go`" {
				t.Errorf("unexpected target files content: %q", match)
			}
			break
		}
	}
	if !found {
		t.Errorf("target files line did not match parser regex\noutput:\n%s", prd)
	}
}

func TestGenerateMiniPRD_DeterministicGolden(t *testing.T) {
	report := loadFixture(t)

	first := generateMiniPRD(report)
	second := generateMiniPRD(report)
	if stripGeneratedLine(first) != stripGeneratedLine(second) {
		t.Error("two runs over the same fixture produced different output")
	}

	goldenPath := filepath.Join("testdata", "golden-prd.md")
	actual := stripGeneratedLine(first)

	if *update {
		if err := os.WriteFile(goldenPath, []byte(actual), 0644); err != nil {
			t.Fatalf("writing golden: %v", err)
		}
	}

	golden, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("reading golden: %v (run with -update to create it)", err)
	}
	if string(golden) != actual {
		t.Errorf("output does not match golden file; run with -update to refresh\ngolden:\n%s\n\nactual:\n%s", string(golden), actual)
	}
}

func TestGenerateMiniPRD_RouteAggregatesReference(t *testing.T) {
	report := GapReport{
		Gaps: []Gap{
			{
				ID:          "B1",
				Category:    "behavior",
				Severity:    "warning",
				TargetFiles: []string{"foo.go"},
			},
			{
				ID:          "R1",
				Category:    "route",
				Severity:    "warning",
				TargetFiles: []string{"server.go"},
			},
		},
	}
	prd := generateMiniPRD(report)
	if strings.Contains(prd, "FR-001") {
		// behavior gap should be FR-001, route should be FR-002
		if !strings.Contains(prd, "### FR-002: Fix route drift in server.go") {
			t.Errorf("expected route gap as FR-002\noutput:\n%s", prd)
		}
	}
	if !strings.Contains(prd, "**Aggregates:**\n- FR-002\n") {
		t.Errorf("expected US Aggregates to reference FR-002\noutput:\n%s", prd)
	}
}

func TestGenerateMiniPRD_TargetlessGapTriage(t *testing.T) {
	report := GapReport{
		Gaps: []Gap{
			{
				ID:            "T1",
				Category:      "ac",
				Severity:      "info",
				Description:   "Missing AC",
				SuggestedBead: "ADD AC",
			},
		},
	}
	prd := generateMiniPRD(report)
	if !strings.Contains(prd, "## NEEDS HUMAN TRIAGE: Gaps Without Target Files") {
		t.Errorf("expected triage heading\noutput:\n%s", prd)
	}
	if strings.Contains(prd, "### FR-") {
		t.Errorf("targetless gap should not produce an FR\noutput:\n%s", prd)
	}
}

func TestGenerateMiniPRD_CriticalRequiredLine(t *testing.T) {
	report := GapReport{
		Gaps: []Gap{
			{
				ID:            "C1",
				Category:      "behavior",
				Severity:      "critical",
				TargetFiles:   []string{"foo.go"},
				SuggestedBead: "FIX",
			},
		},
	}
	prd := generateMiniPRD(report)
	if !strings.Contains(prd, "REQUIRED: change the Go file") {
		t.Errorf("expected REQUIRED line for critical gap\noutput:\n%s", prd)
	}
}

func TestGenerateMiniPRD_ComplianceGate(t *testing.T) {
	report := loadFixture(t)
	prd := generateMiniPRD(report)
	if !strings.Contains(prd, "## COMPLIANCE GATE: Verify Gaps Fixed") {
		t.Errorf("expected COMPLIANCE GATE section\noutput:\n%s", prd)
	}
}
