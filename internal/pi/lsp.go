package pi

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// LSPPackage represents an LSP package to install
type LSPPackage struct {
	Name        string
	Command     string
	NPMName     string
	Description string
}

// LSPComponent represents the installation status of an LSP component
type LSPComponent struct {
	Name      string
	Command   string
	Installed bool
}

// RequiredLSPPackages defines the LSP packages that need to be installed
var RequiredLSPPackages = []LSPPackage{
	{
		Name:        "LSP CLI",
		Command:     "lsp",
		NPMName:     "@pi/lsp-cli",
		Description: "Pi LSP CLI tool",
	},
	{
		Name:        "gopls",
		Command:     "gopls",
		NPMName:     "@pi/gopls-wrapper",
		Description: "Go language server",
	},
	{
		Name:        "typescript-language-server",
		Command:     "typescript-language-server",
		NPMName:     "typescript-language-server",
		Description: "TypeScript language server",
	},
}

// LSPCache provides thread-safe caching for LSP installation status
type LSPCache struct {
	mu      sync.RWMutex
	entries map[string]*cacheEntry
	ttl     time.Duration
	hits    atomic.Uint64
	misses  atomic.Uint64
}

type cacheEntry struct {
	value     bool
	expiresAt time.Time
}

// NewLSPCache creates a new LSP cache with the specified TTL
func NewLSPCache(ttl time.Duration) *LSPCache {
	return &LSPCache{
		entries: make(map[string]*cacheEntry),
		ttl:     ttl,
	}
}

// Get retrieves a cached value if it exists and hasn't expired
func (c *LSPCache) Get(key string) (bool, bool) {
	c.mu.RLock()
	entry, exists := c.entries[key]
	c.mu.RUnlock()

	if !exists {
		c.misses.Add(1)
		return false, false
	}

	if time.Now().After(entry.expiresAt) {
		c.mu.Lock()
		delete(c.entries, key)
		c.mu.Unlock()
		c.misses.Add(1)
		return false, false
	}

	c.hits.Add(1)
	return entry.value, true
}

// Set stores a value in the cache
func (c *LSPCache) Set(key string, value bool) {
	c.mu.Lock()
	c.entries[key] = &cacheEntry{
		value:     value,
		expiresAt: time.Now().Add(c.ttl),
	}
	c.mu.Unlock()
}

// Invalidate removes a specific entry from the cache
func (c *LSPCache) Invalidate(key string) {
	c.mu.Lock()
	delete(c.entries, key)
	c.mu.Unlock()
}

// Clear removes all entries from the cache
func (c *LSPCache) Clear() {
	c.mu.Lock()
	c.entries = make(map[string]*cacheEntry)
	c.mu.Unlock()
}

// Stats returns cache statistics
func (c *LSPCache) Stats() (hits, misses uint64) {
	return c.hits.Load(), c.misses.Load()
}

// Default cache TTL: 5 minutes
const defaultCacheTTL = 5 * time.Minute

// Global cache instance (can be replaced for testing)
var lspCache = NewLSPCache(defaultCacheTTL)

// ResetCache resets the global cache (useful for testing)
func ResetCache() {
	lspCache.Clear()
}

// SetCacheTTL sets the global cache TTL (useful for testing)
func SetCacheTTL(ttl time.Duration) {
	lspCache.ttl = ttl
	lspCache.Clear()
}

// BatchCheckResult represents the result of a batch LSP check
type BatchCheckResult struct {
	Installed map[string]bool
	mu        sync.RWMutex
}

// NewBatchCheckResult creates a new batch check result
func NewBatchCheckResult() *BatchCheckResult {
	return &BatchCheckResult{
		Installed: make(map[string]bool),
	}
}

// Set sets a result for a package
func (b *BatchCheckResult) Set(name string, installed bool) {
	b.mu.Lock()
	b.Installed[name] = installed
	b.mu.Unlock()
}

// Get gets a result for a package
func (b *BatchCheckResult) Get(name string) (bool, bool) {
	b.mu.RLock()
	val, ok := b.Installed[name]
	b.mu.RUnlock()
	return val, ok
}

// isLSPPackageInstalled checks if an LSP package is installed (with caching)
func (sm *SetupManager) isLSPPackageInstalled(pkg LSPPackage) bool {
	// Check cache first
	if installed, found := lspCache.Get(pkg.Command); found {
		return installed
	}

	// Not in cache, perform actual check
	installed := sm.checkLSPPackageInstalledInternal(pkg)

	// Cache the result
	lspCache.Set(pkg.Command, installed)

	return installed
}

