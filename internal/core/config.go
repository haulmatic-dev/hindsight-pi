package core

import (
	"time"
)

// Config represents the Hydra configuration
type Config struct {
	Overseer     OverseerConfig     `yaml:"overseer"`
	Projects     []ProjectConfig    `yaml:"projects,omitempty"`
	MCP          MCPConfig          `yaml:"mcp"`
	API          APIConfig          `yaml:"api"`
	Intelligence IntelligenceConfig `yaml:"intelligence"`
}

// IntelligenceConfig contains configuration for intelligence services
type IntelligenceConfig struct {
	CassMemory CassMemoryConfig `yaml:"cass_memory"`
	TLDR       TLDRConfig       `yaml:"tldr"`
	GPTCache   GPTCacheConfig   `yaml:"gptcache"`
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
	PollInterval   time.Duration `yaml:"poll_interval"`
	MaxWorkers     int           `yaml:"max_workers"`
	DefaultRetries int           `yaml:"default_retries"`
	WorkerTimeout  time.Duration `yaml:"worker_timeout"`
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

// MCPConfig contains MCP Agent Mail settings
type MCPConfig struct {
	Enabled bool   `yaml:"enabled"`
	Port    int    `yaml:"port"`
	BaseURL string `yaml:"base_url"`
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
			PollInterval:   5 * time.Second,
			MaxWorkers:     6,
			DefaultRetries: 3,
			WorkerTimeout:  30 * time.Minute,
		},
		MCP: MCPConfig{
			Enabled: true,
			Port:    8765,
			BaseURL: "http://127.0.0.1:8765",
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
		},
	}
}

// LoadConfig loads configuration from a file
func LoadConfig(path string) (*Config, error) {
	// TODO: Implement YAML parsing
	// For now, return default config
	return DefaultConfig(), nil
}
