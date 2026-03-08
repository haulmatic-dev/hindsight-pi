package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/haulmatic-dev/hydra/internal/beads"
	"github.com/haulmatic-dev/hydra/internal/mcp"
	"github.com/haulmatic-dev/hydra/pkg/types"
)

// IntelligenceBundle contains context and patterns gathered from intelligence services
type IntelligenceBundle struct {
	Patterns        []Pattern       `json:"patterns"`
	ContextEntries  []ContextEntry  `json:"context_entries"`
	ImpactAnalysis  *ImpactAnalysis `json:"impact_analysis,omitempty"`
	ScopeValidated  bool            `json:"scope_validated"`
	ValidationError string          `json:"validation_error,omitempty"`
	GatheredAt      time.Time       `json:"gathered_at"`
}

// Pattern represents a learned pattern from cass_memory
type Pattern struct {
	ID          string    `json:"id"`
	Type        string    `json:"type"`
	Description string    `json:"description"`
	Confidence  float64   `json:"confidence"`
	Tags        []string  `json:"tags"`
	CreatedAt   time.Time `json:"created_at"`
}

// ContextEntry represents a relevant historical context entry
type ContextEntry struct {
	ID        string    `json:"id"`
	BeadType  string    `json:"bead_type"`
	Files     []string  `json:"files"`
	Summary   string    `json:"summary"`
	Relevance float64   `json:"relevance"`
	CreatedAt time.Time `json:"created_at"`
}

// ImpactAnalysis represents TLDR impact analysis results
type ImpactAnalysis struct {
	TargetFiles     []string   `json:"target_files"`
	AffectedFiles   []string   `json:"affected_files"`
	CallGraph       *CallGraph `json:"call_graph,omitempty"`
	Complexity      int        `json:"complexity"`
	RiskLevel       string     `json:"risk_level"`
	Recommendations []string   `json:"recommendations"`
}

// CallGraph represents function call relationships
type CallGraph struct {
	Nodes []CallNode `json:"nodes"`
	Edges []CallEdge `json:"edges"`
}

// CallNode represents a function or method in the call graph
type CallNode struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	File     string `json:"file"`
	NodeType string `json:"node_type"`
}

// CallEdge represents a call relationship between nodes
type CallEdge struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// CassMemoryClient provides access to cass_memory service
type CassMemoryClient struct {
	baseURL    string
	httpClient *http.Client
	queryLimit int
}

// NewCassMemoryClient creates a new cass_memory client
func NewCassMemoryClient(baseURL string, timeout time.Duration, queryLimit int) *CassMemoryClient {
	return &CassMemoryClient{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: timeout,
		},
		queryLimit: queryLimit,
	}
}