// checkLSPPackageInstalledInternal performs the actual installation check
func (sm *SetupManager) checkLSPPackageInstalledInternal(pkg LSPPackage) bool {
	// Try "which" command first (fastest)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	cmd := exec.CommandContext(ctx, "which", pkg.Command)
	if err := cmd.Run(); err == nil {
		return true
	}

	// Fall back to npx check (slower)
	ctx2, cancel2 := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel2()

	var npxCmd *exec.Cmd
	if runtime.GOOS != "windows" {
		npxCmd = exec.CommandContext(ctx2, "npx", "--yes", pkg.NPMName, "--version")
	} else {
		npxCmd = exec.CommandContext(ctx2, "npx.cmd", "--yes", pkg.NPMName, "--version")
	}

	return npxCmd.Run() == nil
}

// BatchCheckLSPInstallation checks multiple LSP packages in parallel with caching
func (sm *SetupManager) BatchCheckLSPInstallation() *BatchCheckResult {
	result := NewBatchCheckResult()

	var wg sync.WaitGroup
	semaphore := make(chan struct{}, 3) // Limit concurrent checks to 3

	for _, pkg := range RequiredLSPPackages {
		wg.Add(1)
		go func(p LSPPackage) {
			defer wg.Done()

			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			installed := sm.isLSPPackageInstalled(p)
			result.Set(p.Name, installed)
		}(pkg)
	}

	wg.Wait()
	return result
}

// CheckAndInstallLSP checks and installs required LSP packages in parallel
func (sm *SetupManager) CheckAndInstallLSP() error {
	var wg sync.WaitGroup
	errorsChan := make(chan error, len(RequiredLSPPackages))
	failedPackages := make(chan string, len(RequiredLSPPackages))

	for _, pkg := range RequiredLSPPackages {
		wg.Add(1)
		go func(p LSPPackage) {
			defer wg.Done()

			log.Printf("[PiSetup] Checking %s...", p.Name)

			if sm.isLSPPackageInstalled(p) {
				log.Printf("[PiSetup] ✅ %s already installed", p.Name)
				return
			}

			log.Printf("[PiSetup] Installing %s...", p.Name)
			if err := sm.installLSPPackage(p); err != nil {
				log.Printf("[PiSetup] ❌ Failed to install %s: %v", p.Name, err)
				errorsChan <- err
				failedPackages <- p.Name
			} else {
				log.Printf("[PiSetup] ✅ %s installed successfully", p.Name)
				// Invalidate cache since we just installed
				lspCache.Invalidate(p.Command)
			}
		}(pkg)
	}

	wg.Wait()
	close(errorsChan)
	close(failedPackages)

	// Collect failed packages
	var failed []string
	for pkgName := range failedPackages {
		failed = append(failed, pkgName)
	}

	if len(failed) > 0 {
		return fmt.Errorf("failed to install LSP packages: %v", failed)
	}

	return nil
}

// installLSPPackage installs a single LSP package
func (sm *SetupManager) installLSPPackage(pkg LSPPackage) error {
	var cmd *exec.Cmd

	// Try global npm install first
	log.Printf("[PiSetup] Installing %s via npm...", pkg.NPMName)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	cmd = exec.CommandContext(ctx, "npm", "install", "-g", pkg.NPMName)
	output, err := cmd.CombinedOutput()

	if err != nil {
		// If global install fails, log the error but don't fail immediately
		// The package might still be available via npx
		log.Printf("[PiSetup] Global install of %s failed, will try npx: %v", pkg.Name, string(output))

		// Verify it works with npx
		ctx2, cancel2 := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel2()

		if runtime.GOOS != "windows" {
			cmd = exec.CommandContext(ctx2, "npx", "--yes", pkg.NPMName, "--version")
		} else {
			cmd = exec.CommandContext(ctx2, "npx.cmd", "--yes", pkg.NPMName, "--version")
		}
		output, err = cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("failed to install %s: %w (output: %s)", pkg.Name, err, string(output))
		}
	}

	return nil
}

// IsLSPInstalled checks if all required LSP packages are installed (with caching)
func (sm *SetupManager) IsLSPInstalled() bool {
	// Quick check using batch operation
	result := sm.BatchCheckLSPInstallation()

	for _, pkg := range RequiredLSPPackages {
		if installed, ok := result.Get(pkg.Name); !ok || !installed {
			return false
		}
	}
	return true
}

