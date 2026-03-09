package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/haulmatic-dev/hydra/internal/api"
	"github.com/haulmatic-dev/hydra/internal/core"
)

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

	// Load configuration
	config, err := core.LoadConfig(*configPath)
	if err != nil {
		log.Printf("Warning: Could not load config: %v", err)
		log.Println("Using default configuration")
		config = core.DefaultConfig()
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
