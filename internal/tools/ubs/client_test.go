package ubs

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

// MockExecutor is a mock implementation of CLIExecutor for testing
type MockExecutor struct {
	ExecuteFunc        func(name string, arg ...string) ([]byte, error)
	ExecuteContextFunc func(ctx context.Context, name string, arg ...string) ([]byte, error)
	CallHistory        []struct {
		Name string
		Args []string
	}
}

func (m *MockExecutor) Execute(name string, arg ...string) ([]byte, error) {
	m.CallHistory = append(m.CallHistory, struct {
		Name string
		Args []string
	}{Name: name, Args: arg})
	if m.ExecuteFunc != nil {
		return m.ExecuteFunc(name, arg...)
	}
	return nil, errors.New("no execute func set")
}

func (m *MockExecutor) ExecuteContext(ctx context.Context, name string, arg ...string) ([]byte, error) {
	m.CallHistory = append(m.CallHistory, struct {
		Name string
		Args []string
	}{Name: name, Args: arg})
	if m.ExecuteContextFunc != nil {
		return m.ExecuteContextFunc(ctx, name, arg...)
	}
	return nil, errors.New("no execute context func set")
}

func TestNewClient_Defaults(t *testing.T) {
	client := NewClient()

	if client == nil {
		t.Fatal("expected non-nil client")
	}

	if !client.enabled {
		t.Error("expected client to be enabled by default")
	}

	if client.command != "ubs" {
		t.Errorf("expected command 'ubs', got %q", client.command)
	}

	if client.timeout != 30*time.Second {
		t.Errorf("expected timeout 30s, got %v", client.timeout)
	}

	if client.healthCheckTTL != 5*time.Minute {
		t.Errorf("expected health check TTL 5m, got %v", client.healthCheckTTL)
	}
}

func TestNewClient_WithOptions(t *testing.T) {
	mockExec := &MockExecutor{}
	client := NewClient(
		WithExecutor(mockExec),
		WithCommand("/custom/ubs"),
		WithTimeout(60*time.Second),
		WithHealthCheckTTL(10*time.Minute),
		WithEnabled(false),
	)

	if client.executor != mockExec {
		t.Error("expected custom executor")
	}

	if client.command != "/custom/ubs" {
		t.Errorf("expected command '/custom/ubs', got %q", client.command)
	}

	if client.timeout != 60*time.Second {
		t.Errorf("expected timeout 60s, got %v", client.timeout)
	}

	if client.healthCheckTTL != 10*time.Minute {
		t.Errorf("expected health check TTL 10m, got %v", client.healthCheckTTL)
	}

	if client.enabled {
		t.Error("expected client to be disabled")
	}
}

func TestClient_HealthCheck_Success(t *testing.T) {
	mockExec := &MockExecutor{
		ExecuteContextFunc: func(ctx context.Context, name string, arg ...string) ([]byte, error) {
			if name == "ubs" && len(arg) > 0 && arg[0] == "--version" {
				return []byte("ubs version 1.0.0"), nil
			}
			return nil, errors.New("unexpected command")
		},
	}

	client := NewClient(WithExecutor(mockExec))
	status := client.HealthCheck()

	if !status.Healthy {
		t.Error("expected healthy status")
	}

	if status.Cached {
		t.Error("expected non-cached result on first call")
	}

	if status.Version != "ubs version 1.0.0" {
		t.Errorf("expected version 'ubs version 1.0.0', got %q", status.Version)
	}

	if status.CheckedAt == 0 {
		t.Error("expected non-zero timestamp")
	}
}

func TestClient_HealthCheck_Failure(t *testing.T) {
	mockExec := &MockExecutor{
		ExecuteContextFunc: func(ctx context.Context, name string, arg ...string) ([]byte, error) {
			return nil, errors.New("command not found")
		},
	}

	client := NewClient(WithExecutor(mockExec))
	status := client.HealthCheck()

	if status.Healthy {
		t.Error("expected unhealthy status")
	}

	if status.Version != "" {
		t.Error("expected empty version on failure")
	}
}

