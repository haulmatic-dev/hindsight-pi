package core

import (
	"time"
)

// Config represents the Hydra configuration
type Config struct {
	Overseer OverseerConfig  `yaml:"overseer"`
	Projects []ProjectConfig `yaml:"projects,omitempty"`
	MCP      MCPConfig       `yaml:"mcp"`
	API      APIConfig       `yaml:"api"`
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
	}
}

// LoadConfig loads configuration from a file
func LoadConfig(path string) (*Config, error) {
	// TODO: Implement YAML parsing
	// For now, return default config
	return DefaultConfig(), nil
}
