// gap-to-prd converts a gap report JSON into a mini-PRD markdown file.
//
// Usage:
//
//	go run tools/gap-to-prd --input gap-report.json --output gaps/gap-prd.md
//
// Exit codes:
//	0 - converted successfully
//	1 - input file not found or invalid
//	2 - configuration error
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"
)

// GapReport mirrors the output from hydra-compliance-analyzer.
type GapReport struct {
	Passed     bool   `json:"passed"`
	PRDPath    string `json:"prd_path"`
	ProjectDir string `json:"project_dir"`
	Cycles     int    `json:"cycles"`
	Checks     []struct {
		Name    string `json:"name"`
		Passed  bool   `json:"passed"`
		Details string `json:"details,omitempty"`
	} `json:"checks"`
	Gaps []struct {
		ID            string   `json:"id"`
		Category      string   `json:"category"`
		Severity      string   `json:"severity"`
		Description   string   `json:"description"`
		PRDRef        string   `json:"prd_ref,omitempty"`
		TargetFiles   []string `json:"target_files,omitempty"`
		SuggestedBead string   `json:"suggested_bead,omitempty"`
	} `json:"gaps"`
	Summary string `json:"summary"`
}

func main() {
	inputPath := flag.String("input", "", "Path to gap-report.json")
	outputPath := flag.String("output", "", "Path to output mini-PRD markdown file")
	flag.Parse()

	if *inputPath == "" {
		fmt.Fprintln(os.Stderr, "Usage: gap-to-prd --input gap-report.json --output gaps/gap-prd.md")
		os.Exit(2)
	}

	data, err := os.ReadFile(*inputPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading input: %v\n", err)
		os.Exit(1)
	}

	var report GapReport
	if err := json.Unmarshal(data, &report); err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing JSON: %v\n", err)
		os.Exit(1)
	}

	if report.Passed {
		fmt.Println("Gap report shows zero gaps. No mini-PRD needed.")
		os.Exit(0)
	}

	prd := generateMiniPRD(report)

	if *outputPath != "" {
		if err := os.WriteFile(*outputPath, []byte(prd), 0644); err != nil {
			fmt.Fprintf(os.Stderr, "Error writing output: %v\n", err)
			os.Exit(2)
		}
		fmt.Printf("Mini-PRD written to %s\n", *outputPath)
	} else {
		fmt.Println(prd)
	}
}

func generateMiniPRD(report GapReport) string {
	var b strings.Builder

	b.WriteString("# Gap PRD: Auto-Generated from Compliance Analysis\n\n")
	b.WriteString(fmt.Sprintf("**Generated:** %s\n\n", time.Now().Format(time.RFC3339)))
	b.WriteString(fmt.Sprintf("**Source PRD:** %s\n\n", report.PRDPath))
	b.WriteString(fmt.Sprintf("**Cycle:** %d\n\n", report.Cycles))
	b.WriteString(fmt.Sprintf("**Summary:** %s\n\n", report.Summary))

	// Group gaps by category
	grouped := groupGapsByCategory(report.Gaps)

	// Generate FRs
	frNum := 1
	for category, gaps := range grouped {
		for _, gap := range gaps {
			b.WriteString(fmt.Sprintf("### FR-%03d: %s\n\n", frNum, titleCase(gap.Category)))
			b.WriteString(fmt.Sprintf("**Type:** %s gap\n\n", category))
			b.WriteString(fmt.Sprintf("**Severity:** %s\n\n", gap.Severity))
			b.WriteString(fmt.Sprintf("**Description:**\n%s\n\n", gap.Description))

			if gap.PRDRef != "" {
				b.WriteString(fmt.Sprintf("**PRD Reference:** %s\n\n", gap.PRDRef))
			}

			if len(gap.TargetFiles) > 0 {
				b.WriteString("**Target Files:**\n")
				for _, f := range gap.TargetFiles {
					b.WriteString(fmt.Sprintf("- `%s`\n", f))
				}
				b.WriteString("\n")
			}

			b.WriteString("**Acceptance Criteria:**\n")
			b.WriteString(fmt.Sprintf("- %s\n", gap.SuggestedBead))
			b.WriteString("- `go build ./...` exits 0\n")
			b.WriteString("- `go test ./...` exits 0\n")
			b.WriteString("\n")

			frNum++
		}
	}

	// Generate US if there are route or AC gaps
	routeOrACGaps := filterGapsByCategory(report.Gaps, "route", "ac")
	if len(routeOrACGaps) > 0 {
		b.WriteString("## US-001: Integration — Fix Missing Routes and ACs\n\n")
		b.WriteString("**As a** system\n")
		b.WriteString("**I want** all PRD routes wired and acceptance criteria tested\n")
		b.WriteString("**So that** the system is fully compliant\n\n")
		b.WriteString("**Aggregates:**\n")
		for i := range routeOrACGaps {
			b.WriteString(fmt.Sprintf("- FR-%03d\n", i+1))
		}
		b.WriteString("\n**Acceptance Criteria:**\n")
		for _, gap := range routeOrACGaps {
			b.WriteString(fmt.Sprintf("- %s\n", gap.SuggestedBead))
		}
		b.WriteString("\n")
	}

	// Compliance gate
	b.WriteString("## COMPLIANCE GATE: Verify Gaps Fixed\n\n")
	b.WriteString("**After all above FRs/US close:**\n")
	b.WriteString("- Re-run `hydra-compliance-analyzer`\n")
	b.WriteString("- Verify zero gaps remain\n")
	b.WriteString("- If gaps found → create new Gap PRD (cycle ++)\n")
	b.WriteString("- If zero gaps → PRD approved\n\n")

	return b.String()
}

