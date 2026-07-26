package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

var binaryPath string

func TestMain(m *testing.M) {
	tmpDir, err := os.MkdirTemp("", "security-scan-test")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(tmpDir)

	binaryPath = filepath.Join(tmpDir, "security-scan")
	build := exec.Command("go", "build", "-o", binaryPath, ".")
	if out, err := build.CombinedOutput(); err != nil {
		panic(string(out))
	}

	os.Exit(m.Run())
}

// runTool executes the compiled security-scan binary and returns stdout, stderr, and the exit code.
func runTool(t *testing.T, project string, env []string, extraArgs ...string) (stdout, stderr string, exitCode int) {
	t.Helper()
	args := []string{}
	if project != "" {
		args = append(args, "--project", project)
	}
	args = append(args, extraArgs...)

	cmd := exec.Command(binaryPath, args...)
	if env != nil {
		cmd.Env = env
	}

	var outBuf, errBuf strings.Builder
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf

	err := cmd.Run()
	exitCode = 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = -1
		}
	}
	return outBuf.String(), errBuf.String(), exitCode
}

// writeStub creates an executable shell script at dir/name.
func writeStub(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0755); err != nil {
		t.Fatalf("writing stub %s: %v", name, err)
	}
	return p
}

func assertJSON(t *testing.T, s string) Result {
	t.Helper()
	var r Result
	if err := json.Unmarshal([]byte(s), &r); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\nstdout:\n%s", err, s)
	}
	return r
}

func TestMissingProject_Exits2(t *testing.T) {
	stdout, stderr, code := runTool(t, "", nil)
	if code != 2 {
		t.Fatalf("expected exit code 2, got %d; stderr:\n%s", code, stderr)
	}
	if stdout != "" {
		t.Fatalf("expected empty stdout on config error, got:\n%s", stdout)
	}
	if !strings.Contains(stderr, "--project") {
		t.Fatalf("stderr did not mention --project; stderr:\n%s", stderr)
	}
}

func TestGitleaksFinding_Redacted(t *testing.T) {
	stubDir := t.TempDir()
	writeStub(t, stubDir, "gitleaks", `#!/bin/sh
echo '{"RuleID":"aws-access-key","File":"cfg/prod.env","StartLine":3,"Secret":"AKIAABCDEFGHIJKLMNOP"}'
exit 1
`)
	writeStub(t, stubDir, "govulncheck", "#!/bin/sh\nexit 0\n")

	projectDir := t.TempDir()
	env := []string{"PATH=" + stubDir}

	stdout, stderr, code := runTool(t, projectDir, env)
	if code != 1 {
		t.Fatalf("expected exit code 1, got %d; stderr:\n%s", code, stderr)
	}

	result := assertJSON(t, stdout)
	if result.Passed {
		t.Fatal("expected passed=false")
	}
	if len(result.Gaps) != 1 {
		t.Fatalf("expected exactly one gap, got %d: %+v", len(result.Gaps), result.Gaps)
	}

	gap := result.Gaps[0]
	if gap.Category != "security" {
		t.Errorf("category: expected security, got %q", gap.Category)
	}
	if gap.Severity != "critical" {
		t.Errorf("severity: expected critical, got %q", gap.Severity)
	}
	if len(gap.TargetFiles) != 1 || gap.TargetFiles[0] != "cfg/prod.env" {
		t.Errorf("target_files: expected [cfg/prod.env], got %v", gap.TargetFiles)
	}
	if strings.Contains(gap.Description, "AKIAABCDEFGHIJKLMNOP") {
		t.Errorf("description leaks secret: %q", gap.Description)
	}
}

func TestZeroFindings_PassedTrue(t *testing.T) {
	stubDir := t.TempDir()
	writeStub(t, stubDir, "gitleaks", "#!/bin/sh\nexit 0\n")
	writeStub(t, stubDir, "govulncheck", "#!/bin/sh\nexit 0\n")

	projectDir := t.TempDir()
	env := []string{"PATH=" + stubDir}

	stdout, stderr, code := runTool(t, projectDir, env)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d; stderr:\n%s", code, stderr)
	}

	result := assertJSON(t, stdout)
	if !result.Passed {
		t.Fatalf("expected passed=true, got %+v", result)
	}
	if len(result.Gaps) != 0 {
		t.Fatalf("expected no gaps, got %d: %+v", len(result.Gaps), result.Gaps)
	}
}

func TestEmptyPath_ScannersUnavailable(t *testing.T) {
	projectDir := t.TempDir()
	env := []string{"PATH="}

	stdout, stderr, code := runTool(t, projectDir, env)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d; stderr:\n%s", code, stderr)
	}

	result := assertJSON(t, stdout)
	if !result.Passed {
		t.Fatalf("expected passed=true when scanners are unavailable, got %+v", result)
	}
	if len(result.Scanners) != 2 {
		t.Fatalf("expected 2 scanner entries, got %+v", result.Scanners)
	}
	for _, s := range result.Scanners {
		if s.Available {
			t.Errorf("expected %s to be unavailable", s.Name)
		}
	}
}

func TestStdoutOnlyJSON(t *testing.T) {
	stubDir := t.TempDir()
	writeStub(t, stubDir, "gitleaks", "#!/bin/sh\nexit 0\n")
	writeStub(t, stubDir, "govulncheck", "#!/bin/sh\nexit 0\n")

	projectDir := t.TempDir()
	env := []string{"PATH=" + stubDir}

	stdout, _, code := runTool(t, projectDir, env)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d", code)
	}

	var raw map[string]any
	if err := json.Unmarshal([]byte(stdout), &raw); err != nil {
		t.Fatalf("stdout does not parse as JSON: %v\nstdout:\n%s", err, stdout)
	}
	if _, ok := raw["passed"]; !ok {
		t.Fatalf("JSON missing 'passed' field: %v", raw)
	}
}

func TestGovulncheckFinding_GoModTarget(t *testing.T) {
	stubDir := t.TempDir()
	writeStub(t, stubDir, "gitleaks", "#!/bin/sh\nexit 0\n")
	writeStub(t, stubDir, "govulncheck", `#!/bin/sh
echo '{"finding": {"osv": "GO-2024-0001", "trace": [{"module": "example.com/dep"}]}}'
exit 0
`)

	projectDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(projectDir, "go.mod"), []byte("module test\n\ngo 1.22\n"), 0644); err != nil {
		t.Fatalf("writing go.mod: %v", err)
	}
	env := []string{"PATH=" + stubDir}

	stdout, stderr, code := runTool(t, projectDir, env)
	if code != 1 {
		t.Fatalf("expected exit code 1, got %d; stderr:\n%s", code, stderr)
	}

	result := assertJSON(t, stdout)
	if len(result.Gaps) != 1 {
		t.Fatalf("expected exactly one gap, got %d: %+v", len(result.Gaps), result.Gaps)
	}

	gap := result.Gaps[0]
	if gap.Severity != "warning" {
		t.Errorf("severity: expected warning, got %q", gap.Severity)
	}
	if len(gap.TargetFiles) != 1 || gap.TargetFiles[0] != "go.mod" {
		t.Errorf("target_files: expected [go.mod], got %v", gap.TargetFiles)
	}
}
