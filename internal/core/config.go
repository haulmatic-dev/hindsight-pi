package core

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

// Config represents the Hydra configuration
type Config struct {
	Overseer     OverseerConfig     `yaml:"overseer"`
	Projects     []ProjectConfig    `yaml:"projects"`
	MCP          MCPConfig          `yaml:"mcp"`
	API          APIConfig          `yaml:"api"`
	Intelligence IntelligenceConfig `yaml:"intelligence"`
	Agents       AgentsConfig       `yaml:"agents"`
	Pi           PiRootConfig       `yaml:"pi"`
}

// AgentsConfig contains agent connector settings
type AgentsConfig struct {
	Enabled      bool     `yaml:"enabled"`
	DefaultAgent string   `yaml:"default_agent"`
	Pi           PiConfig `yaml:"pi"`
}

// PiConfig contains Pi coding agent settings
type PiConfig struct {
	Enabled       bool   `yaml:"enabled"`
	Provider      string `yaml:"provider"`
	Model         string `yaml:"model"`
	ThinkingLevel string `yaml:"thinking_level"`
	Timeout       string `yaml:"timeout"`
	ApiKeyName    string `yaml:"api_key_name"`
}

// PiRootConfig contains Pi-specific configuration settings
type PiRootConfig struct {
	ConfigDir   string         `yaml:"config_dir"`
	PiConfigDir string         `yaml:"pi_config_dir"`
	Skills      PiSkillsConfig `yaml:"skills"`
	LSP         LSPConfig      `yaml:"lsp"`
}

// LSPConfig contains LSP (Language Server Protocol) settings
type LSPConfig struct {
	Enabled bool                       `yaml:"enabled"`
	Servers map[string]LSPServerConfig `yaml:"servers"`
}

// LSPServerConfig contains configuration for an individual LSP server
type LSPServerConfig struct {
	Command      string   `yaml:"command"`
	Args         []string `yaml:"args"`
	InstallCheck string   `yaml:"install_check"`
	InstallCmd   string   `yaml:"install_cmd"`
}

// PiSkillsConfig contains Pi skills configuration
type PiSkillsConfig struct {
	UpdateInterval string       `yaml:"update_interval"`
	AutoUpdate     bool         `yaml:"auto_update"`
	Global         bool         `yaml:"global"`
	List           []SkillEntry `yaml:"list"`
}

// SkillEntry represents a skill in the Pi skills list
type SkillEntry struct {
	Name string `yaml:"name"`
	Repo string `yaml:"repo"`
}

// rawConfig is used for YAML parsing to handle the nested projects structure
type rawConfig struct {
	Overseer     OverseerConfig     `yaml:"overseer"`
	Projects     yaml.Node          `yaml:"projects"` // Can be array or object
	MCP          MCPConfig          `yaml:"mcp"`
	API          APIConfig          `yaml:"api"`
	Intelligence IntelligenceConfig `yaml:"intelligence"`
	Agents       AgentsConfig       `yaml:"agents"`
	Pi           rawPiConfig        `yaml:"pi"`
}

// rawPiConfig is used for YAML parsing of Pi configuration
type rawPiConfig struct {
	ConfigDir   string         `yaml:"config_dir"`
	PiConfigDir string         `yaml:"pi_config_dir"`
	Skills      PiSkillsConfig `yaml:"skills"`
	LSP         LSPConfig      `yaml:"lsp"`
}

// projectsListConfig handles the case where projects is an object with 'list' field
type projectsListConfig struct {
	AutoDiscover bool            `yaml:"auto_discover"`
	List         []ProjectConfig `yaml:"list"`
}

// IntelligenceConfig contains configuration for intelligence services
type IntelligenceConfig struct {
	CassMemory            CassMemoryConfig            `yaml:"cass_memory"`
	TLDR                  TLDRConfig                  `yaml:"tldr"`
	GPTCache              GPTCacheConfig              `yaml:"gptcache"`
	SettledState          SettledStateConfig          `yaml:"settled_state"`
	Patterns              PatternConfig               `yaml:"patterns"`
	CollectiveUnconscious CollectiveUnconsciousConfig `yaml:"collective_unconscious"`
}

