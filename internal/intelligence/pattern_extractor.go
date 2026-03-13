package intelligence

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// ExtractedPattern represents a pattern extracted from a settled bead
type ExtractedPattern struct {
	ID              string                 `json:"id"`
	BeadID          types.BeadID           `json:"bead_id"`
	PatternType     string                 `json:"pattern_type"` // e.g., "error_handling", "architecture", "testing"
	Title           string                 `json:"title"`
	Description     string                 `json:"description"`
	Confidence      float64                `json:"confidence"` // 0.0-1.0
	Files           []string               `json:"files"`      // Files involved
	CodeSnippet     string                 `json:"code_snippet,omitempty"`
	Tags            []string               `json:"tags"`
	Metadata        map[string]interface{} `json:"metadata,omitempty"`
	ExtractedAt     time.Time              `json:"extracted_at"`
	ValidationScore float64                `json:"validation_score"` // Post-extraction validation
}

// PatternExtractorConfig contains configuration for pattern extraction
type PatternExtractorConfig struct {
	Enabled             bool    `yaml:"enabled"`
	StoragePath         string  `yaml:"storage_path"`   // Where to store extracted patterns
	MinConfidence       float64 `yaml:"min_confidence"` // Minimum confidence to store pattern
	MaxPatternsPerBead  int     `yaml:"max_patterns_per_bead"`
	ExtractCodeSnippets bool    `yaml:"extract_code_snippets"`
	ValidatePatterns    bool    `yaml:"validate_patterns"` // Run validation on extracted patterns
}

// DefaultPatternExtractorConfig returns default configuration
func DefaultPatternExtractorConfig() PatternExtractorConfig {
	homeDir, _ := os.UserHomeDir()
	return PatternExtractorConfig{
		Enabled:             true,
		StoragePath:         filepath.Join(homeDir, ".hydra", "patterns"),
		MinConfidence:       0.7,
		MaxPatternsPerBead:  10,
		ExtractCodeSnippets: true,
		ValidatePatterns:    true,
	}
}

// PatternStorage defines the interface for storing extracted patterns
type PatternStorage interface {
	Store(pattern *ExtractedPattern) error
	GetByBeadID(beadID types.BeadID) ([]*ExtractedPattern, error)
	GetByType(patternType string) ([]*ExtractedPattern, error)
	Search(query string) ([]*ExtractedPattern, error)
}

// FilePatternStorage stores patterns on disk as JSON files
type FilePatternStorage struct {
	basePath string
}

// NewFilePatternStorage creates a new file-based pattern storage
func NewFilePatternStorage(basePath string) (*FilePatternStorage, error) {
	if err := os.MkdirAll(basePath, 0755); err != nil {
		return nil, fmt.Errorf("failed to create patterns directory: %w", err)
	}

	return &FilePatternStorage{basePath: basePath}, nil
}

// Store saves a pattern to disk
func (s *FilePatternStorage) Store(pattern *ExtractedPattern) error {
	filename := fmt.Sprintf("%s_%d.json", pattern.BeadID, pattern.ExtractedAt.Unix())
	filepath := filepath.Join(s.basePath, filename)

	data, err := json.MarshalIndent(pattern, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal pattern: %w", err)
	}

	if err := os.WriteFile(filepath, data, 0644); err != nil {
		return fmt.Errorf("failed to write pattern file: %w", err)
	}

	return nil
}

// GetByBeadID retrieves patterns for a specific bead
func (s *FilePatternStorage) GetByBeadID(beadID types.BeadID) ([]*ExtractedPattern, error) {
	patterns := make([]*ExtractedPattern, 0)

	entries, err := os.ReadDir(s.basePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read patterns directory: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		data, err := os.ReadFile(filepath.Join(s.basePath, entry.Name()))
		if err != nil {
			continue
		}

		var pattern ExtractedPattern
		if err := json.Unmarshal(data, &pattern); err != nil {
			continue
		}

		if pattern.BeadID == beadID {
			patterns = append(patterns, &pattern)
		}
	}

	return patterns, nil
}

// GetByType retrieves patterns by type
func (s *FilePatternStorage) GetByType(patternType string) ([]*ExtractedPattern, error) {
	patterns := make([]*ExtractedPattern, 0)

	entries, err := os.ReadDir(s.basePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read patterns directory: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		data, err := os.ReadFile(filepath.Join(s.basePath, entry.Name()))
		if err != nil {
			continue
		}

		var pattern ExtractedPattern
		if err := json.Unmarshal(data, &pattern); err != nil {
			continue
		}

		if pattern.PatternType == patternType {
			patterns = append(patterns, &pattern)
		}
	}

	return patterns, nil
}

// Search performs a simple text search on patterns
func (s *FilePatternStorage) Search(query string) ([]*ExtractedPattern, error) {
	patterns := make([]*ExtractedPattern, 0)

	entries, err := os.ReadDir(s.basePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read patterns directory: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		data, err := os.ReadFile(filepath.Join(s.basePath, entry.Name()))
		if err != nil {
			continue
		}

		var pattern ExtractedPattern
		if err := json.Unmarshal(data, &pattern); err != nil {
			continue
		}

		// Simple text search in title and description
		if containsIgnoreCase(pattern.Title, query) || containsIgnoreCase(pattern.Description, query) {
			patterns = append(patterns, &pattern)
		}
	}

	return patterns, nil
}

