package agents

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// MockExecutor is a mock implementation of the Executor interface for testing
type MockExecutor struct {
	LookPathFunc func(file string) (string, error)
	CommandFunc  func(name string, arg ...string) *exec.Cmd
}

func (m *MockExecutor) LookPath(file string) (string, error) {
	if m.LookPathFunc != nil {
		return m.LookPathFunc(file)
	}
	return "", errors.New("LookPath not implemented")
}

func (m *MockExecutor) Command(name string, arg ...string) *exec.Cmd {
	if m.CommandFunc != nil {
		return m.CommandFunc(name, arg...)
	}
	// Return a command that does nothing and succeeds
	return exec.Command("echo", "mock")
}

// mockLogger creates a logger that writes to a buffer for testing
func mockLogger() (*log.Logger, *bytes.Buffer) {
	var buf bytes.Buffer
	logger := log.New(&buf, "", 0)
	return logger, &buf
}

// parseLogEntry parses a JSON log entry
func parseLogEntry(t *testing.T, line string) map[string]interface{} {
	var entry map[string]interface{}
	err := json.Unmarshal([]byte(line), &entry)
	require.NoError(t, err, "Failed to parse log entry: %s", line)
	return entry
}

// TestCheckInstallation_PiInstalled verifies that CheckInstallation detects installed Pi
func TestCheckInstallation_PiInstalled(t *testing.T) {
	logger, logBuf := mockLogger()

	connector := NewPiConnector(PiConfig{}, logger)

	// Mock the executor to simulate Pi being installed
	mockExec := &MockExecutor{
		LookPathFunc: func(file string) (string, error) {
			if file == "pi" {
				return "/usr/local/bin/pi", nil
			}
			return "", exec.ErrNotFound
		},
	}
	connector.executor = mockExec

	err := connector.CheckInstallation()

	require.NoError(t, err)

	// Verify logs contain expected JSON structure
	logOutput := logBuf.String()
	lines := strings.Split(strings.TrimSpace(logOutput), "\n")
	require.Len(t, lines, 2, "Expected 2 log entries")

	// First log: installation check start
	entry1 := parseLogEntry(t, lines[0])
	assert.Equal(t, "pi_installation_check", entry1["event"])
	assert.Equal(t, "Checking Pi agent installation...", entry1["message"])
	assert.NotEmpty(t, entry1["timestamp"])

	// Second log: found
	entry2 := parseLogEntry(t, lines[1])
	assert.Equal(t, "pi_installation_check", entry2["event"])
	assert.Equal(t, "found", entry2["status"])
	assert.Equal(t, "/usr/local/bin/pi", entry2["path"])
	assert.NotEmpty(t, entry2["timestamp"])
}

// TestCheckInstallation_PiNotInstalled verifies that CheckInstallation detects missing Pi
func TestCheckInstallation_PiNotInstalled(t *testing.T) {
	logger, logBuf := mockLogger()

	connector := NewPiConnector(PiConfig{}, logger)

	// Mock the executor to simulate Pi not being installed
	mockExec := &MockExecutor{
		LookPathFunc: func(file string) (string, error) {
			if file == "pi" {
				return "", exec.ErrNotFound
			}
			return "", exec.ErrNotFound
		},
	}
	connector.executor = mockExec

	err := connector.CheckInstallation()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "pi command not found in PATH")

	// Verify logs contain expected JSON structure
	logOutput := logBuf.String()
	lines := strings.Split(strings.TrimSpace(logOutput), "\n")
	require.Len(t, lines, 2, "Expected 2 log entries")

	// First log: installation check start
	entry1 := parseLogEntry(t, lines[0])
	assert.Equal(t, "pi_installation_check", entry1["event"])

	// Second log: not_found
	entry2 := parseLogEntry(t, lines[1])
	assert.Equal(t, "pi_installation_check", entry2["event"])
	assert.Equal(t, "not_found", entry2["status"])
	assert.NotEmpty(t, entry2["error"])
	assert.NotEmpty(t, entry2["timestamp"])
}