// PatternConfig contains pattern extraction settings
type PatternConfig struct {
	Enabled             bool    `yaml:"enabled"`
	StoragePath         string  `yaml:"storage_path"`
	MinConfidence       float64 `yaml:"min_confidence"`
	MaxPatternsPerBead  int     `yaml:"max_patterns_per_bead"`
	ExtractCodeSnippets bool    `yaml:"extract_code_snippets"`
}

// CollectiveUnconsciousConfig contains Collective Unconscious settings
type CollectiveUnconsciousConfig struct {
	Enabled  bool             `yaml:"enabled"`
	Storage  CUStorageConfig  `yaml:"storage"`
	Matching CUMatchingConfig `yaml:"matching"`
	Review   CUReviewConfig   `yaml:"review"`
	Feedback CUFeedbackConfig `yaml:"feedback"`
	Security CUSecurityConfig `yaml:"security"`
}

// CUStorageConfig contains storage settings for Collective Unconscious
type CUStorageConfig struct {
	Backend  string        `yaml:"backend"` // "cass_memory", "postgres", "hybrid"
	CacheTTL time.Duration `yaml:"cache_ttl"`
}

// CUMatchingConfig contains pattern matching settings
type CUMatchingConfig struct {
	Enabled                bool          `yaml:"enabled"`
	BlockOnFailure         bool          `yaml:"block_on_failure"`
	MaxPatternsInPrompt    int           `yaml:"max_patterns_in_prompt"`
	MinConfidenceThreshold float64       `yaml:"min_confidence_threshold"`
	CacheEnabled           bool          `yaml:"cache_enabled"`
	CacheTTL               time.Duration `yaml:"cache_ttl"`
	QueryTimeout           time.Duration `yaml:"query_timeout"`
}

// CUReviewConfig contains pattern review settings
type CUReviewConfig struct {
	Enabled              bool          `yaml:"enabled"`
	AutoApproveThreshold float64       `yaml:"auto_approve_threshold"`
	AutoDiscardThreshold float64       `yaml:"auto_discard_threshold"`
	RequireHumanReview   bool          `yaml:"require_human_review"`
	MaxPatternsInReview  int           `yaml:"max_patterns_in_review"`
	DuplicateThreshold   float64       `yaml:"duplicate_threshold"`
	ReviewTimeout        time.Duration `yaml:"review_timeout"`
}

// CUFeedbackConfig contains feedback loop settings
type CUFeedbackConfig struct {
	TrackOutcomes           bool    `yaml:"track_outcomes"`
	CollectExplicitFeedback bool    `yaml:"collect_explicit_feedback"`
	DecayRate               float64 `yaml:"decay_rate"` // Per day
}

// CUSecurityConfig contains security settings for patterns
type CUSecurityConfig struct {
	ScanForSecrets   bool     `yaml:"scan_for_secrets"`
	AllowedLicenses  []string `yaml:"allowed_licenses"`
	SanitizePatterns bool     `yaml:"sanitize_patterns"`
}

// CassMemoryConfig contains cass_memory service settings
type CassMemoryConfig struct {
	Enabled     bool          `yaml:"enabled"`
	QueryLimit  int           `yaml:"query_limit"`
	HistoryDays int           `yaml:"history_days"`
	Timeout     time.Duration `yaml:"timeout"`
}

// TLDRIndexingConfig contains TLDR indexing settings
type TLDRIndexingConfig struct {
	Enabled       bool          `yaml:"enabled"`
	MaxConcurrent int           `yaml:"max_concurrent"`
	QueueSize     int           `yaml:"queue_size"`
	RetryAttempts int           `yaml:"retry_attempts"`
	RetryDelay    time.Duration `yaml:"retry_delay"`
}

// TLDRConfig contains TLDR service settings
type TLDRConfig struct {
	Enabled   bool               `yaml:"enabled"`
	BaseURL   string             `yaml:"base_url"`
	Timeout   time.Duration      `yaml:"timeout"`
	MaxFiles  int                `yaml:"max_files"`
	MaxTokens int                `yaml:"max_tokens"`
	Indexing  TLDRIndexingConfig `yaml:"indexing"`
}