// GetInstalledLSPPackages returns a list of installed LSP packages (with caching)
func (sm *SetupManager) GetInstalledLSPPackages() []string {
	result := sm.BatchCheckLSPInstallation()

	var installed []string
	for _, pkg := range RequiredLSPPackages {
		if isInstalled, _ := result.Get(pkg.Name); isInstalled {
			installed = append(installed, pkg.Name)
		}
	}

	return installed
}

// GetMissingLSPPackages returns a list of missing LSP packages (with caching)
func (sm *SetupManager) GetMissingLSPPackages() []string {
	result := sm.BatchCheckLSPInstallation()

	var missing []string
	for _, pkg := range RequiredLSPPackages {
		if isInstalled, _ := result.Get(pkg.Name); !isInstalled {
			missing = append(missing, pkg.Name)
		}
	}

	return missing
}

// CheckLSPInstallation checks all LSP components and returns detailed status for each (with caching)
func (sm *SetupManager) CheckLSPInstallation() []LSPComponent {
	log.Println("[LSP] Checking LSP installation status...")

	components := []LSPComponent{
		{Name: "LSP CLI", Command: "lsp"},
		{Name: "gopls", Command: "gopls"},
		{Name: "typescript-language-server", Command: "typescript-language-server"},
	}

	// Use batch check for better performance
	result := sm.BatchCheckLSPInstallation()

	for i := range components {
		installed, _ := result.Get(components[i].Name)
		components[i].Installed = installed
		if installed {
			log.Printf("[LSP] ✅ %s (%s) is installed", components[i].Name, components[i].Command)
		} else {
			log.Printf("[LSP] ❌ %s (%s) is not installed", components[i].Name, components[i].Command)
		}
	}

	installed := 0
	for _, comp := range components {
		if comp.Installed {
			installed++
		}
	}
	log.Printf("[LSP] Installation check complete: %d/%d components installed", installed, len(components))

	return components
}

// LSPCommandResult represents the result of an LSP command execution
type LSPCommandResult struct {
	Success bool     `json:"success"`
	Data    []string `json:"data,omitempty"`
	Error   string   `json:"error,omitempty"`
}

// LSPServerManager manages LSP server lifecycle
type LSPServerManager struct {
	servers map[string]*exec.Cmd
	mu      sync.RWMutex
}

// NewLSPServerManager creates a new LSP server manager
func NewLSPServerManager() *LSPServerManager {
	return &LSPServerManager{
		servers: make(map[string]*exec.Cmd),
	}
}

// StartServer starts an LSP server if not already running
func (lm *LSPServerManager) StartServer(name, command string, args []string) error {
	lm.mu.Lock()
	defer lm.mu.Unlock()

	if _, exists := lm.servers[name]; exists {
		log.Printf("[LSP] Server '%s' already running", name)
		return nil
	}

	log.Printf("[LSP] Starting %s server...", name)
	cmd := exec.Command(command, args...)

	// Start the server
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start %s server: %w", name, err)
	}

	lm.servers[name] = cmd
	log.Printf("[LSP] ✅ %s server started (PID: %d)", name, cmd.Process.Pid)
	return nil
}

// StopServer stops a running LSP server
func (lm *LSPServerManager) StopServer(name string) error {
	lm.mu.Lock()
	defer lm.mu.Unlock()

	cmd, exists := lm.servers[name]
	if !exists {
		return fmt.Errorf("server '%s' not found", name)
	}

	if err := cmd.Process.Kill(); err != nil {
		return fmt.Errorf("failed to stop %s server: %w", name, err)
	}

	delete(lm.servers, name)
	log.Printf("[LSP] ✅ %s server stopped", name)
	return nil
}

// StopAllServers stops all running LSP servers
func (lm *LSPServerManager) StopAllServers() {
	lm.mu.Lock()
	defer lm.mu.Unlock()

	for name, cmd := range lm.servers {
		if err := cmd.Process.Kill(); err != nil {
			log.Printf("[LSP] Failed to stop %s server: %v", name, err)
		} else {
			log.Printf("[LSP] ✅ %s server stopped", name)
		}
	}

	lm.servers = make(map[string]*exec.Cmd)
}