// TestInstall_Success verifies successful Pi installation via npm
func TestInstall_Success(t *testing.T) {
	logger, logBuf := mockLogger()

	connector := NewPiConnector(PiConfig{}, logger)

	callCount := 0
	mockExec := &MockExecutor{
		LookPathFunc: func(file string) (string, error) {
			callCount++
			if file == "npm" {
				return "/usr/local/bin/npm", nil
			}
			if file == "pi" {
				// Called during verification
				return "/usr/local/bin/pi", nil
			}
			return "", exec.ErrNotFound
		},
		CommandFunc: func(name string, arg ...string) *exec.Cmd {
			// Simulate successful npm install
			return exec.Command("echo", "+ @mariozechner/pi-coding-agent@1.0.0")
		},
	}
	connector.executor = mockExec

	err := connector.Install()

	require.NoError(t, err)

	// Verify npm was checked and pi was verified
	assert.GreaterOrEqual(t, callCount, 2, "Expected LookPath to be called for npm and pi")

	// Verify logs contain all expected steps
	logOutput := logBuf.String()
	assert.Contains(t, logOutput, `"event": "pi_installation_start"`)
	assert.Contains(t, logOutput, `"step": "npm_check"`)
	assert.Contains(t, logOutput, `"step": "npm_install"`)
	assert.Contains(t, logOutput, `"step": "verification"`)
	assert.Contains(t, logOutput, `"event": "pi_installation_complete"`)
}

// TestInstall_NpmNotFound verifies handling of missing npm
func TestInstall_NpmNotFound(t *testing.T) {
	logger, logBuf := mockLogger()

	connector := NewPiConnector(PiConfig{}, logger)

	mockExec := &MockExecutor{
		LookPathFunc: func(file string) (string, error) {
			if file == "npm" {
				return "", exec.ErrNotFound
			}
			return "", exec.ErrNotFound
		},
	}
	connector.executor = mockExec

	err := connector.Install()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "npm not found in PATH")
	assert.Contains(t, err.Error(), "cannot install pi")
	assert.Contains(t, err.Error(), "Please install Node.js and npm first")

	// Verify error is logged
	logOutput := logBuf.String()
	assert.Contains(t, logOutput, `"event": "pi_installation_error"`)
	assert.Contains(t, logOutput, `"error": "npm not found in PATH"`)
}

// TestInstall_NetworkFailure verifies handling of network errors during npm install
func TestInstall_NetworkFailure(t *testing.T) {
	logger, logBuf := mockLogger()

	connector := NewPiConnector(PiConfig{}, logger)

	mockExec := &MockExecutor{
		LookPathFunc: func(file string) (string, error) {
			if file == "npm" {
				return "/usr/local/bin/npm", nil
			}
			return "", exec.ErrNotFound
		},
		CommandFunc: func(name string, arg ...string) *exec.Cmd {
			// Simulate network failure
			return exec.Command("bash", "-c", "echo 'npm ERR! network timeout' >&2 && exit 1")
		},
	}
	connector.executor = mockExec

	err := connector.Install()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "npm install failed")

	// Verify error is logged with npm output
	logOutput := logBuf.String()
	assert.Contains(t, logOutput, `"event": "pi_installation_error"`)
	assert.Contains(t, logOutput, `npm_output`)
}

// TestInstall_VerificationFailure verifies handling of install verification failure
func TestInstall_VerificationFailure(t *testing.T) {
	logger, logBuf := mockLogger()

	connector := NewPiConnector(PiConfig{}, logger)

	lookPathCallCount := 0
	mockExec := &MockExecutor{
		LookPathFunc: func(file string) (string, error) {
			lookPathCallCount++
			if file == "npm" {
				return "/usr/local/bin/npm", nil
			}
			if file == "pi" {
				// Called during verification - Pi still not found after install
				return "", exec.ErrNotFound
			}
			return "", exec.ErrNotFound
		},
		CommandFunc: func(name string, arg ...string) *exec.Cmd {
			// Simulate successful npm install command
			return exec.Command("echo", "success")
		},
	}
	connector.executor = mockExec

	err := connector.Install()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "pi installation verification failed")

	// Verify LookPath was called for both npm and pi
	assert.GreaterOrEqual(t, lookPathCallCount, 2)

	// Verify error is logged
	logOutput := logBuf.String()
	assert.Contains(t, logOutput, `"event": "pi_installation_error"`)
	assert.Contains(t, logOutput, `"error": "verification failed"`)
}