// GPTCacheConfig contains GPTCache service settings
type GPTCacheConfig struct {
	Enabled   bool   `yaml:"enabled"`
	ServerURL string `yaml:"server_url"`
}

// OverseerConfig contains overseer-specific settings
type OverseerConfig struct {
	BeadDiscoveryInterval time.Duration            `yaml:"bead_discovery_interval"`
	MaxWorkers            int                      `yaml:"max_workers"`
	DefaultRetries        int                      `yaml:"default_retries"`
	MaxRetries            int                      `yaml:"max_retries"`
	WorkerTimeout         time.Duration            `yaml:"worker_timeout"`
	RetryDelay            time.Duration            `yaml:"retry_delay"`
	ResourceMonitoring    ResourceMonitoringConfig `yaml:"resource_monitoring"`
	ChainDetection        ChainDetectionConfig     `yaml:"chain_detection"`
}

type ChainDetectionConfig struct {
	Enabled  bool          `yaml:"enabled"`
	Interval time.Duration `yaml:"interval"`
	Timeout  time.Duration `yaml:"timeout"`
}

// ResourceMonitoringConfig contains resource monitoring settings
type ResourceMonitoringConfig struct {
	Enabled          bool    `yaml:"enabled"`
	MaxCPUPercent    float64 `yaml:"max_cpu_percent"`
	MaxMemoryPercent float64 `yaml:"max_memory_percent"`
}

// ProjectConfig represents a project configuration
type ProjectConfig struct {
	Name  string       `yaml:"name"`
	Path  string       `yaml:"path"`
	Type  string       `yaml:"type"` // "single" or "multi"
	Repos []RepoConfig `yaml:"repos,omitempty"`
}

// RepoConfig represents a repository within a multi-repo project
type RepoConfig struct {
	Name string `yaml:"name"`
	Path string `yaml:"path"`
}

// MCPConfig contains MCP (Model Context Protocol) Agent Mail settings
type MCPConfig struct {
	Enabled              bool          `yaml:"enabled"`
	URL                  string        `yaml:"url"`
	Port                 int           `yaml:"port"`
	BaseURL              string        `yaml:"base_url"`
	ReconnectInterval    time.Duration `yaml:"reconnect_interval"`
	MaxReconnectAttempts int           `yaml:"max_reconnect_attempts"`
	HeartbeatInterval    time.Duration `yaml:"heartbeat_interval"`
	FileReservationTTL   time.Duration `yaml:"file_reservation_ttl"`
}

// APIConfig contains API server settings
type APIConfig struct {
	Enabled bool `yaml:"enabled"`
	Port    int  `yaml:"port"`
}

