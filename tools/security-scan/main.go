// security-scan runs gitleaks and govulncheck against a project and reports
// security gaps as JSON.
//
// Usage:
//
//	go run tools/security-scan/main.go --project ./my-project
//
// Exit codes:
//
//	0 - no security gaps found
//	1 - security gaps detected
//	2 - configuration error
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// maxGapsPerScanner caps the number of gaps contributed by a single scanner to
// keep the analyzer report manageable. A final truncation gap is appended when
// the cap is reached.
const maxGapsPerScanner = 50

// scannerTimeout bounds each external scanner invocation. This prevents a
// hanging gitleaks/govulncheck process from blocking the compliance cycle
// indefinitely (the failure mode seen in the original discovery bead).
const scannerTimeout = 5 * time.Minute

// Gap describes a single security finding.
// It mirrors the Gap schema used by tools/hydra-compliance-analyzer so that the
// analyzer can ingest the JSON report directly.
type Gap struct {
	ID            string   `json:"id"`
	Category      string   `json:"category"`
	Severity      string   `json:"severity"`
	Description   string   `json:"description"`
	TargetFiles   []string `json:"target_files,omitempty"`
	SuggestedBead string   `json:"suggested_bead,omitempty"`
}

// ScannerInfo records whether a scanner was available and any error it reported.
type ScannerInfo struct {
	Name      string `json:"name"`
	Available bool   `json:"available"`
	Error     string `json:"error,omitempty"`
}

// Result is the top-level JSON output.
type Result struct {
	Passed   bool          `json:"passed"`
	Gaps     []Gap         `json:"gaps"`
	Scanners []ScannerInfo `json:"scanners"`
}

// gitleaksFinding mirrors the JSON format emitted by gitleaks.
type gitleaksFinding struct {
	RuleID    string `json:"RuleID"`
	File      string `json:"File"`
	StartLine int    `json:"StartLine"`
	Secret    string `json:"Secret"`
}

// govulncheckFinding captures the fields we need from a govulncheck finding line.
type govulncheckFinding struct {
	OSV   string `json:"osv"`
	Trace []struct {
		Module  string `json:"module"`
		Version string `json:"version"`
	} `json:"trace"`
}

var (
	project = flag.String("project", "", "Project directory to scan")
	output  = flag.String("output", "", "Output JSON file path (default: stdout)")
)

func main() {
	flag.Parse()

	if *project == "" {
		fmt.Fprintln(os.Stderr, "Error: --project is required")
		fmt.Fprintf(os.Stderr, "Usage: %s --project <dir> [--output <file>]\n", os.Args[0])
		os.Exit(2)
	}

	result := runScans(*project)

	var out io.Writer
	if *output == "" {
		out = os.Stdout
	} else {
		f, err := os.Create(*output)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error creating output file: %v\n", err)
			os.Exit(2)
		}
		defer f.Close()
		out = f
	}

	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	if err := enc.Encode(result); err != nil {
		fmt.Fprintf(os.Stderr, "Error encoding JSON: %v\n", err)
		os.Exit(2)
	}

	if !result.Passed {
		os.Exit(1)
	}
}

// scannerDef describes a single scanner.
type scannerDef struct {
	name          string
	run           func(string) ([]Gap, error)
	applicable    func(string) bool
	omitIfSkipped bool
}

