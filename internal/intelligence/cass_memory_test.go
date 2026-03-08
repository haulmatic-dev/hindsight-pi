package intelligence

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// createMockCMBinary creates a temporary mock cm binary for testing
func createMockCMBinary(t *testing.T, responses map[string]string) string {
	t.Helper()

	tmpDir := t.TempDir()
	var binaryPath string
	var script string

	if runtime.GOOS == "windows" {
		binaryPath = filepath.Join(tmpDir, "cm.bat")
		script = `@echo off
set command=%1
if "%command%"=="--version" (
    echo cm version 1.0.0
    exit 0
)
if "%command%"=="context" (
    echo {"success":true,"data":{"relevant_bullets":[{"id":"1","content":"Test bullet","category":"testing","score":0.9}],"anti_patterns":[],"history_snippets":[]}}
    exit 0
)
if "%command%"=="add" (
    echo {"success":true,"data":{"id":"new-pattern-123"}}
    exit 0
)
if "%command%"=="outcome" (
    echo {"success":true}
    exit 0
)
echo {"success":false,"error":"unknown command"}
exit 1`
	} else {
		binaryPath = filepath.Join(tmpDir, "cm")
		script = `#!/bin/sh
if [ "$1" = "--version" ]; then
    echo "cm version 1.0.0"
    exit 0
fi
if [ "$1" = "context" ]; then
    echo '{"success":true,"data":{"relevant_bullets":[{"id":"1","content":"Test bullet","category":"testing","score":0.9}],"anti_patterns":[],"history_snippets":[]}}'
    exit 0
fi
if [ "$1" = "add" ]; then
    echo '{"success":true,"data":{"id":"new-pattern-123"}}'
    exit 0
fi
if [ "$1" = "outcome" ]; then
    echo '{"success":true}'
    exit 0
fi
echo '{"success":false,"error":"unknown command"}'
exit 1`
	}

	err := os.WriteFile(binaryPath, []byte(script), 0755)
	if err != nil {
		t.Fatalf("Failed to create mock cm binary: %v", err)
	}

	return binaryPath
}

// createFailingCMBinary creates a mock cm binary that always fails
func createFailingCMBinary(t *testing.T) string {
	t.Helper()

	tmpDir := t.TempDir()
	var binaryPath string
	var script string

	if runtime.GOOS == "windows" {
		binaryPath = filepath.Join(tmpDir, "cm.bat")
		script = "@echo off\necho cm failed\nexit 1"
	} else {
		binaryPath = filepath.Join(tmpDir, "cm")
		script = "#!/bin/sh\necho 'cm failed' >&2\nexit 1"
	}

	err := os.WriteFile(binaryPath, []byte(script), 0755)
	if err != nil {
		t.Fatalf("Failed to create failing cm binary: %v", err)
	}

	return binaryPath
}

// createSlowCMBinary creates a mock cm binary that times out
func createSlowCMBinary(t *testing.T, delay time.Duration) string {
	t.Helper()

	tmpDir := t.TempDir()
	var binaryPath string
	var script string

	if runtime.GOOS == "windows" {
		binaryPath = filepath.Join(tmpDir, "cm.bat")
		script = fmt.Sprintf("@echo off\ntimeout /t %d /nobreak >nul 2>&1\necho {\"success\":true}", int(delay.Seconds()))
	} else {
		binaryPath = filepath.Join(tmpDir, "cm")
		script = fmt.Sprintf("#!/bin/sh\nsleep %f\necho '{\"success\":true}'", delay.Seconds())
	}

	err := os.WriteFile(binaryPath, []byte(script), 0755)
	if err != nil {
		t.Fatalf("Failed to create slow cm binary: %v", err)
	}

	return binaryPath
}