// ExecuteLSPCommand executes an LSP command (definition, reference, outline)
func ExecuteLSPCommand(command, filePath string, line, column int) (*LSPCommandResult, error) {
	log.Printf("[LSP] Executing command: %s %s:%d:%d", command, filePath, line, column)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var cmd *exec.Cmd
	switch command {
	case "definition":
		cmd = exec.CommandContext(ctx, "lsp", "definition", filePath, fmt.Sprintf("%d:%d", line, column))
	case "reference":
		cmd = exec.CommandContext(ctx, "lsp", "reference", filePath, fmt.Sprintf("%d:%d", line, column))
	case "outline":
		cmd = exec.CommandContext(ctx, "lsp", "outline", filePath)
	default:
		return nil, fmt.Errorf("unknown LSP command: %s", command)
	}

	output, err := cmd.CombinedOutput()
	if err != nil {
		return &LSPCommandResult{
			Success: false,
			Error:   fmt.Sprintf("command failed: %v (output: %s)", err, string(output)),
		}, nil
	}

	// Parse output into lines
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	var result []string
	for _, line := range lines {
		if strings.TrimSpace(line) != "" {
			result = append(result, line)
		}
	}

	return &LSPCommandResult{
		Success: true,
		Data:    result,
	}, nil
}

// LSPDefinition gets the definition location for a symbol
func LSPDefinition(filePath string, line, column int) (*LSPCommandResult, error) {
	return ExecuteLSPCommand("definition", filePath, line, column)
}

// LSPReference gets all references to a symbol
func LSPReference(filePath string, line, column int) (*LSPCommandResult, error) {
	return ExecuteLSPCommand("reference", filePath, line, column)
}

// LSPOutline gets the document outline (symbols)
func LSPOutline(filePath string) (*LSPCommandResult, error) {
	return ExecuteLSPCommand("outline", filePath, 0, 0)
}

// LoadMCPConfig loads the MCP configuration from mcp.json
func LoadMCPConfig(configPath string) (*MCPConfig, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read mcp.json: %w", err)
	}

	var config MCPConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse mcp.json: %w", err)
	}

	return &config, nil
}

// AutoStartLSPServers starts all enabled LSP servers from MCP config (optimized)
func (sm *SetupManager) AutoStartLSPServers() error {
	configPath := filepath.Join(sm.piConfigDir, "agent", "mcp.json")
	config, err := LoadMCPConfig(configPath)
	if err != nil {
		// If config doesn't exist, generate it first
		if os.IsNotExist(err) {
			if err := sm.configManager.GenerateMCPConfig(); err != nil {
				return fmt.Errorf("failed to generate MCP config: %w", err)
			}
			config, err = LoadMCPConfig(configPath)
			if err != nil {
				return fmt.Errorf("failed to load generated MCP config: %w", err)
			}
		} else {
			return err
		}
	}

	manager := NewLSPServerManager()
	type startResult struct {
		name string
		err  error
	}

	results := make(chan startResult, len(config.Servers))
	var wg sync.WaitGroup

	// Limit concurrent server starts
	semaphore := make(chan struct{}, 2)

	for name, serverConfig := range config.Servers {
		if !serverConfig.Enabled {
			log.Printf("[LSP] Server '%s' is disabled, skipping", name)
			continue
		}

		wg.Add(1)
		go func(n string, cfg MCPServerConfig) {
			defer wg.Done()

			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			if err := manager.StartServer(n, cfg.Command, cfg.Args); err != nil {
				log.Printf("[LSP] ❌ Failed to start %s server: %v", n, err)
				results <- startResult{name: n, err: err}
			}
		}(name, serverConfig)
	}

	// Close results channel when all goroutines complete
	go func() {
		wg.Wait()
		close(results)
	}()

	// Collect failed servers
	var failedServers []string
	for result := range results {
		if result.err != nil {
			failedServers = append(failedServers, result.name)
		}
	}

	if len(failedServers) > 0 {
		return fmt.Errorf("failed to start LSP servers: %v", failedServers)
	}

	return nil
}

// VerifyLSPCodeAnalysisSkill checks if lsp-code-analysis skill is installed
func (sm *SetupManager) VerifyLSPCodeAnalysisSkill() error {
	skillsDir := sm.getSkillsDir()
	skillPath := filepath.Join(skillsDir, "lsp-code-analysis")
	skillMDPath := filepath.Join(skillPath, "SKILL.md")

	if _, err := os.Stat(skillPath); os.IsNotExist(err) {
		return fmt.Errorf("lsp-code-analysis skill directory not found at %s", skillPath)
	}

	if _, err := os.Stat(skillMDPath); os.IsNotExist(err) {
		return fmt.Errorf("lsp-code-analysis SKILL.md not found at %s", skillMDPath)
	}

	log.Printf("[LSP] ✅ lsp-code-analysis skill verified")
	return nil
}