// runScans executes all configured scanners and aggregates the results.
func runScans(project string) Result {
	result := Result{
		Passed:   true,
		Gaps:     []Gap{},
		Scanners: []ScannerInfo{},
	}

	scanners := []scannerDef{
		{name: "gitleaks", run: runGitleaks, applicable: func(_ string) bool { return true }, omitIfSkipped: false},
		{name: "govulncheck", run: runGovulncheck, applicable: hasGoMod, omitIfSkipped: true},
	}

	for _, s := range scanners {
		info := ScannerInfo{Name: s.name}
		if _, err := exec.LookPath(s.name); err != nil {
			info.Available = false
			result.Scanners = append(result.Scanners, info)
			fmt.Fprintf(os.Stderr, "Scanner not found on PATH: %s\n", s.name)
			continue
		}
		info.Available = true

		if !s.applicable(project) {
			if !s.omitIfSkipped {
				result.Scanners = append(result.Scanners, info)
			}
			fmt.Fprintf(os.Stderr, "Scanner %s skipped: project shape not applicable\n", s.name)
			continue
		}

		gaps, err := s.run(project)
		if err != nil {
			info.Error = err.Error()
			fmt.Fprintf(os.Stderr, "Scanner %s failed: %v\n", s.name, err)
		}
		gaps = capGaps(gaps, s.name)
		if len(gaps) > 0 {
			result.Passed = false
			result.Gaps = append(result.Gaps, gaps...)
		}
		result.Scanners = append(result.Scanners, info)
	}

	return result
}

// capGaps limits the number of gaps from a single scanner and appends a
// truncation gap when the limit is exceeded.
func capGaps(gaps []Gap, scannerName string) []Gap {
	if len(gaps) <= maxGapsPerScanner {
		return gaps
	}
	truncated := gaps[:maxGapsPerScanner]
	truncated = append(truncated, Gap{
		ID:          fmt.Sprintf("%s-truncated", scannerName),
		Category:    "security",
		Severity:    "warning",
		Description: fmt.Sprintf("Scanner %s reported %d findings; only the first %d are included", scannerName, len(gaps), maxGapsPerScanner),
	})
	return truncated
}

// runGitleaks invokes gitleaks and parses its JSON report.
func runGitleaks(project string) ([]Gap, error) {
	// Use a temporary report file for real gitleaks compatibility; stubs that
	// write to stdout are handled by falling back to stdout parsing.
	reportFile := filepath.Join(os.TempDir(), fmt.Sprintf("gitleaks-report-%d.json", os.Getpid()))
	defer os.Remove(reportFile)

	baseArgs := []string{
		"detect",
		"--source", project,
		"--no-banner",
		"--report-format", "json",
		"--report-path", reportFile,
	}

	var out []byte
	var err error
	var cmd *exec.Cmd
	for _, noGit := range []string{"--no-git", ""} {
		args := append([]string{}, baseArgs...)
		if noGit != "" {
			args = append(args, noGit)
		}
		ctx, cancel := context.WithTimeout(context.Background(), scannerTimeout)
		cmd = exec.CommandContext(ctx, "gitleaks", args...)
		out, err = cmd.CombinedOutput()
		cancel()
		if err == nil || (cmd.ProcessState != nil && cmd.ProcessState.ExitCode() == 1) {
			break
		}
		if noGit != "" && strings.Contains(string(out), "unknown flag") {
			fmt.Fprintf(os.Stderr, "gitleaks does not support --no-git, retrying without it\n")
			continue
		}
		break
	}

	var raw []byte
	if data, readErr := os.ReadFile(reportFile); readErr == nil && len(data) > 0 {
		raw = data
	} else {
		raw = out
	}

	// gitleaks exits 1 when it finds leaks; that is expected, not a command error.
	if err != nil && (cmd.ProcessState == nil || cmd.ProcessState.ExitCode() != 1) {
		return nil, fmt.Errorf("gitleaks exited unexpectedly: %w\n%s", err, string(raw))
	}

	return parseGitleaksOutput(project, string(raw))
}

// parseGitleaksOutput converts gitleaks JSON output into gaps.
func parseGitleaksOutput(project, data string) ([]Gap, error) {
	data = strings.TrimSpace(data)
	if data == "" {
		return nil, nil
	}

	// gitleaks may emit either a JSON array or newline-delimited objects.
	if strings.HasPrefix(data, "[") {
		var findings []gitleaksFinding
		if err := json.Unmarshal([]byte(data), &findings); err == nil {
			return gitleaksFindingsToGaps(project, findings), nil
		}
	}

	var findings []gitleaksFinding
	dec := json.NewDecoder(strings.NewReader(data))
	for {
		var f gitleaksFinding
		if err := dec.Decode(&f); err == io.EOF {
			break
		} else if err != nil {
			return nil, fmt.Errorf("parsing gitleaks output: %w", err)
		}
		if f.RuleID != "" {
			findings = append(findings, f)
		}
	}
	return gitleaksFindingsToGaps(project, findings), nil
}

