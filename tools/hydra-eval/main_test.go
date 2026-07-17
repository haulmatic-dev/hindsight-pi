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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func runHydraEval(t *testing.T, args ...string) ([]byte, []byte, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	wd, err := os.Getwd()
	require.NoError(t, err)

	cmd := exec.CommandContext(ctx, "go", append([]string{"run", "."}, args...)...)
	cmd.Dir = wd
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()
	return []byte(stdout.String()), []byte(stderr.String()), err
}

func TestMainIntegration_ValidFixtures(t *testing.T) {
	dir := t.TempDir()
	beadsPath := filepath.Join(dir, "beads.jsonl")
	daemonPath := filepath.Join(dir, "daemon.log")
	outPath := filepath.Join(dir, "out.json")

	err := os.WriteFile(beadsPath, []byte(`{"id":"b1","status":"closed","issue_type":"task","created_at":"2026-07-14T10:00:00Z","closed_at":"2026-07-14T11:00:00Z"}
{"id":"b2","status":"closed","issue_type":"epic","created_at":"2026-07-14T10:00:00Z","closed_at":"2026-07-14T11:00:00Z"}
`), 0644)
	require.NoError(t, err)

	err = os.WriteFile(daemonPath, []byte(`2026/07/14 18:52:25 [CloseEvent] {"bead_id":"b1","verdict":"accepted","reason":"verified","attempt":0}
`), 0644)
	require.NoError(t, err)

	stdout, stderr, err := runHydraEval(t, "--beads-jsonl", beadsPath, "--daemon-log", daemonPath, "--out", outPath)
	assert.NoError(t, err, "stdout: %s, stderr: %s", stdout, stderr)

	outBytes, readErr := os.ReadFile(outPath)
	require.NoError(t, readErr, "output file was not written")

	var metrics Metrics
	require.NoError(t, json.Unmarshal(outBytes, &metrics))

	assert.Equal(t, 2, metrics.TotalBeads)
	assert.Equal(t, 1, metrics.VerifiedAccepts)
}

func TestMainIntegration_MissingDaemonLog(t *testing.T) {
	dir := t.TempDir()
	beadsPath := filepath.Join(dir, "beads.jsonl")
	outPath := filepath.Join(dir, "out.json")

	err := os.WriteFile(beadsPath, []byte(`{"id":"b1","status":"closed","issue_type":"task","created_at":"2026-07-14T10:00:00Z","closed_at":"2026-07-14T11:00:00Z"}
`), 0644)
	require.NoError(t, err)

	stdout, stderr, err := runHydraEval(t, "--beads-jsonl", beadsPath, "--daemon-log", filepath.Join(dir, "missing.log"), "--out", outPath)
	assert.NoError(t, err, "stdout: %s, stderr: %s", stdout, stderr)

	outBytes, readErr := os.ReadFile(outPath)
	require.NoError(t, readErr)

	var metrics Metrics
	require.NoError(t, json.Unmarshal(outBytes, &metrics))

	assert.Equal(t, 1, metrics.TotalBeads)
	assert.Equal(t, 0, metrics.VerifiedAccepts)
}

func TestMainIntegration_MissingBeadsJSONL(t *testing.T) {
	stdout, stderr, err := runHydraEval(t)
	assert.Error(t, err, "expected non-zero exit when --beads-jsonl is missing; stdout: %s, stderr: %s", stdout, stderr)
}

func TestMainIntegration_SinceFiltersEvents(t *testing.T) {
	dir := t.TempDir()
	beadsPath := filepath.Join(dir, "beads.jsonl")
	daemonPath := filepath.Join(dir, "daemon.log")
	outPath := filepath.Join(dir, "out.json")

	err := os.WriteFile(beadsPath, []byte(`{"id":"b1","status":"closed","issue_type":"task","created_at":"2026-07-14T10:00:00Z","closed_at":"2026-07-14T11:00:00Z"}
`), 0644)
	require.NoError(t, err)

	err = os.WriteFile(daemonPath, []byte(`2026/07/17 09:00:00 [CloseEvent] {"bead_id":"b1","verdict":"accepted","reason":"verified","attempt":0}
2026/07/17 11:00:00 [CloseEvent] {"bead_id":"b1","verdict":"accepted","reason":"verified","attempt":0}
`), 0644)
	require.NoError(t, err)

	stdout, stderr, err := runHydraEval(t, "--beads-jsonl", beadsPath, "--daemon-log", daemonPath, "--out", outPath, "--since", "2026/07/17 10:00:00")
	assert.NoError(t, err, "stdout: %s, stderr: %s", stdout, stderr)

	outBytes, readErr := os.ReadFile(outPath)
	require.NoError(t, readErr)

	var metrics Metrics
	require.NoError(t, json.Unmarshal(outBytes, &metrics))
	assert.Equal(t, 1, metrics.VerifiedAccepts)

	stdout, stderr, err = runHydraEval(t, "--beads-jsonl", beadsPath, "--daemon-log", daemonPath, "--out", outPath)
	assert.NoError(t, err, "stdout: %s, stderr: %s", stdout, stderr)

	outBytes, readErr = os.ReadFile(outPath)
	require.NoError(t, readErr)
	require.NoError(t, json.Unmarshal(outBytes, &metrics))
	assert.Equal(t, 2, metrics.VerifiedAccepts)
}

func TestMainIntegration_InvalidSinceExitsNonZero(t *testing.T) {
	dir := t.TempDir()
	beadsPath := filepath.Join(dir, "beads.jsonl")

	err := os.WriteFile(beadsPath, []byte(`{"id":"b1","status":"closed","issue_type":"task","created_at":"2026-07-14T10:00:00Z","closed_at":"2026-07-14T11:00:00Z"}
`), 0644)
	require.NoError(t, err)

	stdout, stderr, err := runHydraEval(t, "--beads-jsonl", beadsPath, "--since", "garbage")
	assert.Error(t, err, "expected non-zero exit for invalid --since; stdout: %s, stderr: %s", stdout, stderr)
	assert.Contains(t, string(stderr), "2006/01/02 15:04:05")
	assert.Contains(t, string(stderr), "2006/01/02")
}

func TestMainIntegration_OutputIsValidJSON(t *testing.T) {
	dir := t.TempDir()
	beadsPath := filepath.Join(dir, "beads.jsonl")
	outPath := filepath.Join(dir, "out.json")

	err := os.WriteFile(beadsPath, []byte(`{"id":"b1","status":"closed","issue_type":"task","created_at":"2026-07-14T10:00:00Z","closed_at":"2026-07-14T11:00:00Z"}
`), 0644)
	require.NoError(t, err)

	stdout, stderr, err := runHydraEval(t, "--beads-jsonl", beadsPath, "--out", outPath)
	assert.NoError(t, err, "stdout: %s, stderr: %s", stdout, stderr)

	outBytes, readErr := os.ReadFile(outPath)
	require.NoError(t, readErr)

	var raw map[string]any
	require.NoError(t, json.Unmarshal(outBytes, &raw))

	stdoutBytes := []byte(stdout)
	if len(stdoutBytes) > 0 {
		var stdoutRaw map[string]any
		require.NoError(t, json.Unmarshal(stdoutBytes, &stdoutRaw))
	}
}
