package core

import (
	"log"

	"github.com/haulmatic-dev/hydra/internal/pi"
)

// SkillUpdaterInitializer creates and configures the skill updater.
type SkillUpdaterInitializer struct {
	config       *Config
	skillUpdater **pi.SkillUpdater
}

// NewSkillUpdaterInitializer creates a new initializer.
func NewSkillUpdaterInitializer(config *Config, skillUpdater **pi.SkillUpdater) *SkillUpdaterInitializer {
	return &SkillUpdaterInitializer{
		config:       config,
		skillUpdater: skillUpdater,
	}
}

// Initialize creates and configures the skill updater.
func (i *SkillUpdaterInitializer) Initialize() error {
	configDir := i.config.Pi.ConfigDir
	if configDir == "" {
		configDir = "./pi-config"
	}

	updateInterval := i.config.Pi.Skills.UpdateInterval
	if updateInterval == "" {
		updateInterval = "24h"
	}

	setupManager := pi.NewSetupManager(configDir, configDir)
	skillUpdater, err := pi.NewSkillUpdater(setupManager, updateInterval)
	if err != nil {
		return &ServiceError{Op: "create skill updater", Err: err}
	}

	*i.skillUpdater = skillUpdater
	log.Printf("[SkillUpdater] Initialized with interval: %s", updateInterval)
	return nil
}
