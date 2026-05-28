package core

import (
	"fmt"
	"log"

	"github.com/haulmatic-dev/hydra/internal/pi"
)

// PiSetupManager initializes and runs the Pi ecosystem setup.
type PiSetupManager struct {
	config *Config
}

// NewPiSetupManager creates a new Pi setup manager.
func NewPiSetupManager(config *Config) *PiSetupManager {
	return &PiSetupManager{config: config}
}

// RunSetup initializes and runs the Pi ecosystem setup.
func (m *PiSetupManager) RunSetup() error {
	log.Println("[Overseer] Initializing Pi ecosystem setup...")

	configDir := m.config.Pi.ConfigDir
	piConfigDir := m.config.Pi.PiConfigDir
	if configDir == "" {
		configDir = ""
	}
	if piConfigDir == "" {
		piConfigDir = ".pi/hydra-config"
	}
	setupManager := pi.NewSetupManager(configDir, piConfigDir)

	result, err := setupManager.RunSetup()
	if err != nil {
		return fmt.Errorf("pi setup failed: %w", err)
	}

	if result.PiInstalled {
		log.Println("[Overseer] ✅ Pi agent ready")
	} else {
		log.Println("[Overseer] ⚠️  Pi agent not installed")
	}

	if result.LSPInstalled {
		log.Println("[Overseer] ✅ LSP servers ready")
	} else {
		log.Println("[Overseer] ⚠️  LSP servers may be missing")
	}

	if result.SkillsInstalled {
		log.Println("[Overseer] ✅ Pi skills ready")
	} else {
		log.Println("[Overseer] ⚠️  Pi skills may be missing")
	}

	if len(result.Errors) > 0 {
		log.Printf("[Overseer] Pi setup completed with %d errors", len(result.Errors))
		for _, err := range result.Errors {
			log.Printf("[Overseer]   - %s", err)
		}
	}

	return nil
}
