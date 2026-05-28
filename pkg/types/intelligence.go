package types

import "time"

// IntelligenceBundle contains context and patterns gathered from intelligence services
type IntelligenceBundle struct {
	Patterns        []Pattern       `json:"patterns"`
	ContextEntries  []ContextEntry  `json:"context_entries"`
	ImpactAnalysis  *ImpactAnalysis `json:"impact_analysis,omitempty"`
	ScopeValidated  bool            `json:"scope_validated"`
	ValidationError string          `json:"validation_error,omitempty"`
	GatheredAt      time.Time       `json:"gathered_at"`
}

// Pattern represents a learned pattern from Hindsight
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
