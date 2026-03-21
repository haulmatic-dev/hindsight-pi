// Package hindsight provides post-execution memory retention for Hydra workers.
// It enables storing structured execution results for future learning and analysis.
package hindsight

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// HindsightConfig contains configuration for the Hindsight memory service
type HindsightConfig struct {
	// Enabled determines if Hindsight is active
	Enabled bool `yaml:"enabled"`

	// BaseURL is the URL of the Hindsight service
	BaseURL string `yaml:"base_url"`

	// Timeout for HTTP requests to Hindsight
	Timeout time.Duration `yaml:"timeout"`

	// DefaultLimit for search results
	DefaultLimit int `yaml:"default_limit"`

	// MinScore is the minimum relevance threshold
	MinScore float64 `yaml:"min_score"`
}

// ExecutionMemory represents a structured record of a bead's execution outcome.
// This data is stored in Hindsight to enable future tasks to learn from past executions.
type ExecutionMemory struct {
	// BeadID is the unique identifier of the executed bead
	BeadID types.BeadID `json:"bead_id"`

	// BeadMetadata contains contextual information about the bead
	BeadMetadata BeadMetadata `json:"bead_metadata"`

	// ExecutionResult contains the outcome of the execution
	ExecutionResult ExecutionOutcome `json:"execution_result"`

	// Files contains information about files that were modified or accessed
	Files FileChanges `json:"files"`

	// Timestamp when this memory was recorded
	RecordedAt time.Time `json:"recorded_at"`

	// WorkerID identifies the worker that executed this bead
	WorkerID string `json:"worker_id"`

	// Version of the execution memory schema
	Version string `json:"version"`
}

// BeadMetadata contains contextual information about the bead
type BeadMetadata struct {
	// Title of the bead
	Title string `json:"title"`

	// Description of the work to be done
	Description string `json:"description"`

	// Type of bead (implement, test, fix, review, etc.)
	Type string `json:"type"`

	// Project the bead belongs to
	Project string `json:"project"`

	// Repo within the project (for multi-repo projects)
	Repo string `json:"repo,omitempty"`

	// Labels attached to the bead
	Labels []string `json:"labels,omitempty"`

	// Dependencies that this bead had
	Dependencies []types.BeadID `json:"dependencies,omitempty"`

	// Priority of the bead
	Priority int `json:"priority"`

	// AgentType used for execution
	AgentType string `json:"agent_type"`
}

// ExecutionOutcome contains the result of executing a bead
type ExecutionOutcome struct {
	// Success indicates whether the execution was successful
	Success bool `json:"success"`

	// ExitCode from the execution process
	ExitCode int `json:"exit_code"`

	// Output captured from stdout
	Output string `json:"output,omitempty"`

	// Error captured from stderr or execution errors
	Error string `json:"error,omitempty"`

	// Duration of the execution
	Duration time.Duration `json:"duration"`

	// Number of retry attempts made
	Attempts int `json:"attempts"`

	// Any error message if the execution failed
	FailureReason string `json:"failure_reason,omitempty"`
}

// FileChanges tracks file modifications during execution
type FileChanges struct {
	// TargetFiles are the files that were intended to be modified
	TargetFiles []string `json:"target_files,omitempty"`

	// ModifiedFiles are files that were actually changed
	ModifiedFiles []string `json:"modified_files,omitempty"`

	// CreatedFiles are new files that were created
	CreatedFiles []string `json:"created_files,omitempty"`

	// DeletedFiles are files that were removed
	DeletedFiles []string `json:"deleted_files,omitempty"`
}

// RetentionResult represents the outcome of a retention operation
type RetentionResult struct {
	// Success indicates whether retention succeeded
	Success bool `json:"success"`

	// MemoryID is the identifier assigned to the stored memory (if successful)
	MemoryID string `json:"memory_id,omitempty"`

	// Error message if retention failed
	Error string `json:"error,omitempty"`
}

// Current schema version
const SchemaVersion = "1.0.0"

// RetentionPolicy defines how long a memory should be retained
type RetentionPolicy string