// redactSecret truncates a secret to its first four characters plus an ellipsis
// so that the gap description is useful without exposing the full value.
func redactSecret(secret string) string {
	if len(secret) <= 4 {
		return "…"
	}
	return secret[:4] + "…"
}

func gitleaksFindingsToGaps(project string, findings []gitleaksFinding) []Gap {
	var gaps []Gap
	for _, f := range findings {
		// Build a description that contains the redacted match but never the
		// full secret value.
		desc := fmt.Sprintf("gitleaks rule %s detected a secret (%s) in %s", f.RuleID, redactSecret(f.Secret), f.File)
		if f.StartLine > 0 {
			desc = fmt.Sprintf("%s:%d", desc, f.StartLine)
		}
		id := fmt.Sprintf("gitleaks-%s-%s", f.RuleID, strings.ReplaceAll(f.File, "/", "-"))
		if f.StartLine > 0 {
			id = fmt.Sprintf("%s-%d", id, f.StartLine)
		}
		gaps = append(gaps, Gap{
			ID:            id,
			Category:      "security",
			Severity:      "critical",
			Description:   desc,
			TargetFiles:   []string{f.File},
			SuggestedBead: fmt.Sprintf("Remove secret from %s:%d and rotate the credential", f.File, f.StartLine),
		})
	}
	return gaps
}

// runGovulncheck invokes govulncheck and parses its JSON output.
// It only runs when the project contains a go.mod file.
func runGovulncheck(project string) ([]Gap, error) {
	ctx, cancel := context.WithTimeout(context.Background(), scannerTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "govulncheck", "-json", "./...")
	cmd.Dir = project
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("govulncheck failed: %w\n%s", err, string(out))
	}
	return parseGovulncheckOutput(project, string(out))
}

// parseGovulncheckOutput converts govulncheck JSON output into gaps.
// Only findings with a non-empty trace (reachable vulnerabilities) are reported,
// and gaps are de-duplicated by OSV ID.
func parseGovulncheckOutput(project, data string) ([]Gap, error) {
	data = strings.TrimSpace(data)
	if data == "" {
		return nil, nil
	}

	seen := make(map[string]bool)
	var gaps []Gap
	dec := json.NewDecoder(strings.NewReader(data))
	for {
		var wrapper struct {
			Finding *govulncheckFinding `json:"finding"`
		}
		if err := dec.Decode(&wrapper); err == io.EOF {
			break
		} else if err != nil {
			// govulncheck emits several line types; ignore unrecognised ones.
			continue
		}
		if wrapper.Finding == nil || wrapper.Finding.OSV == "" {
			continue
		}
		if len(wrapper.Finding.Trace) == 0 {
			continue
		}
		if seen[wrapper.Finding.OSV] {
			continue
		}
		seen[wrapper.Finding.OSV] = true

		module := wrapper.Finding.Trace[0].Module
		desc := fmt.Sprintf("govulncheck found vulnerability %s in module %s", wrapper.Finding.OSV, module)
		gaps = append(gaps, Gap{
			ID:            fmt.Sprintf("govulncheck-%s", wrapper.Finding.OSV),
			Category:      "security",
			Severity:      "warning",
			Description:   desc,
			TargetFiles:   []string{"go.mod"},
			SuggestedBead: fmt.Sprintf("Upgrade %s to a fixed version for %s", module, wrapper.Finding.OSV),
		})
	}
	return gaps, nil
}

func hasGoMod(project string) bool {
	_, err := os.Stat(filepath.Join(project, "go.mod"))
	return err == nil
}
