// hydra-compliance-analyzer runs all compliance checks and outputs
// a structured gap report for downstream gap-to-PRD conversion.
//
// Usage:
//
//	go run tools/hydra-compliance-analyzer --prd docs/PRDs/feature.md --project . --output gap-report.json
//
// Exit codes:
//	0 - zero gaps (fully compliant)
//	1 - gaps found (see output JSON)
//	2 - configuration error
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// GapReport is the structured output of the compliance analyzer.
type GapReport struct {
	Passed     bool    `json:"passed"`
	PRDPath    string  `json:"prd_path"`
	ProjectDir string  `json:"project_dir"`
	Cycles     int     `json:"cycles"`
	Checks     []Check `json:"checks"`
	Gaps       []Gap   `json:"gaps"`
	Summary    string  `json:"summary"`
}

// Check represents a single compliance check result.
type Check struct {
	Name    string `json:"name"`
	Passed  bool   `json:"passed"`
	Details string `json:"details,omitempty"`
}

// Gap represents a single gap that needs to be fixed.
type Gap struct {
	ID            string   `json:"id"`
	Category      string   `json:"category"` // "route" | "export" | "ac" | "placeholder" | "phantom"
	Severity      string   `json:"severity"` // "critical" | "warning" | "info"
	Description   string   `json:"description"`
	PRDRef        string   `json:"prd_ref,omitempty"` // e.g., "US-002" or "FR-003"
	TargetFiles   []string `json:"target_files,omitempty"`
	SuggestedBead string   `json:"suggested_bead,omitempty"`
}

type Config struct {
	PRDPath    string
	ProjectDir string
	OutputPath string
	Cycles     int
}

func main() {
	cfg := parseFlags()

	report := &GapReport{
		PRDPath:    cfg.PRDPath,
		ProjectDir: cfg.ProjectDir,
		Cycles:     cfg.Cycles,
		Checks:     []Check{},
		Gaps:       []Gap{},
	}

	// Run all 5 checks
	runCheck(report, "verify-exports", runVerifyExports(cfg))
	runCheck(report, "verify-routes", runVerifyRoutes(cfg))
	runCheck(report, "verify-us-ac", runVerifyUSAC(cfg))
	runCheck(report, "detect-placeholders", runDetectPlaceholders(cfg))
	runCheck(report, "detect-phantom-files", runDetectPhantomFiles(cfg))

	// Determine pass/fail
	report.Passed = len(report.Gaps) == 0
	if report.Passed {
		report.Summary = fmt.Sprintf("All %d checks passed. Zero gaps found.", len(report.Checks))
	} else {
		report.Summary = fmt.Sprintf("%d gap(s) found across %d checks.", len(report.Gaps), len(report.Checks))
	}

	// Write output
	out, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error marshaling report: %v\n", err)
		os.Exit(2)
	}

	if cfg.OutputPath != "" {
		if err := os.WriteFile(cfg.OutputPath, out, 0644); err != nil {
			fmt.Fprintf(os.Stderr, "Error writing output: %v\n", err)
			os.Exit(2)
		}
		fmt.Printf("Report written to %s\n", cfg.OutputPath)
	} else {
		fmt.Println(string(out))
	}

	if !report.Passed {
		os.Exit(1)
	}
}

func parseFlags() Config {
	prd := flag.String("prd", "", "Path to PRD markdown file")
	project := flag.String("project", ".", "Project root directory")
	output := flag.String("output", "", "Output JSON file path (default: stdout)")
	cycles := flag.Int("cycles", 0, "Current compliance cycle number")
	flag.Parse()

	if *prd == "" {
		fmt.Fprintln(os.Stderr, "Usage: hydra-compliance-analyzer --prd <prd.md> [--project .] [--output gap-report.json]")
		os.Exit(2)
	}

	return Config{
		PRDPath:    *prd,
		ProjectDir: *project,
		OutputPath: *output,
		Cycles:     *cycles,
	}
}

func runCheck(report *GapReport, name string, gaps []Gap) {
	passed := len(gaps) == 0
	check := Check{
		Name:   name,
		Passed: passed,
	}
	if !passed {
		check.Details = fmt.Sprintf("%d gap(s) found", len(gaps))
	}
	report.Checks = append(report.Checks, check)
	report.Gaps = append(report.Gaps, gaps...)
}