// TestCheckInstallation_PathResolution verifies correct path resolution
// With caching enabled, each subtest creates a new connector to avoid cache pollution
func TestCheckInstallation_PathResolution(t *testing.T) {
	testCases := []struct {
		name         string
		path         string
		expectedPath string
	}{
		{
			name:         "system path",
			path:         "/usr/bin/pi",
			expectedPath: "/usr/bin/pi",
		},
		{
			name:         "local bin path",
			path:         "/usr/local/bin/pi",
			expectedPath: "/usr/local/bin/pi",
		},
		{
			name:         "nvm path",
			path:         "/Users/test/.nvm/versions/node/v20.0.0/bin/pi",
			expectedPath: "/Users/test/.nvm/versions/node/v20.0.0/bin/pi",
		},
		{
			name:         "npx path",
			path:         "/Users/test/.npm/_npx/12345/bin/pi",
			expectedPath: "/Users/test/.npm/_npx/12345/bin/pi",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Create a new connector for each subtest to avoid caching issues
			logger, logBuf := mockLogger()
			connector := NewPiConnector(PiConfig{}, logger)

			mockExec := &MockExecutor{
				LookPathFunc: func(file string) (string, error) {
					if file == "pi" {
						return tc.path, nil
					}
					return "", exec.ErrNotFound
				},
			}
			connector.executor = mockExec

			err := connector.CheckInstallation()

			require.NoError(t, err)

			// Verify the resolved path is logged correctly
			logOutput := logBuf.String()
			assert.Contains(t, logOutput, fmt.Sprintf(`"path": "%s"`, tc.expectedPath))
		})
	}
}

// TestInstall_StructuredLogging verifies JSON formatted logs
func TestInstall_StructuredLogging(t *testing.T) {
	logger, logBuf := mockLogger()

	connector := NewPiConnector(PiConfig{}, logger)

	mockExec := &MockExecutor{
		LookPathFunc: func(file string) (string, error) {
			if file == "npm" {
				return "/usr/local/bin/npm", nil
			}
			if file == "pi" {
				return "/usr/local/bin/pi", nil
			}
			return "", exec.ErrNotFound
		},
		CommandFunc: func(name string, arg ...string) *exec.Cmd {
			return exec.Command("echo", `+ @mariozechner/pi-coding-agent@1.2.3`)
		},
	}
	connector.executor = mockExec

	err := connector.Install()
	require.NoError(t, err)

	// Parse all log entries
	logOutput := strings.TrimSpace(logBuf.String())
	lines := strings.Split(logOutput, "\n")

	// Verify each line is valid JSON
	for i, line := range lines {
		var entry map[string]interface{}
		err := json.Unmarshal([]byte(line), &entry)
		require.NoError(t, err, "Line %d is not valid JSON: %s", i+1, line)

		// Verify required fields
		assert.Contains(t, entry, "event", "Log entry %d missing 'event' field", i+1)
		assert.Contains(t, entry, "timestamp", "Log entry %d missing 'timestamp' field", i+1)

		// Verify timestamp is valid RFC3339
		timestamp, ok := entry["timestamp"].(string)
		require.True(t, ok, "Timestamp should be a string")
		_, parseErr := time.Parse(time.RFC3339, timestamp)
		assert.NoError(t, parseErr, "Timestamp should be valid RFC3339")
	}

	// Verify log sequence - just check key events appear
	require.GreaterOrEqual(t, len(lines), 8, "Expected at least 8 log entries")

	// Get all event types
	var events []string
	for _, line := range lines {
		entry := parseLogEntry(t, line)
		events = append(events, entry["event"].(string))
	}

	// Verify key events appear in order
	assert.Equal(t, "pi_installation_start", events[0])
	assert.Contains(t, events, "pi_installation_step")
	assert.Contains(t, events, "pi_installation_check")
	assert.Equal(t, "pi_installation_complete", events[len(events)-1])
}

