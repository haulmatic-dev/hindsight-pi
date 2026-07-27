package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

var (
	analyzerBin     string
	securityScanBin string
)

// TestMain builds the analyzer and security-scan binaries once for all
// integration tests, then delegates to the test runner. This avoids
// rebuilding per test while still exercising the real compiled binaries (no
// mocks of internal dependencies).
func TestMain(m *testing.M) {
	tmpDir, err := os.MkdirTemp("", "hydra-compliance-analyzer-test")
	if err != nil {
		panic(fmt.Sprintf("creating temp dir: %v", err))
	}
	defer os.RemoveAll(tmpDir)

	analyzerBin = filepath.Join(tmpDir, "hydra-compliance-analyzer")
	securityScanBin = filepath.Join(tmpDir, "security-scan")

	cwd, err := os.Getwd()
	if err != nil {
		panic(fmt.Sprintf("getting cwd: %v", err))
	}

	// Build the analyzer binary from this module.
	buildAnalyzer := exec.Command("go", "build", "-o", analyzerBin, ".")
	buildAnalyzer.Dir = cwd
	if out, err := buildAnalyzer.CombinedOutput(); err != nil {
		panic(fmt.Sprintf("building analyzer: %v\n%s", err, string(out)))
	}

	// Build the security-scan binary. It is its OWN module (tools/security-scan
	// has a go.mod), so it cannot be built by package path from the repo root —
	// doing so fails with "main module does not contain package". Build it from
	// inside its module directory instead, with an absolute output path.
	securityScanSrc := filepath.Join(cwd, "..", "security-scan")
	absSecurityScanBin, err := filepath.Abs(securityScanBin)
	if err != nil {
		panic(fmt.Sprintf("resolving security-scan output path: %v", err))
	}
	buildSecurityScan := exec.Command("go", "build", "-o", absSecurityScanBin, ".")
	buildSecurityScan.Dir = securityScanSrc
	if out, err := buildSecurityScan.CombinedOutput(); err != nil {
		panic(fmt.Sprintf("building security-scan in %s: %v\n%s", securityScanSrc, err, string(out)))
	}

	os.Exit(m.Run())
}

// copyFile copies src to dst, preserving execute permission.
func copyFile(t *testing.T, src, dst string) {
	t.Helper()
	in, err := os.Open(src)
	if err != nil {
		t.Fatalf("opening %s: %v", src, err)
	}
	defer in.Close()

	info, err := in.Stat()
	if err != nil {
		t.Fatalf("stat %s: %v", src, err)
	}

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode().Perm())
	if err != nil {
		t.Fatalf("creating %s: %v", dst, err)
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		t.Fatalf("copying %s to %s: %v", src, dst, err)
	}
}

// writePRD creates a minimal PRD markdown file required by the analyzer flags.
func writePRD(t *testing.T, dir string) string {
	t.Helper()
	p := filepath.Join(dir, "prd.md")
	if err := os.WriteFile(p, []byte("# PRD\n\nTest fixture.\n"), 0o644); err != nil {
		t.Fatalf("writing prd: %v", err)
	}
	return p
}

// runAnalyzer executes the compiled analyzer binary and returns its exit code
// and stderr output. The stdout is captured via --output when requested.
func runAnalyzer(t *testing.T, projectDir, prdPath, outputPath string, env []string) (int, string) {
	t.Helper()
	args := []string{"--prd", prdPath, "--project", projectDir}
	if outputPath != "" {
		args = append(args, "--output", outputPath)
	}

	cmd := exec.Command(analyzerBin, args...)
	if env != nil {
		cmd.Env = env
	}

	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return exitErr.ExitCode(), stderr.String()
		}
		return -1, stderr.String()
	}
	return 0, stderr.String()
}

// readReport parses the analyzer JSON output file.
func readReport(t *testing.T, path string) GapReport {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading output %s: %v", path, err)
	}
	var report GapReport
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatalf("parsing output %s: %v\n%s", path, err, string(data))
	}
	return report
}

// gitleaksStub emits a single canned gitleaks finding to the path specified by
// --report-path and exits with the code gitleaks uses for "leaks found".
const gitleaksStub = `#!/bin/bash
report_path=""
for ((i=1; i<=$#; i++)); do
  if [[ "${!i}" == "--report-path" ]]; then
    j=$((i+1))
    report_path="${!j}"
  fi
done
if [[ -z "$report_path" ]]; then
  echo "Error: --report-path required" >&2
  exit 2
fi
mkdir -p "$(dirname "$report_path")"
cat > "$report_path" <<'EOF'
[
  {
    "RuleID": "aws-access-key",
    "File": "cfg/prod.env",
    "StartLine": 3,
    "Secret": "AKIAABCDEFGHIJKLMNOP"
  }
]
EOF
exit 1
`

