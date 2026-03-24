// Package agents provides multi-agent consensus facilitation for complex beads.
// The consensus facilitator spawns expert reviewers who analyze beads independently
// and vote on approval, rejection, or change requests using Hindsight context.
package agents

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/haulmatic-dev/hydra/internal/hindsight"
	"github.com/haulmatic-dev/hydra/pkg/types"
)

// Complexity thresholds and configuration defaults.
const (
	DefaultParticipantCount      = 3
	ArchitectureParticipantCount = 4
	DefaultMaxRounds             = 4
	DefaultTimeout               = 30 * time.Minute
	MinParticipants              = 2
	MaxParticipants              = 4
	MinRounds                    = 2
	MaxRounds                    = 6
	ComplexityFileThreshold      = 5
)

// Architecture indicators for determining if a bead requires consensus.
var (
	// Labels that indicate architectural complexity
	architectureLabels = []string{"architecture", "arch", "design", "refactor", "consensus"}

	// Bead types that indicate architectural complexity
	architectureTypes = []string{"architecture", "design", "refactor"}

	// Keywords in title/description indicating architectural complexity
	architectureKeywords = []string{"architecture", "refactor", "redesign", "restructure", "pattern"}
)

// ConsensusConfig defines multi-agent discussion parameters for complex beads.
type ConsensusConfig struct {
	Required         bool          `json:"required" yaml:"required"`
	Topic            string        `json:"topic" yaml:"topic"`
	Description      string        `json:"description" yaml:"description"`
	ParticipantCount int           `json:"participant_count" yaml:"participant_count"` // 2-4
	MaxRounds        int           `json:"max_rounds" yaml:"max_rounds"`               // default: 4
	Timeout          time.Duration `json:"timeout" yaml:"timeout"`                     // default: 30m
}

// IsComplexBead determines if a bead requires consensus based on complexity criteria.
// Returns true if bead has more than 5 files or involves architectural changes.
func IsComplexBead(bead types.Bead) bool {
	if isArchitectureBead(bead) {
		return true
	}
	return exceedsFileThreshold(bead, ComplexityFileThreshold)
}

// isArchitectureBead checks if a bead involves architectural changes by examining
// its labels, type, and title/description content.
func isArchitectureBead(bead types.Bead) bool {
	if matchesAny(bead.Labels, architectureLabels, strings.EqualFold) {
		return true
	}
	if matchesAny([]string{bead.Type}, architectureTypes, strings.EqualFold) {
		return true
	}
	return containsAny(strings.ToLower(bead.Title+" "+bead.Description), architectureKeywords)
}

// matchesAny checks if any item in the source slice matches any item in the target
// slice using the provided match function.
func matchesAny(source, target []string, matchFn func(string, string) bool) bool {
	for _, s := range source {
		for _, t := range target {
			if matchFn(s, t) {
				return true
			}
		}
	}
	return false
}

// containsAny checks if the content contains any of the substrings.
func containsAny(content string, substrings []string) bool {
	for _, substr := range substrings {
		if strings.Contains(content, substr) {
			return true
		}
	}
	return false
}

// exceedsFileThreshold checks if the bead's file count exceeds the threshold.
func exceedsFileThreshold(bead types.Bead, threshold int) bool {
	fileCountStr, ok := bead.Metadata["file_count"]
	if !ok {
		return false
	}

	fileCount, err := strconv.Atoi(fileCountStr)
	if err != nil {
		return false
	}

	return fileCount > threshold
}

// ConsensusVote represents a reviewer's vote on a bead.
type ConsensusVote string

const (
	VoteApprove        ConsensusVote = "approve"
	VoteReject         ConsensusVote = "reject"
	VoteRequestChanges ConsensusVote = "request_changes"
)

// ExpertReview contains a single expert's analysis and vote.
type ExpertReview struct {
	ExpertID      string             `json:"expert_id"`
	AgentType     types.AgentType    `json:"agent_type"`
	Analysis      string             `json:"analysis"`
	Vote          ConsensusVote      `json:"vote"`
	Confidence    float64            `json:"confidence"` // 0.0-1.0
	HindsightUsed []hindsight.Memory `json:"hindsight_used,omitempty"`
	Timestamp     time.Time          `json:"timestamp"`
}

// ConsensusResult contains the outcome of a consensus session.
type ConsensusResult struct {
	BeadID           types.BeadID   `json:"bead_id"`
	Topic            string         `json:"topic"`
	Status           string         `json:"status"` // "approved", "rejected", "changes_requested"
	Reviews          []ExpertReview `json:"reviews"`
	ConsensusReached bool           `json:"consensus_reached"`
	Confidence       float64        `json:"confidence"` // Average confidence of all reviewers
	FinalDecision    string         `json:"final_decision"`
	ParticipantCount int            `json:"participant_count"`
	CompletedAt      time.Time      `json:"completed_at"`
}

