package core

import (
	"testing"
	"time"
)

func TestConfigChangeDetector_DetectChanges(t *testing.T) {
	detector := NewConfigChangeDetector()

	tests := []struct {
		name     string
		oldCfg   *Config
		newCfg   *Config
		expected int // number of expected changes
		check    func([]ToolConfigChange) bool
	}{
		{
			name:     "no changes when configs are identical",
			oldCfg:   createTestConfig(),
			newCfg:   createTestConfig(),
			expected: 0,
		},
		{
			oldCfg:   createTestConfig(),
			expected: 1,
			check: func(changes []ToolConfigChange) bool {
				return len(changes) == 1 &&
					changes[0].HasChanged
			},
		},
		{
			oldCfg:   createTestConfig(),
			expected: 1,
			check: func(changes []ToolConfigChange) bool {
				return len(changes) == 1 &&
					changes[0].EnabledBefore == true &&
					changes[0].EnabledAfter == false
			},
		},
		{
			name:     "detect TLDR timeout change",
			oldCfg:   createTestConfig(),
			newCfg:   modifyConfig(createTestConfig(), func(c *Config) { c.Intelligence.TLDR.Timeout = 20 * time.Second }),
			expected: 1,
			check: func(changes []ToolConfigChange) bool {
				return len(changes) == 1 &&
					changes[0].Tool == ToolTLDR &&
					changes[0].HasChanged
			},
		},
		{
			name:     "detect TLDR BaseURL change",
			oldCfg:   createTestConfig(),
			newCfg:   modifyConfig(createTestConfig(), func(c *Config) { c.Intelligence.TLDR.BaseURL = "http://localhost:9999" }),
			expected: 1,
			check: func(changes []ToolConfigChange) bool {
				return len(changes) == 1 && changes[0].Tool == ToolTLDR
			},
		},
		{
			name:     "detect GPTCache enabled change",
			oldCfg:   createTestConfig(),
			newCfg:   modifyConfig(createTestConfig(), func(c *Config) { c.Intelligence.GPTCache.Enabled = false }),
			expected: 1,
			check: func(changes []ToolConfigChange) bool {
				return len(changes) == 1 &&
					changes[0].Tool == ToolGPTCache &&
					changes[0].EnabledBefore == true &&
					changes[0].EnabledAfter == false
			},
		},
		{
			name:     "detect MCP enabled change",
			oldCfg:   createTestConfig(),
			newCfg:   modifyConfig(createTestConfig(), func(c *Config) { c.MCP.Enabled = false }),
			expected: 1,
			check: func(changes []ToolConfigChange) bool {
				return len(changes) == 1 &&
					changes[0].Tool == ToolMCP &&
					changes[0].EnabledBefore == true &&
					changes[0].EnabledAfter == false
			},
		},
		{
			name:     "detect MCP URL change",
			oldCfg:   createTestConfig(),
			newCfg:   modifyConfig(createTestConfig(), func(c *Config) { c.MCP.URL = "ws://localhost:9999/ws" }),
			expected: 1,
			check: func(changes []ToolConfigChange) bool {
				return len(changes) == 1 && changes[0].Tool == ToolMCP
			},
		},
		{
			name:   "detect multiple changes",
			oldCfg: createTestConfig(),
			newCfg: modifyConfig(createTestConfig(), func(c *Config) {
				c.Intelligence.TLDR.Enabled = false
			}),
			expected: 2,
			check: func(changes []ToolConfigChange) bool {
				if len(changes) != 2 {
					return false
				}
				foundCass := false
				foundTLDR := false
				for _, c := range changes {
						foundCass = true
					}
					if c.Tool == ToolTLDR {
						foundTLDR = true
					}
				}
				return foundCass && foundTLDR
			},
		},
		{
			name:     "nil old config returns all as changes",
			oldCfg:   nil,
			newCfg:   createTestConfig(),
		},
		{
			name:     "nil new config returns no changes",
			oldCfg:   createTestConfig(),
			newCfg:   nil,
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			changes := detector.DetectChanges(tt.oldCfg, tt.newCfg)

			if len(changes) != tt.expected {
				t.Errorf("expected %d changes, got %d", tt.expected, len(changes))
			}

			if tt.check != nil && !tt.check(changes) {
				t.Errorf("changes validation failed: %+v", changes)
			}
		})
	}
}

	detector := NewConfigChangeDetector()

	tests := []struct {
		name     string
		expected bool
	}{
		{
			name:     "no change",
			expected: false,
		},
		{
			name:     "timeout changed",
			expected: true,
		},
		{
			name:     "enabled changed",
			expected: true,
		},
		{
			name:     "query limit changed",
			expected: true,
		},
		{
			name:     "history days changed",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestConfigChangeDetector_tldrChanged(t *testing.T) {
	detector := NewConfigChangeDetector()

	tests := []struct {
		name     string
		old      TLDRConfig
		new      TLDRConfig
		expected bool
	}{
		{
			name:     "no change",
			old:      TLDRConfig{Enabled: true, Timeout: 10 * time.Second},
			new:      TLDRConfig{Enabled: true, Timeout: 10 * time.Second},
			expected: false,
		},
		{
			name:     "timeout changed",
			old:      TLDRConfig{Timeout: 10 * time.Second},
			new:      TLDRConfig{Timeout: 20 * time.Second},
			expected: true,
		},
		{
			name:     "BaseURL changed",
			old:      TLDRConfig{BaseURL: "http://localhost:3000"},
			new:      TLDRConfig{BaseURL: "http://localhost:3001"},
			expected: true,
		},
		{
			name:     "max files changed",
			old:      TLDRConfig{MaxFiles: 50},
			new:      TLDRConfig{MaxFiles: 100},
			expected: true,
		},
		{
			name:     "max tokens changed",
			old:      TLDRConfig{MaxTokens: 100000},
			new:      TLDRConfig{MaxTokens: 200000},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := detector.tldrChanged(tt.old, tt.new)
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestConfigChangeDetector_gptCacheChanged(t *testing.T) {
	detector := NewConfigChangeDetector()

	tests := []struct {
		name     string
		old      GPTCacheConfig
		new      GPTCacheConfig
		expected bool
	}{
		{
			name:     "no change",
			old:      GPTCacheConfig{Enabled: true, ServerURL: "http://localhost:8080"},
			new:      GPTCacheConfig{Enabled: true, ServerURL: "http://localhost:8080"},
			expected: false,
		},
		{
			name:     "enabled changed",
			old:      GPTCacheConfig{Enabled: true},
			new:      GPTCacheConfig{Enabled: false},
			expected: true,
		},
		{
			name:     "server URL changed",
			old:      GPTCacheConfig{ServerURL: "http://localhost:8080"},
			new:      GPTCacheConfig{ServerURL: "http://localhost:8081"},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := detector.gptCacheChanged(tt.old, tt.new)
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestConfigChangeDetector_mcpChanged(t *testing.T) {
	detector := NewConfigChangeDetector()

	tests := []struct {
		name     string
		old      MCPConfig
		new      MCPConfig
		expected bool
	}{
		{
			name:     "no change",
			old:      MCPConfig{Enabled: true, URL: "ws://localhost:8765/ws", Port: 8765},
			new:      MCPConfig{Enabled: true, URL: "ws://localhost:8765/ws", Port: 8765},
			expected: false,
		},
		{
			name:     "enabled changed",
			old:      MCPConfig{Enabled: true},
			new:      MCPConfig{Enabled: false},
			expected: true,
		},
		{
			name:     "URL changed",
			old:      MCPConfig{URL: "ws://localhost:8765/ws"},
			new:      MCPConfig{URL: "ws://localhost:8766/ws"},
			expected: true,
		},
		{
			name:     "port changed",
			old:      MCPConfig{Port: 8765},
			new:      MCPConfig{Port: 8766},
			expected: true,
		},
		{
			name:     "reconnect interval changed",
			old:      MCPConfig{ReconnectInterval: 5 * time.Second},
			new:      MCPConfig{ReconnectInterval: 10 * time.Second},
			expected: true,
		},
		{
			name:     "heartbeat interval changed",
			old:      MCPConfig{HeartbeatInterval: 30 * time.Second},
			new:      MCPConfig{HeartbeatInterval: 60 * time.Second},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := detector.mcpChanged(tt.old, tt.new)
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

// Helper functions

func createTestConfig() *Config {
	return &Config{
		Intelligence: IntelligenceConfig{
				Enabled:     true,
				Timeout:     5 * time.Second,
				QueryLimit:  10,
				HistoryDays: 30,
			},
			TLDR: TLDRConfig{
				Enabled:   true,
				BaseURL:   "http://localhost:8787",
				Timeout:   10 * time.Second,
				MaxFiles:  50,
				MaxTokens: 100000,
			},
			GPTCache: GPTCacheConfig{
				Enabled:   true,
				ServerURL: "http://localhost:8788",
			},
		},
		MCP: MCPConfig{
			Enabled:              true,
			URL:                  "ws://localhost:8765/ws",
			Port:                 8765,
			BaseURL:              "http://localhost:8765",
			ReconnectInterval:    5 * time.Second,
			MaxReconnectAttempts: 10,
			HeartbeatInterval:    30 * time.Second,
			FileReservationTTL:   30 * time.Minute,
		},
	}
}

func modifyConfig(cfg *Config, modifier func(*Config)) *Config {
	// Create a copy to avoid modifying the original
	cloned := cfg.Clone()
	modifier(cloned)
	return cloned
}