// TestCheckInstallation_StructuredLogging verifies JSON formatted logs for CheckInstallation
func TestCheckInstallation_StructuredLogging(t *testing.T) {
	logger, logBuf := mockLogger()

	connector := NewPiConnector(PiConfig{}, logger)

	mockExec := &MockExecutor{
		LookPathFunc: func(file string) (string, error) {
			return "/usr/bin/pi", nil
		},
	}
	connector.executor = mockExec

	err := connector.CheckInstallation()
	require.NoError(t, err)

	logOutput := strings.TrimSpace(logBuf.String())
	lines := strings.Split(logOutput, "\n")

	for _, line := range lines {
		var entry map[string]interface{}
		err := json.Unmarshal([]byte(line), &entry)
		require.NoError(t, err, "Log line is not valid JSON: %s", line)

		assert.Contains(t, entry, "event")
		assert.Contains(t, entry, "timestamp")

		// Verify event field is always a string
		_, ok := entry["event"].(string)
		assert.True(t, ok, "event field should be a string")
	}
}

// TestInstall_NpmInstallOutputEscaping verifies proper escaping of npm output in logs
func TestInstall_NpmInstallOutputEscaping(t *testing.T) {
	logger, logBuf := mockLogger()

	connector := NewPiConnector(PiConfig{}, logger)

	mockExec := &MockExecutor{
		LookPathFunc: func(file string) (string, error) {
			if file == "npm" {
				return "/usr/bin/npm", nil
			}
			if file == "pi" {
				return "/usr/bin/pi", nil
			}
			return "", exec.ErrNotFound
		},
		CommandFunc: func(name string, arg ...string) *exec.Cmd {
			// Return output containing quotes that need escaping
			return exec.Command("echo", `added 1 package, and audited 2 packages in 3s
{"some": "json", "nested": {"key": "value"}}
path: /usr/local/bin/pi`)
		},
	}
	connector.executor = mockExec

	err := connector.Install()
	require.NoError(t, err)

	// Verify the log output is valid JSON even with special characters
	logOutput := logBuf.String()
	lines := strings.Split(strings.TrimSpace(logOutput), "\n")

	for i, line := range lines {
		var entry map[string]interface{}
		err := json.Unmarshal([]byte(line), &entry)
		assert.NoError(t, err, "Line %d should be valid JSON: %s", i+1, line)
	}
}

// TestCheckInstallation_ConcurrentAccess verifies thread safety
func TestCheckInstallation_ConcurrentAccess(t *testing.T) {
	logger, _ := mockLogger()

	connector := NewPiConnector(PiConfig{}, logger)

	mockExec := &MockExecutor{
		LookPathFunc: func(file string) (string, error) {
			return "/usr/bin/pi", nil
		},
	}
	connector.executor = mockExec

	// Run multiple concurrent checks
	done := make(chan error, 10)
	for i := 0; i < 10; i++ {
		go func() {
			done <- connector.CheckInstallation()
		}()
	}

	// Collect all results
	for i := 0; i < 10; i++ {
		err := <-done
		assert.NoError(t, err)
	}
}

// TestInstall_ConcurrentAccess verifies thread safety during install
func TestInstall_ConcurrentAccess(t *testing.T) {
	logger, _ := mockLogger()

	connector := NewPiConnector(PiConfig{}, logger)

	mockExec := &MockExecutor{
		LookPathFunc: func(file string) (string, error) {
			if file == "npm" {
				return "/usr/bin/npm", nil
			}
			return "/usr/bin/pi", nil
		},
		CommandFunc: func(name string, arg ...string) *exec.Cmd {
			return exec.Command("echo", "success")
		},
	}
	connector.executor = mockExec

	// Run multiple concurrent installs
	done := make(chan error, 5)
	for i := 0; i < 5; i++ {
		go func() {
			done <- connector.Install()
		}()
	}

	// Collect all results
	for i := 0; i < 5; i++ {
		err := <-done
		assert.NoError(t, err)
	}
}

// TestCheckInstallation_MultipleCalls verifies caching behavior on multiple sequential calls
// With TTL-based caching, LookPath should only be called once within the cache window
func TestCheckInstallation_MultipleCalls(t *testing.T) {
	logger, _ := mockLogger()

	connector := NewPiConnector(PiConfig{}, logger)

	callCount := 0
	mockExec := &MockExecutor{
		LookPathFunc: func(file string) (string, error) {
			callCount++
			return "/usr/bin/pi", nil
		},
	}
	connector.executor = mockExec

	// Call multiple times
	for i := 0; i < 3; i++ {
		err := connector.CheckInstallation()
		require.NoError(t, err)
	}

	// With caching, LookPath should only be called once
	assert.Equal(t, 1, callCount, "Expected LookPath to be called only once due to caching")
}