func TestClient_HealthCheck_Cached(t *testing.T) {
	callCount := 0
	mockExec := &MockExecutor{
		ExecuteContextFunc: func(ctx context.Context, name string, arg ...string) ([]byte, error) {
			callCount++
			return []byte("ubs version 1.0.0"), nil
		},
	}

	client := NewClient(WithExecutor(mockExec))

	// First call
	client.HealthCheck()
	if callCount != 1 {
		t.Errorf("expected 1 call, got %d", callCount)
	}

	// Second call within TTL should use cache
	status := client.HealthCheck()
	if callCount != 1 {
		t.Errorf("expected 1 call (cached), got %d", callCount)
	}

	if !status.Cached {
		t.Error("expected cached result")
	}
}

func TestClient_IsHealthy(t *testing.T) {
	tests := []struct {
		name     string
		enabled  bool
		execFunc func(ctx context.Context, name string, arg ...string) ([]byte, error)
		want     bool
	}{
		{
			name:    "enabled and healthy",
			enabled: true,
			execFunc: func(ctx context.Context, name string, arg ...string) ([]byte, error) {
				return []byte("ubs version 1.0.0"), nil
			},
			want: true,
		},
		{
			name:    "enabled but unhealthy",
			enabled: true,
			execFunc: func(ctx context.Context, name string, arg ...string) ([]byte, error) {
				return nil, errors.New("not found")
			},
			want: false,
		},
		{
			name:    "disabled",
			enabled: false,
			execFunc: func(ctx context.Context, name string, arg ...string) ([]byte, error) {
				return []byte("ubs version 1.0.0"), nil
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockExec := &MockExecutor{ExecuteContextFunc: tt.execFunc}
			client := NewClient(
				WithExecutor(mockExec),
				WithEnabled(tt.enabled),
			)

			got := client.IsHealthy()
			if got != tt.want {
				t.Errorf("IsHealthy() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestClient_Scan_Success(t *testing.T) {
	expectedResult := UBSScanResult{
		Project:      "test-project",
		FilesScanned: 2,
		BugsFound:    1,
		Bugs: []Bug{
			{
				Rule:     "unused-variable",
				Severity: SeverityWarning,
				File:     "main.go",
				Line:     10,
				Column:   5,
				Message:  "Variable 'x' is declared but not used",
			},
		},
		Score: 99.0,
	}

	output, _ := json.Marshal(expectedResult)

	mockExec := &MockExecutor{
		ExecuteContextFunc: func(ctx context.Context, name string, arg ...string) ([]byte, error) {
			if name == "ubs" {
				return output, nil
			}
			return nil, errors.New("unexpected command")
		},
	}

	client := NewClient(WithExecutor(mockExec))
	result, err := client.Scan("/test/project", ScanOptions{})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Project != expectedResult.Project {
		t.Errorf("expected project %q, got %q", expectedResult.Project, result.Project)
	}

	if result.BugsFound != expectedResult.BugsFound {
		t.Errorf("expected %d bugs, got %d", expectedResult.BugsFound, result.BugsFound)
	}
}

func TestClient_Scan_Disabled(t *testing.T) {
	client := NewClient(WithEnabled(false))
	_, err := client.Scan("/test/project", ScanOptions{})

	if err == nil {
		t.Error("expected error when client is disabled")
	}

	if err.Error() != "ubs client is disabled" {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestClient_Scan_Timeout(t *testing.T) {
	mockExec := &MockExecutor{
		ExecuteContextFunc: func(ctx context.Context, name string, arg ...string) ([]byte, error) {
			// Simulate timeout by waiting for context cancellation
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(100 * time.Millisecond):
				return nil, errors.New("should have timed out")
			}
		},
	}

	client := NewClient(
		WithExecutor(mockExec),
		WithTimeout(10*time.Millisecond),
	)

	_, err := client.Scan("/test/project", ScanOptions{})

	if err == nil {
		t.Error("expected timeout error")
	}

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("expected deadline exceeded error, got: %v", err)
	}
}

func TestClient_Scan_InvalidJSON(t *testing.T) {
	mockExec := &MockExecutor{
		ExecuteContextFunc: func(ctx context.Context, name string, arg ...string) ([]byte, error) {
			return []byte("invalid json"), nil
		},
	}

	client := NewClient(WithExecutor(mockExec))
	_, err := client.Scan("/test/project", ScanOptions{})

	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestClient_ScanFiles(t *testing.T) {
	mockExec := &MockExecutor{
		ExecuteContextFunc: func(ctx context.Context, name string, arg ...string) ([]byte, error) {
			// Verify args
			expectedArgs := []string{"scan", "--format=json", "file1.go", "file2.go"}
			if len(arg) != len(expectedArgs) {
				t.Errorf("expected %d args, got %d", len(expectedArgs), len(arg))
			}

			return []byte(`{"project":"test","files_scanned":2,"bugs_found":0,"bugs":[],"score":100}`), nil
		},
	}

	client := NewClient(WithExecutor(mockExec))
	result, err := client.ScanFiles("/test/project", []string{"file1.go", "file2.go"}, "")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.FilesScanned != 2 {
		t.Errorf("expected 2 files scanned, got %d", result.FilesScanned)
	}
}

func TestClient_ScanStaged(t *testing.T) {
	mockExec := &MockExecutor{
		ExecuteContextFunc: func(ctx context.Context, name string, arg ...string) ([]byte, error) {
			// Verify args
			hasStaged := false
			for _, a := range arg {
				if a == "--staged" {
					hasStaged = true
					break
				}
			}

			if !hasStaged {
				t.Error("expected --staged flag")
			}

			return []byte(`{"project":"test","files_scanned":1,"bugs_found":0,"bugs":[],"score":100}`), nil
		},
	}

	client := NewClient(WithExecutor(mockExec))
	result, err := client.ScanStaged("/test/project", "")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result == nil {
		t.Error("expected non-nil result")
	}
}

func TestClient_ScanDiff(t *testing.T) {
	mockExec := &MockExecutor{
		ExecuteContextFunc: func(ctx context.Context, name string, arg ...string) ([]byte, error) {
			// Verify args
			hasDiff := false
			for _, a := range arg {
				if a == "--diff" {
					hasDiff = true
					break
				}
			}

			if !hasDiff {
				t.Error("expected --diff flag")
			}

			return []byte(`{"project":"test","files_scanned":1,"bugs_found":0,"bugs":[],"score":100}`), nil
		},
	}

	client := NewClient(WithExecutor(mockExec))
	result, err := client.ScanDiff("/test/project", "")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result == nil {
		t.Error("expected non-nil result")
	}
}

func TestClient_buildScanArgs(t *testing.T) {
	tests := []struct {
		name string
		opts ScanOptions
		want []string
	}{
		{
			name: "basic scan",
			opts: ScanOptions{},
			want: []string{"scan", "--format=json"},
		},
		{
			name: "staged scan",
			opts: ScanOptions{Staged: true},
			want: []string{"scan", "--format=json", "--staged"},
		},
		{
			name: "diff scan",
			opts: ScanOptions{Diff: true},
			want: []string{"scan", "--format=json", "--diff"},
		},
		{
			name: "with min severity",
			opts: ScanOptions{MinSeverity: SeverityCritical},
			want: []string{"scan", "--format=json", "--min-severity", "critical"},
		},
		{
			name: "with files",
			opts: ScanOptions{Files: []string{"file1.go", "file2.go"}},
			want: []string{"scan", "--format=json", "file1.go", "file2.go"},
		},
		{
			name: "all options",
			opts: ScanOptions{
				Staged:      true,
				MinSeverity: SeverityWarning,
				Files:       []string{"file.go"},
			},
			want: []string{"scan", "--format=json", "--staged", "--min-severity", "warning", "file.go"},
		},
	}

	client := NewClient()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := client.buildScanArgs(tt.opts)
			if len(got) != len(tt.want) {
				t.Errorf("buildScanArgs() = %v, want %v", got, tt.want)
			}
			for i := range tt.want {
				if i >= len(got) || got[i] != tt.want[i] {
					t.Errorf("buildScanArgs()[%d] = %v, want %v", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestClient_ScanAndEvaluate(t *testing.T) {
	result := UBSScanResult{
		BugsFound: 1,
		Bugs: []Bug{
			{Severity: SeverityWarning},
		},
		Score: 99.0,
	}
	output, _ := json.Marshal(result)

	mockExec := &MockExecutor{
		ExecuteContextFunc: func(ctx context.Context, name string, arg ...string) ([]byte, error) {
			return output, nil
		},
	}

	client := NewClient(WithExecutor(mockExec))
	scanResult, passed, err := client.ScanAndEvaluate(
		"/test/project",
		ScanOptions{},
		DefaultQualityThreshold(),
	)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if scanResult == nil {
		t.Fatal("expected non-nil result")
	}

	if !passed {
		t.Error("expected to pass quality gate")
	}
}

func TestClient_GetCriticalBugs(t *testing.T) {
	result := UBSScanResult{
		Bugs: []Bug{
			{Severity: SeverityCritical, Rule: "bug1"},
			{Severity: SeverityWarning, Rule: "bug2"},
			{Severity: SeverityCritical, Rule: "bug3"},
		},
	}

	client := NewClient()
	critical := client.GetCriticalBugs(&result)

	if len(critical) != 2 {
		t.Errorf("expected 2 critical bugs, got %d", len(critical))
	}

	for _, bug := range critical {
		if bug.Severity != SeverityCritical {
			t.Errorf("expected critical severity, got %s", bug.Severity)
		}
	}
}

func TestClient_GetWarningBugs(t *testing.T) {
	result := UBSScanResult{
		Bugs: []Bug{
			{Severity: SeverityCritical, Rule: "bug1"},
			{Severity: SeverityWarning, Rule: "bug2"},
			{Severity: SeverityWarning, Rule: "bug3"},
		},
	}

	client := NewClient()
	warnings := client.GetWarningBugs(&result)

	if len(warnings) != 2 {
		t.Errorf("expected 2 warning bugs, got %d", len(warnings))
	}
}

func TestClient_GetCriticalBugs_NilResult(t *testing.T) {
	client := NewClient()
	critical := client.GetCriticalBugs(nil)

	if critical != nil {
		t.Error("expected nil for nil result")
	}
}

func TestClient_FormatBugReport(t *testing.T) {
	tests := []struct {
		name   string
		result *UBSScanResult
		want   string
	}{
		{
			name:   "nil result",
			result: nil,
			want:   "No bugs found.",
		},
		{
			name: "no bugs",
			result: &UBSScanResult{
				BugsFound: 0,
			},
			want: "No bugs found.",
		},
		{
			name: "with bugs",
			result: &UBSScanResult{
				FilesScanned: 2,
				BugsFound:    1,
				Score:        95.5,
				Bugs: []Bug{
					{
						Rule:     "test-rule",
						Severity: SeverityWarning,
						File:     "test.go",
						Line:     10,
						Column:   5,
						Message:  "Test message",
					},
				},
			},
			want: "Found 1 bug(s)",
		},
	}

	client := NewClient()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := client.FormatBugReport(tt.result)
			if !containsSubstring(got, tt.want) {
				t.Errorf("FormatBugReport() = %q, want to contain %q", got, tt.want)
			}
		})
	}
}

// Helper function
func containsSubstring(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || strings.Contains(s, substr))
}

func TestClient_IsAvailable(t *testing.T) {
	mockExec := &MockExecutor{
		ExecuteContextFunc: func(ctx context.Context, name string, arg ...string) ([]byte, error) {
			return []byte("ubs version 1.0.0"), nil
		},
	}

	client := NewClient(WithExecutor(mockExec))

	if !client.IsAvailable() {
		t.Error("expected client to be available")
	}
}

func TestDefaultExecutor(t *testing.T) {
	exec := &DefaultExecutor{}

	// Test that DefaultExecutor implements CLIExecutor interface
	var _ CLIExecutor = exec
}

// Integration test (skipped by default)
func TestIntegration_UBSClient(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test")
	}

	client := NewClient()

	// Test health check against real ubs
	status := client.HealthCheck()
	t.Logf("Health status: %+v", status)

	if !status.Healthy {
		t.Skip("ubs not available, skipping integration test")
	}
}