func containsIgnoreCase(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr ||
		(len(s) > len(substr) && containsSubstringIgnoreCase(s, substr)))
}

func containsSubstringIgnoreCase(s, substr string) bool {
	// Simple case-insensitive substring check
	for i := 0; i <= len(s)-len(substr); i++ {
		match := true
		for j := 0; j < len(substr); j++ {
			if toLower(s[i+j]) != toLower(substr[j]) {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

func toLower(c byte) byte {
	if c >= 'A' && c <= 'Z' {
		return c + ('a' - 'A')
	}
	return c
}

// BeadPatternExtractor extracts patterns from settled beads
type BeadPatternExtractor struct {
	config  PatternExtractorConfig
	storage PatternStorage
	enabled bool
}

// NewBeadPatternExtractor creates a new pattern extractor
func NewBeadPatternExtractor(config PatternExtractorConfig) (*BeadPatternExtractor, error) {
	storage, err := NewFilePatternStorage(config.StoragePath)
	if err != nil {
		return nil, err
	}

	return &BeadPatternExtractor{
		config:  config,
		storage: storage,
		enabled: config.Enabled,
	}, nil
}

// IsEnabled returns whether the extractor is enabled
func (e *BeadPatternExtractor) IsEnabled() bool {
	return e.enabled
}

// Extract extracts patterns from a settled bead
func (e *BeadPatternExtractor) Extract(beadID types.BeadID, info *types.SettledBeadInfo) error {
	if !e.enabled {
		return nil
	}

	log.Printf("[PatternExtractor] Extracting patterns from bead: %s", beadID)

	// Generate patterns based on bead characteristics
	patterns := e.generatePatterns(beadID, info)

	// Filter by confidence threshold
	var validPatterns []*ExtractedPattern
	for _, pattern := range patterns {
		if pattern.Confidence >= e.config.MinConfidence {
			validPatterns = append(validPatterns, pattern)
		}
	}

	// Limit patterns per bead
	if len(validPatterns) > e.config.MaxPatternsPerBead {
		validPatterns = validPatterns[:e.config.MaxPatternsPerBead]
	}

	// Store patterns
	for _, pattern := range validPatterns {
		if err := e.storage.Store(pattern); err != nil {
			log.Printf("[PatternExtractor] Failed to store pattern: %v", err)
		}
	}

	log.Printf("[PatternExtractor] Extracted %d patterns from bead %s", len(validPatterns), beadID)
	return nil
}

// generatePatterns creates patterns from bead information
func (e *BeadPatternExtractor) generatePatterns(beadID types.BeadID, info *types.SettledBeadInfo) []*ExtractedPattern {
	patterns := make([]*ExtractedPattern, 0)

	// Pattern 1: Bead type pattern
	patterns = append(patterns, &ExtractedPattern{
		ID:          fmt.Sprintf("%s_type_%d", beadID, time.Now().Unix()),
		BeadID:      beadID,
		PatternType: "bead_characteristics",
		Title:       fmt.Sprintf("Bead Type Pattern: %s", beadID.Project()),
		Description: fmt.Sprintf("Pattern extracted from settled bead %s with stability score %.2f", beadID, info.StabilityScore),
		Confidence:  info.StabilityScore,
		Tags:        []string{"settled", "immutable", string(beadID.Project())},
		ExtractedAt: time.Now(),
	})

	// Pattern 2: Settlement pattern (if we have check count data)
	if info.CheckCount > 0 {
		patterns = append(patterns, &ExtractedPattern{
			ID:          fmt.Sprintf("%s_settlement_%d", beadID, time.Now().Unix()),
			BeadID:      beadID,
			PatternType: "settlement_behavior",
			Title:       "Settlement Check Pattern",
			Description: fmt.Sprintf("Bead required %d checks before settlement", info.CheckCount),
			Confidence:  0.8,
			Tags:        []string{"settlement", "checks", "stability"},
			Metadata: map[string]interface{}{
				"check_count":     info.CheckCount,
				"settled_at":      info.SettledAt,
				"stability_score": info.StabilityScore,
			},
			ExtractedAt: time.Now(),
		})
	}

	// Pattern 3: Immutability hash pattern
	if info.ImmutabilityHash != "" {
		patterns = append(patterns, &ExtractedPattern{
			ID:          fmt.Sprintf("%s_immutable_%d", beadID, time.Now().Unix()),
			BeadID:      beadID,
			PatternType: "immutability_verification",
			Title:       "Immutability Verification Pattern",
			Description: "Pattern representing the immutable state of the bead at settlement",
			Confidence:  1.0,
			Tags:        []string{"immutable", "verification", "hash"},
			Metadata: map[string]interface{}{
				"immutability_hash": info.ImmutabilityHash,
			},
			ExtractedAt: time.Now(),
		})
	}

	return patterns
}

// GetPatternsForBead retrieves all patterns for a specific bead
func (e *BeadPatternExtractor) GetPatternsForBead(beadID types.BeadID) ([]*ExtractedPattern, error) {
	return e.storage.GetByBeadID(beadID)
}

// SearchPatterns searches all extracted patterns
func (e *BeadPatternExtractor) SearchPatterns(query string) ([]*ExtractedPattern, error) {
	return e.storage.Search(query)
}