const (
	// RetentionPolicyForever means the memory is part of institutional knowledge
	// and should never be deleted
	RetentionPolicyForever RetentionPolicy = "forever"
	// RetentionPolicyStandard means the memory follows normal retention rules
	RetentionPolicyStandard RetentionPolicy = "standard"
	// RetentionPolicyShort means the memory is temporary and can be purged quickly
	RetentionPolicyShort RetentionPolicy = "short"
)

// ContextProcessorResult represents the result of context processing for a bead.
// This is stored in Hindsight to enable traceability of context enrichment
// and becomes part of the institutional knowledge base.
type ContextProcessorResult struct {
	// BeadID references the original bead for traceability
	BeadID types.BeadID `json:"bead_id"`

	// BeadMetadata contains contextual information about the bead
	BeadMetadata BeadMetadata `json:"bead_metadata"`

	// Query is the search query used to retrieve context
	Query string `json:"query"`

	// Memories contains the retrieved memories
	Memories []MemoryReference `json:"memories"`

	// FromCache indicates if results were served from cache
	FromCache bool `json:"from_cache"`

	// QueryTime is the duration of the query
	QueryTime time.Duration `json:"query_time"`

	// EnrichedAt is when the enrichment occurred
	EnrichedAt time.Time `json:"enriched_at"`

	// ProcessorID identifies the context processor instance
	ProcessorID string `json:"processor_id"`

	// Version of the context processor result schema
	Version string `json:"version"`

	// RetentionPolicy determines how long this result should be kept
	RetentionPolicy RetentionPolicy `json:"retention_policy"`

	// SourceBeadID is the original bead that triggered this context query
	// This provides full traceability back to the source
	SourceBeadID types.BeadID `json:"source_bead_id"`
}

// MemoryReference represents a reference to a memory from context processing
type MemoryReference struct {
	ID        string  `json:"id"`
	Title     string  `json:"title,omitempty"`
	Type      string  `json:"type"`
	Score     float64 `json:"score"`
	Relevance float64 `json:"relevance"`
	Project   string  `json:"project"`
}

// NewContextProcessorResult creates a new ContextProcessorResult with default values.
// It automatically sets the retention policy to "forever" for institutional knowledge.
func NewContextProcessorResult(bead types.Bead, processorID string) *ContextProcessorResult {
	return &ContextProcessorResult{
		BeadID:          bead.ID,
		SourceBeadID:    bead.ID, // Reference to original bead for traceability
		ProcessorID:     processorID,
		Version:         SchemaVersion,
		EnrichedAt:      time.Now(),
		RetentionPolicy: RetentionPolicyForever, // Forever retention for institutional knowledge
		BeadMetadata: BeadMetadata{
			Title:       bead.Title,
			Description: bead.Description,
			Type:        bead.Type,
			Project:     bead.Project,
			Repo:        bead.Repo,
			Labels:      bead.Labels,
			Priority:    bead.Priority,
		},
	}
}

// WithQuery sets the search query
func (cpr *ContextProcessorResult) WithQuery(query string) *ContextProcessorResult {
	cpr.Query = query
	return cpr
}

// WithMemories sets the retrieved memories
func (cpr *ContextProcessorResult) WithMemories(memories []Memory) *ContextProcessorResult {
	cpr.Memories = make([]MemoryReference, 0, len(memories))
	for _, m := range memories {
		cpr.Memories = append(cpr.Memories, MemoryReference{
			ID:        m.ID,
			Title:     m.Title,
			Type:      m.Type,
			Score:     m.Score,
			Relevance: m.Relevance,
			Project:   m.Project,
		})
	}
	return cpr
}

// WithCacheInfo sets cache information
func (cpr *ContextProcessorResult) WithCacheInfo(fromCache bool, queryTime time.Duration) *ContextProcessorResult {
	cpr.FromCache = fromCache
	cpr.QueryTime = queryTime
	return cpr
}

// WithSourceBead sets the source bead ID for traceability
// This allows tracking the original bead even when processing related beads
func (cpr *ContextProcessorResult) WithSourceBead(sourceBeadID types.BeadID) *ContextProcessorResult {
	cpr.SourceBeadID = sourceBeadID
	return cpr
}

