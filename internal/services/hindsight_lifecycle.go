package services

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// HindsightLifecycle manages the Hindsight server lifecycle including
// startup, monitoring, and restart on memory pressure or idle timeout.
type HindsightLifecycle struct {
	mu            sync.RWMutex
	ctx           context.Context
	cancel        context.CancelFunc
	process       *os.Process
	port          int
	lastActivity  time.Time
	idleTimeout   time.Duration
	maxMemoryMB   int64
	checkInterval time.Duration
	isRunning     bool
	apiKey        string
	provider      string
	model         string
	baseURL       string
}

// HindsightLifecycleConfig holds configuration for the lifecycle manager
type HindsightLifecycleConfig struct {
	Port          int           // Hindsight server port (default: 9418)
	IdleTimeout   time.Duration // Restart after idle for this duration (default: 5m)
	MaxMemoryMB   int64         // Restart if memory exceeds this (default: 8192 = 8GB)
	CheckInterval time.Duration // How often to check memory/idle (default: 30s)
	APIKey        string        // LLM API key
	Provider      string        // LLM provider (default: openai)
	Model         string        // LLM model (default: kimi-k2-5)
	BaseURL       string        // LLM base URL
}

// NewHindsightLifecycle creates a new Hindsight lifecycle manager
func NewHindsightLifecycle(config HindsightLifecycleConfig) *HindsightLifecycle {
	ctx, cancel := context.WithCancel(context.Background())

	// Set defaults
	if config.Port == 0 {
		config.Port = 8888
	}
	if config.IdleTimeout == 0 {
		config.IdleTimeout = 5 * time.Minute
	}
	if config.MaxMemoryMB == 0 {
		config.MaxMemoryMB = 8192 // 8GB default
	}
	if config.CheckInterval == 0 {
		config.CheckInterval = 30 * time.Second
	}
	if config.Provider == "" {
		config.Provider = "openai"
	}
	if config.Model == "" {
		config.Model = "kimi-k2-5"
	}

	return &HindsightLifecycle{
		ctx:           ctx,
		cancel:        cancel,
		port:          config.Port,
		idleTimeout:   config.IdleTimeout,
		maxMemoryMB:   config.MaxMemoryMB,
		checkInterval: config.CheckInterval,
		apiKey:        config.APIKey,
		provider:      config.Provider,
		model:         config.Model,
		baseURL:       config.BaseURL,
		lastActivity:  time.Now(),
	}
}

// Start starts the Hindsight server and begins monitoring
func (h *HindsightLifecycle) Start() error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.isRunning {
		return nil
	}

	log.Printf("[HindsightLifecycle] Starting Hindsight server on port %d...", h.port)

	// Start the Hindsight server process
	if err := h.startServer(); err != nil {
		return fmt.Errorf("failed to start Hindsight server: %w", err)
	}

	h.isRunning = true
	h.lastActivity = time.Now()

	// Start monitoring goroutine
	go h.monitor()

	log.Printf("[HindsightLifecycle] Hindsight server started successfully (PID: %d)", h.process.Pid)
	return nil
}

// startServer starts the Hindsight server process
func (h *HindsightLifecycle) startServer() error {
	// Prepare command
	cmd := exec.CommandContext(h.ctx, "hindsight-api",
		"--host", "127.0.0.1",
		"--port", strconv.Itoa(h.port),
		"--log-level", "warning",
	)

	// Set up environment
	cmd.Env = os.Environ()
	cmd.Env = append(cmd.Env, fmt.Sprintf("HINDSIGHT_API_LLM_PROVIDER=%s", h.provider))
	cmd.Env = append(cmd.Env, fmt.Sprintf("HINDSIGHT_API_LLM_API_KEY=%s", h.apiKey))
	cmd.Env = append(cmd.Env, fmt.Sprintf("HINDSIGHT_API_LLM_MODEL=%s", h.model))
	if h.baseURL != "" {
		cmd.Env = append(cmd.Env, fmt.Sprintf("HINDSIGHT_API_LLM_BASE_URL=%s", h.baseURL))
	}

	// Create new process group
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}

	// Start the process
	if err := cmd.Start(); err != nil {
		return err
	}

	h.process = cmd.Process

	// Wait a moment for server to initialize
	time.Sleep(3 * time.Second)

	// Verify it's running
	if !h.isServerHealthy() {
		h.stopServer()
		return fmt.Errorf("Hindsight server failed health check")
	}

	return nil
}

// stopServer stops the Hindsight server process
func (h *HindsightLifecycle) stopServer() {
	if h.process == nil {
		return
	}

	log.Printf("[HindsightLifecycle] Stopping Hindsight server (PID: %d)...", h.process.Pid)

	// Kill the entire process group
	syscall.Kill(-h.process.Pid, syscall.SIGTERM)

	// Wait for graceful shutdown
	done := make(chan struct{})
	go func() {
		h.process.Wait()
		close(done)
	}()

	select {
	case <-done:
		log.Printf("[HindsightLifecycle] Hindsight server stopped gracefully")
	case <-time.After(10 * time.Second):
		// Force kill
		syscall.Kill(-h.process.Pid, syscall.SIGKILL)
		log.Printf("[HindsightLifecycle] Hindsight server force killed")
	}

	h.process = nil
	h.isRunning = false
}

