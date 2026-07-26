// gap-to-prd converts a gap report JSON into a mini-PRD markdown file.
//
// Usage:
//
//	go run tools/gap-to-prd --input gap-report.json --output gaps/gap-prd.md
//
// Exit codes:
//
//	0 - converted successfully
//	1 - input file not found or invalid
//	2 - configuration error
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
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
	Gaps    []Gap  `json:"gaps"`
	Summary string `json:"summary"`
}

// Gap represents a single compliance gap.
type Gap struct {
	ID             string   `json:"id"`
	Category       string   `json:"category"`
	Severity       string   `json:"severity"`
	Description    string   `json:"description"`
	SourceEvidence string   `json:"source_evidence"`
	GoEvidence     string   `json:"go_evidence"`
	PRDRef         string   `json:"prd_ref,omitempty"`
	TargetFiles    []string `json:"target_files,omitempty"`
	SuggestedBead  string   `json:"suggested_bead,omitempty"`
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

	// Partition gaps into those with target files (become FRs) and targetless (triage).
	var targetGaps, targetlessGaps []Gap
	for _, g := range report.Gaps {
		if len(g.TargetFiles) > 0 {
			targetGaps = append(targetGaps, g)
		} else {
			targetlessGaps = append(targetlessGaps, g)
		}
	}

	// Group target gaps by category deterministically.
	grouped := groupGapsByCategory(targetGaps)

	// Emit FRs with deterministic numbering and remember the number for each gap.
	frNum := 1
	frByGapID := make(map[string]int)
	for _, category := range sortedKeys(grouped) {
		gaps := grouped[category]
		sort.Slice(gaps, func(i, j int) bool { return gaps[i].ID < gaps[j].ID })
		for _, gap := range gaps {
			frByGapID[gap.ID] = frNum
			emitFR(&b, frNum, category, gap)
			frNum++
		}
	}

	// Generate US if there are route or AC gaps that have target files.
	routeOrACGaps := filterGapsByCategory(targetGaps, "route", "ac")
	if len(routeOrACGaps) > 0 {
		emitUS(&b, routeOrACGaps, frByGapID)
	}

	// Triage section for gaps without target files.
	if len(targetlessGaps) > 0 {
		emitTriage(&b, targetlessGaps)
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

func emitFR(b *strings.Builder, frNum int, category string, gap Gap) {
	b.WriteString(fmt.Sprintf("### FR-%03d: Fix %s drift in %s\n\n", frNum, gap.Category, primaryTargetFile(gap.TargetFiles)))
	b.WriteString("**Type:** fr\n\n")
	b.WriteString(fmt.Sprintf("**Priority:** %s\n\n", priorityForSeverity(gap.Severity)))
	b.WriteString(fmt.Sprintf("**Description:**\n%s\n\n", gap.Description))

	if gap.PRDRef != "" {
		b.WriteString(fmt.Sprintf("**PRD Reference:** %s\n\n", gap.PRDRef))
	}

	b.WriteString(fmt.Sprintf("**Target Files:** %s\n\n", inlineTargetFiles(gap.TargetFiles)))
	b.WriteString("**Test Command:** `go test ./...`\n\n")
	b.WriteString("**Build Command:** `go build ./...`\n\n")

	b.WriteString("**Acceptance Criteria:**\n")
	// Make the AC concrete: agents interpret "Verify X is
	// intentional" as "do nothing", which closes the bead
	// without actually changing code. Force a comparison +
	// change instruction.
	b.WriteString(fmt.Sprintf("- Source evidence (Java): %s\n", gap.SourceEvidence))
	b.WriteString(fmt.Sprintf("- Go evidence (current): %s\n", gap.GoEvidence))
	if gap.Severity == "critical" {
		b.WriteString(fmt.Sprintf("- REQUIRED: change the Go file so the value matches the source. The previous SuggestedBead was: '%s' — that was not sufficient; agents closed these as 'verified intentional' without making the change.\n", gap.SuggestedBead))
	} else {
		b.WriteString(fmt.Sprintf("- Either change the Go file to match the source, OR update the PRD's §4.5.2 Behaviour Inventory to document the intentional divergence. The previous SuggestedBead was: '%s' — on its own that's not enough.\n", gap.SuggestedBead))
	}
	b.WriteString("- `go build ./...` exits 0\n")
	b.WriteString("- `go test ./...` exits 0\n")
	b.WriteString("- Re-run `tools/verify-source-vs-go --source-language auto --source-ref development --go-source .` and confirm zero gaps of category '" + gap.Category + "'\n")
	b.WriteString("\n")
}

func emitUS(b *strings.Builder, gaps []Gap, frByGapID map[string]int) {
	b.WriteString("## US-001: Integration — Fix Missing Routes and ACs\n\n")
	b.WriteString("**As a** system\n")
	b.WriteString("**I want** all PRD routes wired and acceptance criteria tested\n")
	b.WriteString("**So that** the system is fully compliant\n\n")
	b.WriteString("**Aggregates:**\n")
	for _, gap := range gaps {
		b.WriteString(fmt.Sprintf("- FR-%03d\n", frByGapID[gap.ID]))
	}
	b.WriteString("\n**Acceptance Criteria:**\n")
	for _, gap := range gaps {
		b.WriteString(fmt.Sprintf("- %s\n", gap.SuggestedBead))
	}
	b.WriteString("\n")
}

func emitTriage(b *strings.Builder, gaps []Gap) {
	b.WriteString("## NEEDS HUMAN TRIAGE: Gaps Without Target Files\n\n")
	for _, gap := range gaps {
		b.WriteString(fmt.Sprintf("- **%s** (%s): %s\n", gap.ID, gap.Category, gap.Description))
	}
	b.WriteString("\n")
}

func groupGapsByCategory(gaps []Gap) map[string][]Gap {
	grouped := make(map[string][]Gap)
	for _, g := range gaps {
		grouped[g.Category] = append(grouped[g.Category], g)
	}
	return grouped
}

func filterGapsByCategory(gaps []Gap, categories ...string) []Gap {
	var result []Gap
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

func sortedKeys(m map[string][]Gap) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func priorityForSeverity(severity string) string {
	if severity == "critical" {
		return "P0"
	}
	return "P1"
}

func inlineTargetFiles(targets []string) string {
	seen := make(map[string]bool)
	var unique []string
	for _, t := range targets {
		if !seen[t] {
			seen[t] = true
			unique = append(unique, t)
		}
	}
	parts := make([]string, len(unique))
	for i, f := range unique {
		parts[i] = fmt.Sprintf("`%s`", f)
	}
	return strings.Join(parts, ", ")
}

// primaryTargetFile returns the basename of the first target file,
// or "(no target file)" when the gap has no TargetFiles. Used in
// the FR title so the agent can find the file to edit at a glance.
func primaryTargetFile(targets []string) string {
	if len(targets) == 0 {
		return "(no target file)"
	}
	base := targets[0]
	if i := strings.LastIndex(base, "/"); i >= 0 {
		base = base[i+1:]
	}
	return base
}