func groupGapsByCategory(gaps []struct {
	ID            string   `json:"id"`
	Category      string   `json:"category"`
	Severity      string   `json:"severity"`
	Description   string   `json:"description"`
	PRDRef        string   `json:"prd_ref,omitempty"`
	TargetFiles   []string `json:"target_files,omitempty"`
	SuggestedBead string   `json:"suggested_bead,omitempty"`
}) map[string][]struct {
	ID            string   `json:"id"`
	Category      string   `json:"category"`
	Severity      string   `json:"severity"`
	Description   string   `json:"description"`
	PRDRef        string   `json:"prd_ref,omitempty"`
	TargetFiles   []string `json:"target_files,omitempty"`
	SuggestedBead string   `json:"suggested_bead,omitempty"`
} {
	grouped := make(map[string][]struct {
		ID            string   `json:"id"`
		Category      string   `json:"category"`
		Severity      string   `json:"severity"`
		Description   string   `json:"description"`
		PRDRef        string   `json:"prd_ref,omitempty"`
		TargetFiles   []string `json:"target_files,omitempty"`
		SuggestedBead string   `json:"suggested_bead,omitempty"`
	})
	for _, g := range gaps {
		grouped[g.Category] = append(grouped[g.Category], g)
	}
	return grouped
}

func filterGapsByCategory(gaps []struct {
	ID            string   `json:"id"`
	Category      string   `json:"category"`
	Severity      string   `json:"severity"`
	Description   string   `json:"description"`
	PRDRef        string   `json:"prd_ref,omitempty"`
	TargetFiles   []string `json:"target_files,omitempty"`
	SuggestedBead string   `json:"suggested_bead,omitempty"`
}, categories ...string) []struct {
	ID            string   `json:"id"`
	Category      string   `json:"category"`
	Severity      string   `json:"severity"`
	Description   string   `json:"description"`
	PRDRef        string   `json:"prd_ref,omitempty"`
	TargetFiles   []string `json:"target_files,omitempty"`
	SuggestedBead string   `json:"suggested_bead,omitempty"`
} {
	var result []struct {
		ID            string   `json:"id"`
		Category      string   `json:"category"`
		Severity      string   `json:"severity"`
		Description   string   `json:"description"`
		PRDRef        string   `json:"prd_ref,omitempty"`
		TargetFiles   []string `json:"target_files,omitempty"`
		SuggestedBead string   `json:"suggested_bead,omitempty"`
	}
	for _, g := range gaps {
		for _, c := range categories {
			if g.Category == c {
				result = append(result, g)
				break
			}
		}
	}
	return result
}

func titleCase(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