// ToExecutionMemory converts a ContextProcessorResult to an ExecutionMemory for retention.
// This allows context processor results to be stored alongside execution memories.
func (cpr *ContextProcessorResult) ToExecutionMemory() *ExecutionMemory {
	// Convert memories to a serializable format for content
	memoriesJSON, _ := json.Marshal(cpr.Memories)

	return &ExecutionMemory{
		BeadID:     cpr.BeadID,
		WorkerID:   cpr.ProcessorID,
		Version:    cpr.Version,
		RecordedAt: cpr.EnrichedAt,
		BeadMetadata: BeadMetadata{
			Title:       cpr.BeadMetadata.Title,
			Description: cpr.BeadMetadata.Description,
			Type:        cpr.BeadMetadata.Type,
			Project:     cpr.BeadMetadata.Project,
			Repo:        cpr.BeadMetadata.Repo,
			Labels:      cpr.BeadMetadata.Labels,
			Priority:    cpr.BeadMetadata.Priority,
			AgentType:   "context-processor",
		},
		ExecutionResult: ExecutionOutcome{
			Success:  true,
			Output:   string(memoriesJSON),
			Duration: cpr.QueryTime,
		},
	}
}

// NewExecutionMemory creates a new ExecutionMemory with default values
func NewExecutionMemory(bead types.Bead, workerID string) *ExecutionMemory {
	return &ExecutionMemory{
		BeadID:     bead.ID,
		WorkerID:   workerID,
		Version:    SchemaVersion,
		RecordedAt: time.Now(),
		BeadMetadata: BeadMetadata{
			Title:        bead.Title,
			Description:  bead.Description,
			Type:         bead.Type,
			Project:      bead.Project,
			Repo:         bead.Repo,
			Labels:       bead.Labels,
			Dependencies: bead.Dependencies,
			Priority:     bead.Priority,
		},
		Files: FileChanges{
			TargetFiles: extractTargetFiles(bead),
		},
	}
}

// WithOutcome sets the execution outcome
func (em *ExecutionMemory) WithOutcome(success bool, exitCode int, output, errorMsg string, duration time.Duration, attempts int) *ExecutionMemory {
	em.ExecutionResult = ExecutionOutcome{
		Success:  success,
		ExitCode: exitCode,
		Output:   output,
		Error:    errorMsg,
		Duration: duration,
		Attempts: attempts,
	}
	if !success {
		em.ExecutionResult.FailureReason = errorMsg
	}
	return em
}

// WithAgentType sets the agent type used for execution
func (em *ExecutionMemory) WithAgentType(agentType string) *ExecutionMemory {
	em.BeadMetadata.AgentType = agentType
	return em
}

// WithModifiedFiles sets the files that were modified
func (em *ExecutionMemory) WithModifiedFiles(files []string) *ExecutionMemory {
	em.Files.ModifiedFiles = files
	return em
}

// WithCreatedFiles sets the files that were created
func (em *ExecutionMemory) WithCreatedFiles(files []string) *ExecutionMemory {
	em.Files.CreatedFiles = files
	return em
}

// extractTargetFiles extracts target files from bead metadata
func extractTargetFiles(bead types.Bead) []string {
	var files []string

	if bead.Metadata != nil {
		// Check for single target file
		if targetFile, ok := bead.Metadata["target_file"]; ok && targetFile != "" {
			files = append(files, targetFile)
		}

		// Check for multiple target files
		if targetFiles, ok := bead.Metadata["target_files"]; ok && targetFiles != "" {
			// Try to parse as JSON array
			var parsedFiles []string
			if err := json.Unmarshal([]byte(targetFiles), &parsedFiles); err == nil {
				files = append(files, parsedFiles...)
			} else {
				// Fall back to comma-separated
				for _, f := range strings.Split(targetFiles, ",") {
					f = strings.TrimSpace(f)
					if f != "" {
						files = append(files, f)
					}
				}
			}
		}
	}

	return deduplicate(files)
}

