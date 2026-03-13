package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/haulmatic-dev/hydra/internal/api"
	"github.com/haulmatic-dev/hydra/internal/core"
)

// sourceShellEnv sources environment variables from the user's shell configuration
// This allows Hydra to access environment variables defined in .zshrc/.bashrc when running as a daemon
func sourceShellEnv() {
	// Determine which shell to use
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/zsh" // Default to zsh on macOS
	}

	// Check if we're already in a shell with the variables loaded
	if os.Getenv("KIMI_API_KEY") != "" {
		log.Printf("[Setup] Environment already contains KIMI_API_KEY, skipping shell sourcing")
		return
	}

	log.Printf("[Setup] Sourcing environment from %s...", shell)

	// Determine shell-specific config files
	var configCmd string
	switch {
	case strings.Contains(shell, "zsh"):
		configCmd = "source ~/.zshrc 2>/dev/null || source ~/.zprofile 2>/dev/null || source ~/.zshenv 2>/dev/null"
	case strings.Contains(shell, "bash"):
		configCmd = "source ~/.bashrc 2>/dev/null || source ~/.bash_profile 2>/dev/null || source ~/.profile 2>/dev/null"
	case strings.Contains(shell, "fish"):
		configCmd = "source ~/.config/fish/config.fish 2>/dev/null || source ~/.fishrc 2>/dev/null"
	default:
		// Generic fallback - try common locations
		configCmd = "source ~/.profile 2>/dev/null || source ~/.bashrc 2>/dev/null || source ~/.zshrc 2>/dev/null"
	}

	// Run shell as login shell and print environment
	// Using 'env -0' to get null-separated output for proper parsing
	fullCmd := fmt.Sprintf("%s; env -0", configCmd)
	cmd := exec.Command(shell, "-c", fullCmd)
	output, err := cmd.Output()
	if err != nil {
		log.Printf("[Setup] Warning: Could not source shell environment: %v", err)
		return
	}

	// Parse null-separated environment variables
	envVars := strings.Split(string(output), "\x00")
	importedCount := 0
	for _, envVar := range envVars {
		if envVar == "" {
			continue
		}
		parts := strings.SplitN(envVar, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := parts[0]
		value := parts[1]

		// Only import if not already set in current environment
		if os.Getenv(key) == "" && value != "" {
			os.Setenv(key, value)
			importedCount++
			// Log important keys
			if strings.Contains(strings.ToLower(key), "api_key") || strings.Contains(strings.ToLower(key), "token") {
				log.Printf("[Setup] Imported %s (%d chars)", key, len(value))
			}
		}
	}

	log.Printf("[Setup] Imported %d environment variables from shell", importedCount)
}

// setupPath ensures Hydra's bin directory is in PATH for wrapper scripts
func setupPath() {
	// Get the directory of the current executable (hydra binary)
	execPath, err := os.Executable()
	if err != nil {
		log.Printf("[Setup] Warning: Could not determine executable path: %v", err)
		return
	}

	// Get the bin directory (where hydra binary is located)
	binDir := filepath.Dir(execPath)

	// Get current PATH
	currentPath := os.Getenv("PATH")
	pathEntries := strings.Split(currentPath, string(filepath.ListSeparator))

	// Check if binDir is already in PATH
	for _, entry := range pathEntries {
		if entry == binDir {
			log.Printf("[Setup] Hydra bin directory already in PATH: %s", binDir)
			return
		}
	}

	// Prepend binDir to PATH
	newPath := binDir + string(filepath.ListSeparator) + currentPath
	os.Setenv("PATH", newPath)
	log.Printf("[Setup] Added Hydra bin directory to PATH: %s", binDir)
}

func main() {
	var (
		configPath = flag.String("config", "hydra.yaml", "Path to configuration file")
		debug      = flag.Bool("debug", false, "Enable debug logging")
	)
	flag.Parse()

	if *debug {
		log.SetFlags(log.LstdFlags | log.Lshortfile)
	}

	fmt.Println("🐍 Hydra - Agent Swarm Orchestrator")
	fmt.Println("====================================")

	// Source environment variables from shell (for API keys when running as daemon)
	sourceShellEnv()

	// Ensure Hydra bin directory is in PATH for wrapper scripts
	setupPath()

	// Load configuration
	config, err := core.LoadConfig(*configPath)
	if err != nil {
		log.Printf("Warning: Could not load config: %v", err)
		log.Println("Using default configuration")
		config = core.DefaultConfig()
	}

	// Set API key from config for Pi agents
	if config.Agents.Pi.ApiKeyName != "" {
		apiKey := os.Getenv(config.Agents.Pi.ApiKeyName)
		if apiKey == "" {
			log.Printf("[Setup] Warning: API key %s not found in environment", config.Agents.Pi.ApiKeyName)
		} else {
			log.Printf("[Setup] API key %s loaded (%d chars)", config.Agents.Pi.ApiKeyName, len(apiKey))
		}
	}

	// Create overseer
	overseer := core.NewOverseer(config)

	// Create API server
	apiServer := api.NewServer(config, overseer)

	// Create config watcher for dynamic tool client refresh
	var configWatcher *core.ConfigWatcher
	if *configPath != "" {
		var err error
		// Create custom file watcher with desired settings
		fileWatcher, err := core.NewPollingFileWatcher(
			*configPath,
			func() error { return nil }, // Handler will be called by ConfigWatcher
			core.WithPollingInterval(5*time.Second),
			core.WithDebounceDelay(500*time.Millisecond),
		)
		if err != nil {
			log.Printf("Warning: Could not create file watcher: %v", err)
		} else {
			configWatcher, err = core.NewConfigWatcher(
				*configPath,
				createConfigChangeHandler(overseer),
				core.WithFileWatcher(fileWatcher),
			)
			if err != nil {
				log.Printf("Warning: Could not create config watcher: %v", err)
			}
		}
	}

	// Setup signal handling for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		fmt.Println("\n🛑 Received shutdown signal, stopping Hydra...")
		if configWatcher != nil {
			configWatcher.Stop()
		}
		cancel()
	}()

	// Start API server in goroutine
	if apiServer != nil {
		go func() {
			if err := apiServer.Start(ctx); err != nil {
				log.Printf("API server error: %v", err)
			}
		}()
	}

	// Start config watcher for dynamic tool client refresh
	if configWatcher != nil {
		if err := configWatcher.Start(); err != nil {
			log.Printf("Warning: Could not start config watcher: %v", err)
		} else {
			log.Printf("Config watcher started: %s", *configPath)
		}
	}

	// Start overseer (blocking)
	if err := overseer.Start(ctx); err != nil {
		log.Fatalf("Overseer failed: %v", err)
	}

	fmt.Println("👋 Hydra stopped")
}

// createConfigChangeHandler creates a handler that refreshes tool clients when config changes
func createConfigChangeHandler(overseer *core.Overseer) core.ConfigChangeHandler {
	return func(oldConfig, newConfig *core.Config) error {
		log.Printf("Config change detected, refreshing tool clients...")
		overseer.UpdateConfig(newConfig)
		return nil
	}
}
