package core

import (
	"log"
	"time"

	"github.com/haulmatic-dev/hydra/internal/intelligence"
	"github.com/haulmatic-dev/hydra/internal/mcp"
)

// ConfigManager handles dynamic configuration changes for the overseer.
type ConfigManager struct {
	overseer *Overseer
}

// NewConfigManager creates a new config manager.
func NewConfigManager(overseer *Overseer) *ConfigManager {
	return &ConfigManager{overseer: overseer}
}

// UpdateConfig applies a new configuration to the overseer.
func (cm *ConfigManager) UpdateConfig(newConfig *Config) error {
	o := cm.overseer

	if newConfig == nil {
		log.Printf("[Overseer] Cannot update with nil config")
		return nil
	}

	// Detect which tool configs have changed
	detector := NewConfigChangeDetector()
	changes := detector.DetectChanges(o.config, newConfig)

	if len(changes) == 0 {
		log.Printf("[Overseer] No tool configuration changes detected")
		return nil
	}

	log.Printf("[Overseer] Detected %d tool configuration changes", len(changes))

	// Update the config reference
	o.config = newConfig

	// Refresh affected clients
	cm.RefreshToolClients(changes)

	// Update operational parameters
	o.stateController.SetMaxWorkers(newConfig.Overseer.MaxWorkers)
	o.stateController.SetBeadDiscoveryInterval(newConfig.Overseer.BeadDiscoveryInterval)

	log.Printf("[Config] Configuration updated successfully")
	return nil
}

// RefreshToolClients reinitializes tools that have changed.
func (cm *ConfigManager) RefreshToolClients(changes []ToolConfigChange) {
	for _, change := range changes {
		cm.applyToolConfigChange(change)
	}
}

func (cm *ConfigManager) applyToolConfigChange(change ToolConfigChange) {
	log.Printf("[Config] Tool %s config changed (enabled: %v -> %v)",
		change.Tool, change.EnabledBefore, change.EnabledAfter)

	switch change.Tool {
	case ToolTLDR:
		cm.refreshTLDRClient(change)
	case ToolGPTCache:
		cm.refreshGPTCacheClient(change)
	case ToolMCP:
		cm.refreshMCPClient(change)
	}
}

func (cm *ConfigManager) refreshTLDRClient(change ToolConfigChange) {
	o := cm.overseer
	o.intelMutex.Lock()
	defer o.intelMutex.Unlock()

	config := o.config.Intelligence.TLDR

	if !config.Enabled {
		if o.tldrClient != nil {
			o.tldrClient = nil
			log.Printf("[Intelligence] TLDR client disabled")
		}
		return
	}

	o.tldrClient = intelligence.NewTLDRClient(
		config.BaseURL,
		true,
		"llm-tldr",
		"",
		config.Timeout,
	)

	if change.EnabledBefore && change.EnabledAfter {
		log.Printf("[Intelligence] TLDR client reinitialized")
	} else {
		log.Printf("[Intelligence] TLDR client initialized")
	}
}

func (cm *ConfigManager) refreshGPTCacheClient(change ToolConfigChange) {
	o := cm.overseer
	o.intelMutex.Lock()
	defer o.intelMutex.Unlock()

	config := o.config.Intelligence.GPTCache

	if !config.Enabled {
		if o.gptCache != nil {
			o.gptCache = nil
			log.Printf("[Intelligence] GPTCache client disabled")
		}
		return
	}

	o.gptCache = NewGPTCacheClient(config.ServerURL, 5*time.Second)

	if change.EnabledBefore && change.EnabledAfter {
		log.Printf("[Intelligence] GPTCache client reinitialized")
	} else {
		log.Printf("[Intelligence] GPTCache client initialized")
	}
}

func (cm *ConfigManager) refreshMCPClient(change ToolConfigChange) {
	o := cm.overseer
	o.mcpMu.Lock()
	defer o.mcpMu.Unlock()

	if !change.EnabledAfter {
		if o.mcpClient != nil {
			if err := o.mcpClient.Disconnect(); err != nil {
				log.Printf("[MCP] Error disconnecting: %v", err)
			}
			o.mcpClient = nil
			log.Printf("[MCP] Client disabled")
		}
		return
	}

	agentID := "hydra-overseer-" + time.Now().Format("20060102-150405")
	client, err := mcp.NewHTTPClient(o.config.MCP.URL, agentID)
	if err != nil {
		log.Printf("[MCP] Error creating client: %v", err)
		return
	}

	o.mcpClient = client
	log.Printf("[MCP] Client reinitialized")
}