// ConsensusFacilitator manages the consensus process for complex beads.
type ConsensusFacilitator struct {
	bead            types.Bead
	config          ConsensusConfig
	expertAgents    []ExpertAgent
	hindsightClient *hindsight.Client
	reviews         []ExpertReview
}

// ExpertAgent represents an expert reviewer participating in consensus.
type ExpertAgent struct {
	ID              string            `json:"id"`
	AgentType       types.AgentType   `json:"agent_type"`
	Specialty       string            `json:"specialty"`
	HindsightClient *hindsight.Client `json:"-"`
}

// NewConsensusFacilitator creates a new facilitator for the given bead.
// Returns error if bead doesn't meet complexity criteria for consensus.
func NewConsensusFacilitator(bead types.Bead, hindsightClient *hindsight.Client) (*ConsensusFacilitator, error) {
	if !IsComplexBead(bead) {
		return nil, fmt.Errorf("bead %s does not meet complexity criteria for consensus", bead.ID)
	}

	// Default consensus configuration
	config := ConsensusConfig{
		Required:         true,
		Topic:            bead.Title,
		Description:      bead.Description,
		ParticipantCount: 3,                // Default to 3 experts
		MaxRounds:        4,                // Default 4 rounds
		Timeout:          30 * time.Minute, // Default 30 minute timeout
	}

	// Adjust participant count based on bead type
	if isArchitectureBead(bead) {
		config.ParticipantCount = 4 // Architecture beads need more reviewers
	}

	if err := ValidateConsensusConfig(config); err != nil {
		return nil, fmt.Errorf("invalid consensus config: %w", err)
	}

	return &ConsensusFacilitator{
		bead:            bead,
		config:          config,
		hindsightClient: hindsightClient,
		expertAgents:    make([]ExpertAgent, 0),
		reviews:         make([]ExpertReview, 0),
	}, nil
}

// SpawnExpertReviewers spawns 2-4 expert reviewers based on bead complexity.
// Each expert analyzes the bead independently using Hindsight context.
func (f *ConsensusFacilitator) SpawnExpertReviewers(ctx context.Context) ([]ExpertAgent, error) {
	// Placeholder implementation - to be implemented in GREEN phase
	panic("not implemented")
}

// ConductReview orchestrates the independent analysis phase where each expert
// reviews the bead using Hindsight context and provides their vote.
func (f *ConsensusFacilitator) ConductReview(ctx context.Context) ([]ExpertReview, error) {
	// Placeholder implementation - to be implemented in GREEN phase
	panic("not implemented")
}

// CalculateConsensus aggregates votes and determines if consensus is reached.
// Returns approved if majority approve, rejected if majority reject,
// changes_requested if mixed or majority request changes.
func (f *ConsensusFacilitator) CalculateConsensus(reviews []ExpertReview) (*ConsensusResult, error) {
	// Placeholder implementation - to be implemented in GREEN phase
	panic("not implemented")
}

// StoreConsensusResults persists the consensus results to Hindsight for future reference.
// This enables future beads to learn from past consensus decisions.
func (f *ConsensusFacilitator) StoreConsensusResults(ctx context.Context, result *ConsensusResult) error {
	// Placeholder implementation - to be implemented in GREEN phase
	panic("not implemented")
}

// GetConsensusResult returns the final consensus result for the bead.
func (f *ConsensusFacilitator) GetConsensusResult() *ConsensusResult {
	// Placeholder implementation - to be implemented in GREEN phase
	panic("not implemented")
}

// ValidateConsensusConfig validates consensus configuration constraints.
// Participant count must be 2-4, max_rounds must be 2-6.
func ValidateConsensusConfig(config ConsensusConfig) error {
	if config.ParticipantCount < 2 {
		return fmt.Errorf("participant_count must be at least 2, got %d", config.ParticipantCount)
	}
	if config.ParticipantCount > 4 {
		return fmt.Errorf("participant_count must be at most 4, got %d", config.ParticipantCount)
	}
	if config.MaxRounds < 2 {
		return fmt.Errorf("max_rounds must be at least 2, got %d", config.MaxRounds)
	}
	if config.MaxRounds > 6 {
		return fmt.Errorf("max_rounds must be at most 6, got %d", config.MaxRounds)
	}
	if config.Timeout == 0 {
		config.Timeout = 30 * time.Minute
	}
	return nil
}
