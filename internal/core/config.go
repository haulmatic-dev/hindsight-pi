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
	Overseer        OverseerConfig        `yaml:"overseer"`
	Projects        []ProjectConfig       `yaml:"projects"`
	MCP             MCPConfig             `yaml:"mcp"`
	API             APIConfig             `yaml:"api"`
	Intelligence    IntelligenceConfig    `yaml:"intelligence"`
	Agents          AgentsConfig          `yaml:"agents"`
	Pi              PiRootConfig          `yaml:"pi"`
	Output          OutputConfig          `yaml:"output"`
	Validation      ValidationConfig      `yaml:"validation"`
	ComplianceCycle ComplianceCycleConfig `yaml:"compliance_cycle"`
}

// AgentsConfig contains agent connector settings
type AgentsConfig struct {
	Enabled      bool     `yaml:"enabled"`
	DefaultAgent string   `yaml:"default_agent"`
	Pi           PiConfig `yaml:"pi"`
}

// PhaseModel is a per-phase provider/model override for Pi workers.
type PhaseModel struct {
	Provider string `yaml:"provider"`
	Model    string `yaml:"model"`
}

// PiConfig contains Pi coding agent settings
type PiConfig struct {
	Enabled       bool   `yaml:"enabled"`
	Provider      string `yaml:"provider"`
	Model         string `yaml:"model"`
	ThinkingLevel string `yaml:"thinking_level"`
	Timeout       string `yaml:"timeout"`
	ApiKeyName    string `yaml:"api_key_name"`

	// Mode controls whether the Pi connector uses RPC JSONL mode or the legacy
	// piped text mode. Recognised values: "rpc". Anything else, including the
	// empty default, selects text mode.
	Mode string `yaml:"mode"`

	// RPCAutoConfirm, when true, answers confirm UI asks with confirmed:true.
	// Default false keeps the deny-by-default policy for unattended headless runs.
	RPCAutoConfirm bool `yaml:"rpc_auto_confirm"`

	// FallbackProvider/FallbackModel are used when the primary provider's
	// circuit breaker opens after repeated rate-limit (429) failures.
	// Empty disables failover.
	FallbackProvider string `yaml:"fallback_provider"`
	FallbackModel    string `yaml:"fallback_model"`

	// PhaseModels maps a phase key (e.g. red, green, refactor) to an optional
	// provider/model override. Partial overrides are allowed: a non-empty
	// Provider or Model replaces the corresponding primary field, while empty
	// fields fall back to the primary.
	PhaseModels map[string]PhaseModel `yaml:"phase_models"`

	// Extensions is the allowlist of Pi extensions to load (rpc mode only —
	// text mode has no extension host). Names are package names or the short
	// aliases in cmd/worker's resolver, e.g. ["rtk", "caveman", "context-mode"].
	//
	// An allowlist rather than "load everything" because loading everything is
	// expensive: measured on one identical turn, all installed extensions cost
	// 48,315 tokens versus 12,059 for rtk alone — 4x, for extensions a headless
	// worker cannot use (themes, editor widgets, subagent UI). Empty disables
	// extensions entirely, which is the default and today's behaviour.
	Extensions []string `yaml:"extensions"`

	// PhaseExtensions overrides Extensions for a specific phase (red, green,
	// refactor, …), for cases where a phase needs something the others do not.
	// A phase with no entry uses Extensions.
	PhaseExtensions map[string][]string `yaml:"phase_extensions"`
}

// defaultPiMode is the protocol workers use unless the config says otherwise.
//
// rpc rather than text because text mode initialises no extension host at all —
// verified: a marker extension loaded with -e in text mode is imported but
// nothing runs, and `pi --verbose` reports no extensions. Over rpc the same
// setup produces real work: "RTK rewrite: git status --short -> rtk git status
// --short". Set `mode: text` explicitly to opt back out.
const defaultPiMode = "rpc"

// defaultPiExtensions is the allowlist every phase gets unless overridden.
//
// An allowlist rather than Pi's own discovery because discovery loads
// everything installed: measured on one identical turn, all installed
// extensions cost 48,315 tokens against 12,059 for rtk alone. The excluded ones
// (themes, editor widgets, subagent UI) cannot be used by a headless worker.
// Set `extensions: []` explicitly to run with none.
var defaultPiExtensions = []string{"rtk", "caveman", "context-mode"}