func TestNewCassMemoryClient(t *testing.T) {
	tests := []struct {
		name          string
		opts          []ClientOption
		wantEnabled   bool
		wantTimeout   time.Duration
		wantCmdPath   string
		setupMock     bool
		mockAvailable bool
	}{
		{
			name:          "default configuration with available binary",
			opts:          nil,
			wantEnabled:   false, // Will be determined by auto-detection (which fails in tests without mock)
			wantTimeout:   10 * time.Second,
			wantCmdPath:   "/Users/buddhi/.local/bin/cm",
			setupMock:     false,
			mockAvailable: false,
		},
		{
			name: "custom timeout",
			opts: []ClientOption{
				WithTimeout(30 * time.Second),
			},
			wantEnabled: false,
			wantTimeout: 30 * time.Second,
			wantCmdPath: "/Users/buddhi/.local/bin/cm",
		},
		{
			name: "custom command path",
			opts: []ClientOption{
				WithCommandPath("/custom/path/cm"),
			},
			wantEnabled: false,
			wantTimeout: 10 * time.Second,
			wantCmdPath: "/custom/path/cm",
		},
		{
			name: "explicitly enabled",
			opts: []ClientOption{
				WithEnabled(true),
			},
			wantEnabled: false, // Still false because binary doesn't exist
			wantTimeout: 10 * time.Second,
			wantCmdPath: "/Users/buddhi/.local/bin/cm",
		},
		{
			name: "explicitly disabled",
			opts: []ClientOption{
				WithEnabled(false),
			},
			wantEnabled: false,
			wantTimeout: 10 * time.Second,
			wantCmdPath: "/Users/buddhi/.local/bin/cm",
		},
		{
			name: "all options combined",
			opts: []ClientOption{
				WithTimeout(5 * time.Second),
				WithCommandPath("/opt/cm"),
				WithEnabled(true),
			},
			wantEnabled: false,
			wantTimeout: 5 * time.Second,
			wantCmdPath: "/opt/cm",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewCassMemoryClient(tt.opts...)

			if client.timeout != tt.wantTimeout {
				t.Errorf("timeout = %v, want %v", client.timeout, tt.wantTimeout)
			}

			if client.cmdPath != tt.wantCmdPath {
				t.Errorf("cmdPath = %v, want %v", client.cmdPath, tt.wantCmdPath)
			}

			if tt.setupMock && tt.mockAvailable {
				if !client.enabled {
					t.Errorf("enabled = %v, want %v", client.enabled, tt.wantEnabled)
				}
			}
		})
	}

	t.Run("auto-detection with available binary", func(t *testing.T) {
		mockPath := createMockCMBinary(t, nil)

		client := NewCassMemoryClient(
			WithCommandPath(mockPath),
		)

		if !client.enabled {
			t.Errorf("enabled = %v, want true for available binary", client.enabled)
		}
	})
}