// DefaultConfig returns the default configuration
func DefaultConfig() *Config {
	return &Config{
		Overseer: OverseerConfig{
			BeadDiscoveryInterval: 5 * time.Second,
			MaxWorkers:            6,
			DefaultRetries:        3,
			WorkerTimeout:         30 * time.Minute,
			ChainDetection: ChainDetectionConfig{
				Enabled:  true,
				Interval: 10 * time.Second,
				Timeout:  10 * time.Minute,
			},
		},
		MCP: MCPConfig{
			Enabled:              true,
			Port:                 8765,
			BaseURL:              "http://127.0.0.1:8765",
			URL:                  "ws://127.0.0.1:8765/ws",
			ReconnectInterval:    5 * time.Second,
			MaxReconnectAttempts: 10,
			HeartbeatInterval:    30 * time.Second,
			FileReservationTTL:   30 * time.Minute,
		},
		API: APIConfig{
			Enabled: true,
			Port:    8080,
		},
		Intelligence: IntelligenceConfig{
			CassMemory: CassMemoryConfig{
				Enabled:     true,
				QueryLimit:  10,
				HistoryDays: 30,
				Timeout:     5 * time.Second,
			},
			TLDR: TLDRConfig{
				Enabled:   true,
				BaseURL:   "http://127.0.0.1:8787",
				Timeout:   10 * time.Second,
				MaxFiles:  50,
				MaxTokens: 100000,
				Indexing: TLDRIndexingConfig{
					Enabled:       true,
					MaxConcurrent: 2,
					QueueSize:     100,
					RetryAttempts: 3,
					RetryDelay:    5 * time.Second,
				},
			},
			GPTCache: GPTCacheConfig{
				Enabled:   true,
				ServerURL: "http://127.0.0.1:8788",
			},
			SettledState: DefaultSettledStateConfig(),
			Patterns: PatternConfig{
				Enabled:             true,
				StoragePath:         "",
				MinConfidence:       0.7,
				MaxPatternsPerBead:  10,
				ExtractCodeSnippets: true,
			},
			CollectiveUnconscious: CollectiveUnconsciousConfig{
				Enabled: true,
				Storage: CUStorageConfig{
					Backend:  "cass_memory",
					CacheTTL: 5 * time.Minute,
				},
				Matching: CUMatchingConfig{
					Enabled:                true,
					BlockOnFailure:         true,
					MaxPatternsInPrompt:    3,
					MinConfidenceThreshold: 0.50, // Lowered from 0.70 for more pattern matches
					CacheEnabled:           true,
					CacheTTL:               5 * time.Minute,
					QueryTimeout:           5 * time.Second,
				},
				Review: CUReviewConfig{
					Enabled:              true,
					AutoApproveThreshold: 0.75, // Lowered from 0.90 for more auto-approvals
					AutoDiscardThreshold: 0.40, // Lowered from 0.50 to keep borderline patterns,
					RequireHumanReview:   false,
					MaxPatternsInReview:  100,
					DuplicateThreshold:   0.95,
					ReviewTimeout:        24 * time.Hour,
				},
				Feedback: CUFeedbackConfig{
					TrackOutcomes:           true,
					CollectExplicitFeedback: true,
					DecayRate:               0.01, // Per day
				},
				Security: CUSecurityConfig{
					ScanForSecrets:   true,
					AllowedLicenses:  []string{"MIT", "Apache-2.0", "BSD-3-Clause"},
					SanitizePatterns: true,
				},
			},
		},
		Agents: AgentsConfig{
			Enabled:      true,
			DefaultAgent: "pi",
			Pi: PiConfig{
				Enabled:       true,
				Provider:      "kimi",
				Model:         "k2.5",
				ThinkingLevel: "medium",
				Timeout:       "10m",
			},
		},
		Pi: PiRootConfig{
			ConfigDir: "./pi-config",
			Skills: PiSkillsConfig{
				UpdateInterval: "24h",
				AutoUpdate:     true,
				Global:         false,
				List:           []SkillEntry{},
			},
			LSP: LSPConfig{
				Enabled: true,
				Servers: map[string]LSPServerConfig{},
			},
		},
	}
}

// ConfigCache provides thread-safe caching for loaded configurations
type ConfigCache struct {
	mu          sync.RWMutex
	config      *Config
	path        string
	modTime     time.Time
	hits        uint64
	misses      uint64
	validations uint64
}

// Global cache instance
var globalCache = &ConfigCache{}

// CacheStats returns cache statistics
func (cc *ConfigCache) Stats() (hits, misses, validations uint64) {
	cc.mu.RLock()
	defer cc.mu.RUnlock()
	return cc.hits, cc.misses, cc.validations
}

// ResetCache clears the global config cache
func ResetCache() {
	globalCache.mu.Lock()
	defer globalCache.mu.Unlock()
	globalCache.config = nil
	globalCache.path = ""
	globalCache.modTime = time.Time{}
	globalCache.hits = 0
	globalCache.misses = 0
	globalCache.validations = 0
}

// getCachedConfig retrieves cached config if still valid
func (cc *ConfigCache) get(path string) (*Config, bool) {
	cc.mu.RLock()
	defer cc.mu.RUnlock()

	if cc.config == nil || cc.path != path {
		return nil, false
	}

	// Check if file has been modified
	info, err := os.Stat(path)
	if err != nil {
		return nil, false
	}

	if info.ModTime() != cc.modTime {
		return nil, false
	}

	cc.hits++
	// Return a clone to prevent external modification
	return cc.config.Clone(), true
}

// set stores config in cache
func (cc *ConfigCache) set(path string, cfg *Config, modTime time.Time) {
	cc.mu.Lock()
	defer cc.mu.Unlock()
	cc.config = cfg.Clone()
	cc.path = path
	cc.modTime = modTime
	cc.misses++
}

