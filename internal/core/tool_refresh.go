package core

import (
	"fmt"
	"log"
	"time"
)

// ToolType identifies which tool configuration has changed
type ToolType int

const (
	ToolNone ToolType = iota
	ToolBV
	ToolBeads
	ToolGPTCache
	ToolMCP
)

func (t ToolType) String() string {
	switch t {
	case ToolGPTCache:
		return "GPTCache"
	case ToolMCP:
		return "MCP"
	default:
		return fmt.Sprintf("Tool(%d)", t)
	}
}

// ToolConfigChange represents a specific tool configuration change
type ToolConfigChange struct {
	Tool          ToolType
	EnabledBefore bool
	EnabledAfter  bool
	HasChanged    bool // True if any config field changed
}

// IsTransition returns true if the tool's enabled state changed
func (c ToolConfigChange) IsTransition() bool {
	return c.EnabledBefore != c.EnabledAfter
}

// IsEnable returns true if the tool was just enabled
func (c ToolConfigChange) IsEnable() bool {
	return !c.EnabledBefore && c.EnabledAfter
}

// IsDisable returns true if the tool was just disabled
func (c ToolConfigChange) IsDisable() bool {
	return c.EnabledBefore && !c.EnabledAfter
}

// IsReconfigure returns true if the tool stayed enabled but config changed
func (c ToolConfigChange) IsReconfigure() bool {
	return c.EnabledBefore && c.EnabledAfter && c.HasChanged
}

// ConfigChangeDetector detects which tool configurations have changed between two configs
type ConfigChangeDetector struct{}

// NewConfigChangeDetector creates a new detector
func NewConfigChangeDetector() *ConfigChangeDetector {
	return &ConfigChangeDetector{}
}

// DetectChanges analyzes old and new config and returns list of changed tool configurations
func (d *ConfigChangeDetector) DetectChanges(oldCfg, newCfg *Config) []ToolConfigChange {
	if oldCfg == nil || newCfg == nil {
		return d.detectAllChanges(newCfg)
	}

	var changes []ToolConfigChange

	if d.gptCacheChanged(oldCfg.Intelligence.GPTCache, newCfg.Intelligence.GPTCache) {
		changes = append(changes, ToolConfigChange{
			Tool:          ToolGPTCache,
			EnabledBefore: oldCfg.Intelligence.GPTCache.Enabled,
			EnabledAfter:  newCfg.Intelligence.GPTCache.Enabled,
			HasChanged:    true,
		})
	}

	if d.mcpChanged(oldCfg.MCP, newCfg.MCP) {
		changes = append(changes, ToolConfigChange{
			Tool:          ToolMCP,
			EnabledBefore: oldCfg.MCP.Enabled,
			EnabledAfter:  newCfg.MCP.Enabled,
			HasChanged:    true,
		})
	}

	return changes
}

// detectAllChanges returns all tool configs as changed (for initial load)
func (d *ConfigChangeDetector) detectAllChanges(cfg *Config) []ToolConfigChange {
	if cfg == nil {
		return nil
	}

	return []ToolConfigChange{
		{
			Tool:          ToolGPTCache,
			EnabledBefore: false,
			EnabledAfter:  cfg.Intelligence.GPTCache.Enabled,
			HasChanged:    true,
		},
		{
			Tool:          ToolMCP,
			EnabledBefore: false,
			EnabledAfter:  cfg.MCP.Enabled,
			HasChanged:    true,
		},
	}
}

// gptCacheChanged checks if GPTCache config has changed
func (d *ConfigChangeDetector) gptCacheChanged(old, new GPTCacheConfig) bool {
	return old.Enabled != new.Enabled ||
		old.ServerURL != new.ServerURL
}

// mcpChanged checks if MCP config has changed
func (d *ConfigChangeDetector) mcpChanged(old, new MCPConfig) bool {
	return old.Enabled != new.Enabled ||
		old.URL != new.URL ||
		old.Port != new.Port ||
		old.BaseURL != new.BaseURL ||
		old.ReconnectInterval != new.ReconnectInterval ||
		old.MaxReconnectAttempts != new.MaxReconnectAttempts ||
		old.HeartbeatInterval != new.HeartbeatInterval ||
		old.FileReservationTTL != new.FileReservationTTL
}

// RefreshHandler creates and manages tool clients based on config changes
type RefreshHandler struct {
	gptCache *GPTCacheClient
	config   *Config
	logger   *log.Logger
}

// NewRefreshHandler creates a new refresh handler
func NewRefreshHandler(config *Config, logger *log.Logger) *RefreshHandler {
	if logger == nil {
		logger = log.Default()
	}
	return &RefreshHandler{
		config: config,
		logger: logger,
	}
}

// ApplyChange applies a single tool configuration change
func (h *RefreshHandler) ApplyChange(change ToolConfigChange) {
	switch change.Tool {
	case ToolGPTCache:
		h.refreshGPTCache(change)
	}
}

// refreshTLDR handles TLDR client refresh

// refreshGPTCache handles GPTCache client refresh
func (h *RefreshHandler) refreshGPTCache(change ToolConfigChange) {
	cfg := h.config.Intelligence.GPTCache

	if change.IsDisable() {
		h.gptCache = nil
		h.logger.Printf("[Intelligence] %s client disabled", change.Tool)
		return
	}

	h.gptCache = NewGPTCacheClient(cfg.ServerURL, 5*time.Second)

	h.logRefresh(change.Tool.String(), change.IsReconfigure(),
		"server", cfg.ServerURL,
	)
}

// logRefresh logs a standardized refresh message
func (h *RefreshHandler) logRefresh(toolName string, wasReconfigure bool, keyValues ...interface{}) {
	var action string
	if wasReconfigure {
		action = "reinitialized"
	} else {
		action = "initialized"
	}

	// Build key=value pairs
	var pairs string
	for i := 0; i < len(keyValues); i += 2 {
		if i > 0 {
			pairs += ", "
		}
		key := keyValues[i]
		value := keyValues[i+1]
		pairs += fmt.Sprintf("%v: %v", key, value)
	}

	h.logger.Printf("[Intelligence] %s client %s (%s)", toolName, action, pairs)
}

// GetGPTCacheClient returns the current GPTCache client
func (h *RefreshHandler) GetGPTCacheClient() *GPTCacheClient {
	return h.gptCache
}

// SetClients allows injection of existing clients (for testing or initialization)
func (h *RefreshHandler) SetClients(gpt *GPTCacheClient) {
	h.gptCache = gpt
}
