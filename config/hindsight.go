// Package config provides configuration structures for Hydra's knowledge sharing system.
// This includes cross-project memory bank settings and project attribution configuration.
package config

import (
	"time"
)

// HindsightConfig contains configuration for the cross-project knowledge sharing system.
// It enables a single "hydra" memory bank shared across all projects with project
// attribution for filtering and query capabilities.
type HindsightConfig struct {
	// Enabled determines if hindsight knowledge sharing is active
	Enabled bool `yaml:"enabled"`

	// MemoryBank defines the shared memory bank settings
	MemoryBank MemoryBankConfig `yaml:"memory_bank"`

	// ProjectAttribution configures how project metadata is tracked
	ProjectAttribution ProjectAttributionConfig `yaml:"project_attribution"`

	// CrossProject enables cross-project pattern sharing
	CrossProject CrossProjectConfig `yaml:"cross_project"`

	// Query configuration for filtering and searching
	Query QueryConfig `yaml:"query"`
}

// MemoryBankConfig defines the shared memory bank settings.
// Hydra uses a single "hydra" memory bank shared across all projects
type MemoryBankConfig struct {
	// Name is the identifier for the memory bank (always "hydra" for shared bank)
	Name string `yaml:"name"`

	// Backend storage type (e.g., "cass_memory", "postgres", "sqlite")
	Backend string `yaml:"backend"`

	// Connection string or path for the backend
	Connection string `yaml:"connection,omitempty"`

	// MaxSize is the maximum size of the memory bank in MB
	MaxSize int `yaml:"max_size"`

	// RetentionDays is how long to keep memories (default: 365)
	RetentionDays int `yaml:"retention_days"`
}

// ProjectAttributionConfig configures how project metadata is tracked for filtering.
// Each memory entry includes project attribution to enable project-specific queries.
type ProjectAttributionConfig struct {
	// Enabled determines if project attribution is active
	Enabled bool `yaml:"enabled"`

	// RequiredFields lists which metadata fields must be present
	RequiredFields []string `yaml:"required_fields"`

	// IndexedFields lists which fields are indexed for fast filtering
	IndexedFields []string `yaml:"indexed_fields"`

	// MetadataSchema defines the structure of project metadata
	MetadataSchema map[string]string `yaml:"metadata_schema"`
}

// CrossProjectConfig enables cross-project pattern sharing and availability.
type CrossProjectConfig struct {
	// Enabled determines if cross-project sharing is active
	Enabled bool `yaml:"enabled"`

	// AvailabilityWindow is the maximum time before patterns are available
	// across projects (default: 24 hours)
	AvailabilityWindow time.Duration `yaml:"availability_window"`

	// ShareableCategories lists which pattern categories can be shared
	ShareableCategories []string `yaml:"shareable_categories"`

	// ProjectWhitelist limits sharing to specific projects (empty = all)
	ProjectWhitelist []string `yaml:"project_whitelist,omitempty"`

	// ProjectBlacklist excludes specific projects from sharing
	ProjectBlacklist []string `yaml:"project_blacklist,omitempty"`
}

// QueryConfig configures how queries work with project filtering.
type QueryConfig struct {
	// DefaultLimit is the default number of results to return
	DefaultLimit int `yaml:"default_limit"`

	// MaxLimit is the maximum number of results allowed
	MaxLimit int `yaml:"max_limit"`

	// EnableProjectFilter determines if project filtering is available
	EnableProjectFilter bool `yaml:"enable_project_filter"`

	// DefaultProjects lists projects to include by default (empty = all)
	DefaultProjects []string `yaml:"default_projects,omitempty"`

	// MinScoreThreshold is the minimum relevance score for results
	MinScoreThreshold float64 `yaml:"min_score_threshold"`
}

// DefaultHindsightConfig returns the default hindsight configuration.
// This provides a single "hydra" memory bank shared across all projects
// with project attribution for filtering.
func DefaultHindsightConfig() *HindsightConfig {
	return &HindsightConfig{
		Enabled: true,
		MemoryBank: MemoryBankConfig{
			Name:          "hydra",
			Backend:       "cass_memory",
			MaxSize:       1024, // 1GB
			RetentionDays: 365,
		},
		ProjectAttribution: ProjectAttributionConfig{
			Enabled:        true,
			RequiredFields: []string{"project", "timestamp"},
			IndexedFields:  []string{"project", "type", "category", "timestamp"},
			MetadataSchema: map[string]string{
				"project":   "string",
				"repo":      "string",
				"type":      "string",
				"category":  "string",
				"timestamp": "datetime",
				"agent":     "string",
			},
		},
		CrossProject: CrossProjectConfig{
			Enabled:             true,
			AvailabilityWindow:  24 * time.Hour,
			ShareableCategories: []string{"pattern", "anti-pattern", "best-practice", "lesson-learned"},
		},
		Query: QueryConfig{
			DefaultLimit:        10,
			MaxLimit:            100,
			EnableProjectFilter: true,
			MinScoreThreshold:   0.75,
		},
	}
}

// Validate validates the hindsight configuration.
func (c *HindsightConfig) Validate() error {
	if c == nil {
		return &ValidationError{Field: "config", Message: "config is nil"}
	}

	if c.Enabled {
		if c.MemoryBank.Name == "" {
			return &ValidationError{Field: "memory_bank.name", Message: "memory bank name is required"}
		}

		if c.MemoryBank.Backend == "" {
			return &ValidationError{Field: "memory_bank.backend", Message: "backend is required"}
		}

		if c.CrossProject.AvailabilityWindow < time.Hour {
			return &ValidationError{Field: "cross_project.availability_window", Message: "availability window must be at least 1 hour"}
		}

		if c.CrossProject.AvailabilityWindow > 7*24*time.Hour {
			return &ValidationError{Field: "cross_project.availability_window", Message: "availability window must not exceed 7 days"}
		}
	}

	return nil
}

// IsProjectShareable checks if a project is allowed to participate in cross-project sharing.
func (c *HindsightConfig) IsProjectShareable(project string) bool {
	if !c.CrossProject.Enabled {
		return false
	}

	// Check blacklist
	for _, p := range c.CrossProject.ProjectBlacklist {
		if p == project {
			return false
		}
	}

	// Check whitelist (if specified, only whitelisted projects can share)
	if len(c.CrossProject.ProjectWhitelist) > 0 {
		for _, p := range c.CrossProject.ProjectWhitelist {
			if p == project {
				return true
			}
		}
		return false
	}

	return true
}

// IsCategoryShareable checks if a category can be shared across projects.
func (c *HindsightConfig) IsCategoryShareable(category string) bool {
	if !c.CrossProject.Enabled {
		return false
	}

	for _, c := range c.CrossProject.ShareableCategories {
		if c == category {
			return true
		}
	}
	return false
}

// ValidationError represents a configuration validation error.
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	return e.Field + ": " + e.Message
}
