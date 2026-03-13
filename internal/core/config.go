package core

import (
	"fmt"
	"os"
	"strings"
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

// rawConfig is used for YAML parsing to handle the nested projects structure
type rawConfig struct {
	Overseer     OverseerConfig     `yaml:"overseer"`
	Projects     yaml.Node          `yaml:"projects"` // Can be array or object
	MCP          MCPConfig          `yaml:"mcp"`
	API          APIConfig          `yaml:"api"`
	Intelligence IntelligenceConfig `yaml:"intelligence"`
	Agents       AgentsConfig       `yaml:"agents"`
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

// TLDRConfig contains TLDR service settings
type TLDRConfig struct {
	Enabled   bool          `yaml:"enabled"`
	BaseURL   string        `yaml:"base_url"`
	Timeout   time.Duration `yaml:"timeout"`
	MaxFiles  int           `yaml:"max_files"`
	MaxTokens int           `yaml:"max_tokens"`
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
	}
}

// LoadConfig loads configuration from a YAML file
func LoadConfig(path string) (*Config, error) {
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

	var messages []string
	for _, err := range v {
		messages = append(messages, err.Error())
	}
	return fmt.Sprintf("%d validation errors:\n  - %s", len(v), strings.Join(messages, "\n  - "))
}

// IsValid returns true if there are no validation errors
func (v ValidationErrors) IsValid() bool {
	return len(v) == 0
}

// ValidateConfig validates the config and returns all validation errors
func ValidateConfig(cfg *Config) ValidationErrors {
	var errors ValidationErrors

	if cfg == nil {
		errors = append(errors, ValidationError{
			Field:   "config",
			Message: "config is nil",
		})
		return errors
	}

	if len(cfg.Projects) == 0 {
		errors = append(errors, ValidationError{
			Field:   "projects",
			Message: "no projects configured",
		})
	}

	// Validate overseer settings
	// max_workers can be 0 (unlimited), but if set must be positive
	if cfg.Overseer.MaxWorkers < 0 {
		errors = append(errors, ValidationError{
			Field:   "overseer.max_workers",
			Message: "max_workers cannot be negative (use 0 for unlimited)",
		})
	}

	if cfg.Overseer.BeadDiscoveryInterval < 1*time.Second {
		errors = append(errors, ValidationError{
			Field:   "overseer.bead_discovery_interval",
			Message: "bead_discovery_interval must be at least 1 second",
		})
	}

	// Validate project paths
	for i, proj := range cfg.Projects {
		if proj.Path == "" {
			errors = append(errors, ValidationError{
				Field:   fmt.Sprintf("projects[%d].path", i),
				Message: "project path is required",
			})
		}
		if proj.Name == "" {
			errors = append(errors, ValidationError{
				Field:   fmt.Sprintf("projects[%d].name", i),
				Message: "project name is required",
			})
		}
	}

	return errors
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
