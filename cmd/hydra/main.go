package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

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

	// Setup signal handling for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		fmt.Println("\n🛑 Received shutdown signal, stopping Hydra...")
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

	// Start overseer (blocking)
	if err := overseer.Start(ctx); err != nil {
		log.Fatalf("Overseer failed: %v", err)
	}

	fmt.Println("👋 Hydra stopped")
}
