package core

import (
	"log"
	"reflect"
	"time"

	"github.com/haulmatic-dev/hydra/pkg/types"
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
	toolChanges := detector.DetectChanges(o.config, newConfig)

	// Discover and diff projects from the new config so project list changes
	// are applied without requiring a daemon restart.
	newProjects, err := cm.discoverProjects(newConfig)
	if err != nil {
		log.Printf("[Config] Project discovery failed during hot reload: %v", err)
		return err
	}
	added, removed, projectsChanged := diffProjects(o.projects, newProjects)

	if len(toolChanges) == 0 && !projectsChanged {
		log.Printf("[Overseer] No configuration changes detected")
		return nil
	}

	if len(toolChanges) > 0 {
		log.Printf("[Overseer] Detected %d tool configuration changes", len(toolChanges))
	}
	if projectsChanged {
		log.Printf("[Config] Detected project list changes: +%d, -%d", len(added), len(removed))
	}

	// Update the config reference
	o.config = newConfig

	// Refresh affected clients
	if len(toolChanges) > 0 {
		cm.RefreshToolClients(toolChanges)
	}

	// Update operational parameters
	o.stateController.SetMaxWorkers(newConfig.Overseer.MaxWorkers)
	o.concurrencyGovernor.SetMaxWorkers(newConfig.Overseer.MaxWorkers)
	o.stateController.SetBeadDiscoveryInterval(newConfig.Overseer.BeadDiscoveryInterval)

	// Rebuild project-scoped state for added/removed projects without
	// touching in-flight workers.
	if projectsChanged {
		o.projects = newProjects

		if o.beadFileReader != nil {
			o.beadFileReader.SetProjects(newProjects)
		}
		if o.beadAnalyzer != nil {
			o.beadAnalyzer.SetProjects(newProjects)
		}
		if o.beadDiscovery != nil {
			o.beadDiscovery.SetProjects(newProjects)
		}
		if o.projectResolver != nil {
			o.projectResolver.SetProjects(newProjects)
		}
		if o.chainRegistrar != nil && len(added) > 0 {
			o.chainRegistrar.RegisterFromProjects(added)
		}
		if o.intelligenceHub != nil && len(added) > 0 {
			o.intelligenceHub.QueueInitialIndexing(added)
		}

		logProjectListChange(added, removed)
	}

	log.Printf("[Config] Configuration updated successfully")
	return nil
}

// discoverProjects resolves the project list from a config.
func (cm *ConfigManager) discoverProjects(cfg *Config) ([]types.Project, error) {
	scanner := NewProjectScanner(cfg)
	if err := scanner.Discover(); err != nil {
		return nil, err
	}
	return scanner.Projects(), nil
}

// diffProjects compares the old and new project lists and returns added and
// removed projects using the project name as the stable key. A project whose
// details changed while its name stayed the same is reported as both removed
// and added so downstream state is rebuilt.
func diffProjects(oldProjects, newProjects []types.Project) (added, removed []types.Project, changed bool) {
	oldByName := make(map[string]types.Project, len(oldProjects))
	for _, p := range oldProjects {
		oldByName[p.Name] = p
	}
	newByName := make(map[string]types.Project, len(newProjects))
	for _, p := range newProjects {
		newByName[p.Name] = p
	}

	for name, p := range newByName {
		oldP, exists := oldByName[name]
		if !exists {
			added = append(added, p)
			continue
		}
		if !reflect.DeepEqual(oldP, p) {
			added = append(added, p)
			removed = append(removed, oldP)
		}
	}
	for name, p := range oldByName {
		if _, ok := newByName[name]; !ok {
			removed = append(removed, p)
		}
	}

	return added, removed, len(added) > 0 || len(removed) > 0 || len(oldProjects) != len(newProjects)
}

func logProjectListChange(added, removed []types.Project) {
	for _, p := range added {
		log.Printf("[Config] Added project: %s (%s)", p.Name, p.Path)
	}
	for _, p := range removed {
		log.Printf("[Config] Removed project: %s (%s)", p.Name, p.Path)
	}
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
	case ToolGPTCache:
		cm.refreshGPTCacheClient(change)
	case ToolMCP:
		cm.refreshMCPClient(change)
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

	if !change.EnabledAfter {
		if err := o.mcpManager.Stop(); err != nil {
			log.Printf("[MCP] Error stopping: %v", err)
		}
		log.Printf("[MCP] Client disabled")
		return
	}

	if err := o.mcpManager.Initialize(&o.config.MCP); err != nil {
		log.Printf("[MCP] Error reinitializing client: %v", err)
		return
	}

	log.Printf("[MCP] Client reinitialized")
}