func TestCassMemoryClient_Context(t *testing.T) {
	tests := []struct {
		name        string
		task        string
		enabled     bool
		mockBinary  func(t *testing.T) string
		wantErr     bool
		errContains string
		validate    func(t *testing.T, resp *CassContextResponse)
	}{
		{
			name:    "disabled client returns empty response",
			task:    "test task",
			enabled: false,
			wantErr: false,
			validate: func(t *testing.T, resp *CassContextResponse) {
				if !resp.Success {
					t.Error("expected success=true for disabled client")
				}
				if len(resp.Data.RelevantBullets) != 0 {
					t.Error("expected empty bullets for disabled client")
				}
				if len(resp.Data.AntiPatterns) != 0 {
					t.Error("expected empty anti-patterns for disabled client")
				}
				if len(resp.Data.HistorySnippets) != 0 {
					t.Error("expected empty history snippets for disabled client")
				}
			},
		},
		{
			name:        "empty task validation",
			task:        "",
			enabled:     true,
			wantErr:     true,
			errContains: "task cannot be empty",
		},
		{
			name:        "whitespace-only task validation",
			task:        "   \t\n  ",
			enabled:     true,
			wantErr:     true,
			errContains: "task cannot be empty",
		},
		{
			name:    "successful context query",
			task:    "test task",
			enabled: true,
			mockBinary: func(t *testing.T) string {
				return createMockCMBinary(t, nil)
			},
			wantErr: false,
			validate: func(t *testing.T, resp *CassContextResponse) {
				if !resp.Success {
					t.Errorf("expected success=true, got success=%v", resp.Success)
				}
				if len(resp.Data.RelevantBullets) != 1 {
					t.Errorf("expected 1 bullet, got %d", len(resp.Data.RelevantBullets))
				} else {
					bullet := resp.Data.RelevantBullets[0]
					if bullet.ID != "1" {
						t.Errorf("expected bullet ID='1', got '%s'", bullet.ID)
					}
					if bullet.Content != "Test bullet" {
						t.Errorf("expected bullet Content='Test bullet', got '%s'", bullet.Content)
					}
					if bullet.Category != "testing" {
						t.Errorf("expected bullet Category='testing', got '%s'", bullet.Category)
					}
					if bullet.Score != 0.9 {
						t.Errorf("expected bullet Score=0.9, got %f", bullet.Score)
					}
				}
			},
		},
		{
			name:    "command failure",
			task:    "test task",
			enabled: true,
			mockBinary: func(t *testing.T) string {
				return createFailingCMBinary(t)
			},
			wantErr:     true,
			errContains: "failed to get context",
		},
		{
			name:    "JSON parsing error",
			task:    "test task",
			enabled: true,
			mockBinary: func(t *testing.T) string {
				tmpDir := t.TempDir()
				var binaryPath string
				var script string

				if runtime.GOOS == "windows" {
					binaryPath = filepath.Join(tmpDir, "cm.bat")
					script = "@echo off\nif \"%1\"==\"--version\" (\n    echo cm version 1.0.0\n    exit 0\n)\necho invalid json {"
				} else {
					binaryPath = filepath.Join(tmpDir, "cm")
					script = "#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then\n    echo \"cm version 1.0.0\"\n    exit 0\nfi\necho 'invalid json {'"
				}

				err := os.WriteFile(binaryPath, []byte(script), 0755)
				if err != nil {
					t.Fatalf("Failed to create mock: %v", err)
				}

				return binaryPath
			},
			wantErr:     true,
			errContains: "failed to parse context response",
		},
		{
			name:    "cm returns error in response",
			task:    "test task",
			enabled: true,
			mockBinary: func(t *testing.T) string {
				tmpDir := t.TempDir()
				var binaryPath string
				var script string

				if runtime.GOOS == "windows" {
					binaryPath = filepath.Join(tmpDir, "cm.bat")
					script = "@echo off\nif \"%1\"==\"--version\" (\n    echo cm version 1.0.0\n    exit 0\n)\necho {\"success\":false,\"error\":\"database connection failed\"}"
				} else {
					binaryPath = filepath.Join(tmpDir, "cm")
					script = "#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then\n    echo \"cm version 1.0.0\"\n    exit 0\nfi\necho '{\"success\":false,\"error\":\"database connection failed\"}'"
				}

				err := os.WriteFile(binaryPath, []byte(script), 0755)
				if err != nil {
					t.Fatalf("Failed to create mock: %v", err)
				}

				return binaryPath
			},
			wantErr:     true,
			errContains: "cm context error: database connection failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var client *CassMemoryClient

			if tt.mockBinary != nil {
				mockPath := tt.mockBinary(t)
				client = &CassMemoryClient{
					enabled: tt.enabled,
					timeout: 5 * time.Second,
					cmdPath: mockPath,
				}
			} else {
				client = &CassMemoryClient{
					enabled: tt.enabled,
					timeout: 5 * time.Second,
					cmdPath: "/nonexistent/cm",
				}
			}

			resp, err := client.Context(tt.task)

			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error, got nil")
					return
				}
				if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("error should contain %q, got: %v", tt.errContains, err)
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if tt.validate != nil {
				tt.validate(t, resp)
			}
		})
	}

	t.Run("timeout behavior", func(t *testing.T) {
		mockPath := createSlowCMBinary(t, 5*time.Second)

		client := &CassMemoryClient{
			enabled: true,
			timeout: 100 * time.Millisecond,
			cmdPath: mockPath,
		}

		_, err := client.Context("test task")
		if err == nil {
			t.Error("expected timeout error, got nil")
			return
		}

		if !strings.Contains(err.Error(), "timed out") {
			t.Errorf("expected timeout error, got: %v", err)
		}
	})
}