// InstallLSPComponents installs missing LSP components using appropriate package managers
// Verifies each installation succeeds and returns error if any installation fails
func (sm *SetupManager) InstallLSPComponents() error {
	var failedComponents []string
	var mu sync.Mutex

	log.Println("[LSP] Starting LSP component installation...")

	var wg sync.WaitGroup
	semaphore := make(chan struct{}, 2) // Limit concurrent installations to 2

	for _, pkg := range RequiredLSPPackages {
		// Skip if already installed (uses cache)
		if sm.isLSPPackageInstalled(pkg) {
			log.Printf("[LSP] ✅ %s already installed, skipping", pkg.Name)
			continue
		}

		wg.Add(1)
		go func(p LSPPackage) {
			defer wg.Done()

			semaphore <- struct{}{}        // Acquire semaphore
			defer func() { <-semaphore }() // Release semaphore

			log.Printf("[LSP] Installing %s...", p.Name)

			var installed bool

			// Use appropriate package manager based on component name
			switch p.Name {
			case "gopls":
				// Install gopls via go install
				log.Printf("[LSP] Installing gopls via 'go install'...")
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
				cmd := exec.CommandContext(ctx, "go", "install", "golang.org/x/tools/gopls@latest")
				output, err := cmd.CombinedOutput()
				cancel()
				if err != nil {
					log.Printf("[LSP] ❌ Failed to install gopls: %v\nOutput: %s", err, string(output))
					mu.Lock()
					failedComponents = append(failedComponents, p.Name)
					mu.Unlock()
					return
				}
				installed = true

			case "typescript-language-server":
				// Install typescript-language-server via npm
				log.Printf("[LSP] Installing typescript-language-server via npm...")
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
				cmd := exec.CommandContext(ctx, "npm", "install", "-g", "typescript-language-server")
				output, err := cmd.CombinedOutput()
				cancel()
				if err != nil {
					log.Printf("[LSP] ❌ Failed to install typescript-language-server: %v\nOutput: %s", err, string(output))
					mu.Lock()
					failedComponents = append(failedComponents, p.Name)
					mu.Unlock()
					return
				}
				_ = output // Suppress unused variable warning
				installed = true

			case "LSP CLI":
				// Install lsp-client via pip (if needed) or npm
				log.Printf("[LSP] Installing LSP CLI via npm...")
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
				cmd := exec.CommandContext(ctx, "npm", "install", "-g", p.NPMName)
				output, err := cmd.CombinedOutput()
				cancel()
				if err != nil {
					// Try pip as fallback for lsp-client
					log.Printf("[LSP] npm install failed, trying pip for lsp-client...")
					ctx2, cancel2 := context.WithTimeout(context.Background(), 2*time.Minute)
					cmd = exec.CommandContext(ctx2, "pip", "install", "lsp-client")
					output, err = cmd.CombinedOutput()
					cancel2()
					if err != nil {
						log.Printf("[LSP] ❌ Failed to install LSP CLI via both npm and pip")
						mu.Lock()
						failedComponents = append(failedComponents, p.Name)
						mu.Unlock()
						return
					}
					_ = output
				}
				_ = output
				installed = true

			default:
				// Default to npm install for other packages
				log.Printf("[LSP] Installing %s via npm...", p.Name)
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
				cmd := exec.CommandContext(ctx, "npm", "install", "-g", p.NPMName)
				output, err := cmd.CombinedOutput()
				cancel()
				if err != nil {
					log.Printf("[LSP] ❌ Failed to install %s: %v\nOutput: %s", p.Name, err, string(output))
					mu.Lock()
					failedComponents = append(failedComponents, p.Name)
					mu.Unlock()
					return
				}
				installed = true
			}

			// Verify the installation succeeded (uses cache, will refresh)
			if installed && !sm.isLSPPackageInstalled(p) {
				log.Printf("[LSP] ❌ Installation verification failed for %s", p.Name)
				mu.Lock()
				failedComponents = append(failedComponents, p.Name)
				mu.Unlock()
				return
			}

			// Invalidate cache for this package
			lspCache.Invalidate(p.Command)

			log.Printf("[LSP] ✅ %s installed and verified successfully", p.Name)
		}(pkg)
	}

	wg.Wait()

	if len(failedComponents) > 0 {
		return fmt.Errorf("failed to install LSP components: %v", failedComponents)
	}

	log.Printf("[LSP] LSP component installation completed successfully")
	return nil
}

// GetCacheStats returns cache statistics for monitoring
func GetCacheStats() (hits, misses uint64, hitRate float64) {
	hits, misses = lspCache.Stats()
	total := hits + misses
	if total > 0 {
		hitRate = float64(hits) / float64(total) * 100
	}
	return hits, misses, hitRate
}