// Restart restarts the Hindsight server
func (h *HindsightLifecycle) Restart() error {
	h.mu.Lock()
	defer h.mu.Unlock()

	log.Printf("[HindsightLifecycle] Restarting Hindsight server...")

	// Stop current server
	h.stopServer()

	// Small delay to ensure cleanup
	time.Sleep(2 * time.Second)

	// Start new server
	if err := h.startServer(); err != nil {
		return fmt.Errorf("failed to restart Hindsight server: %w", err)
	}

	h.isRunning = true
	h.lastActivity = time.Now()

	log.Printf("[HindsightLifecycle] Hindsight server restarted successfully")
	return nil
}

// Stop stops the Hindsight server and monitoring
func (h *HindsightLifecycle) Stop() {
	h.mu.Lock()
	defer h.mu.Unlock()

	if !h.isRunning {
		return
	}

	log.Printf("[HindsightLifecycle] Stopping Hindsight lifecycle manager...")

	h.cancel()
	h.stopServer()

	log.Printf("[HindsightLifecycle] Hindsight lifecycle manager stopped")
}

// RecordActivity updates the last activity timestamp
func (h *HindsightLifecycle) RecordActivity() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.lastActivity = time.Now()
}

// GetMemoryUsageMB returns the current memory usage in MB
func (h *HindsightLifecycle) GetMemoryUsageMB() int64 {
	h.mu.RLock()
	process := h.process
	h.mu.RUnlock()

	if process == nil {
		return 0
	}

	// Read RSS from /proc on Linux or use ps on macOS
	cmd := exec.Command("ps", "-o", "rss=", "-p", strconv.Itoa(process.Pid))
	output, err := cmd.Output()
	if err != nil {
		return 0
	}

	rssKB, err := strconv.ParseInt(strings.TrimSpace(string(output)), 10, 64)
	if err != nil {
		return 0
	}

	return rssKB / 1024 // Convert KB to MB
}

// isServerHealthy checks if the Hindsight server is responding
func (h *HindsightLifecycle) isServerHealthy() bool {
	// Try to connect to the health endpoint
	// This is a simple check - in production you might want a proper HTTP client
	cmd := exec.Command("curl", "-s", "-o", "/dev/null", "-w", "%{http_code}",
		fmt.Sprintf("http://127.0.0.1:%d/health", h.port))
	output, err := cmd.Output()
	if err != nil {
		return false
	}

	return strings.TrimSpace(string(output)) == "200"
}

// monitor continuously monitors Hindsight and restarts if needed
func (h *HindsightLifecycle) monitor() {
	ticker := time.NewTicker(h.checkInterval)
	defer ticker.Stop()

	for {
		select {
		case <-h.ctx.Done():
			return
		case <-ticker.C:
			h.checkAndRestartIfNeeded()
		}
	}
}

// checkAndRestartIfNeeded checks conditions and restarts if necessary
func (h *HindsightLifecycle) checkAndRestartIfNeeded() {
	h.mu.RLock()
	isRunning := h.isRunning
	lastActivity := h.lastActivity
	idleTimeout := h.idleTimeout
	maxMemoryMB := h.maxMemoryMB
	h.mu.RUnlock()

	if !isRunning {
		return
	}

	// Check if server is healthy first (also counts as activity)
	if h.isServerHealthy() {
		h.RecordActivity()
	} else {
		log.Printf("[HindsightLifecycle] Health check failed, restarting...")
		if err := h.Restart(); err != nil {
			log.Printf("[HindsightLifecycle] Failed to restart: %v", err)
		}
		return
	}

	// Check idle timeout
	idleDuration := time.Since(lastActivity)
	if idleDuration > idleTimeout {
		log.Printf("[HindsightLifecycle] Idle timeout reached (idle for %v), restarting...", idleDuration)
		if err := h.Restart(); err != nil {
			log.Printf("[HindsightLifecycle] Failed to restart: %v", err)
		}
		return
	}

	// Check memory usage
	memoryMB := h.GetMemoryUsageMB()
	if memoryMB > maxMemoryMB {
		log.Printf("[HindsightLifecycle] Memory limit exceeded (%d MB > %d MB), restarting...", memoryMB, maxMemoryMB)
		if err := h.Restart(); err != nil {
			log.Printf("[HindsightLifecycle] Failed to restart: %v", err)
		}
		return
	}
}

// IsRunning returns whether Hindsight is currently running
func (h *HindsightLifecycle) IsRunning() bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.isRunning
}

// GetLastActivity returns the time of last activity
func (h *HindsightLifecycle) GetLastActivity() time.Time {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.lastActivity
}

// GetStats returns current statistics
func (h *HindsightLifecycle) GetStats() map[string]interface{} {
	h.mu.RLock()
	defer h.mu.RUnlock()

	return map[string]interface{}{
		"is_running":    h.isRunning,
		"port":          h.port,
		"pid":           h.getPID(),
		"memory_mb":     h.GetMemoryUsageMB(),
		"last_activity": h.lastActivity,
		"idle_duration": time.Since(h.lastActivity).String(),
		"idle_timeout":  h.idleTimeout.String(),
		"max_memory_mb": h.maxMemoryMB,
	}
}

func (h *HindsightLifecycle) getPID() int {
	if h.process == nil {
		return 0
	}
	return h.process.Pid
}