// runTool builds the tool (if needed) and runs the compiled binary.
func runTool(toolName string, args []string, cfg Config) ([]byte, error) {
	toolsDir := filepath.Join(cfg.ProjectDir, "tools")

	// Build first to avoid "Go build:" prefix in output
	buildCmd := exec.Command("go", "build", filepath.Join(toolsDir, toolName))
	buildCmd.Dir = cfg.ProjectDir
	buildCmd.CombinedOutput()

	// Run compiled binary
	binPath := filepath.Join(toolsDir, toolName, toolName)
	cmd := exec.Command(binPath, args...)
	cmd.Dir = cfg.ProjectDir
	return cmd.CombinedOutput()
}

func runVerifyExports(cfg Config) []Gap {
	out, err := runTool("verify-exports", []string{
		"--prd", cfg.PRDPath,
		"--source", cfg.ProjectDir,
		"--lang", "go",
	}, cfg)
	if err != nil {
		// Non-zero exit is expected when violations exist
	}

	var result struct {
		Passed     bool `json:"passed"`
		Violations []struct {
			Export   string `json:"export"`
			Message  string `json:"message"`
			Severity string `json:"severity"`
		} `json:"violations"`
	}

	if jsonErr := json.Unmarshal(out, &result); jsonErr != nil {
		return []Gap{{
			ID:          "export-parse-error",
			Category:    "export",
			Severity:    "warning",
			Description: fmt.Sprintf("Failed to parse verify-exports output: %v", jsonErr),
		}}
	}

	if result.Passed {
		return nil
	}

	var gaps []Gap
	for _, v := range result.Violations {
		sev := "warning"
		if v.Severity == "error" {
			sev = "critical"
		}
		gaps = append(gaps, Gap{
			ID:            fmt.Sprintf("export-%s", slug(v.Export)),
			Category:      "export",
			Severity:      sev,
			Description:   fmt.Sprintf("Export %s: %s", v.Export, v.Message),
			SuggestedBead: fmt.Sprintf("FIX: Export %s signature mismatch", v.Export),
		})
	}
	return gaps
}

func runVerifyRoutes(cfg Config) []Gap {
	out, err := runTool("verify-routes", []string{
		"--prd", cfg.PRDPath,
		"--router", "internal/api/server.go",
	}, cfg)
	if err != nil {
		// Non-zero exit expected when routes missing
	}

	var result struct {
		Passed  bool `json:"passed"`
		Missing []struct {
			Route  string `json:"route"`
			Method string `json:"method"`
			Path   string `json:"path"`
			Issue  string `json:"issue"`
		} `json:"missing"`
	}

	if jsonErr := json.Unmarshal(out, &result); jsonErr != nil {
		return []Gap{{
			ID:          "route-parse-error",
			Category:    "route",
			Severity:    "warning",
			Description: fmt.Sprintf("Failed to parse verify-routes output: %v", jsonErr),
		}}
	}

	if result.Passed {
		return nil
	}

	var gaps []Gap
	for _, m := range result.Missing {
		gaps = append(gaps, Gap{
			ID:            fmt.Sprintf("route-%s-%s", slug(m.Method), slug(m.Path)),
			Category:      "route",
			Severity:      "critical",
			Description:   fmt.Sprintf("Route %s not wired: %s", m.Route, m.Issue),
			TargetFiles:   []string{"internal/api/server.go"},
			SuggestedBead: fmt.Sprintf("WIRE: %s route handler", m.Route),
		})
	}
	return gaps
}

