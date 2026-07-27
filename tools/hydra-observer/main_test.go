package main

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// runObserver builds the current package and executes the resulting binary
// in the module directory. Using a compiled binary instead of `go run` lets
// tests verify the exact exit code returned by the program.
func runObserver(t *testing.T, args ...string) ([]byte, []byte, error) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working directory: %v", err)
	}

	bin := filepath.Join(t.TempDir(), "hydra-observer")
	buildCmd := exec.CommandContext(ctx, "go", "build", "-o", bin, ".")
	buildCmd.Dir = wd
	if err := buildCmd.Run(); err != nil {
		t.Fatalf("failed to build observer binary: %v", err)
	}

	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = wd

	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err = cmd.Run()
	return []byte(stdout.String()), []byte(stderr.String()), err
}

// lastJSONLine returns the final non-empty line of b as a generic JSON map.
func lastJSONLine(t *testing.T, b []byte) map[string]any {
	t.Helper()

	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	var last string
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.TrimSpace(lines[i]) != "" {
			last = lines[i]
			break
		}
	}

	if last == "" {
		t.Fatalf("expected at least one non-empty stdout line, got %q", string(b))
	}

	var summary map[string]any
	if err := json.Unmarshal([]byte(last), &summary); err != nil {
		t.Fatalf("final stdout line is not valid JSON: %q: %v", last, err)
	}
	return summary
}

// markdownFiles returns the .md files in dir.
func markdownFiles(t *testing.T, dir string) []string {
	t.Helper()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("failed to read dir %q: %v", dir, err)
	}

	var files []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
			files = append(files, e.Name())
		}
	}
	return files
}

func TestCLI_NoLogExits2(t *testing.T) {
	_, stderr, err := runObserver(t, "--backlog-dir", t.TempDir())
	if err == nil {
		t.Fatalf("expected non-zero exit when --log is missing; stderr: %s", stderr)
	}

	exitErr, ok := err.(*exec.ExitError)
	if !ok {
		t.Fatalf("expected *exec.ExitError, got %T: %v", err, err)
	}
	if exitErr.ExitCode() != 2 {
		t.Fatalf("expected exit code 2 when --log is missing, got %d", exitErr.ExitCode())
	}

	if !strings.Contains(string(stderr), "--log") {
		t.Errorf("stderr %q does not contain --log", stderr)
	}
}

func TestCLI_EmptyDiffLogCreatesBacklogAndRerunDedupes(t *testing.T) {
	backlogDir := t.TempDir()
	logPath := filepath.Join("testdata", "empty-diff.log")

	// First run: one new backlog entry.
	stdout1, stderr1, err1 := runObserver(t, "--log", logPath, "--backlog-dir", backlogDir)
	if err1 == nil {
		t.Fatalf("expected exit 1 when a new backlog entry is written; stdout: %s, stderr: %s", stdout1, stderr1)
	}

	md1 := markdownFiles(t, backlogDir)
	if len(md1) != 1 {
		t.Fatalf("expected exactly one backlog file after first run, got %d", len(md1))
	}

	summary1 := lastJSONLine(t, stdout1)
	if summary1["new_entries"] != float64(1) {
		t.Errorf("expected new_entries=1, got %v", summary1["new_entries"])
	}

	// Second run with the same inputs must not create duplicates.
	stdout2, stderr2, err2 := runObserver(t, "--log", logPath, "--backlog-dir", backlogDir)
	if err2 != nil {
		t.Fatalf("expected exit 0 on deduplicated rerun; stdout: %s, stderr: %s", stdout2, stderr2)
	}

	md2 := markdownFiles(t, backlogDir)
	if len(md2) != 1 {
		t.Fatalf("expected still exactly one backlog file after rerun, got %d", len(md2))
	}
	if len(md1) != len(md2) || md1[0] != md2[0] {
		t.Errorf("rerun must not add or rename backlog files: got %v then %v", md1, md2)
	}

	summary2 := lastJSONLine(t, stdout2)
	if summary2["new_entries"] != float64(0) {
		t.Errorf("expected new_entries=0 on rerun, got %v", summary2["new_entries"])
	}
}

func TestCLI_MetricsRegression(t *testing.T) {
	backlogDir := t.TempDir()
	metricsPath := filepath.Join("testdata", "metrics-regression.json")
	baselinePath := filepath.Join("testdata", "baseline-0.8.json")

	stdout, stderr, err := runObserver(
		t,
		"--log", filepath.Join("testdata", "quiet.log"),
		"--metrics", metricsPath,
		"--baseline", baselinePath,
		"--backlog-dir", backlogDir,
	)
	if err == nil {
		t.Fatalf("expected exit 1 when a regression is found; stdout: %s, stderr: %s", stdout, stderr)
	}

	md := markdownFiles(t, backlogDir)
	if len(md) == 0 {
		t.Fatalf("expected at least one backlog file for the regression")
	}

	var matched bool
	for _, name := range md {
		content, readErr := os.ReadFile(filepath.Join(backlogDir, name))
		if readErr != nil {
			t.Fatalf("failed to read backlog file %q: %v", name, readErr)
		}
		if strings.Contains(string(content), "one_shot_rate") {
			matched = true
			break
		}
	}
	if !matched {
		t.Errorf("expected a backlog file whose content contains 'one_shot_rate'")
	}
}

func TestCLI_NoBaselineCreatesBaselineAndNoRegression(t *testing.T) {
	parentDir := t.TempDir()
	backlogDir := filepath.Join(parentDir, "backlog")
	if err := os.Mkdir(backlogDir, 0755); err != nil {
		t.Fatalf("failed to create backlog dir: %v", err)
	}

	metricsPath := filepath.Join("testdata", "quiet-metrics.json")
	defaultBaseline := filepath.Join(parentDir, "observer-baseline.json")

	if _, err := os.Stat(defaultBaseline); err == nil {
		t.Fatalf("baseline must not exist before the run")
	}

	stdout, stderr, err := runObserver(t, "--log", filepath.Join("testdata", "quiet.log"), "--metrics", metricsPath, "--backlog-dir", backlogDir)
	if err != nil {
		t.Fatalf("expected exit 0 when baseline is initialised; stdout: %s, stderr: %s", stdout, stderr)
	}

	if _, err := os.Stat(defaultBaseline); err != nil {
		t.Errorf("default baseline file should be created: %v", err)
	}

	md := markdownFiles(t, backlogDir)
	if len(md) != 0 {
		t.Errorf("no regression entry should be written when the baseline is initialised, got %d files", len(md))
	}
}