// LoadConfig loads configuration from a YAML file with caching
func LoadConfig(path string) (*Config, error) {
	// Try cache first
	if cfg, ok := globalCache.get(path); ok {
		return cfg, nil
	}

	// Load from file
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	// Get file info for cache timestamp
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("failed to stat config file: %w", err)
	}

	// Start with default config
	config := DefaultConfig()

	// Parse raw config to handle projects array vs object
	var raw rawConfig
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("failed to parse config YAML: %w", err)
	}

	// Copy non-project fields
	config.Overseer = raw.Overseer
	config.MCP = raw.MCP
	config.API = raw.API
	config.Intelligence = raw.Intelligence
	config.Agents = raw.Agents
	config.Pi = PiRootConfig{
		ConfigDir:   raw.Pi.ConfigDir,
		PiConfigDir: raw.Pi.PiConfigDir,
		Skills:      raw.Pi.Skills,
		LSP:         raw.Pi.LSP,
	}

	// Handle projects field - can be:
	// 1. An array of ProjectConfig (direct list)
	// 2. An object with 'auto_discover' and 'list' fields
	// 3. Empty/null (use auto-discovery)
	if !isZeroNode(raw.Projects) {
		if raw.Projects.Kind == yaml.SequenceNode {
			// Projects is an array
			if err := raw.Projects.Decode(&config.Projects); err != nil {
				return nil, fmt.Errorf("failed to parse projects array: %w", err)
			}
		} else if raw.Projects.Kind == yaml.MappingNode {
			// Projects is an object - check if it's the {auto_discover, list} format
			var projectsObj projectsListConfig
			if err := raw.Projects.Decode(&projectsObj); err == nil && len(projectsObj.List) > 0 {
				// It's the object format with list
				config.Projects = projectsObj.List
			} else {
				// Try parsing as direct array anyway
				if err := raw.Projects.Decode(&config.Projects); err != nil {
					return nil, fmt.Errorf("failed to parse projects: %w", err)
				}
			}
		}
	}

	// Cache the loaded config
	globalCache.set(path, config, info.ModTime())

	return config, nil
}

// LoadConfigUncached loads configuration without using cache (for testing)
func LoadConfigUncached(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	// Start with default config
	config := DefaultConfig()

	// Parse raw config to handle projects array vs object
	var raw rawConfig
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("failed to parse config YAML: %w", err)
	}

	// Copy non-project fields
	config.Overseer = raw.Overseer
	config.MCP = raw.MCP
	config.API = raw.API
	config.Intelligence = raw.Intelligence
	config.Agents = raw.Agents
	config.Pi = PiRootConfig{
		ConfigDir:   raw.Pi.ConfigDir,
		PiConfigDir: raw.Pi.PiConfigDir,
		Skills:      raw.Pi.Skills,
		LSP:         raw.Pi.LSP,
	}

	// Handle projects field
	if !isZeroNode(raw.Projects) {
		if raw.Projects.Kind == yaml.SequenceNode {
			if err := raw.Projects.Decode(&config.Projects); err != nil {
				return nil, fmt.Errorf("failed to parse projects array: %w", err)
			}
		} else if raw.Projects.Kind == yaml.MappingNode {
			var projectsObj projectsListConfig
			if err := raw.Projects.Decode(&projectsObj); err == nil && len(projectsObj.List) > 0 {
				config.Projects = projectsObj.List
			} else {
				if err := raw.Projects.Decode(&config.Projects); err != nil {
					return nil, fmt.Errorf("failed to parse projects: %w", err)
				}
			}
		}
	}

	return config, nil
}

// isZeroNode checks if a yaml.Node is empty/zero value
func isZeroNode(n yaml.Node) bool {
	return n.Kind == 0 && n.Tag == "" && len(n.Content) == 0
}

// Clone creates a deep copy of the Config
func (c *Config) Clone() *Config {
	if c == nil {
		return nil
	}

	cloned := &Config{
		Overseer:     c.Overseer,
		MCP:          c.MCP,
		API:          c.API,
		Intelligence: c.Intelligence,
		Agents:       c.Agents,
		Pi:           c.Pi,
	}

	// Deep copy Projects slice
	if c.Projects != nil {
		cloned.Projects = make([]ProjectConfig, len(c.Projects))
		copy(cloned.Projects, c.Projects)
	}

	return cloned
}