func TestCassMemoryClient_AddPattern(t *testing.T) {
	tests := []struct {
		name        string
		pattern     string
		category    string
		enabled     bool
		mockBinary  func(t *testing.T) string
		wantErr     bool
		errContains string
	}{
		{
			name:        "disabled client",
			pattern:     "test pattern",
			category:    "testing",
			enabled:     false,
			wantErr:     true,
			errContains: "client is not enabled",
		},
		{
			name:        "empty pattern validation",
			pattern:     "",
			category:    "testing",
			enabled:     true,
			wantErr:     true,
			errContains: "pattern cannot be empty",
		},
		{
			name:        "empty category validation",
			pattern:     "test pattern",
			category:    "",
			enabled:     true,
			wantErr:     true,
			errContains: "category cannot be empty",
		},
		{
			name:     "successful pattern addition",
			pattern:  "test pattern",
			category: "testing",
			enabled:  true,
			mockBinary: func(t *testing.T) string {
				return createMockCMBinary(t, nil)
			},
			wantErr: false,
		},
		{
			name:        "whitespace-only pattern validation",
			pattern:     "   \t\n  ",
			category:    "testing",
			enabled:     true,
			wantErr:     true,
			errContains: "pattern cannot be empty",
		},
		{
			name:        "whitespace-only category validation",
			pattern:     "test pattern",
			category:    "   \t\n  ",
			enabled:     true,
			wantErr:     true,
			errContains: "category cannot be empty",
		},
		{
			name:     "cm add command failure",
			pattern:  "test pattern",
			category: "testing",
			enabled:  true,
			mockBinary: func(t *testing.T) string {
				return createFailingCMBinary(t)
			},
			wantErr:     true,
			errContains: "failed to add pattern",
		},
		{
			name:     "cm returns error response",
			pattern:  "test pattern",
			category: "testing",
			enabled:  true,
			mockBinary: func(t *testing.T) string {
				tmpDir := t.TempDir()
				var binaryPath string
				var script string

				if runtime.GOOS == "windows" {
					binaryPath = filepath.Join(tmpDir, "cm.bat")
					script = "@echo off\nif \"%1\"==\"--version\" (\n    echo cm version 1.0.0\n    exit 0\n)\nif \"%1\"==\"add\" (\n    echo {\"success\":false,\"error\":\"duplicate pattern\"}\n    exit 0\n)\necho {\"success\":false,\"error\":\"unknown command\"}\nexit 1"
				} else {
					binaryPath = filepath.Join(tmpDir, "cm")
					script = "#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then\n    echo \"cm version 1.0.0\"\n    exit 0\nfi\nif [ \"$1\" = \"add\" ]; then\n    echo '{\"success\":false,\"error\":\"duplicate pattern\"}'\n    exit 0\nfi\necho '{\"success\":false,\"error\":\"unknown command\"}'\nexit 1"
				}

				err := os.WriteFile(binaryPath, []byte(script), 0755)
				if err != nil {
					t.Fatalf("Failed to create mock: %v", err)
				}

				return binaryPath
			},
			wantErr:     true,
			errContains: "cm add error: duplicate pattern",
		},
		{
			name:     "JSON parsing error",
			pattern:  "test pattern",
			category: "testing",
			enabled:  true,
			mockBinary: func(t *testing.T) string {
				tmpDir := t.TempDir()
				var binaryPath string
				var script string

				if runtime.GOOS == "windows" {
					binaryPath = filepath.Join(tmpDir, "cm.bat")
					script = "@echo off\nif \"%1\"==\"--version\" (\n    echo cm version 1.0.0\n    exit 0\n)\necho invalid json"
				} else {
					binaryPath = filepath.Join(tmpDir, "cm")
					script = "#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then\n    echo \"cm version 1.0.0\"\n    exit 0\nfi\necho 'invalid json'"
				}

				err := os.WriteFile(binaryPath, []byte(script), 0755)
				if err != nil {
					t.Fatalf("Failed to create mock: %v", err)
				}

				return binaryPath
			},
			wantErr:     true,
			errContains: "failed to parse add response",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var client *CassMemoryClient

			if tt.mockBinary != nil {
				mockPath := tt.mockBinary(t)
				client = &CassMemoryClient{
					enabled: tt.enabled,
					timeout: 5 * time.Second,
					cmdPath: mockPath,
				}
			} else {
				client = &CassMemoryClient{
					enabled: tt.enabled,
					timeout: 5 * time.Second,
					cmdPath: "/nonexistent/cm",
				}
			}

			err := client.AddPattern(tt.pattern, tt.category)

			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error, got nil")
					return
				}
				if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("error should contain %q, got: %v", tt.errContains, err)
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestCassMemoryClient_RecordOutcome(t *testing.T) {
	tests := []struct {
		name       string
		status     string
		ruleIDs    []string
		duration   time.Duration
		enabled    bool
		mockBinary func(t *testing.T) string
		wantErr    bool
	}{
		{
			name:     "disabled client succeeds silently",
			status:   "success",
			ruleIDs:  []string{"rule1", "rule2"},
			duration: 5 * time.Second,
			enabled:  false,
			wantErr:  false,
		},
		{
			name:     "empty status succeeds silently",
			status:   "",
			ruleIDs:  []string{"rule1"},
			duration: 5 * time.Second,
			enabled:  true,
			wantErr:  false,
		},
		{
			name:     "empty rule IDs succeeds silently",
			status:   "success",
			ruleIDs:  []string{},
			duration: 5 * time.Second,
			enabled:  true,
			wantErr:  false,
		},
		{
			name:     "nil rule IDs succeeds silently",
			status:   "success",
			ruleIDs:  nil,
			duration: 5 * time.Second,
			enabled:  true,
			wantErr:  false,
		},
		{
			name:     "successful outcome recording - success",
			status:   "success",
			ruleIDs:  []string{"rule1", "rule2"},
			duration: 5 * time.Second,
			enabled:  true,
			mockBinary: func(t *testing.T) string {
				return createMockCMBinary(t, nil)
			},
			wantErr: false,
		},
		{
			name:     "successful outcome recording - failure",
			status:   "failure",
			ruleIDs:  []string{"rule1"},
			duration: 3 * time.Second,
			enabled:  true,
			mockBinary: func(t *testing.T) string {
				return createMockCMBinary(t, nil)
			},
			wantErr: false,
		},
		{
			name:     "successful outcome recording - partial",
			status:   "partial",
			ruleIDs:  []string{"rule1", "rule2", "rule3"},
			duration: 10 * time.Second,
			enabled:  true,
			mockBinary: func(t *testing.T) string {
				return createMockCMBinary(t, nil)
			},
			wantErr: false,
		},
		{
			name:     "unknown status is normalized to 'unknown'",
			status:   "invalid_status",
			ruleIDs:  []string{"rule1"},
			duration: 5 * time.Second,
			enabled:  true,
			mockBinary: func(t *testing.T) string {
				return createMockCMBinary(t, nil)
			},
			wantErr: false,
		},
		{
			name:     "command failure is ignored (best effort)",
			status:   "success",
			ruleIDs:  []string{"rule1"},
			duration: 5 * time.Second,
			enabled:  true,
			mockBinary: func(t *testing.T) string {
				return createFailingCMBinary(t)
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var client *CassMemoryClient

			if tt.mockBinary != nil {
				mockPath := tt.mockBinary(t)
				client = &CassMemoryClient{
					enabled: tt.enabled,
					timeout: 5 * time.Second,
					cmdPath: mockPath,
				}
			} else {
				client = &CassMemoryClient{
					enabled: tt.enabled,
					timeout: 5 * time.Second,
					cmdPath: "/nonexistent/cm",
				}
			}

			err := client.RecordOutcome(tt.status, tt.ruleIDs, tt.duration)

			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestGetCategoryForAgentType(t *testing.T) {
	originalMap := make(map[string]string)
	for k, v := range agentTypeToCategory {
		originalMap[k] = v
	}

	tests := []struct {
		name      string
		agentType string
		want      string
	}{
		{"orchestrator maps to orchestration", "orchestrator", "orchestration"},
		{"test-specialist maps to testing", "test-specialist", "testing"},
		{"code-reviewer maps to review", "code-reviewer", "review"},
		{"documentation maps to documentation", "documentation", "documentation"},
		{"security-auditor maps to security", "security-auditor", "security"},
		{"performance-analyst maps to performance", "performance-analyst", "performance"},
		{"refactoring maps to refactoring", "refactoring", "refactoring"},
		{"bug-fixer maps to debugging", "bug-fixer", "debugging"},
		{"feature-developer maps to development", "feature-developer", "development"},
		{"devops maps to infrastructure", "devops", "infrastructure"},
		{"database maps to database", "database", "database"},
		{"frontend maps to frontend", "frontend", "frontend"},
		{"backend maps to backend", "backend", "backend"},
		{"api-designer maps to api-design", "api-designer", "api-design"},
		{"ai-ml maps to ai-ml", "ai-ml", "ai-ml"},
		{"unknown type returns as-is", "custom-agent", "custom-agent"},
		{"empty string returns empty", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GetCategoryForAgentType(tt.agentType)
			if got != tt.want {
				t.Errorf("GetCategoryForAgentType(%q) = %q, want %q", tt.agentType, got, tt.want)
			}
		})
	}

	t.Run("custom mapping registration", func(t *testing.T) {
		agentTypeToCategory = make(map[string]string)
		for k, v := range originalMap {
			agentTypeToCategory[k] = v
		}

		RegisterAgentTypeMapping("my-custom-agent", "custom-category")

		got := GetCategoryForAgentType("my-custom-agent")
		if got != "custom-category" {
			t.Errorf("GetCategoryForAgentType(\"my-custom-agent\") = %q, want \"custom-category\"", got)
		}

		got2 := GetCategoryForAgentType("another-unknown")
		if got2 != "another-unknown" {
			t.Errorf("GetCategoryForAgentType(\"another-unknown\") = %q, want \"another-unknown\"", got2)
		}
	})
}

func TestCassMemoryClient_IsAvailable(t *testing.T) {
	tests := []struct {
		name       string
		mockBinary func(t *testing.T) string
		want       bool
	}{
		{
			name: "binary exists and works",
			mockBinary: func(t *testing.T) string {
				return createMockCMBinary(t, nil)
			},
			want: true,
		},
		{
			name: "binary fails",
			mockBinary: func(t *testing.T) string {
				return createFailingCMBinary(t)
			},
			want: false,
		},
		{
			name: "empty cmd path",
			mockBinary: func(t *testing.T) string {
				return ""
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var cmdPath string
			if tt.mockBinary != nil {
				cmdPath = tt.mockBinary(t)
			}

			client := &CassMemoryClient{
				enabled: false,
				timeout: 5 * time.Second,
				cmdPath: cmdPath,
			}

			got := client.isAvailable()
			if got != tt.want {
				t.Errorf("isAvailable() = %v, want %v", got, tt.want)
			}
		})
	}

	t.Run("binary doesn't exist", func(t *testing.T) {
		client := &CassMemoryClient{
			enabled: false,
			timeout: 5 * time.Second,
			cmdPath: "/definitely/does/not/exist/cm",
		}

		got := client.isAvailable()
		if got {
			t.Error("isAvailable() = true for non-existent binary, want false")
		}
	})
}

func TestCassMemoryClient_Execute(t *testing.T) {
	t.Run("disabled client returns error", func(t *testing.T) {
		client := &CassMemoryClient{
			enabled: false,
			timeout: 5 * time.Second,
			cmdPath: "/nonexistent/cm",
		}

		_, err := client.execute("context", "test")
		if err == nil {
			t.Error("expected error for disabled client, got nil")
			return
		}

		if !strings.Contains(err.Error(), "not enabled") {
			t.Errorf("error should contain 'not enabled', got: %v", err)
		}
	})

	t.Run("command timeout", func(t *testing.T) {
		mockPath := createSlowCMBinary(t, 10*time.Second)

		client := &CassMemoryClient{
			enabled: true,
			timeout: 100 * time.Millisecond,
			cmdPath: mockPath,
		}

		_, err := client.execute("context", "test")
		if err == nil {
			t.Error("expected timeout error, got nil")
			return
		}

		if !strings.Contains(err.Error(), "timed out") {
			t.Errorf("expected timeout error, got: %v", err)
		}
	})
}

func TestGetAvailableCategories(t *testing.T) {
	originalMap := make(map[string]string)
	for k, v := range agentTypeToCategory {
		originalMap[k] = v
	}
	defer func() {
		agentTypeToCategory = originalMap
	}()

	categories := GetAvailableCategories()

	seen := make(map[string]bool)
	for _, cat := range categories {
		if seen[cat] {
			t.Errorf("duplicate category in results: %s", cat)
		}
		seen[cat] = true
	}

	if len(categories) == 0 {
		t.Error("expected non-empty categories list")
	}

	expectedCategories := []string{"orchestration", "testing", "review", "security"}
	for _, expected := range expectedCategories {
		found := false
		for _, cat := range categories {
			if cat == expected {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected category %s not found in results", expected)
		}
	}
}

func TestGetAvailableAgentTypes(t *testing.T) {
	originalMap := make(map[string]string)
	for k, v := range agentTypeToCategory {
		originalMap[k] = v
	}
	defer func() {
		agentTypeToCategory = originalMap
	}()

	types := GetAvailableAgentTypes()

	if len(types) != len(agentTypeToCategory) {
		t.Errorf("expected %d agent types, got %d", len(agentTypeToCategory), len(types))
	}

	for _, agentType := range types {
		if _, ok := agentTypeToCategory[agentType]; !ok {
			t.Errorf("agent type %s returned but not in map", agentType)
		}
	}
}

func TestCassContextResponse_JSON(t *testing.T) {
	jsonData := `{
		"success": true,
		"data": {
			"relevant_bullets": [
				{
					"id": "bullet-1",
					"content": "Test content",
					"category": "testing",
					"score": 0.95,
					"tags": ["unit-test", "go"]
				}
			],
			"anti_patterns": [],
			"history_snippets": [
				{
					"id": "snippet-1",
					"content": "Previous context",
					"timestamp": "2024-01-15T10:30:00Z",
					"source": "previous-task"
				}
			]
		}
	}`

	var response CassContextResponse
	err := json.Unmarshal([]byte(jsonData), &response)
	if err != nil {
		t.Fatalf("failed to unmarshal JSON: %v", err)
	}

	if !response.Success {
		t.Error("expected success=true")
	}

	if len(response.Data.RelevantBullets) != 1 {
		t.Fatalf("expected 1 bullet, got %d", len(response.Data.RelevantBullets))
	}

	bullet := response.Data.RelevantBullets[0]
	if bullet.ID != "bullet-1" {
		t.Errorf("expected ID='bullet-1', got '%s'", bullet.ID)
	}
	if len(bullet.Tags) != 2 {
		t.Errorf("expected 2 tags, got %d", len(bullet.Tags))
	}

	if len(response.Data.HistorySnippets) != 1 {
		t.Fatalf("expected 1 snippet, got %d", len(response.Data.HistorySnippets))
	}

	snippet := response.Data.HistorySnippets[0]
	if snippet.Timestamp.IsZero() {
		t.Error("expected non-zero timestamp")
	}
}

func TestAddResponse_JSON(t *testing.T) {
	jsonData := `{"success":true,"data":{"id":"new-pattern-123"}}`

	var response AddResponse
	err := json.Unmarshal([]byte(jsonData), &response)
	if err != nil {
		t.Fatalf("failed to unmarshal JSON: %v", err)
	}

	if !response.Success {
		t.Error("expected success=true")
	}
	if response.Data.ID != "new-pattern-123" {
		t.Errorf("expected ID='new-pattern-123', got '%s'", response.Data.ID)
	}

	errorJson := `{"success":false,"error":"duplicate pattern"}`
	var errorResponse AddResponse
	err = json.Unmarshal([]byte(errorJson), &errorResponse)
	if err != nil {
		t.Fatalf("failed to unmarshal error JSON: %v", err)
	}

	if errorResponse.Success {
		t.Error("expected success=false")
	}
	if errorResponse.Error != "duplicate pattern" {
		t.Errorf("expected Error='duplicate pattern', got '%s'", errorResponse.Error)
	}
}

func TestOutcomeResponse_JSON(t *testing.T) {
	jsonData := `{"success":true}`

	var response OutcomeResponse
	err := json.Unmarshal([]byte(jsonData), &response)
	if err != nil {
		t.Fatalf("failed to unmarshal JSON: %v", err)
	}

	if !response.Success {
		t.Error("expected success=true")
	}

	errorJson := `{"success":false,"error":"database error"}`
	var errorResponse OutcomeResponse
	err = json.Unmarshal([]byte(errorJson), &errorResponse)
	if err != nil {
		t.Fatalf("failed to unmarshal error JSON: %v", err)
	}

	if errorResponse.Success {
		t.Error("expected success=false")
	}
	if errorResponse.Error != "database error" {
		t.Errorf("expected Error='database error', got '%s'", errorResponse.Error)
	}
}

func TestCassMemoryClient_IsEnabled(t *testing.T) {
	tests := []struct {
		name    string
		enabled bool
		want    bool
	}{
		{"enabled", true, true},
		{"disabled", false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &CassMemoryClient{
				enabled: tt.enabled,
				timeout: 5 * time.Second,
				cmdPath: "/nonexistent/cm",
			}

			got := client.IsEnabled()
			if got != tt.want {
				t.Errorf("IsEnabled() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestContext_CommandArguments(t *testing.T) {
	tmpDir := t.TempDir()
	var binaryPath string
	var script string
	var argsFile string

	if runtime.GOOS == "windows" {
		binaryPath = filepath.Join(tmpDir, "cm.bat")
		argsFile = filepath.Join(os.TempDir(), "cm_context_args.txt")
		script = fmt.Sprintf("@echo off\nif \"%%1\"==\"--version\" (\n    echo cm version 1.0.0\n    exit 0\n)\necho %%* \u003e \"%s\"\necho {\"success\":true,\"data\":{\"relevant_bullets\":[],\"anti_patterns\":[],\"history_snippets\":[]}}", argsFile)
	} else {
		binaryPath = filepath.Join(tmpDir, "cm")
		argsFile = "/tmp/cm_context_args.txt"
		script = fmt.Sprintf("#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then\n    echo \"cm version 1.0.0\"\n    exit 0\nfi\necho \"$@\" \u003e %s\necho '{\"success\":true,\"data\":{\"relevant_bullets\":[],\"anti_patterns\":[],\"history_snippets\":[]}}'", argsFile)
	}

	err := os.WriteFile(binaryPath, []byte(script), 0755)
	if err != nil {
		t.Fatalf("Failed to create mock: %v", err)
	}

	client := &CassMemoryClient{
		enabled: true,
		timeout: 5 * time.Second,
		cmdPath: binaryPath,
	}

	_, err = client.Context("test task query")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	args, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("failed to read captured args: %v", err)
	}

	argsStr := string(args)

	expectedArgs := []string{"context", "test task query", "--json", "--limit", "10", "--history", "5", "--days", "30"}
	for _, arg := range expectedArgs {
		if !strings.Contains(argsStr, arg) {
			t.Errorf("expected argument %q not found in captured args: %s", arg, argsStr)
		}
	}
}

func TestAddPattern_CommandArguments(t *testing.T) {
	tmpDir := t.TempDir()
	var binaryPath string
	var script string
	var argsFile string

	if runtime.GOOS == "windows" {
		binaryPath = filepath.Join(tmpDir, "cm.bat")
		argsFile = filepath.Join(os.TempDir(), "cm_add_args.txt")
		script = fmt.Sprintf("@echo off\nif \"%%1\"==\"--version\" (\n    echo cm version 1.0.0\n    exit 0\n)\necho %%* \u003e \"%s\"\necho {\"success\":true,\"data\":{\"id\":\"new-id\"}}", argsFile)
	} else {
		binaryPath = filepath.Join(tmpDir, "cm")
		argsFile = "/tmp/cm_add_args.txt"
		script = fmt.Sprintf("#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then\n    echo \"cm version 1.0.0\"\n    exit 0\nfi\necho \"$@\" \u003e %s\necho '{\"success\":true,\"data\":{\"id\":\"new-id\"}}'", argsFile)
	}

	err := os.WriteFile(binaryPath, []byte(script), 0755)
	if err != nil {
		t.Fatalf("Failed to create mock: %v", err)
	}

	client := &CassMemoryClient{
		enabled: true,
		timeout: 5 * time.Second,
		cmdPath: binaryPath,
	}

	err = client.AddPattern("new pattern to add", "testing")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	args, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("failed to read captured args: %v", err)
	}

	argsStr := string(args)

	expectedArgs := []string{"add", "new pattern to add", "--category", "testing", "--json"}
	for _, arg := range expectedArgs {
		if !strings.Contains(argsStr, arg) {
			t.Errorf("expected argument %q not found in captured args: %s", arg, argsStr)
		}
	}
}

func TestRecordOutcome_CommandArguments(t *testing.T) {
	tmpDir := t.TempDir()
	var binaryPath string
	var script string
	var argsFile string

	if runtime.GOOS == "windows" {
		binaryPath = filepath.Join(tmpDir, "cm.bat")
		argsFile = filepath.Join(os.TempDir(), "cm_outcome_args.txt")
		script = fmt.Sprintf("@echo off\nif \"%%1\"==\"--version\" (\n    echo cm version 1.0.0\n    exit 0\n)\necho %%* \u003e \"%s\"\necho {\"success\":true}", argsFile)
	} else {
		binaryPath = filepath.Join(tmpDir, "cm")
		argsFile = "/tmp/cm_outcome_args.txt"
		script = fmt.Sprintf("#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then\n    echo \"cm version 1.0.0\"\n    exit 0\nfi\necho \"$@\" \u003e %s\necho '{\"success\":true}'", argsFile)
	}

	err := os.WriteFile(binaryPath, []byte(script), 0755)
	if err != nil {
		t.Fatalf("Failed to create mock: %v", err)
	}

	client := &CassMemoryClient{
		enabled: true,
		timeout: 5 * time.Second,
		cmdPath: binaryPath,
	}

	ruleIDs := []string{"rule-1", "rule-2", "rule-3"}
	duration := 42 * time.Second

	err = client.RecordOutcome("success", ruleIDs, duration)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	args, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("failed to read captured args: %v", err)
	}

	argsStr := string(args)

	expectedArgs := []string{"outcome", "success", "rule-1,rule-2,rule-3", "--duration", "42", "--json"}
	for _, arg := range expectedArgs {
		if !strings.Contains(argsStr, arg) {
			t.Errorf("expected argument %q not found in captured args: %s", arg, argsStr)
		}
	}
}