// deduplicate removes duplicate strings from a slice
func deduplicate(items []string) []string {
	seen := make(map[string]bool)
	result := make([]string, 0, len(items))
	for _, item := range items {
		if !seen[item] {
			seen[item] = true
			result = append(result, item)
		}
	}
	return result
}

// ConsensusDecision represents a stored multi-agent consensus result.
// This is stored in Hindsight with type "consensus-decisions" for learning
// from past consensus outcomes and improving future decision-making.
type ConsensusDecision struct {
	// BeadID is the unique identifier of the bead that was reviewed
	BeadID types.BeadID `json:"bead_id"`

	// Proposal is the title/description of what was being decided
	Proposal string `json:"proposal"`

	// Votes contains all individual expert votes and their details
	Votes []ConsensusVoteDetail `json:"votes"`

	// Outcome is the final consensus result (approved, rejected, changes_requested)
	Outcome string `json:"outcome"`

	// DissentingViews contains opinions from experts who disagreed with the outcome
	DissentingViews []DissentingView `json:"dissenting_views,omitempty"`

	// Confidence is the average confidence score across all reviewers (0.0-1.0)
	Confidence float64 `json:"confidence"`

	// ConsensusReached indicates if unanimous or majority consensus was achieved
	ConsensusReached bool `json:"consensus_reached"`

	// StartedAt is when the consensus process began
	StartedAt time.Time `json:"started_at"`

	// CompletedAt is when the consensus process ended
	CompletedAt time.Time `json:"completed_at"`

	// Duration is how long the consensus process took
	Duration time.Duration `json:"duration"`

	// ParticipantCount is the number of expert reviewers
	ParticipantCount int `json:"participant_count"`

	// Version of the consensus decision schema
	Version string `json:"version"`

	// Project this consensus belongs to
	Project string `json:"project"`

	// Repo within the project (for multi-repo projects)
	Repo string `json:"repo,omitempty"`
}

// ConsensusVoteDetail represents a single expert's vote in the consensus process
type ConsensusVoteDetail struct {
	// ExpertID is the identifier of the expert reviewer
	ExpertID string `json:"expert_id"`

	// AgentType is the type of agent that cast this vote
	AgentType string `json:"agent_type"`

	// Vote is the actual vote (approve, reject, request_changes)
	Vote string `json:"vote"`

	// Confidence is the expert's confidence in their vote (0.0-1.0)
	Confidence float64 `json:"confidence"`

	// Analysis is the expert's reasoning/analysis
	Analysis string `json:"analysis,omitempty"`

	// Timestamp when this vote was cast
	Timestamp time.Time `json:"timestamp"`
}

// DissentingView represents an expert opinion that disagreed with the final outcome
type DissentingView struct {
	// ExpertID is the identifier of the dissenting expert
	ExpertID string `json:"expert_id"`

	// AgentType is the type of agent
	AgentType string `json:"agent_type"`

	// TheirVote is what they voted for
	TheirVote string `json:"their_vote"`

	// TheirReasoning explains why they voted differently
	TheirReasoning string `json:"their_reasoning"`

	// Confidence is their confidence level
	Confidence float64 `json:"confidence"`
}

// ToExecutionMemory converts a ConsensusDecision to an ExecutionMemory for retention.
// This allows consensus decisions to be stored in Hindsight alongside other memories.
func (cd *ConsensusDecision) ToExecutionMemory() *ExecutionMemory {
	// Convert votes to JSON for storage in content
	votesJSON, _ := json.Marshal(cd.Votes)
	dissentingJSON, _ := json.Marshal(cd.DissentingViews)

	// Build a comprehensive content description
	content := fmt.Sprintf("Consensus Decision for %s\n\n", cd.BeadID)
	content += fmt.Sprintf("Proposal: %s\n", cd.Proposal)
	content += fmt.Sprintf("Outcome: %s\n", cd.Outcome)
	content += fmt.Sprintf("Consensus Reached: %v\n", cd.ConsensusReached)
	content += fmt.Sprintf("Confidence: %.2f\n", cd.Confidence)
	content += fmt.Sprintf("Participants: %d\n", cd.ParticipantCount)
	content += fmt.Sprintf("Duration: %v\n", cd.Duration)
	content += fmt.Sprintf("\nVotes: %s\n", string(votesJSON))
	content += fmt.Sprintf("\nDissenting Views: %s\n", string(dissentingJSON))

	return &ExecutionMemory{
		BeadID:     cd.BeadID,
		WorkerID:   "consensus-facilitator",
		Version:    cd.Version,
		RecordedAt: cd.CompletedAt,
		BeadMetadata: BeadMetadata{
			Title:       fmt.Sprintf("Consensus: %s", cd.Proposal),
			Description: content,
			Type:        "consensus-decisions",
			Project:     cd.Project,
			Repo:        cd.Repo,
			Labels:      []string{"consensus", "multi-agent", cd.Outcome},
			Priority:    0,
			AgentType:   "consensus-facilitator",
		},
		ExecutionResult: ExecutionOutcome{
			Success:  cd.ConsensusReached,
			Output:   content,
			Duration: cd.Duration,
		},
	}
}