// ValidationError represents a single validation error
type ValidationError struct {
	Field   string
	Message string
}

func (v ValidationError) Error() string {
	return fmt.Sprintf("%s: %s", v.Field, v.Message)
}

// ValidationErrors holds multiple validation errors
type ValidationErrors []ValidationError

func (v ValidationErrors) Error() string {
	if len(v) == 0 {
		return "no validation errors"
	}
	if len(v) == 1 {
		return v[0].Error()
	}

	// Pre-allocate with exact capacity needed
	messages := make([]string, 0, len(v))
	for _, err := range v {
		messages = append(messages, err.Error())
	}
	return fmt.Sprintf("%d validation errors:\n  - %s", len(v), strings.Join(messages, "\n  - "))
}

// IsValid returns true if there are no validation errors
func (v ValidationErrors) IsValid() bool {
	return len(v) == 0
}

// Pre-allocated constants for validation to reduce allocations
const (
	fieldProjects                  = "projects"
	fieldOverseerMaxWorkers        = "overseer.max_workers"
	fieldOverseerBeadDiscovery     = "overseer.bead_discovery_interval"
	fieldPiConfigDir               = "pi.config_dir"
	fieldPiSkillsUpdateInterval    = "pi.skills.update_interval"
	fieldPiSkillsListName          = "pi.skills.list[%d].name"
	fieldPiSkillsListRepo          = "pi.skills.list[%d].repo"
	msgNoProjects                  = "no projects configured"
	msgMaxWorkersNegative          = "max_workers cannot be negative (use 0 for unlimited)"
	msgBeadDiscoveryIntervalTooLow = "bead_discovery_interval must be at least 1 second"
	msgProjectPathRequired         = "project path is required"
	msgProjectNameRequired         = "project name is required"
	msgConfigDirInvalidChars       = "config_dir contains invalid characters"
	msgUpdateIntervalInvalid       = "update_interval must be a valid duration (e.g., '24h', '30m'): %v"
	msgUpdateIntervalTooShort      = "update_interval must be at least 1 minute"
	msgUpdateIntervalTooLong       = "update_interval should not exceed 30 days"
	msgSkillNameRequired           = "skill name is required"
	msgSkillRepoInvalid            = "repo must be a valid git URL (https:// or git@)"
)

// ValidateConfig validates the config and returns all validation errors
func ValidateConfig(cfg *Config) ValidationErrors {
	if cfg == nil {
		return ValidationErrors{{Field: "config", Message: "config is nil"}}
	}

	var errors ValidationErrors
	// Pre-allocate with reasonable capacity (typical configs have < 10 errors)
	errors = make(ValidationErrors, 0, 8)

	// Check if we have any projects
	if len(cfg.Projects) == 0 {
		errors = append(errors, ValidationError{
			Field:   fieldProjects,
			Message: msgNoProjects,
		})
	}

	// Validate overseer settings - fast checks first
	if cfg.Overseer.MaxWorkers < 0 {
		errors = append(errors, ValidationError{
			Field:   fieldOverseerMaxWorkers,
			Message: msgMaxWorkersNegative,
		})
	}

	if cfg.Overseer.BeadDiscoveryInterval < time.Second {
		errors = append(errors, ValidationError{
			Field:   fieldOverseerBeadDiscovery,
			Message: msgBeadDiscoveryIntervalTooLow,
		})
	}

	// Validate project paths
	for i, proj := range cfg.Projects {
		if proj.Path == "" {
			errors = append(errors, ValidationError{
				Field:   fmt.Sprintf("projects[%d].path", i),
				Message: msgProjectPathRequired,
			})
		}
		if proj.Name == "" {
			errors = append(errors, ValidationError{
				Field:   fmt.Sprintf("projects[%d].name", i),
				Message: msgProjectNameRequired,
			})
		}
	}

	// Validate PI configuration
	if piErrors := validatePiConfigFast(&cfg.Pi); len(piErrors) > 0 {
		errors = append(errors, piErrors...)
	}

	return errors
}