// QueryContext queries cass_memory for relevant context
func (c *CassMemoryClient) QueryContext(beadType string, files []string, days int) ([]ContextEntry, error) {
	if c.baseURL == "" {
		return nil, fmt.Errorf("cass_memory base URL not configured")
	}

	reqBody := map[string]interface{}{
		"bead_type": beadType,
		"files":     files,
		"days":      days,
		"limit":     c.queryLimit,
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/api/v1/context/query", c.baseURL)
	req, err := http.NewRequest("POST", url, strings.NewReader(string(jsonBody)))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("cass_memory request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("cass_memory returned status %d", resp.StatusCode)
	}

	var result struct {
		Entries []ContextEntry `json:"entries"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode cass_memory response: %w", err)
	}

	return result.Entries, nil
}

// TLDRClient provides access to TLDR impact analysis service
type TLDRClient struct {
	baseURL    string
	httpClient *http.Client
	maxFiles   int
	maxTokens  int
}

// NewTLDRClient creates a new TLDR client
func NewTLDRClient(baseURL string, timeout time.Duration, maxFiles, maxTokens int) *TLDRClient {
	return &TLDRClient{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: timeout,
		},
		maxFiles:  maxFiles,
		maxTokens: maxTokens,
	}
}

// AnalyzeImpact performs impact analysis on target files
func (c *TLDRClient) AnalyzeImpact(projectPath string, files []string, depth int) (*ImpactAnalysis, error) {
	if c.baseURL == "" {
		return nil, fmt.Errorf("TLDR base URL not configured")
	}

	// Limit files to maxFiles
	if len(files) > c.maxFiles {
		files = files[:c.maxFiles]
	}

	reqBody := map[string]interface{}{
		"project_path": projectPath,
		"files":        files,
		"depth":        depth,
		"max_tokens":   c.maxTokens,
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/api/v1/analyze/impact", c.baseURL)
	req, err := http.NewRequest("POST", url, strings.NewReader(string(jsonBody)))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("TLDR request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("TLDR returned status %d", resp.StatusCode)
	}

	var result ImpactAnalysis
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode TLDR response: %w", err)
	}

	return &result, nil
}

// GPTCacheClient provides access to GPTCache service
type GPTCacheClient struct {
	serverURL  string
	httpClient *http.Client
}

// NewGPTCacheClient creates a new GPTCache client
func NewGPTCacheClient(serverURL string, timeout time.Duration) *GPTCacheClient {
	if timeout == 0 {
		timeout = 5 * time.Second
	}
	return &GPTCacheClient{
		serverURL: serverURL,
		httpClient: &http.Client{
			Timeout: timeout,
		},
	}
}

// IsHealthy checks if the GPTCache service is available
func (c *GPTCacheClient) IsHealthy() bool {
	if c.serverURL == "" {
		return false
	}

	url := fmt.Sprintf("%s/health", c.serverURL)
	resp, err := c.httpClient.Get(url)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	return resp.StatusCode == http.StatusOK
}

// Retry configuration constants
const (
	defaultMaxRetries    = 3
	defaultBaseDelay     = 5 * time.Second
	defaultMaxDelay      = 5 * time.Minute
	defaultRetryCooldown = 10 * time.Second
)

// beadRetryInfo tracks retry state for a bead
type beadRetryInfo struct {
	count     int
	lastError time.Time
	lastErr   error
}

// calculateBackoff calculates exponential backoff delay with jitter protection
// Formula: baseDelay * 2^attempt, capped at maxDelay
func calculateBackoff(attempt int, baseDelay, maxDelay time.Duration) time.Duration {
	if attempt <= 0 {
		return baseDelay
	}

	// Calculate exponential delay: baseDelay * 2^attempt
	delay := baseDelay * time.Duration(1<<uint(attempt))

	// Cap at maxDelay
	if delay > maxDelay {
		delay = maxDelay
	}

	return delay
}

// BeadsClient defines the interface for beads operations
// This allows for mocking in tests
type BeadsClient interface {
	ListBeads(projectPath, repoName string) ([]types.Bead, error)
	GetBead(projectPath, repoName string, beadID types.BeadID) (*types.Bead, error)
	CloseBead(projectPath, repoName string, beadID types.BeadID, reason string) error
	GetReadyBeads(projectPath, repoName string) ([]types.Bead, error)
}

// Overseer is the main scheduler daemon
type Overseer struct {
	config        *Config
	projects      []types.Project
	activeWorkers map[string]*types.Worker
	workerMutex   sync.RWMutex
	running       bool
	paused        bool
	pauseMutex    sync.RWMutex
	pollInterval  time.Duration
	maxWorkers    int
	maxWorkersMux sync.RWMutex
	beadsClient   BeadsClient
	dag           *DAG
	dagMu         sync.RWMutex
	completed     map[string]bool           // Track completed beads for this session
	retries       map[string]*beadRetryInfo // Track retry state for failed beads
	retriesMu     sync.RWMutex              // Mutex for retries map
	baseDelay     time.Duration             // Base delay for exponential backoff
	maxDelay      time.Duration             // Maximum delay for exponential backoff
	cassClient    *CassMemoryClient         // cass_memory client for context retrieval
	tldrClient    *TLDRClient               // TLDR client for impact analysis
	gptCache      *GPTCacheClient           // GPTCache client for caching
	intelMutex    sync.RWMutex              // Mutex for intelligence clients

	// MCP (Model Context Protocol) fields
	mcpClient          *mcp.Client                 // MCP WebSocket client
	mcpReservationMgr  *mcp.ReservationManager     // MCP reservation manager
	mcpConfig          *MCPConfig                  // MCP configuration
	activeReservations map[string]*mcp.Reservation // Active file reservations
	mcpMu              sync.RWMutex                // Mutex for MCP operations
	mcpMessageHandler  func(mcp.Message)           // Handler for incoming MCP messages
}

// NewOverseer creates a new overseer instance
func NewOverseer(config *Config) *Overseer {
	maxRetries := config.Overseer.DefaultRetries
	if maxRetries <= 0 {
		maxRetries = defaultMaxRetries
	}

	o := &Overseer{
		config:        config,
		activeWorkers: make(map[string]*types.Worker),
		pollInterval:  config.Overseer.PollInterval,
		maxWorkers:    config.Overseer.MaxWorkers,
		beadsClient:   beads.NewClient(),
		dag:           NewDAG(),
		completed:     make(map[string]bool),
		retries:       make(map[string]*beadRetryInfo),
		baseDelay:     defaultBaseDelay,
		maxDelay:      defaultMaxDelay,
	}

	// Initialize intelligence clients if enabled
	o.initializeIntelligenceClients()

	// Initialize MCP client if enabled
	if config.MCP.Enabled {
		o.initializeMCP()
	}

	// Override defaults if configured
	if config.Overseer.WorkerTimeout > 0 {
		// WorkerTimeout can be used to derive maxDelay
		o.maxDelay = config.Overseer.WorkerTimeout / 6 // Reasonable fraction
		if o.maxDelay > defaultMaxDelay {
			o.maxDelay = defaultMaxDelay
		}
	}

	return o
}

// initializeIntelligenceClients creates intelligence service clients based on config
func (o *Overseer) initializeIntelligenceClients() {
	o.intelMutex.Lock()
	defer o.intelMutex.Unlock()

	intellConfig := o.config.Intelligence

	// Initialize cass_memory client if enabled
	if intellConfig.CassMemory.Enabled {
		o.cassClient = NewCassMemoryClient(
			"http://127.0.0.1:5000", // Default cass_memory port
			intellConfig.CassMemory.Timeout,
			intellConfig.CassMemory.QueryLimit,
		)
		log.Printf("[Intelligence] cass_memory client initialized (limit: %d, timeout: %v)",
			intellConfig.CassMemory.QueryLimit, intellConfig.CassMemory.Timeout)
	}

	// Initialize TLDR client if enabled
	if intellConfig.TLDR.Enabled {
		o.tldrClient = NewTLDRClient(
			intellConfig.TLDR.BaseURL,
			intellConfig.TLDR.Timeout,
			intellConfig.TLDR.MaxFiles,
			intellConfig.TLDR.MaxTokens,
		)
		log.Printf("[Intelligence] TLDR client initialized (baseURL: %s, maxFiles: %d)",
			intellConfig.TLDR.BaseURL, intellConfig.TLDR.MaxFiles)
	}

	// Initialize GPTCache client if enabled
	if intellConfig.GPTCache.Enabled {
		o.gptCache = NewGPTCacheClient(
			intellConfig.GPTCache.ServerURL,
			5*time.Second,
		)
		log.Printf("[Intelligence] GPTCache client initialized (server: %s)",
			intellConfig.GPTCache.ServerURL)
	}
}

// runPreExecutionIntelligence gathers intelligence before executing a bead
// This method is designed to be non-blocking and degrades gracefully if services are unavailable
func (o *Overseer) runPreExecutionIntelligence(bead *types.Bead) (*IntelligenceBundle, error) {
	startTime := time.Now()
	bundle := &IntelligenceBundle{
		Patterns:       make([]Pattern, 0),
		ContextEntries: make([]ContextEntry, 0),
		GatheredAt:     time.Now(),
		ScopeValidated: true, // Default to true, will set to false if validation fails
	}

	beadIDStr := string(bead.ID)
	log.Printf("[Intelligence] Starting pre-execution intelligence for bead: %s", beadIDStr)

	o.intelMutex.RLock()
	cassClient := o.cassClient
	tldrClient := o.tldrClient
	o.intelMutex.RUnlock()

	// Extract target files from bead metadata or description
	targetFiles := o.extractTargetFiles(bead)
	if len(targetFiles) > 0 {
		log.Printf("[Intelligence] Extracted %d target files for analysis", len(targetFiles))
	}

	// 1. Query cass_memory for relevant context (if enabled)
	if cassClient != nil && o.config.Intelligence.CassMemory.Enabled {
		ctxEntries, err := o.queryCassMemoryContext(cassClient, bead, targetFiles)
		if err != nil {
			// Graceful degradation: log error but continue
			o.logGracefulDegradation("Intelligence", "cass_memory query",
				"continuing without context", err)
		} else {
			bundle.ContextEntries = ctxEntries
			log.Printf("[Intelligence] Retrieved %d context entries from cass_memory", len(ctxEntries))
		}
	}

	// 2. Analyze impact with TLDR (if enabled and files available)
	if tldrClient != nil && o.config.Intelligence.TLDR.Enabled && len(targetFiles) > 0 {
		impact, err := o.analyzeImpactWithTLDR(tldrClient, bead, targetFiles)
		if err != nil {
			// Graceful degradation: log error but continue
			o.logGracefulDegradation("Intelligence", "TLDR impact analysis",
				"continuing without impact analysis", err)
		} else {
			bundle.ImpactAnalysis = impact
			log.Printf("[Intelligence] TLDR impact analysis complete: %d files affected, risk: %s",
				len(impact.AffectedFiles), impact.RiskLevel)
		}
	}

	// 3. Validate scope against limits
	if err := o.validateScope(bundle, targetFiles); err != nil {
		bundle.ScopeValidated = false
		bundle.ValidationError = err.Error()
		log.Printf("[Intelligence] Scope validation failed for bead %s: %v", beadIDStr, err)
	}

	duration := time.Since(startTime)
	log.Printf("[Intelligence] Pre-execution intelligence complete for %s (duration: %v, entries: %d, validated: %t)",
		beadIDStr, duration, len(bundle.ContextEntries), bundle.ScopeValidated)

	return bundle, nil
}

// queryCassMemoryContext queries cass_memory for relevant context with error handling
func (o *Overseer) queryCassMemoryContext(client *CassMemoryClient, bead *types.Bead, files []string) ([]ContextEntry, error) {
	// Create a context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), o.config.Intelligence.CassMemory.Timeout)
	defer cancel()

	// Run query in goroutine to allow cancellation
	type result struct {
		entries []ContextEntry
		err     error
	}

	resultChan := make(chan result, 1)

	go func() {
		entries, err := client.QueryContext(
			bead.Type,
			files,
			o.config.Intelligence.CassMemory.HistoryDays,
		)
		resultChan <- result{entries: entries, err: err}
	}()

	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("cass_memory query timed out: %w", ctx.Err())
	case res := <-resultChan:
		return res.entries, res.err
	}
}

// analyzeImpactWithTLDR performs impact analysis using TLDR service
func (o *Overseer) analyzeImpactWithTLDR(client *TLDRClient, bead *types.Bead, files []string) (*ImpactAnalysis, error) {
	// Create a context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), o.config.Intelligence.TLDR.Timeout)
	defer cancel()

	// Find project path for the bead
	projectPath := o.findProjectPath(bead)
	if projectPath == "" {
		return nil, fmt.Errorf("could not find project path for bead %s", bead.ID)
	}

	// Run analysis in goroutine to allow cancellation
	type result struct {
		analysis *ImpactAnalysis
		err      error
	}

	resultChan := make(chan result, 1)

	go func() {
		// Use depth 2 for moderate analysis depth
		analysis, err := client.AnalyzeImpact(projectPath, files, 2)
		resultChan <- result{analysis: analysis, err: err}
	}()

	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("TLDR analysis timed out: %w", ctx.Err())
	case res := <-resultChan:
		return res.analysis, res.err
	}
}

// extractTargetFiles extracts target file paths from bead metadata and description
func (o *Overseer) extractTargetFiles(bead *types.Bead) []string {
	var files []string
	seen := make(map[string]bool)

	// Check metadata for explicit file list
	if bead.Metadata != nil {
		if fileList, ok := bead.Metadata["files"]; ok {
			for _, file := range strings.Split(fileList, ",") {
				file = strings.TrimSpace(file)
				if file != "" && !seen[file] {
					files = append(files, file)
					seen[file] = true
				}
			}
		}
	}

	// Parse description for file references (e.g., "modify internal/core/config.go")
	// This is a simple heuristic - can be enhanced with more sophisticated parsing
	desc := bead.Description + " " + bead.Title
	words := strings.Fields(desc)

	for _, word := range words {
		// Check for common file patterns
		if strings.Contains(word, "/") || strings.Contains(word, ".go") ||
			strings.Contains(word, ".js") || strings.Contains(word, ".ts") ||
			strings.Contains(word, ".py") || strings.Contains(word, ".java") {
			// Clean up the word (remove punctuation)
			cleaned := strings.Trim(word, ".,;:!?()[]{}\"")
			if cleaned != "" && !seen[cleaned] && len(cleaned) < 200 {
				files = append(files, cleaned)
				seen[cleaned] = true
			}
		}
	}

	return files
}

// findProjectPath finds the project path for a given bead
func (o *Overseer) findProjectPath(bead *types.Bead) string {
	for _, project := range o.projects {
		if project.Name == bead.Project {
			if bead.Repo != "" && project.Type == types.RepoTypeMulti {
				// For multi-repo projects, return the specific repo path
				return filepath.Join(project.Path, bead.Repo)
			}
			return project.Path
		}
	}
	return ""
}

// validateScope validates the scope of changes against configured limits
func (o *Overseer) validateScope(bundle *IntelligenceBundle, targetFiles []string) error {
	config := o.config.Intelligence

	// Validate file count against TLDR max files
	if config.TLDR.Enabled && len(targetFiles) > config.TLDR.MaxFiles {
		return fmt.Errorf("too many target files: %d exceeds limit of %d",
			len(targetFiles), config.TLDR.MaxFiles)
	}

	// Validate impact scope if analysis available
	if bundle.ImpactAnalysis != nil {
		totalFiles := len(targetFiles) + len(bundle.ImpactAnalysis.AffectedFiles)

		if totalFiles > config.TLDR.MaxFiles*2 {
			return fmt.Errorf("impact scope too large: %d total files affected exceeds safe threshold",
				totalFiles)
		}

		// Check complexity score
		if bundle.ImpactAnalysis.Complexity > 100 {
			return fmt.Errorf("complexity score too high: %d (max recommended: 100)",
				bundle.ImpactAnalysis.Complexity)
		}

		// Flag high-risk changes for review
		if bundle.ImpactAnalysis.RiskLevel == "high" {
			log.Printf("[Intelligence] Warning: High risk level detected for change scope")
		}
	}

	return nil
}

// shouldRetry checks if a bead should be retried based on retry count and cooldown
func (o *Overseer) shouldRetry(beadID string) bool {
	o.retriesMu.RLock()
	defer o.retriesMu.RUnlock()

	retryInfo, exists := o.retries[beadID]
	if !exists {
		// No retries yet, allow first attempt
		return true
	}

	maxRetries := o.config.Overseer.DefaultRetries
	if maxRetries <= 0 {
		maxRetries = defaultMaxRetries
	}

	// Check if we've exceeded max retries
	if retryInfo.count >= maxRetries {
		log.Printf("[Retry] Bead %s exhausted all %d retry attempts", beadID, maxRetries)
		return false
	}

	// Check if enough time has passed since last attempt (cooldown + exponential backoff)
	backoff := calculateBackoff(retryInfo.count, o.baseDelay, o.maxDelay)
	minInterval := defaultRetryCooldown + backoff
	elapsed := time.Since(retryInfo.lastError)

	if elapsed < minInterval {
		log.Printf("[Retry] Bead %s in cooldown period (%v remaining)", beadID, minInterval-elapsed)
		return false
	}

	return true
}

// recordRetry records a retry attempt for a bead
func (o *Overseer) recordRetry(beadID string, err error) {
	o.retriesMu.Lock()
	defer o.retriesMu.Unlock()

	if info, exists := o.retries[beadID]; exists {
		info.count++
		info.lastError = time.Now()
		info.lastErr = err
		log.Printf("[Retry] Bead %s retry attempt %d recorded: %v", beadID, info.count, err)
	} else {
		o.retries[beadID] = &beadRetryInfo{
			count:     1,
			lastError: time.Now(),
			lastErr:   err,
		}
		log.Printf("[Retry] Bead %s first retry recorded: %v", beadID, err)
	}
}

// clearRetry removes retry tracking for a bead (called on success)
func (o *Overseer) clearRetry(beadID string) {
	o.retriesMu.Lock()
	defer o.retriesMu.Unlock()

	if _, exists := o.retries[beadID]; exists {
		delete(o.retries, beadID)
		log.Printf("[Retry] Bead %s retry state cleared after successful execution", beadID)
	}
}

// getRetryCount returns the current retry count for a bead
func (o *Overseer) getRetryCount(beadID string) int {
	o.retriesMu.RLock()
	defer o.retriesMu.RUnlock()

	if info, exists := o.retries[beadID]; exists {
		return info.count
	}
	return 0
}

// logError logs an error with structured context information
func (o *Overseer) logError(component, operation, beadID string, err error, severity string) {
	retryCount := o.getRetryCount(beadID)

	switch severity {
	case "fatal":
		log.Printf("[FATAL] [%s] %s failed | bead=%s | retries=%d | error=%v",
			component, operation, beadID, retryCount, err)
	case "error":
		log.Printf("[ERROR] [%s] %s failed | bead=%s | retries=%d | error=%v",
			component, operation, beadID, retryCount, err)
	case "warn", "warning":
		log.Printf("[WARN] [%s] %s issue | bead=%s | retries=%d | error=%v",
			component, operation, beadID, retryCount, err)
	default:
		log.Printf("[INFO] [%s] %s | bead=%s | retries=%d | error=%v",
			component, operation, beadID, retryCount, err)
	}
}

// logGracefulDegradation logs a non-fatal error that the system recovered from
func (o *Overseer) logGracefulDegradation(component, operation, action string, err error) {
	log.Printf("[DEGRADED] [%s] %s failed but continuing | recovery=%s | error=%v",
		component, operation, action, err)
}

// isIntelligenceEnabled checks if any intelligence services are enabled
func (o *Overseer) isIntelligenceEnabled() bool {
	o.intelMutex.RLock()
	defer o.intelMutex.RUnlock()

	return o.config.Intelligence.CassMemory.Enabled ||
		o.config.Intelligence.TLDR.Enabled ||
		o.config.Intelligence.GPTCache.Enabled
}

// Start begins the overseer's main loop
func (o *Overseer) Start(ctx context.Context) error {
	log.Println("🚀 Starting Hydra overseer...")

	// Discover projects
	if err := o.discoverProjects(); err != nil {
		return fmt.Errorf("failed to discover projects: %w", err)
	}

	log.Printf("📁 Discovered %d projects", len(o.projects))

	o.running = true
	ticker := time.NewTicker(o.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			o.running = false
			return nil
		case <-ticker.C:
			if err := o.tick(); err != nil {
				log.Printf("Error during tick: %v", err)
			}
		}
	}
}

// discoverProjects finds all projects in ~/source-code/
func (o *Overseer) discoverProjects() error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}

	rootPath := filepath.Join(homeDir, "source-code")

	// If projects are configured, use those
	if len(o.config.Projects) > 0 {
		for _, pc := range o.config.Projects {
			project := types.Project{
				Name: pc.Name,
				Path: pc.Path,
				Type: types.RepoType(pc.Type),
			}

			if pc.Type == "multi" {
				for _, rc := range pc.Repos {
					project.Repos = append(project.Repos, types.Repo{
						Name: rc.Name,
						Path: rc.Path,
					})
				}
			}

			o.projects = append(o.projects, project)
		}
		return nil
	}

	// Auto-discovery
	entries, err := os.ReadDir(rootPath)
	if err != nil {
		if os.IsNotExist(err) {
			log.Printf("⚠️  Projects directory not found: %s", rootPath)
			return nil
		}
		return fmt.Errorf("failed to read projects directory: %w", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		projectPath := filepath.Join(rootPath, entry.Name())
		project, err := o.inspectProject(projectPath, entry.Name())
		if err != nil {
			log.Printf("⚠️  Could not inspect project %s: %v", entry.Name(), err)
			continue
		}

		if project != nil {
			o.projects = append(o.projects, *project)
		}
	}

	return nil
}

// inspectProject determines if a directory is a project and its type
func (o *Overseer) inspectProject(path, name string) (*types.Project, error) {
	// Check for .beads directory at root (single repo)
	beadsPath := filepath.Join(path, ".beads", "beads.db")
	if _, err := os.Stat(beadsPath); err == nil {
		return &types.Project{
			Name: name,
			Path: path,
			Type: types.RepoTypeSingle,
		}, nil
	}

	// Check subdirectories for .beads (multi-repo)
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}

	var repos []types.Repo
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		subBeadsPath := filepath.Join(path, entry.Name(), ".beads", "beads.db")
		if _, err := os.Stat(subBeadsPath); err == nil {
			repos = append(repos, types.Repo{
				Name: entry.Name(),
				Path: entry.Name(),
			})
		}
	}

	if len(repos) > 0 {
		return &types.Project{
			Name:  name,
			Path:  path,
			Type:  types.RepoTypeMulti,
			Repos: repos,
		}, nil
	}

	// No beads found
	return nil, nil
}

// tick performs one iteration of the scheduling loop
func (o *Overseer) tick() error {
	// Check if paused
	if o.IsPaused() {
		return nil
	}

	// Get active worker count
	o.workerMutex.RLock()
	activeCount := len(o.activeWorkers)
	o.workerMutex.RUnlock()

	maxWorkers := o.GetMaxWorkers()
	availableSlots := maxWorkers - activeCount
	if availableSlots <= 0 {
		return nil
	}

	// Build/refresh DAG from all beads
	if err := o.buildDAG(); err != nil {
		// Graceful degradation: log error and continue with existing DAG
		o.logGracefulDegradation("Overseer", "buildDAG", "using existing DAG", err)
		// Continue with existing DAG if rebuild fails
	}

	// Get ready beads from DAG (InDegree == 0)
	readyBeads, err := o.getReadyBeadsFromDAG()
	if err != nil {
		return fmt.Errorf("failed to get ready beads from DAG: %w", err)
	}

	// Include failed beads that are eligible for retry
	retryBeads := o.getRetryEligibleBeads(readyBeads)

	if len(retryBeads) == 0 {
		return nil
	}

	// Count retry beads separately for logging
	originalCount := len(readyBeads)
	retryCount := len(retryBeads) - originalCount

	if retryCount > 0 {
		log.Printf("📊 Active workers: %d/%d, Ready beads: %d (original: %d, retries: %d)",
			activeCount, maxWorkers, len(retryBeads), originalCount, retryCount)
	} else {
		log.Printf("📊 Active workers: %d/%d, Ready beads from DAG: %d", activeCount, maxWorkers, len(retryBeads))
	}

	// Spawn workers for ready beads
	for _, bead := range retryBeads {
		if availableSlots <= 0 {
			break
		}

		beadIDStr := string(bead.ID)

		// Check if this is a retry and log appropriately
		if retryInfo, exists := o.retries[beadIDStr]; exists {
			log.Printf("[Retry] Attempting retry %d/%d for bead %s",
				retryInfo.count+1, o.config.Overseer.DefaultRetries, bead.ID)
		}

		// Skip approval gates (require manual br close)
		if bead.Type == "approval-gate" {
			log.Printf("⏸️  Skipping approval gate: %s", bead.ID)
			continue
		}

		if err := o.spawnWorker(bead); err != nil {
			// Record retry and log error
			o.recordRetry(beadIDStr, err)
			o.logError("Overseer", "spawnWorker", beadIDStr, err, "error")
			continue
		}

		// Clear retry state on successful spawn
		o.clearRetry(beadIDStr)
		availableSlots--
	}

	return nil
}

// getRetryEligibleBeads returns beads that are eligible for retry
// It filters the ready beads and adds any failed beads that should be retried
func (o *Overseer) getRetryEligibleBeads(readyBeads []types.Bead) []types.Bead {
	o.retriesMu.RLock()
	defer o.retriesMu.RUnlock()

	if len(o.retries) == 0 {
		return readyBeads
	}

	// Create a map of already ready beads for deduplication
	readyMap := make(map[string]types.Bead, len(readyBeads))
	for _, bead := range readyBeads {
		readyMap[string(bead.ID)] = bead
	}

	// Check failed beads for retry eligibility
	for beadID, retryInfo := range o.retries {
		// Skip if already in ready pool
		if _, exists := readyMap[beadID]; exists {
			continue
		}

		maxRetries := o.config.Overseer.DefaultRetries
		if maxRetries <= 0 {
			maxRetries = defaultMaxRetries
		}

		// Check if retries exhausted
		if retryInfo.count >= maxRetries {
			log.Printf("[Retry] Bead %s exhausted all %d retry attempts, skipping", beadID, maxRetries)
			continue
		}

		// Check cooldown period
		backoff := calculateBackoff(retryInfo.count, o.baseDelay, o.maxDelay)
		minInterval := defaultRetryCooldown + backoff
		if time.Since(retryInfo.lastError) < minInterval {
			continue
		}

		// Try to fetch the bead from the DAG
		if bead, err := o.getBeadFromDAG(beadID); err == nil {
			readyBeads = append(readyBeads, bead)
			log.Printf("[Retry] Bead %s added to retry queue (attempt %d/%d)",
				beadID, retryInfo.count+1, maxRetries)
		} else {
			log.Printf("[Retry] Could not fetch bead %s for retry: %v", beadID, err)
		}
	}

	return readyBeads
}

// getBeadFromDAG retrieves a bead from the DAG by ID
func (o *Overseer) getBeadFromDAG(beadID string) (types.Bead, error) {
	o.dagMu.RLock()
	defer o.dagMu.RUnlock()

	// Try to find the bead in the DAG
	// This is a simplified implementation - in production, the DAG might have
	// a direct lookup method
	for _, node := range o.dag.GetReadyNodes() {
		if string(node.Bead.ID) == beadID {
			return node.Bead, nil
		}
	}

	return types.Bead{}, fmt.Errorf("bead %s not found in DAG", beadID)
}

// buildDAG fetches all beads from all projects and builds/refreshs the DAG
func (o *Overseer) buildDAG() error {
	o.dagMu.Lock()
	defer o.dagMu.Unlock()

	// If DAG is nil, create a new one (graceful degradation)
	if o.dag == nil {
		log.Printf("[DAG] DAG was nil, creating new instance")
		o.dag = NewDAG()
	}

	// Clear existing DAG
	o.dag.Clear()

	var allBeads []types.Bead
	var fetchErrors []error

	// Fetch all beads from all projects
	for _, project := range o.projects {
		if project.Type == types.RepoTypeSingle {
			beads, err := o.beadsClient.ListBeads(project.Path, "")
			if err != nil {
				// Graceful degradation: log error and continue
				fetchErr := &BeadFetchError{
					Op:      "list",
					Project: project.Name,
					Err:     err,
				}
				o.logGracefulDegradation("beadsClient", "ListBeads",
					fmt.Sprintf("skipping project %s", project.Name), fetchErr)
				fetchErrors = append(fetchErrors, fetchErr)
				continue
			}

			// Fetch full details for each bead to get dependencies
			for _, bead := range beads {
				fullBead, err := o.beadsClient.GetBead(project.Path, "", bead.ID)
				if err != nil {
					// Graceful degradation: use list bead if details fetch fails
					fetchErr := &BeadFetchError{
						Op:      "get",
						Project: project.Name,
						Err:     err,
					}
					o.logGracefulDegradation("beadsClient", "GetBead",
						fmt.Sprintf("using list data for %s", bead.ID), fetchErr)
					allBeads = append(allBeads, bead)
				} else {
					allBeads = append(allBeads, *fullBead)
				}
			}
		} else {
			// Multi-repo project
			for _, repo := range project.Repos {
				beads, err := o.beadsClient.ListBeads(project.Path, repo.Name)
				if err != nil {
					// Graceful degradation: log error and continue
					fetchErr := &BeadFetchError{
						Op:      "list",
						Project: project.Name,
						Repo:    repo.Name,
						Err:     err,
					}
					o.logGracefulDegradation("beadsClient", "ListBeads",
						fmt.Sprintf("skipping repo %s/%s", project.Name, repo.Name), fetchErr)
					fetchErrors = append(fetchErrors, fetchErr)
					continue
				}

				// Fetch full details for each bead to get dependencies
				for _, bead := range beads {
					fullBead, err := o.beadsClient.GetBead(project.Path, repo.Name, bead.ID)
					if err != nil {
						// Graceful degradation: use list bead if details fetch fails
						fetchErr := &BeadFetchError{
							Op:      "get",
							Project: project.Name,
							Repo:    repo.Name,
							Err:     err,
						}
						o.logGracefulDegradation("beadsClient", "GetBead",
							fmt.Sprintf("using list data for %s", bead.ID), fetchErr)
						allBeads = append(allBeads, bead)
					} else {
						allBeads = append(allBeads, *fullBead)
					}
				}
			}
		}
	}

	log.Printf("[DAG] Building DAG with %d beads (from %d projects)", len(allBeads), len(o.projects))

	// Add all beads as nodes to the DAG
	nodeErrors := 0
	for _, bead := range allBeads {
		if err := o.dag.AddNode(bead); err != nil {
			if errors.Is(err, ErrNodeAlreadyExists) {
				log.Printf("[DAG] Duplicate bead skipped: %s", bead.ID)
				continue
			}
			o.logError("DAG", "AddNode", string(bead.ID), err, "warn")
			nodeErrors++
		}
	}

	// Add edges for dependencies
	edgeErrors := 0
	for _, bead := range allBeads {
		beadID := string(bead.ID)
		for _, depID := range bead.Dependencies {
			depIDStr := string(depID)
			if err := o.dag.AddEdge(beadID, depIDStr); err != nil {
				switch {
				case errors.Is(err, ErrNodeNotFound):
					log.Printf("[DAG] Dependency not found: %s -> %s (may be in different project)", depIDStr, beadID)
				case errors.Is(err, ErrSelfReference):
					log.Printf("[DAG] Self-reference detected in bead %s, skipping edge", beadID)
				case errors.Is(err, ErrEdgeCreatesCycle):
					log.Printf("[DAG] Cycle would be created: %s -> %s, skipping edge", depIDStr, beadID)
				default:
					o.logError("DAG", "AddEdge", beadID, err, "warn")
				}
				edgeErrors++
				// Continue even if edge creation fails (graceful degradation)
			}
		}
	}

	// Detect and log cycles
	if cycle, err := o.dag.DetectCycle(); err != nil {
		o.logError("DAG", "DetectCycle", "", err, "error")
	} else if cycle != nil {
		log.Printf("[DAG] Cycle detected: %v", cycle)
	}

	stats := DAGStats{
		TotalNodes:     o.dag.GetNodeCount(),
		ReadyNodes:     len(o.dag.GetReadyNodes()),
		BlockedNodes:   o.dag.GetBlockedCount(),
		CompletedNodes: o.dag.GetCompletedCount(),
		FailedNodes:    o.dag.GetFailedCount(),
	}

	log.Printf("[DAG] Build complete: %+v (fetch errors: %d, node errors: %d, edge errors: %d)",
		stats, len(fetchErrors), nodeErrors, edgeErrors)

	// Return error only if we couldn't fetch any beads at all
	if len(allBeads) == 0 && len(fetchErrors) > 0 {
		return fmt.Errorf("failed to fetch any beads: %d fetch errors occurred", len(fetchErrors))
	}

	return nil
}

// getReadyBeadsFromDAG returns beads that have no incomplete dependencies (InDegree == 0)
func (o *Overseer) getReadyBeadsFromDAG() ([]types.Bead, error) {
	o.dagMu.RLock()
	defer o.dagMu.RUnlock()

	// Get all nodes with InDegree == 0
	nodes := o.dag.GetReadyNodes()

	// Filter and collect ready beads
	var readyBeads []types.Bead
	o.workerMutex.RLock()
	for _, node := range nodes {
		beadID := string(node.Bead.ID)

		// Filter: only StatusOpen
		if node.Bead.Status != types.StatusOpen {
			continue
		}

		// Filter: not already running
		if _, isRunning := o.activeWorkers[beadID]; isRunning {
			continue
		}

		// Filter: not already completed in this session
		if o.completed[beadID] {
			continue
		}

		// Skip approval gates (handled in tick, but also filter here)
		if node.Bead.Type == "approval-gate" {
			continue
		}

		readyBeads = append(readyBeads, node.Bead)
	}
	o.workerMutex.RUnlock()

	// Sort by priority (high to low)
	sort.Slice(readyBeads, func(i, j int) bool {
		return readyBeads[i].Priority > readyBeads[j].Priority
	})

	log.Printf("📋 Found %d ready beads from DAG (after filtering)", len(readyBeads))

	return readyBeads, nil
}

// getReadyBeads retrieves beads that are ready to execute
func (o *Overseer) getReadyBeads() ([]types.Bead, error) {
	var readyBeads []types.Bead

	for _, project := range o.projects {
		if project.Type == types.RepoTypeSingle {
			beads, err := o.beadsClient.GetReadyBeads(project.Path, "")
			if err != nil {
				log.Printf("⚠️  Could not get beads for project %s: %v", project.Name, err)
				continue
			}
			readyBeads = append(readyBeads, beads...)
		} else {
			// Multi-repo project
			for _, repo := range project.Repos {
				beads, err := o.beadsClient.GetReadyBeads(project.Path, repo.Name)
				if err != nil {
					log.Printf("⚠️  Could not get beads for repo %s/%s: %v", project.Name, repo.Name, err)
					continue
				}
				readyBeads = append(readyBeads, beads...)
			}
		}
	}

	return readyBeads, nil
}

// spawnWorker creates and starts a worker for a bead
func (o *Overseer) spawnWorker(bead types.Bead) error {
	beadIDStr := bead.ID.String()

	// Check if worker already exists
	o.workerMutex.RLock()
	if _, exists := o.activeWorkers[beadIDStr]; exists {
		o.workerMutex.RUnlock()
		o.logError("spawnWorker", "duplicate_check", beadIDStr, fmt.Errorf("worker already exists"), "warn")
		return fmt.Errorf("worker already exists for bead %s", beadIDStr)
	}
	o.workerMutex.RUnlock()

	// Check if bead has exhausted retries
	if retryInfo, exists := o.retries[beadIDStr]; exists {
		maxRetries := o.config.Overseer.DefaultRetries
		if maxRetries <= 0 {
			maxRetries = defaultMaxRetries
		}
		if retryInfo.count >= maxRetries {
			err := &RetryExhaustedError{
				BeadID:    beadIDStr,
				Attempts:  retryInfo.count,
				LastError: retryInfo.lastErr,
			}
			o.logError("spawnWorker", "retry_exhausted", beadIDStr, err, "error")
			return err
		}
	}

	// Determine agent type
	agentTypes := types.DetermineAgentTypes(bead.Type)
	if len(agentTypes) == 0 {
		err := fmt.Errorf("no agent type found for bead type: %s", bead.Type)
		o.logError("spawnWorker", "agent_type", beadIDStr, err, "error")
		return err
	}

	agentType := agentTypes[0] // Use primary agent

	retryCount := o.getRetryCount(beadIDStr)
	if retryCount > 0 {
		log.Printf("[Worker] Spawning retry worker for bead: %s (attempt %d, agent: %s)", beadIDStr, retryCount+1, agentType)
	} else {
		log.Printf("[Worker] Spawning worker for bead: %s (agent: %s)", beadIDStr, agentType)
	}

	// Gather pre-execution intelligence (non-blocking, graceful degradation)
	var intelBundle *IntelligenceBundle
	if o.isIntelligenceEnabled() {
		intelBundle, _ = o.runPreExecutionIntelligence(&bead)
		// Intelligence errors are logged within runPreExecutionIntelligence
		// We continue with nil bundle if gathering fails
	}

	// Find project path
	projectPath := o.findProjectPath(&bead)
	if projectPath == "" {
		projectPath = bead.Project // Fallback to project name
	}

	// Build worker command - use compiled binary if available, otherwise fallback to go run
	workerBinary := os.Getenv("HYDRA_WORKER_BINARY")
	if workerBinary == "" {
		workerBinary = filepath.Join(os.Getenv("HYDRA_HOME"), "bin", "worker")
	}

	// Check if compiled binary exists, otherwise use go run
	var cmd *exec.Cmd
	if _, err := os.Stat(workerBinary); err == nil {
		cmd = exec.Command(workerBinary)
	} else {
		// Fallback to go run
		cmd = exec.Command("go", "run", "cmd/worker/main.go")
	}
	env := append(os.Environ(),
		fmt.Sprintf("HYDRA_BEAD_ID=%s", beadIDStr),
		fmt.Sprintf("HYDRA_PROJECT=%s", bead.Project),
		fmt.Sprintf("HYDRA_PROJECT_PATH=%s", projectPath),
		fmt.Sprintf("HYDRA_REPO=%s", bead.Repo),
		fmt.Sprintf("HYDRA_AGENT_TYPE=%s", agentType),
		fmt.Sprintf("HYDRA_RETRY_COUNT=%d", retryCount),
	)

	// Pass intelligence bundle to worker if available
	if intelBundle != nil {
		intelJSON, err := json.Marshal(intelBundle)
		if err == nil {
			env = append(env, fmt.Sprintf("HYDRA_INTELLIGENCE=%s", string(intelJSON)))
			log.Printf("[Worker] Intelligence bundle passed to worker for bead %s (%d bytes)",
				beadIDStr, len(intelJSON))
		} else {
			log.Printf("[Worker] Failed to marshal intelligence bundle: %v", err)
		}
	}

	cmd.Env = env

	// Start process
	if err := cmd.Start(); err != nil {
		spawnErr := &WorkerSpawnError{
			BeadID:    beadIDStr,
			AgentType: string(agentType),
			Err:       err,
		}
		o.logError("spawnWorker", "process_start", beadIDStr, spawnErr, "error")
		return spawnErr
	}

	// Create worker record
	worker := &types.Worker{
		ID:        beadIDStr,
		BeadID:    bead.ID,
		PID:       cmd.Process.Pid,
		AgentType: agentType,
		StartTime: time.Now(),
		Status:    "running",
	}

	// Track worker
	o.workerMutex.Lock()
	o.activeWorkers[beadIDStr] = worker
	o.workerMutex.Unlock()

	// Monitor in goroutine
	go o.monitorWorker(worker, cmd)

	return nil
}

// monitorWorker watches a worker process until completion
func (o *Overseer) monitorWorker(worker *types.Worker, cmd *exec.Cmd) {
	// Wait for process to complete
	err := cmd.Wait()

	// Determine success/failure
	exitCode := 0
	if err != nil {
		if exitError, ok := err.(*exec.ExitError); ok {
			exitCode = exitError.ExitCode()
		} else {
			exitCode = -1
		}
	}

	success := exitCode == 0
	duration := time.Since(worker.StartTime)

	// Update DAG node status based on completion result
	var newlyReady []string
	if success {
		log.Printf("[Worker] Completed: %s (duration: %v)", worker.ID, duration)
		worker.Status = "completed"

		// Clear retry state on success
		o.clearRetry(worker.ID)

		// Mark as completed in DAG - this reduces InDegree of dependents
		o.dagMu.Lock()
		ready, err := o.dag.MarkCompleted(worker.ID)
		o.dagMu.Unlock()
		if err != nil {
			o.logError("DAG", "MarkCompleted", worker.ID, err, "warn")
		} else {
			newlyReady = ready
			if len(ready) > 0 {
				log.Printf("[DAG] %d dependents now ready after %s completion", len(ready), worker.ID)
			}
		}
	} else {
		// Log worker failure with structured error
		workerErr := fmt.Errorf("worker exited with code %d", exitCode)
		o.recordRetry(worker.ID, workerErr)

		retryCount := o.getRetryCount(worker.ID)
		maxRetries := o.config.Overseer.DefaultRetries
		if maxRetries <= 0 {
			maxRetries = defaultMaxRetries
		}

		if retryCount >= maxRetries {
			// Exhausted retries, log as error
			log.Printf("[Worker] Failed permanently: %s (exit code: %d, duration: %v, retries: %d/%d)",
				worker.ID, exitCode, duration, retryCount, maxRetries)
			worker.Status = "failed"
		} else {
			// Will retry, log as warning
			backoff := calculateBackoff(retryCount, o.baseDelay, o.maxDelay)
			log.Printf("[Worker] Failed but will retry: %s (exit code: %d, duration: %v, retry %d/%d, backoff: %v)",
				worker.ID, exitCode, duration, retryCount, maxRetries, backoff)
			worker.Status = "retry_pending"
		}

		// Mark as failed in DAG - dependents remain blocked
		o.dagMu.Lock()
		if err := o.dag.MarkFailed(worker.ID); err != nil {
			o.logError("DAG", "MarkFailed", worker.ID, err, "warn")
		}
		o.dagMu.Unlock()
	}

	// Remove from active workers
	o.workerMutex.Lock()
	delete(o.activeWorkers, worker.ID)
	o.workerMutex.Unlock()

	// Close bead via beadsClient
	if err := o.closeWorkerBead(worker, success); err != nil {
		o.logError("beadsClient", "CloseBead", worker.ID, err, "warn")
	}

	// If successful and new beads are ready, trigger immediate scheduling
	if success && len(newlyReady) > 0 {
		log.Printf("[Scheduler] Triggering immediate scheduling for %d newly ready beads", len(newlyReady))
		go o.triggerImmediateSchedule()
	}

	// If failed but retries remain, trigger scheduling for retry candidates
	if !success && o.getRetryCount(worker.ID) < o.config.Overseer.DefaultRetries {
		log.Printf("[Scheduler] Triggering scheduling to attempt retry for %s", worker.ID)
		go o.triggerImmediateSchedule()
	}

	// Send completion message via MCP
	o.sendMCPWorkerCompletion(worker, success, exitCode, duration)

	// TODO: Record results to cass_memory
}

// closeWorkerBead closes the bead via beadsClient based on worker completion
func (o *Overseer) closeWorkerBead(worker *types.Worker, success bool) error {
	// Extract project and repo from BeadID
	projectName := worker.BeadID.Project()
	repoName := worker.BeadID.Repo()

	log.Printf("[closeWorkerBead] Attempting to close bead %s for project %s (repo: %s)",
		worker.BeadID.String(), projectName, repoName)

	// Find project to get the full path
	var projectPath string
	for _, p := range o.projects {
		if p.Name == projectName {
			projectPath = p.Path
			log.Printf("[closeWorkerBead] Found project %s at path: %s", projectName, projectPath)
			break
		}
	}

	// Fallback: try to find project by walking configured projects
	if projectPath == "" && o.config != nil {
		log.Printf("[closeWorkerBead] Searching in %d configured projects", len(o.config.Projects))
		for _, proj := range o.config.Projects {
			if proj.Name == projectName {
				projectPath = proj.Path
				log.Printf("[closeWorkerBead] Found in config: %s", projectPath)
				break
			}
		}
	}

	// Final fallback: try common paths
	if projectPath == "" {
		potentialPaths := []string{
			filepath.Join("/tmp", projectName),
			filepath.Join(os.Getenv("HOME"), "source-code", projectName),
			filepath.Join(os.Getenv("HOME"), "projects", projectName),
		}

		for _, path := range potentialPaths {
			if _, err := os.Stat(path); err == nil {
				if _, err := os.Stat(filepath.Join(path, ".beads")); err == nil {
					projectPath = path
					log.Printf("[closeWorkerBead] Found in common location: %s", projectPath)
					break
				}
			}
		}
	}

	if projectPath == "" {
		log.Printf("[closeWorkerBead] ERROR: Project %s not found in %d configured projects",
			projectName, len(o.projects))
		for i, p := range o.projects {
			log.Printf("[closeWorkerBead]   Project %d: %s at %s", i, p.Name, p.Path)
		}
		return fmt.Errorf("project %s not found", projectName)
	}

	// Determine close reason based on success
	reason := "completed"
	if !success {
		reason = "failed"
	}

	log.Printf("[closeWorkerBead] Closing bead %s with reason: %s (path: %s)",
		worker.BeadID.String(), reason, projectPath)

	return o.beadsClient.CloseBead(projectPath, repoName, worker.BeadID, reason)
}

// triggerImmediateSchedule spawns workers for ready beads without waiting for next poll
func (o *Overseer) triggerImmediateSchedule() {
	// Check if overseer is paused
	if o.IsPaused() {
		log.Println("⏸️  Immediate scheduling skipped: overseer is paused")
		return
	}

	// Get available worker slots
	o.workerMutex.RLock()
	activeCount := len(o.activeWorkers)
	o.workerMutex.RUnlock()

	maxWorkers := o.GetMaxWorkers()
	availableSlots := maxWorkers - activeCount
	if availableSlots <= 0 {
		log.Printf("📊 Immediate scheduling skipped: no available slots (%d/%d workers active)", activeCount, maxWorkers)
		return
	}

	// Get ready beads from DAG
	o.dagMu.RLock()
	readyNodes := o.dag.GetReadyNodes()
	o.dagMu.RUnlock()

	if len(readyNodes) == 0 {
		return
	}

	log.Printf("📊 Immediate schedule: %d/%d workers active, %d ready beads in DAG", activeCount, maxWorkers, len(readyNodes))

	// Convert nodes to beads and filter out already running ones
	spawned := 0
	for _, node := range readyNodes {
		if availableSlots <= 0 {
			break
		}

		beadID := string(node.Bead.ID)

		// Skip if already running
		if o.isBeadRunning(beadID) {
			continue
		}

		// Skip approval gates (require manual br close)
		if node.Bead.Type == "approval-gate" {
			log.Printf("⏸️  Skipping approval gate: %s", beadID)
			continue
		}

		// Spawn worker for this bead
		if err := o.spawnWorker(node.Bead); err != nil {
			log.Printf("❌ Failed to spawn worker for bead %s: %v", beadID, err)
			continue
		}

		spawned++
		availableSlots--
	}

	if spawned > 0 {
		log.Printf("🚀 Immediate scheduling complete: spawned %d workers", spawned)
	}
}

// isBeadRunning checks if a bead is currently being executed by an active worker
func (o *Overseer) isBeadRunning(beadID string) bool {
	o.workerMutex.RLock()
	defer o.workerMutex.RUnlock()

	_, exists := o.activeWorkers[beadID]
	return exists
}

// GetActiveWorkerCount returns the number of currently active workers
func (o *Overseer) GetActiveWorkerCount() int {
	o.workerMutex.RLock()
	defer o.workerMutex.RUnlock()
	return len(o.activeWorkers)
}

// GetActiveWorkers returns a slice of all active workers
func (o *Overseer) GetActiveWorkers() []types.Worker {
	o.workerMutex.RLock()
	defer o.workerMutex.RUnlock()

	workers := make([]types.Worker, 0, len(o.activeWorkers))
	for _, worker := range o.activeWorkers {
		workers = append(workers, *worker)
	}
	return workers
}

// GetProjects returns the list of discovered projects
func (o *Overseer) GetProjects() []types.Project {
	return o.projects
}

// IsRunning returns whether the overseer is currently running
func (o *Overseer) IsRunning() bool {
	return o.running
}

// IsPaused returns whether the overseer is paused
func (o *Overseer) IsPaused() bool {
	o.pauseMutex.RLock()
	defer o.pauseMutex.RUnlock()
	return o.paused
}

// Pause stops the overseer from spawning new workers
func (o *Overseer) Pause() {
	o.pauseMutex.Lock()
	defer o.pauseMutex.Unlock()
	o.paused = true
	log.Println("⏸️  Overseer paused")
}

// Resume allows the overseer to spawn workers again
func (o *Overseer) Resume() {
	o.pauseMutex.Lock()
	defer o.pauseMutex.Unlock()
	o.paused = false
	log.Println("▶️  Overseer resumed")
}

// SetMaxWorkers updates the maximum number of concurrent workers
func (o *Overseer) SetMaxWorkers(count int) {
	o.maxWorkersMux.Lock()
	defer o.maxWorkersMux.Unlock()
	o.maxWorkers = count
	log.Printf("🔧 Max workers set to %d", count)
}

// GetMaxWorkers returns the current maximum worker count
func (o *Overseer) GetMaxWorkers() int {
	o.maxWorkersMux.RLock()
	defer o.maxWorkersMux.RUnlock()
	return o.maxWorkers
}

// DAGStats represents statistics about the DAG
type DAGStats struct {
	TotalNodes     int `json:"total_nodes"`
	ReadyNodes     int `json:"ready_nodes"`
	BlockedNodes   int `json:"blocked_nodes"`
	CompletedNodes int `json:"completed_nodes"`
	FailedNodes    int `json:"failed_nodes"`
}

// GetDAGStats returns statistics about the current DAG
func (o *Overseer) GetDAGStats() DAGStats {
	o.dagMu.RLock()
	defer o.dagMu.RUnlock()

	if o.dag == nil {
		return DAGStats{}
	}

	return DAGStats{
		TotalNodes:     o.dag.GetNodeCount(),
		ReadyNodes:     len(o.dag.GetReadyNodes()),
		BlockedNodes:   o.dag.GetBlockedCount(),
		CompletedNodes: o.dag.GetCompletedCount(),
		FailedNodes:    o.dag.GetFailedCount(),
	}
}

// GetReadyBeadsFromDAG returns beads that are ready for execution from the DAG
func (o *Overseer) GetReadyBeadsFromDAG() []types.Bead {
	o.dagMu.RLock()
	defer o.dagMu.RUnlock()

	if o.dag == nil {
		return nil
	}

	readyNodes := o.dag.GetReadyNodes()
	beads := make([]types.Bead, len(readyNodes))
	for i, node := range readyNodes {
		beads[i] = node.Bead
	}

	return beads
}

// GetBeadDependencies returns the IDs of beads that the given bead depends on.
// This is a thread-safe wrapper around the DAG's GetDependencies method.
func (o *Overseer) GetBeadDependencies(beadID string) ([]string, error) {
	o.dagMu.RLock()
	defer o.dagMu.RUnlock()

	if o.dag == nil {
		return nil, fmt.Errorf("DAG not initialized")
	}

	return o.dag.GetDependencies(beadID)
}

// GetBeadDependents returns the IDs of beads that depend on the given bead.
// This is a thread-safe wrapper around the DAG's GetDependents method.
func (o *Overseer) GetBeadDependents(beadID string) ([]string, error) {
	o.dagMu.RLock()
	defer o.dagMu.RUnlock()

	if o.dag == nil {
		return nil, fmt.Errorf("DAG not initialized")
	}

	return o.dag.GetDependents(beadID)
}

// GetBeadFromDAG retrieves a single bead from the DAG by its ID.
// Returns error if the bead is not found in the DAG.
func (o *Overseer) GetBeadFromDAG(beadID string) (types.Bead, error) {
	o.dagMu.RLock()
	defer o.dagMu.RUnlock()

	if o.dag == nil {
		return types.Bead{}, fmt.Errorf("DAG not initialized")
	}

	node, exists := o.dag.GetNode(beadID)
	if !exists {
		return types.Bead{}, fmt.Errorf("bead %s not found in DAG", beadID)
	}

	return node.Bead, nil
}

// SetPollInterval updates the poll interval for the overseer
func (o *Overseer) SetPollInterval(interval time.Duration) {
	o.pollInterval = interval
	log.Printf("🔧 Poll interval set to %v", interval)
}

// GetPollInterval returns the current poll interval
func (o *Overseer) GetPollInterval() time.Duration {
	return o.pollInterval
}

// SetRetryDelay updates the base retry delay for the overseer
func (o *Overseer) SetRetryDelay(delay time.Duration) {
	o.baseDelay = delay
	log.Printf("🔧 Retry delay set to %v", delay)
}

// GetRetryDelay returns the current base retry delay
func (o *Overseer) GetRetryDelay() time.Duration {
	return o.baseDelay
}

// CloseBead closes a bead using the beads client
func (o *Overseer) CloseBead(projectPath, repoName string, beadID types.BeadID, reason string) error {
	return o.beadsClient.CloseBead(projectPath, repoName, beadID, reason)
}

// GetBeadsClient returns the beads client
func (o *Overseer) GetBeadsClient() BeadsClient {
	return o.beadsClient
}

// GetDAG returns the current DAG instance.
// This provides access to the dependency graph for field resolvers.
func (o *Overseer) GetDAG() *DAG {
	o.dagMu.RLock()
	defer o.dagMu.RUnlock()
	return o.dag
}

// initializeMCP initializes the MCP client and reservation manager
func (o *Overseer) initializeMCP() error {
	o.mcpMu.Lock()
	defer o.mcpMu.Unlock()

	if !o.config.MCP.Enabled {
		return nil
	}

	o.mcpConfig = &o.config.MCP
	o.activeReservations = make(map[string]*mcp.Reservation)

	// Initialize reservation manager with configured TTL
	ttl := o.config.MCP.FileReservationTTL
	if ttl <= 0 {
		ttl = 30 * time.Minute
	}
	o.mcpReservationMgr = mcp.NewReservationManager()

	// Initialize WebSocket MCP client
	agentID := fmt.Sprintf("hydra-overseer-%d", time.Now().Unix())
	wsURL := o.config.MCP.URL
	if wsURL == "" {
		wsURL = fmt.Sprintf("ws://127.0.0.1:%d/ws", o.config.MCP.Port)
	}

	var err error
	o.mcpClient, err = mcp.NewClient(wsURL, agentID)
	if err != nil {
		log.Printf("[MCP] Failed to create MCP client: %v", err)
		return fmt.Errorf("failed to create MCP client: %w", err)
	}

	// Set up message handler
	o.mcpClient.SetMessageHandler(o.handleMCPMessage)

	log.Printf("[MCP] MCP client initialized (URL: %s, Agent: %s)", wsURL, agentID)
	return nil
}

// StartMCPListener connects to the MCP server and starts listening for messages
func (o *Overseer) StartMCPListener() error {
	o.mcpMu.Lock()
	defer o.mcpMu.Unlock()

	if o.mcpClient == nil {
		return fmt.Errorf("MCP client not initialized")
	}

	if err := o.mcpClient.Connect(); err != nil {
		log.Printf("[MCP] Failed to connect: %v", err)
		return fmt.Errorf("failed to connect to MCP server: %w", err)
	}

	// Register the overseer as an agent
	metadata := mcp.RegistrationMetadata{
		AgentID:   "hydra-overseer",
		Type:      "orchestrator",
		StartTime: time.Now(),
		Metadata: map[string]string{
			"role":    "overseer",
			"version": "1.0.0",
		},
	}

	if err := o.mcpClient.Register(metadata); err != nil {
		log.Printf("[MCP] Failed to register: %v", err)
		// Continue even if registration fails
	} else {
		log.Printf("[MCP] Successfully registered with MCP server")
	}

	return nil
}

// StopMCP disconnects from the MCP server
func (o *Overseer) StopMCP() error {
	o.mcpMu.Lock()
	defer o.mcpMu.Unlock()

	if o.mcpClient == nil {
		return nil
	}

	// Deregister before disconnecting
	if o.mcpClient.IsConnected() {
		if err := o.mcpClient.Deregister(); err != nil {
			log.Printf("[MCP] Failed to deregister: %v", err)
		}
	}

	if err := o.mcpClient.Disconnect(); err != nil {
		log.Printf("[MCP] Error during disconnect: %v", err)
		return err
	}

	log.Printf("[MCP] Disconnected from MCP server")
	return nil
}

// handleMCPMessage processes incoming MCP messages
func (o *Overseer) handleMCPMessage(msg mcp.Message) {
	log.Printf("[MCP] Received message: type=%s, from=%s", msg.Type, msg.From)

	switch msg.Type {
	case mcp.MsgTypeTaskComplete:
		o.handleTaskCompleteMessage(&msg)
	case mcp.MsgTypeTaskStart:
		o.handleTaskStartMessage(&msg)
	case mcp.MsgTypeHeartbeat:
		// Heartbeat messages are handled automatically
		log.Printf("[MCP] Heartbeat from %s", msg.From)
	case mcp.MsgTypeLog:
		o.handleLogMessage(&msg)
	case mcp.MsgTypeFileReserved:
		o.handleFileReservedMessage(&msg)
	case mcp.MsgTypeFileReleased:
		o.handleFileReleasedMessage(&msg)
	default:
		log.Printf("[MCP] Unknown message type: %s", msg.Type)
	}
}

// HandleMCPMessage processes an MCP message (public API)
func (o *Overseer) HandleMCPMessage(msg *mcp.Message) error {
	if msg == nil {
		return fmt.Errorf("nil message")
	}
	o.handleMCPMessage(*msg)
	return nil
}

// handleTaskCompleteMessage processes task completion messages from workers
func (o *Overseer) handleTaskCompleteMessage(msg *mcp.Message) {
	beadID, ok := msg.Payload["bead_id"].(string)
	if !ok || beadID == "" {
		log.Printf("[MCP] Task complete message missing bead_id")
		return
	}

	status, _ := msg.Payload["status"].(string)
	exitCode := 0
	if code, ok := msg.Payload["exit_code"].(float64); ok {
		exitCode = int(code)
	}

	log.Printf("[MCP] Task complete: bead=%s, status=%s, exit_code=%d", beadID, status, exitCode)

	// Trigger scheduling cycle for next tasks
	go o.triggerImmediateSchedule()
}

// handleTaskStartMessage processes task start messages
func (o *Overseer) handleTaskStartMessage(msg *mcp.Message) {
	beadID, ok := msg.Payload["bead_id"].(string)
	if !ok || beadID == "" {
		return
	}

	log.Printf("[MCP] Task started: bead=%s, agent=%s", beadID, msg.From)
}

// handleLogMessage processes log messages from agents
func (o *Overseer) handleLogMessage(msg *mcp.Message) {
	level, _ := msg.Payload["level"].(string)
	message, _ := msg.Payload["message"].(string)
	beadID, _ := msg.Payload["bead_id"].(string)

	log.Printf("[MCP Log] [%s] [bead=%s] %s", level, beadID, message)
}

// handleFileReservedMessage processes file reservation messages
func (o *Overseer) handleFileReservedMessage(msg *mcp.Message) {
	beadID, _ := msg.Payload["bead_id"].(string)
	filePath, _ := msg.Payload["file_path"].(string)

	log.Printf("[MCP] File reserved: bead=%s, file=%s", beadID, filePath)
}

// handleFileReleasedMessage processes file release messages
func (o *Overseer) handleFileReleasedMessage(msg *mcp.Message) {
	beadID, _ := msg.Payload["bead_id"].(string)
	filePath, _ := msg.Payload["file_path"].(string)
	committed, _ := msg.Payload["committed"].(bool)

	log.Printf("[MCP] File released: bead=%s, file=%s, committed=%v", beadID, filePath, committed)
}

// GetActiveReservations returns all active file reservations
func (o *Overseer) GetActiveReservations() []*mcp.Reservation {
	o.mcpMu.RLock()
	defer o.mcpMu.RUnlock()

	if o.mcpReservationMgr == nil {
		return nil
	}

	allRes := o.mcpReservationMgr.GetAllReservations()
	var active []*mcp.Reservation
	for _, res := range allRes {
		if !res.IsExpired() {
			active = append(active, res)
		}
	}

	return active
}

// ForceReleaseReservation forcibly releases a reservation by ID
func (o *Overseer) ForceReleaseReservation(reservationID string) error {
	o.mcpMu.Lock()
	defer o.mcpMu.Unlock()

	if o.mcpReservationMgr == nil {
		return fmt.Errorf("reservation manager not initialized")
	}

	o.mcpReservationMgr.RemoveReservation(reservationID)
	log.Printf("[MCP] Force released reservation: %s", reservationID)
	return nil
}

// BroadcastToWorkers sends a broadcast message to all connected workers
func (o *Overseer) BroadcastToWorkers(msgType string, payload map[string]interface{}) error {
	o.mcpMu.RLock()
	client := o.mcpClient
	o.mcpMu.RUnlock()

	if client == nil {
		return fmt.Errorf("MCP client not initialized")
	}

	if !client.IsConnected() {
		return fmt.Errorf("MCP client not connected")
	}

	if err := client.Broadcast(msgType, payload); err != nil {
		return fmt.Errorf("failed to broadcast message: %w", err)
	}

	log.Printf("[MCP] Broadcast message sent: type=%s", msgType)
	return nil
}

// SendLogToMCP sends a log message to the MCP server
func (o *Overseer) SendLogToMCP(level, message string) error {
	o.mcpMu.RLock()
	client := o.mcpClient
	o.mcpMu.RUnlock()

	if client == nil || !client.IsConnected() {
		return fmt.Errorf("MCP client not available")
	}

	return client.SendLog(level, message)
}

// GetMCPStatus returns the current MCP connection status
func (o *Overseer) GetMCPStatus() mcp.ClientStatus {
	o.mcpMu.RLock()
	defer o.mcpMu.RUnlock()

	if o.mcpClient == nil {
		return mcp.ClientStatus{
			Enabled:   o.config.MCP.Enabled,
			Connected: false,
		}
	}

	return o.mcpClient.GetStatus()
}

// IsMCPConnected returns whether the MCP client is connected
func (o *Overseer) IsMCPConnected() bool {
	o.mcpMu.RLock()
	defer o.mcpMu.RUnlock()

	if o.mcpClient == nil {
		return false
	}

	return o.mcpClient.IsConnected()
}

// sendMCPWorkerCompletion sends a task completion message via MCP
func (o *Overseer) sendMCPWorkerCompletion(worker *types.Worker, success bool, exitCode int, duration time.Duration) {
	o.mcpMu.RLock()
	client := o.mcpClient
	o.mcpMu.RUnlock()

	if client == nil || !client.IsConnected() {
		return
	}

	status := "success"
	if !success {
		if o.getRetryCount(worker.ID) >= o.config.Overseer.DefaultRetries {
			status = "failure"
		} else {
			status = "partial"
		}
	}

	payload := map[string]interface{}{
		"bead_id":    worker.ID,
		"status":     status,
		"exit_code":  exitCode,
		"duration":   duration.String(),
		"agent_type": string(worker.AgentType),
	}

	if err := client.Broadcast(mcp.MsgTypeTaskComplete, payload); err != nil {
		log.Printf("[MCP] Failed to send task completion: %v", err)
	} else {
		log.Printf("[MCP] Task completion broadcast: bead=%s, status=%s", worker.ID, status)
	}
}
