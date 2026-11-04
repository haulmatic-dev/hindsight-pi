package main

import (
	"context"
	"errors"
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

	"github.com/haulmatic-dev/hydra/internal/agents"
	"github.com/haulmatic-dev/hydra/internal/api"
	"github.com/haulmatic-dev/hydra/internal/core"
	"github.com/haulmatic-dev/hydra/internal/logx"
	"github.com/haulmatic-dev/hydra/pkg/types"
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
		configPath  = flag.String("config", "hydra.yaml", "Path to configuration file")
		debug       = flag.Bool("debug", false, "Enable debug logging")
		rpcSelftest = flag.String("rpc-selftest", "", "Run a one-shot RPC self-test with the given prompt and exit")
	)
	flag.Parse()

	// Subcommand fast path: `hydra gates verify <project>` dry-runs the
	// project's declared gates on the real checkout (bootstrap verification)
	// and exits — before any daemon machinery starts.
	if pos := flag.Args(); len(pos) > 0 && pos[0] == "gates" {
		runGatesSubcommand(*configPath, pos[1:])
		return
	}

	// Subcommand fast path: `hydra drain-quarantine [--apply] [<project>]`
	// cleans up hydra/quarantine/* branches deterministically and exits.
	if pos := flag.Args(); len(pos) > 0 && pos[0] == "drain-quarantine" {
		runDrainQuarantineSubcommand(*configPath, pos[1:])
		return
	}

	// Subcommand fast path: `hydra status` prints a compact fleet rollup
	// (workers, verdicts, boards, queue health) and exits.
	if pos := flag.Args(); len(pos) > 0 && pos[0] == "status" {
		runStatusSubcommand(*configPath)
		return
	}

	// Subcommand fast path: `hydra triage` runs one PR-triage pass over all
	// configured projects (group overlapping dogfood/* PRs, post advisory
	// comments) and exits — the operator's verification door for FR-030.
	if pos := flag.Args(); len(pos) > 0 && pos[0] == "triage" {
		runTriageSubcommand(*configPath)
		return
	}

	// Subcommand fast path: `hydra induce-skills` runs one skill-induction
	// pass over worker execution records and exits (staging-only output).
	if pos := flag.Args(); len(pos) > 0 && pos[0] == "induce-skills" {
		runInduceSkillsSubcommand(*configPath)
		return
	}

	// Configure leveled logging (HYDRA_LOG_LEVEL: debug|info|warn|error).
	logx.Init()
	if *debug {
		log.SetFlags(log.LstdFlags | log.Lshortfile)
		logx.SetLevel(logx.LevelDebug)
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
		// Duplicate project names are fail-closed: name-based resolution
		// silently picks the first match and chains integrate into the wrong
		// project. Degraded-default startup would hide the config error.
		var dupErr *core.DuplicateProjectNameError
		if errors.As(err, &dupErr) {
			log.Fatalf("Fatal: invalid configuration: %v", err)
		}
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

	// RPC self-test path: prove the Pi connector in-process, then exit.
	// This branch must never start the daemon.
	if *rpcSelftest != "" {
		code := runRPCSelfTest(context.Background(), *rpcSelftest, config)
		os.Exit(code)
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

	// Start skill induction loop (no-op unless intelligence.skill_induction.enabled)
	startSkillInductionLoop(ctx, config)

	// Start overseer (blocking)
	if err := overseer.Start(ctx); err != nil {
		log.Fatalf("Overseer failed: %v", err)
	}

	fmt.Println("👋 Hydra stopped")
}

// runRPCSelfTest executes one prompt through the Pi connector using Hydra's
// loaded configuration, reports the result, and returns an exit code.
func runRPCSelfTest(ctx context.Context, prompt string, config *core.Config) int {
	piConfig := agents.PiConfig{
		Provider:      config.Agents.Pi.Provider,
		Model:         config.Agents.Pi.Model,
		ThinkingLevel: config.Agents.Pi.ThinkingLevel,
		Timeout:       config.Agents.Pi.Timeout,
		ApiKeyName:    config.Agents.Pi.ApiKeyName,
		ConfigDir:     config.Pi.ConfigDir,
		AutoConfirm:   config.Agents.Pi.AutoConfirm(),
	}
	connector := agents.NewPiConnector(piConfig, log.Default())

	projectPath, _ := os.Getwd()

	session, err := connector.Start(ctx, types.Bead{ID: types.BeadID("hydra-selftest"), Title: "rpc-selftest"}, agents.AgentOptions{ProjectPath: projectPath})
	if err != nil {
		fmt.Printf("rpc-selftest: success=false tokens=0 duration=0s\n")
		fmt.Printf("start error: %v\n", err)
		return 1
	}
	defer func() {
		if err := connector.Cleanup(ctx, session); err != nil {
			log.Printf("rpc-selftest cleanup error: %v", err)
		}
	}()

	result, err := connector.Execute(ctx, session, prompt)
	if result == nil {
		result = &agents.AgentResult{Success: false, ExitCode: 1, Error: err.Error(), Duration: 0}
	}

	tokens := 0
	if stats, statsErr := connector.SessionStats(ctx, session); statsErr == nil {
		tokens = stats.Total
	}

	fmt.Printf("rpc-selftest: success=%v tokens=%d duration=%v\n", result.Success, tokens, result.Duration)
	if result.Output != "" {
		fmt.Println(result.Output)
	}
	if !result.Success && result.Error != "" {
		fmt.Printf("error: %s\n", result.Error)
	}

	if result.Success {
		return 0
	}
	return 1
}

// createConfigChangeHandler creates a handler that refreshes tool clients when config changes
func createConfigChangeHandler(overseer *core.Overseer) core.ConfigChangeHandler {
	return func(oldConfig, newConfig *core.Config) error {
		log.Printf("Config change detected, refreshing tool clients...")
		overseer.UpdateConfig(newConfig)
		return nil
	}
}