// validatePiConfigFast is an optimized version of validatePiConfig
func validatePiConfigFast(cfg *PiRootConfig) ValidationErrors {
	if cfg == nil {
		return nil
	}

	var errors ValidationErrors
	errors = make(ValidationErrors, 0, 4)

	// Fast path: skip empty config_dir check
	if cfg.ConfigDir != "" {
		// Check for null bytes only (most common invalid char)
		if strings.Contains(cfg.ConfigDir, "\x00") {
			errors = append(errors, ValidationError{
				Field:   fieldPiConfigDir,
				Message: msgConfigDirInvalidChars,
			})
		}
	}

	// Validate update_interval
	if cfg.Skills.UpdateInterval != "" {
		if duration, err := time.ParseDuration(cfg.Skills.UpdateInterval); err != nil {
			errors = append(errors, ValidationError{
				Field:   fieldPiSkillsUpdateInterval,
				Message: fmt.Sprintf(msgUpdateIntervalInvalid, err),
			})
		} else {
			// Single comparison using constants
			const minDuration = time.Minute
			const maxDuration = 30 * 24 * time.Hour

			if duration < minDuration {
				errors = append(errors, ValidationError{
					Field:   fieldPiSkillsUpdateInterval,
					Message: msgUpdateIntervalTooShort,
				})
			} else if duration > maxDuration {
				errors = append(errors, ValidationError{
					Field:   fieldPiSkillsUpdateInterval,
					Message: msgUpdateIntervalTooLong,
				})
			}
		}
	}

	// Validate skill entries - pre-check if any skills exist
	if len(cfg.Skills.List) > 0 {
		for i, skill := range cfg.Skills.List {
			if skill.Name == "" {
				errors = append(errors, ValidationError{
					Field:   fmt.Sprintf(fieldPiSkillsListName, i),
					Message: msgSkillNameRequired,
				})
			}
			if skill.Repo != "" && !isValidSkillRepoFast(skill.Repo) {
				errors = append(errors, ValidationError{
					Field:   fmt.Sprintf(fieldPiSkillsListRepo, i),
					Message: msgSkillRepoInvalid,
				})
			}
		}
	}

	return errors
}

// isValidSkillRepoFast is an optimized version with early returns
func isValidSkillRepoFast(repo string) bool {
	if repo == "" {
		return true
	}

	// Check most common prefixes first (https and git@)
	if len(repo) > 8 {
		switch repo[0] {
		case 'h':
			if repo[0:8] == "https://" {
				return true
			}
			if len(repo) > 7 && repo[0:7] == "http://" {
				return true
			}
		case 'g':
			if len(repo) > 4 && repo[0:4] == "git@" {
				return true
			}
			if len(repo) > 6 && repo[0:6] == "git://" {
				return true
			}
		case 's':
			if len(repo) > 6 && repo[0:6] == "ssh://" {
				return true
			}
		case '/':
			return true
		case '.':
			if len(repo) > 1 && repo[1] == '/' {
				return true
			}
		}
	}

	return false
}

// ValidateConfigSingleError validates the config and returns a single error if any validation fails.
func ValidateConfigSingleError(cfg *Config) error {
	if cfg == nil {
		return fmt.Errorf("config is nil")
	}

	errors := ValidateConfig(cfg)
	if !errors.IsValid() {
		return errors
	}
	return nil
}

// ConfigValidator validates configuration files (deprecated: use ValidateConfig or ExtendedValidator)
type ConfigValidator struct{}

// NewConfigValidator creates a new config validator (deprecated: use ValidateConfig or ExtendedValidator)
func NewConfigValidator() *ConfigValidator {
	return &ConfigValidator{}
}

// Validate checks if the config is valid (deprecated: use ValidateConfig or ExtendedValidator.Validate)
func (v *ConfigValidator) Validate(cfg *Config) error {
	return ValidateConfigSingleError(cfg)
}

// validatePiConfig validates PI-specific configuration settings (kept for backward compatibility)
func validatePiConfig(cfg *PiRootConfig) ValidationErrors {
	return validatePiConfigFast(cfg)
}

// isValidSkillRepo checks if a skill repository URL is valid (kept for backward compatibility)
func isValidSkillRepo(repo string) bool {
	return isValidSkillRepoFast(repo)
}