// applyPiDefaults fills in Pi settings the YAML did not specify.
//
// This runs AFTER the raw config is copied over, not in DefaultConfig, because
// LoadConfig assigns `config.Agents = raw.Agents` wholesale — anything
// DefaultConfig put in Agents.Pi is discarded the moment a config file contains
// an `agents:` block, which every real one does. Defaults set only in
// DefaultConfig would therefore be silently inert, the same failure already
// found with min_score, a self-referential fallback_provider and mcp.enabled.
//
// The distinction between "unset" and "explicitly empty" is what makes both
// opt-outs work: `mode: text` is a non-empty value, and `extensions: []`
// unmarshals to an empty-but-non-nil slice.
func (c *Config) applyPiDefaults() {
	if strings.TrimSpace(c.Agents.Pi.Mode) == "" {
		c.Agents.Pi.Mode = defaultPiMode
	}
	if c.Agents.Pi.Extensions == nil {
		c.Agents.Pi.Extensions = append([]string(nil), defaultPiExtensions...)
	}
}

// ExtensionsForPhase returns the extension allowlist for a phase, falling back
// to the default Extensions list when the phase has no override. Phase lookup is
// case-insensitive, matching ModelForPhase.
func (p PiConfig) ExtensionsForPhase(phase string) []string {
	if len(p.PhaseExtensions) > 0 {
		key := strings.ToLower(strings.TrimSpace(phase))
		if key != "" {
			if ext, ok := p.PhaseExtensions[key]; ok {
				return ext
			}
		}
	}
	return p.Extensions
}

// RPCEnabled reports whether the Pi connector should run in RPC JSONL mode.
// Only the exact, case-insensitive value "rpc" enables it; the empty default
// and any other value keep the existing text mode, preserving today's
// behaviour when agents.pi.mode is unset.
func (p PiConfig) RPCEnabled() bool {
	return strings.ToLower(strings.TrimSpace(p.Mode)) == "rpc"
}

// AutoConfirm returns the rpc_auto_confirm setting. When true, confirm asks
// are approved; when false (the default), they are denied.
func (p PiConfig) AutoConfirm() bool {
	return p.RPCAutoConfirm
}

// ModelForPhase returns the provider and model for a phase, falling back to
// the primary provider/model when the phase has no override or the override
// field is empty. Phase lookup is case-insensitive.
func (p PiConfig) ModelForPhase(phase string) (string, string) {
	prov, model := p.Provider, p.Model
	key := strings.ToLower(strings.TrimSpace(phase))
	if override, ok := p.PhaseModels[key]; ok {
		if override.Provider != "" {
			prov = override.Provider
		}
		if override.Model != "" {
			model = override.Model
		}
	}
	return prov, model
}

// PiRootConfig contains Pi-specific configuration settings
type PiRootConfig struct {
	ConfigDir       string         `yaml:"config_dir"`
	PiConfigDir     string         `yaml:"pi_config_dir"`
	WorkerConfigDir string         `yaml:"worker_config_dir"`
	Skills          PiSkillsConfig `yaml:"skills"`
	StandardAgent   PiAgentConfig  `yaml:"standard-agent"`
}

// PiAgentConfig contains a Pi agent sub-configuration (standard-agent, validation-agent)
type PiAgentConfig struct {
	LLM LLMConfig `yaml:"llm"`
}

// LLMConfig contains LLM provider settings
type LLMConfig struct {
	Provider      string `yaml:"provider"`
	Model         string `yaml:"model"`
	ThinkingLevel string `yaml:"thinking_level"`
	Timeout       string `yaml:"timeout"`
	ApiKeyName    string `yaml:"api_key_name"`
}

// PiSkillsConfig contains Pi skills configuration
type PiSkillsConfig struct {
	UpdateInterval string `yaml:"update_interval"`
	AutoUpdate     bool   `yaml:"auto_update"`
}

// OutputConfig controls publishable output behaviour for a repository.
type OutputConfig struct {
	Push   bool   `yaml:"push"`
	Remote string `yaml:"remote"`
}

// OutputPushEnabled reports whether the output/push feature is enabled.
func (c *Config) OutputPushEnabled() bool {
	if c == nil {
		return false
	}
	return c.Output.Push
}

// OutputPushRemote returns the configured push remote. When push is enabled
// but no remote is configured, it defaults to "origin".
func (c *Config) OutputPushRemote() string {
	if c == nil || !c.Output.Push {
		return ""
	}
	if c.Output.Remote == "" {
		return "origin"
	}
	return c.Output.Remote
}