func runVerifyUSAC(cfg Config) []Gap {
	out, err := runTool("verify-us-ac", []string{
		"--prd", cfg.PRDPath,
	}, cfg)
	if err != nil {
		// Non-zero exit expected when ACs missing
	}

	var result struct {
		Passed      bool `json:"passed"`
		Unsatisfied []struct {
			USID    string `json:"us_id"`
			USTitle string `json:"us_title"`
			Text    string `json:"text"`
		} `json:"unsatisfied"`
	}

	if jsonErr := json.Unmarshal(out, &result); jsonErr != nil {
		return []Gap{{
			ID:          "ac-parse-error",
			Category:    "ac",
			Severity:    "warning",
			Description: fmt.Sprintf("Failed to parse verify-us-ac output: %v", jsonErr),
		}}
	}

	if result.Passed {
		return nil
	}

	var gaps []Gap
	for _, u := range result.Unsatisfied {
		gaps = append(gaps, Gap{
			ID:            fmt.Sprintf("ac-%s-%s", slug(u.USID), slug(u.Text[:30])),
			Category:      "ac",
			Severity:      "warning",
			Description:   fmt.Sprintf("%s (%s): %s", u.USID, u.USTitle, u.Text),
			PRDRef:        u.USID,
			SuggestedBead: fmt.Sprintf("TEST: %s acceptance criterion", u.USID),
		})
	}
	return gaps
}

func runDetectPlaceholders(cfg Config) []Gap {
	out, err := runTool("detect-placeholders", []string{
		"--source", cfg.ProjectDir,
	}, cfg)
	if err != nil {
		// Non-zero exit expected when placeholders found
	}

	var result struct {
		Passed     bool `json:"passed"`
		Violations []struct {
			Path    string `json:"path"`
			Message string `json:"message"`
		} `json:"violations"`
	}

	if jsonErr := json.Unmarshal(out, &result); jsonErr != nil {
		return []Gap{{
			ID:          "placeholder-parse-error",
			Category:    "placeholder",
			Severity:    "info",
			Description: fmt.Sprintf("Failed to parse detect-placeholders output: %v", jsonErr),
		}}
	}

	if result.Passed {
		return nil
	}

	var gaps []Gap
	for _, v := range result.Violations {
		gaps = append(gaps, Gap{
			ID:            fmt.Sprintf("placeholder-%s", slug(v.Path)),
			Category:      "placeholder",
			Severity:      "warning",
			Description:   fmt.Sprintf("Placeholder in %s: %s", v.Path, v.Message),
			TargetFiles:   []string{v.Path},
			SuggestedBead: fmt.Sprintf("FIX: Remove placeholder in %s", v.Path),
		})
	}
	return gaps
}

func runDetectPhantomFiles(cfg Config) []Gap {
	out, err := runTool("detect-phantom-files", []string{
		"--prd", cfg.PRDPath,
		"--source", cfg.ProjectDir,
	}, cfg)
	if err != nil {
		// Non-zero exit expected when phantom files found
	}

	var result struct {
		Passed       bool `json:"passed"`
		PhantomFiles []struct {
			Path  string `json:"path"`
			Issue string `json:"issue"`
		} `json:"phantom_files"`
	}

	if jsonErr := json.Unmarshal(out, &result); jsonErr != nil {
		return []Gap{{
			ID:          "phantom-parse-error",
			Category:    "phantom",
			Severity:    "info",
			Description: fmt.Sprintf("Failed to parse detect-phantom-files output: %v", jsonErr),
		}}
	}

	if result.Passed {
		return nil
	}

	var gaps []Gap
	for _, p := range result.PhantomFiles {
		gaps = append(gaps, Gap{
			ID:            fmt.Sprintf("phantom-%s", slug(p.Path)),
			Category:      "phantom",
			Severity:      "info",
			Description:   fmt.Sprintf("Phantom file %s: %s", p.Path, p.Issue),
			TargetFiles:   []string{p.Path},
			SuggestedBead: fmt.Sprintf("CLEANUP: Document or remove %s", p.Path),
		})
	}
	return gaps
}

// Helpers

func slug(s string) string {
	s = strings.ToLower(s)
	s = strings.ReplaceAll(s, " ", "-")
	s = strings.ReplaceAll(s, "/", "-")
	s = strings.ReplaceAll(s, "{", "")
	s = strings.ReplaceAll(s, "}", "")
	s = strings.ReplaceAll(s, ":", "")
	for strings.Contains(s, "--") {
		s = strings.ReplaceAll(s, "--", "-")
	}
	return strings.Trim(s, "-")
}