// TestInstall_NpmGlobalInstallArgs verifies correct npm arguments
func TestInstall_NpmGlobalInstallArgs(t *testing.T) {
	logger, _ := mockLogger()

	connector := NewPiConnector(PiConfig{}, logger)

	capturedArgs := []string{}
	mockExec := &MockExecutor{
		LookPathFunc: func(file string) (string, error) {
			return "/usr/bin/" + file, nil
		},
		CommandFunc: func(name string, arg ...string) *exec.Cmd {
			if name == "npm" {
				capturedArgs = append([]string{name}, arg...)
			}
			return exec.Command("echo", "success")
		},
	}
	connector.executor = mockExec

	err := connector.Install()
	require.NoError(t, err)

	// Verify npm was called with correct arguments
	require.Len(t, capturedArgs, 4, "Expected 4 arguments: npm, install, -g, package")
	assert.Equal(t, "npm", capturedArgs[0])
	assert.Equal(t, "install", capturedArgs[1])
	assert.Equal(t, "-g", capturedArgs[2])
	assert.Equal(t, "@mariozechner/pi-coding-agent", capturedArgs[3])
}

// TestCheckInstallation_CustomError verifies custom error messages
func TestCheckInstallation_CustomError(t *testing.T) {
	logger, _ := mockLogger()

	connector := NewPiConnector(PiConfig{}, logger)

	customErr := errors.New("custom lookup error")
	mockExec := &MockExecutor{
		LookPathFunc: func(file string) (string, error) {
			return "", customErr
		},
	}
	connector.executor = mockExec

	err := connector.CheckInstallation()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "pi command not found in PATH")
	assert.ErrorIs(t, err, customErr)
}

// TestInstall_EmptyNpmOutput handles empty npm install output
func TestInstall_EmptyNpmOutput(t *testing.T) {
	logger, logBuf := mockLogger()

	connector := NewPiConnector(PiConfig{}, logger)

	mockExec := &MockExecutor{
		LookPathFunc: func(file string) (string, error) {
			if file == "npm" {
				return "/usr/bin/npm", nil
			}
			return "/usr/bin/pi", nil
		},
		CommandFunc: func(name string, arg ...string) *exec.Cmd {
			// Return empty output
			return exec.Command("true")
		},
	}
	connector.executor = mockExec

	err := connector.Install()
	require.NoError(t, err)

	// Verify empty output is handled correctly in logs
	logOutput := logBuf.String()
	assert.Contains(t, logOutput, `"npm_output": ""`)
}

// TestInstall_NpmCommandNotExecutable verifies handling of non-executable npm
func TestInstall_NpmCommandNotExecutable(t *testing.T) {
	logger, _ := mockLogger()

	connector := NewPiConnector(PiConfig{}, logger)

	mockExec := &MockExecutor{
		LookPathFunc: func(file string) (string, error) {
			if file == "npm" {
				return "/not/executable/npm", nil
			}
			return "", exec.ErrNotFound
		},
		CommandFunc: func(name string, arg ...string) *exec.Cmd {
			// Simulate permission denied
			return exec.Command("bash", "-c", "exit 126")
		},
	}
	connector.executor = mockExec

	err := connector.Install()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "npm install failed")
}

// TestCheckInstallation_DifferentPiVersions verifies handling of different Pi versions
func TestCheckInstallation_DifferentPiVersions(t *testing.T) {
	logger, logBuf := mockLogger()

	connector := NewPiConnector(PiConfig{}, logger)

	mockExec := &MockExecutor{
		LookPathFunc: func(file string) (string, error) {
			return "/usr/local/bin/pi", nil
		},
	}
	connector.executor = mockExec

	err := connector.CheckInstallation()
	require.NoError(t, err)

	// Verify path is logged regardless of version
	logOutput := logBuf.String()
	assert.Contains(t, logOutput, `"path": "/usr/local/bin/pi"`)
}