// rawConfig is used for YAML parsing to handle the nested projects structure
type rawConfig struct {
	Overseer        OverseerConfig        `yaml:"overseer"`
	Projects        yaml.Node             `yaml:"projects"` // Can be array or object
	MCP             MCPConfig             `yaml:"mcp"`
	API             APIConfig             `yaml:"api"`
	Intelligence    IntelligenceConfig    `yaml:"intelligence"`
	Agents          AgentsConfig          `yaml:"agents"`
	Pi              PiRootConfig          `yaml:"pi"`
	Output          OutputConfig          `yaml:"output"`
	Validation      ValidationConfig      `yaml:"validation"`
	ComplianceCycle ComplianceCycleConfig `yaml:"compliance_cycle"`
}

// projectsListConfig handles the case where projects is an object with 'list' field
type projectsListConfig struct {
	AutoDiscover bool            `yaml:"auto_discover"`
	List         []ProjectConfig `yaml:"list"`
}

// IntelligenceConfig contains configuration for intelligence services
type IntelligenceConfig struct {
	TLDR                  TLDRConfig                  `yaml:"tldr"`
	GPTCache              GPTCacheConfig              `yaml:"gptcache"`
	SettledState          SettledStateConfig          `yaml:"settled_state"`
	Patterns              PatternConfig               `yaml:"patterns"`
	CollectiveUnconscious CollectiveUnconsciousConfig `yaml:"collective_unconscious"`
	Hindsight             HindsightConfig             `yaml:"hindsight"`
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
	Backend  string        `yaml:"backend"` // "postgres", "hybrid"
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

// HindsightConfig contains Hindsight memory service settings
type HindsightConfig struct {
	Enabled bool          `yaml:"enabled"`
	BaseURL string        `yaml:"base_url"`
	Timeout time.Duration `yaml:"timeout"`
	Limit   int           `yaml:"limit"`

	// MaxTokens bounds the recalled context by token budget. This replaces the
	// former min_score, which the recall API does not accept and therefore
	// silently ignored — every recall returned the whole bank (~80KB) and the
	// client discarded most of it unfiltered. Measured: max_tokens=800 cuts the
	// recall response from ~80KB to ~16KB.
	MaxTokens int `yaml:"max_tokens"`
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
	// WorktreeIsolation runs each chain in its own git worktree and commits
	// its work on verified-close, integrating back to the working branch on
	// chain completion. Off by default (opt-in).
	WorktreeIsolation bool `yaml:"worktree_isolation"`
}

type ChainDetectionConfig struct {
	Enabled  bool          `yaml:"enabled"`
	Interval time.Duration `yaml:"interval"`
	Timeout  time.Duration `yaml:"timeout"`
}

// ValidationConfig contains self-healing validation loop settings
type ValidationConfig struct {
	Enabled                  bool   `yaml:"enabled"`
	TDDValidationEnabled     bool   `yaml:"tdd_validation_enabled"`
	PRDValidationEnabled     bool   `yaml:"prd_validation_enabled"`
	MaxRounds                int    `yaml:"max_rounds"`
	AgentVerificationEnabled bool   `yaml:"agent_verification_enabled"`
	EnforceTargetScope       string `yaml:"enforce_target_scope"`

	// CloseBlockedOnFailed (default true) gates whether a bead that
	// closes with `close_reason=failed` (worker exit non-zero, retry
	// exhaustion) creates an auto-fix bead and keeps dependents
	// blocked. When false, the legacy behaviour is preserved: failed
	// closures propagate normally and dependents unblock. The default
	// is the strict one because the legacy behaviour let 57% of beads
	// in one audit close as `failed` while their dependents unblocked.
	CloseBlockedOnFailed bool `yaml:"close_blocked_on_failed"`

	// BlockStubImplementations (default true) rejects bead closures
	// whose target files contain stub patterns (empty bodies,
	// placeholder comments, _ = x discards). See
	// docs/operations/validation-stub-detection.md.
	BlockStubImplementations bool `yaml:"block_stub_implementations"`

	// RequireDiffOnClose (default true) rejects a completed bead close
	// when the worker left no uncommitted changes on disk for the bead's
	// target files (or the repo when none are declared). Workers do not
	// commit their work, so an empty diff means the "agent reported
	// success but edited nothing" case. Ops can disable this if a
	// workflow legitimately closes beads without file changes.
	RequireDiffOnClose bool `yaml:"require_diff_on_close"`
}

// TargetScopeMode returns the normalized scope gate enforcement mode.
// Valid values are "off", "warn", and "block" (case-insensitive).
// Empty or unrecognized values fall back to "warn".
func (v ValidationConfig) TargetScopeMode() string {
	switch strings.ToLower(strings.TrimSpace(v.EnforceTargetScope)) {
	case "off":
		return "off"
	case "block":
		return "block"
	default:
		return "warn"
	}
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
		Validation: ValidationConfig{
			Enabled:                  true,
			TDDValidationEnabled:     true,
			PRDValidationEnabled:     true,
			MaxRounds:                3,
			AgentVerificationEnabled: true,
			EnforceTargetScope:       "warn",
			RequireDiffOnClose:       true,
		},
		ComplianceCycle: ComplianceCycleConfig{
			Enabled:      false,
			MaxCycles:    5,
			PollInterval: 30 * time.Second,
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
			Hindsight: HindsightConfig{
				Enabled:   true,
				BaseURL:   "http://127.0.0.1:8000",
				Timeout:   10 * time.Second,
				Limit:     5,
				MaxTokens: 800,
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
				Enabled: true,
				// Provider/Model/ThinkingLevel/Timeout left empty so that
				// pi.standard-agent.llm in the YAML can override, or the
				// overseer fills in safe defaults.
			},
		},
		Pi: PiRootConfig{
			ConfigDir: "./pi-config",
			Skills: PiSkillsConfig{
				UpdateInterval: "24h",
				AutoUpdate:     true,
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
		return nil, &ConfigError{Op: "read", Err: err}
	}

	// Get file info for cache timestamp
	info, err := os.Stat(path)
	if err != nil {
		return nil, &ConfigError{Op: "stat", Err: err}
	}

	// Start with default config
	config := DefaultConfig()

	// Parse raw config to handle projects array vs object
	var raw rawConfig
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, &ConfigError{Op: "parse", Err: err}
	}

	// Copy non-project fields
	config.Overseer = raw.Overseer
	config.MCP = raw.MCP
	config.API = raw.API
	config.Intelligence = raw.Intelligence
	config.Agents = raw.Agents
	config.applyPiDefaults()
	config.Validation = raw.Validation
	config.ComplianceCycle = raw.ComplianceCycle
	config.Pi = PiRootConfig{
		ConfigDir:       raw.Pi.ConfigDir,
		PiConfigDir:     raw.Pi.PiConfigDir,
		WorkerConfigDir: raw.Pi.WorkerConfigDir,
		Skills:          raw.Pi.Skills,
		StandardAgent:   raw.Pi.StandardAgent,
	}
	config.Output = raw.Output
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
		return nil, &ConfigError{Op: "read", Err: err}
	}

	// Start with default config
	config := DefaultConfig()

	// Parse raw config to handle projects array vs object
	var raw rawConfig
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, &ConfigError{Op: "parse", Err: err}
	}

	// Copy non-project fields
	config.Overseer = raw.Overseer
	config.MCP = raw.MCP
	config.API = raw.API
	config.Intelligence = raw.Intelligence
	config.Agents = raw.Agents
	config.applyPiDefaults()
	config.Pi = PiRootConfig{
		ConfigDir:       raw.Pi.ConfigDir,
		PiConfigDir:     raw.Pi.PiConfigDir,
		WorkerConfigDir: raw.Pi.WorkerConfigDir,
		Skills:          raw.Pi.Skills,
		StandardAgent:   raw.Pi.StandardAgent,
	}
	config.Output = raw.Output
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
		Output:       c.Output,
	}

	// Deep copy Projects slice
	if c.Projects != nil {
		cloned.Projects = make([]ProjectConfig, len(c.Projects))
		copy(cloned.Projects, c.Projects)
	}

	// Deep copy Agents.Pi.PhaseModels map so mutations of the returned
	// config do not affect the cached config.
	if c.Agents.Pi.PhaseModels != nil {
		cloned.Agents.Pi.PhaseModels = make(map[string]PhaseModel, len(c.Agents.Pi.PhaseModels))
		for k, v := range c.Agents.Pi.PhaseModels {
			cloned.Agents.Pi.PhaseModels[k] = v
		}
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

// validatePiConfig validates PI-specific configuration settings (kept for backward compatibility)
func validatePiConfig(cfg *PiRootConfig) ValidationErrors {
	return validatePiConfigFast(cfg)
}