// TestSecurityScanIntegration_FindingProducesSecurityGap verifies US-001:
// with a stub gitleaks on PATH that reports one secret, the analyzer exits
// non-zero, records a security-scan check as failed, and emits a security
// gap whose description redacts the secret value.
func TestSecurityScanIntegration_FindingProducesSecurityGap(t *testing.T) {
	fixtureDir := t.TempDir()
	prdPath := writePRD(t, fixtureDir)

	// Make security-scan resolvable via HYDRA_TOOLS_DIR (no project-vendored copy).
	toolsDir := t.TempDir()
	securityScanDir := filepath.Join(toolsDir, "security-scan")
	if err := os.MkdirAll(securityScanDir, 0o755); err != nil {
		t.Fatalf("creating tools dir: %v", err)
	}
	copyFile(t, securityScanBin, filepath.Join(securityScanDir, "security-scan"))

	// Put a stub gitleaks on PATH.
	stubDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(stubDir, "gitleaks"), []byte(gitleaksStub), 0o755); err != nil {
		t.Fatalf("writing gitleaks stub: %v", err)
	}

	env := append(os.Environ(),
		"HYDRA_TOOLS_DIR="+toolsDir,
		"PATH="+stubDir+":"+os.Getenv("PATH"),
	)

	outPath := filepath.Join(t.TempDir(), "out.json")
	exitCode, stderr := runAnalyzer(t, fixtureDir, prdPath, outPath, env)
	if exitCode != 1 {
		t.Fatalf("expected analyzer exit code 1, got %d; stderr:\n%s", exitCode, stderr)
	}

	report := readReport(t, outPath)

	var securityCheck *Check
	for i := range report.Checks {
		if report.Checks[i].Name == "security-scan" {
			securityCheck = &report.Checks[i]
			break
		}
	}
	if securityCheck == nil {
		t.Fatalf("expected a security-scan check in %+v", report.Checks)
	}
	if securityCheck.Passed {
		t.Fatalf("expected security-scan check to be failed, got passed")
	}

	foundSecurityGap := false
	for _, g := range report.Gaps {
		if g.Category == "security" {
			foundSecurityGap = true
			if strings.Contains(g.Description, "AKIAABCDEFGHIJKLMNOP") {
				t.Errorf("security gap description leaks secret: %q", g.Description)
			}
		}
	}
	if !foundSecurityGap {
		t.Fatalf("expected a security gap in %+v", report.Gaps)
	}
}

// TestSecurityScanIntegration_MissingToolNoSecurityGaps verifies the fail-open
// behavior: when no security-scan binary is resolvable, the analyzer completes
// without error and produces no security-category gaps.
func TestSecurityScanIntegration_MissingToolNoSecurityGaps(t *testing.T) {
	fixtureDir := t.TempDir()
	prdPath := writePRD(t, fixtureDir)

	// Ensure HYDRA_TOOLS_DIR is not set so the tool is not resolvable.
	env := filterEnv(os.Environ(), "HYDRA_TOOLS_DIR")

	outPath := filepath.Join(t.TempDir(), "out.json")
	exitCode, stderr := runAnalyzer(t, fixtureDir, prdPath, outPath, env)
	if exitCode != 0 && exitCode != 1 {
		t.Fatalf("expected analyzer exit code 0 or 1, got %d; stderr:\n%s", exitCode, stderr)
	}
	if strings.Contains(stderr, "panic") {
		t.Fatalf("analyzer panicked; stderr:\n%s", stderr)
	}

	report := readReport(t, outPath)

	for _, g := range report.Gaps {
		if g.Category == "security" {
			t.Errorf("expected no security gaps, found %+v", g)
		}
	}

	var securityCheck *Check
	for i := range report.Checks {
		if report.Checks[i].Name == "security-scan" {
			securityCheck = &report.Checks[i]
			break
		}
	}
	if securityCheck == nil {
		t.Fatalf("expected a security-scan check in %+v", report.Checks)
	}
	if !securityCheck.Passed {
		t.Fatalf("expected security-scan check to pass when tool is missing, got failed")
	}
}

// filterEnv returns a copy of env with the named variables removed.
func filterEnv(env []string, names ...string) []string {
	remove := make(map[string]struct{}, len(names))
	for _, n := range names {
		remove[n] = struct{}{}
	}
	var out []string
	for _, e := range env {
		if idx := strings.IndexByte(e, '='); idx != -1 {
			if _, ok := remove[e[:idx]]; ok {
				continue
			}
		}
		out = append(out, e)
	}
	return out
}