// NewConsensusDecision creates a new ConsensusDecision from a ConsensusResult.
// This transforms the agent-level result into a storage-ready format.
func NewConsensusDecision(bead types.Bead, result interface {
	GetBeadID() types.BeadID
	GetTopic() string
	GetStatus() string
	GetReviews() []interface {
		GetExpertID() string
		GetAgentType() types.AgentType
		GetVote() string
		GetConfidence() float64
		GetAnalysis() string
		GetTimestamp() time.Time
	}
	GetConsensusReached() bool
	GetConfidence() float64
	GetParticipantCount() int
	GetCompletedAt() time.Time
	GetStartedAt() time.Time
}) *ConsensusDecision {
	cd := &ConsensusDecision{
		BeadID:           result.GetBeadID(),
		Proposal:         result.GetTopic(),
		Outcome:          result.GetStatus(),
		ConsensusReached: result.GetConsensusReached(),
		Confidence:       result.GetConfidence(),
		ParticipantCount: result.GetParticipantCount(),
		CompletedAt:      result.GetCompletedAt(),
		StartedAt:        result.GetStartedAt(),
		Duration:         result.GetCompletedAt().Sub(result.GetStartedAt()),
		Version:          SchemaVersion,
		Project:          bead.Project,
		Repo:             bead.Repo,
		Votes:            make([]ConsensusVoteDetail, 0),
		DissentingViews:  make([]DissentingView, 0),
	}

	// Convert reviews to vote details
	for _, review := range result.GetReviews() {
		vote := ConsensusVoteDetail{
			ExpertID:   review.GetExpertID(),
			AgentType:  string(review.GetAgentType()),
			Vote:       review.GetVote(),
			Confidence: review.GetConfidence(),
			Analysis:   review.GetAnalysis(),
			Timestamp:  review.GetTimestamp(),
		}
		cd.Votes = append(cd.Votes, vote)

		// Check if this is a dissenting view (vote differs from outcome)
		if !voteMatchesOutcome(review.GetVote(), result.GetStatus()) {
			cd.DissentingViews = append(cd.DissentingViews, DissentingView{
				ExpertID:       review.GetExpertID(),
				AgentType:      string(review.GetAgentType()),
				TheirVote:      review.GetVote(),
				TheirReasoning: review.GetAnalysis(),
				Confidence:     review.GetConfidence(),
			})
		}
	}

	return cd
}

// voteMatchesOutcome checks if a vote aligns with the final outcome
func voteMatchesOutcome(vote, outcome string) bool {
	// Normalize to lowercase for comparison
	vote = strings.ToLower(vote)
	outcome = strings.ToLower(outcome)

	// Direct match
	if vote == outcome {
		return true
	}

	// "approve" vote aligns with "approved" outcome
	if vote == "approve" && outcome == "approved" {
		return true
	}

	// "reject" vote aligns with "rejected" outcome
	if vote == "reject" && outcome == "rejected" {
		return true
	}

	// "request_changes" vote aligns with "changes_requested" outcome
	if vote == "request_changes" && outcome == "changes_requested" {
		return true
	}

	return false
}
