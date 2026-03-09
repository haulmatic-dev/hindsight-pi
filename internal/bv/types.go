package bv

import (
	"time"
)

// InsightsResponse represents graph analysis and insights from bv --robot-insights
type InsightsResponse struct {
	Project         string          `json:"project"`
	TotalBeads      int             `json:"total_beads"`
	OpenBeads       int             `json:"open_beads"`
	InProgressBeads int             `json:"in_progress_beads"`
	CompletedBeads  int             `json:"completed_beads"`
	BlockedBeads    int             `json:"blocked_beads"`
	Bottlenecks     []Bottleneck    `json:"bottlenecks"`
	Keystones       []Keystone      `json:"keystones"`
	Cycles          []CycleInfo     `json:"cycles"`
	Orphans         []OrphanInfo    `json:"orphans"`
	Metrics         InsightsMetrics `json:"metrics"`
}

// Bottleneck represents a bottleneck in the dependency graph
type Bottleneck struct {
	BeadID          string `json:"bead_id"`
	Title           string `json:"title"`
	BlockedCount    int    `json:"blocked_count"`
	DependencyDepth int    `json:"dependency_depth"`
	Priority        int    `json:"priority"`
}

// Keystone represents a keystone bead (high impact)
type Keystone struct {
	BeadID          string `json:"bead_id"`
	Title           string `json:"title"`
	ImpactScore     int    `json:"impact_score"`
	DownstreamCount int    `json:"downstream_count"`
}

// CycleInfo represents a detected dependency cycle
type CycleInfo struct {
	Beads   []string `json:"beads"`
	Length  int      `json:"length"`
	Message string   `json:"message"`
}

// OrphanInfo represents an orphan bead (no dependencies or dependents)
type OrphanInfo struct {
	BeadID string `json:"bead_id"`
	Title  string `json:"title"`
	Reason string `json:"reason"`
}

// InsightsMetrics contains various graph metrics
type InsightsMetrics struct {
	AvgDependencies float64 `json:"avg_dependencies"`
	MaxDependencies int     `json:"max_dependencies"`
	GraphDepth      int     `json:"graph_depth"`
	Parallelizable  int     `json:"parallelizable"`
}

// TriageResponse represents unified triage from bv --robot-triage
type TriageResponse struct {
	Recommendations []TriageRecommendation `json:"recommendations"`
	ReadyCount      int                    `json:"ready_count"`
	BlockedCount    int                    `json:"blocked_count"`
	Attention       []AttentionItem        `json:"attention"`
	GeneratedAt     time.Time              `json:"generated_at"`
}

// TriageRecommendation represents a single triage recommendation
type TriageRecommendation struct {
	BeadID        string   `json:"bead_id"`
	Title         string   `json:"title"`
	Type          string   `json:"type"`
	Priority      int      `json:"priority"`
	Confidence    float64  `json:"confidence"`
	Reason        string   `json:"reason"`
	Labels        []string `json:"labels"`
	EstimatedTime string   `json:"estimated_time,omitempty"`
}

// AttentionItem represents items needing attention
type AttentionItem struct {
	BeadID   string `json:"bead_id"`
	Title    string `json:"title"`
	Severity string `json:"severity"`
	Message  string `json:"message"`
}

// PlanResponse represents dependency-respecting execution plan from bv --robot-plan
type PlanResponse struct {
	Phases         []PlanPhase `json:"phases"`
	TotalBeads     int         `json:"total_beads"`
	TotalPhases    int         `json:"total_phases"`
	Parallelizable int         `json:"parallelizable"`
	CriticalPath   []string    `json:"critical_path"`
}

// PlanPhase represents a phase in the execution plan
type PlanPhase struct {
	PhaseNumber int        `json:"phase_number"`
	Beads       []PlanBead `json:"beads"`
	Parallel    bool       `json:"parallel"`
}

// PlanBead represents a bead in the plan
type PlanBead struct {
	BeadID       string   `json:"bead_id"`
	Title        string   `json:"title"`
	Type         string   `json:"type"`
	Priority     int      `json:"priority"`
	Dependencies []string `json:"dependencies"`
}

// AlertsResponse represents alerts from bv --robot-alerts
type AlertsResponse struct {
	Alerts      []Alert   `json:"alerts"`
	Drift       DriftInfo `json:"drift"`
	GeneratedAt time.Time `json:"generated_at"`
}

// Alert represents a single alert
type Alert struct {
	ID        string    `json:"id"`
	Type      string    `json:"type"`
	Severity  string    `json:"severity"`
	Title     string    `json:"title"`
	Message   string    `json:"message"`
	BeadID    string    `json:"bead_id,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// DriftInfo represents drift information
type DriftInfo struct {
	HasDrift bool         `json:"has_drift"`
	Level    string       `json:"level,omitempty"`
	Message  string       `json:"message,omitempty"`
	Metrics  DriftMetrics `json:"metrics,omitempty"`
}

// DriftMetrics contains drift metric details
type DriftMetrics struct {
	AddedBeads    int `json:"added_beads"`
	RemovedBeads  int `json:"removed_beads"`
	ModifiedBeads int `json:"modified_beads"`
}

// PriorityResponse represents priority recommendations from bv --robot-priority
type PriorityResponse struct {
	Priorities  []PriorityItem `json:"priorities"`
	Categories  []Category     `json:"categories"`
	GeneratedAt time.Time      `json:"generated_at"`
}

// PriorityItem represents a single priority item
type PriorityItem struct {
	BeadID     string  `json:"bead_id"`
	Title      string  `json:"title"`
	Rank       int     `json:"rank"`
	Score      float64 `json:"score"`
	Urgency    float64 `json:"urgency"`
	Impact     float64 `json:"impact"`
	Confidence float64 `json:"confidence"`
	Reasoning  string  `json:"reasoning"`
}

// Category represents a priority category
type Category struct {
	Name        string   `json:"name"`
	Count       int      `json:"count"`
	Description string   `json:"description"`
	BeadIDs     []string `json:"bead_ids"`
}

// CapacityResponse represents capacity simulation from bv --robot-capacity
type CapacityResponse struct {
	Agents           int              `json:"agents"`
	TotalBeads       int              `json:"total_beads"`
	OpenBeads        int              `json:"open_beads"`
	EstimatedDays    float64          `json:"estimated_days"`
	ThroughputPerDay float64          `json:"throughput_per_day"`
	CompletionDate   *time.Time       `json:"completion_date,omitempty"`
	Bottlenecks      []string         `json:"bottlenecks"`
	LimitingFactors  []LimitingFactor `json:"limiting_factors"`
}

// LimitingFactor represents a capacity limiting factor
type LimitingFactor struct {
	Type        string `json:"type"`
	Description string `json:"description"`
	Impact      string `json:"impact"`
}

// FileHotspotsResponse represents file hotspots from bv --robot-file-hotspots
type FileHotspotsResponse struct {
	Hotspots []FileHotspot `json:"hotspots"`
	Total    int           `json:"total"`
}

// FileHotspot represents a single file hotspot
type FileHotspot struct {
	Path            string   `json:"path"`
	TouchCount      int      `json:"touch_count"`
	Beads           []string `json:"beads"`
	LastModified    string   `json:"last_modified,omitempty"`
	ChangeFrequency float64  `json:"change_frequency,omitempty"`
}

// DriftResponse represents drift check from bv --robot-drift or --check-drift
type DriftResponse struct {
	HasDrift     bool         `json:"has_drift"`
	Level        string       `json:"level,omitempty"` // critical, warning, info
	Message      string       `json:"message,omitempty"`
	FromBaseline string       `json:"from_baseline,omitempty"`
	Changes      DriftChanges `json:"changes"`
	CheckedAt    time.Time    `json:"checked_at"`
}

// DriftChanges represents detailed drift changes
type DriftChanges struct {
	Added    []DriftBead `json:"added"`
	Removed  []DriftBead `json:"removed"`
	Modified []DriftBead `json:"modified"`
}

// DriftBead represents a bead in drift analysis
type DriftBead struct {
	BeadID      string `json:"bead_id"`
	Title       string `json:"title"`
	ChangeType  string `json:"change_type,omitempty"`
	Description string `json:"description,omitempty"`
}

// SearchResponse represents semantic search results from bv --robot-search
type SearchResponse struct {
	Query       string         `json:"query"`
	Results     []SearchResult `json:"results"`
	Total       int            `json:"total"`
	SearchTime  string         `json:"search_time,omitempty"`
	GeneratedAt time.Time      `json:"generated_at"`
}

// SearchResult represents a single search result
type SearchResult struct {
	BeadID      string  `json:"bead_id"`
	Title       string  `json:"title"`
	Description string  `json:"description,omitempty"`
	Score       float64 `json:"score"`
	Relevance   float64 `json:"relevance"`
	Type        string  `json:"type"`
	Status      string  `json:"status"`
}

// RelatedBeadsResponse represents related beads from bv --robot-related
type RelatedBeadsResponse struct {
	BeadID      string            `json:"bead_id"`
	Related     []RelatedBeadInfo `json:"related"`
	Categories  []RelatedCategory `json:"categories"`
	GeneratedAt time.Time         `json:"generated_at"`
}

// RelatedBeadInfo represents a single related bead
type RelatedBeadInfo struct {
	BeadID      string  `json:"bead_id"`
	Title       string  `json:"title"`
	Type        string  `json:"type"`
	Status      string  `json:"status"`
	Relation    string  `json:"relation"`
	Strength    float64 `json:"strength"`
	Description string  `json:"description,omitempty"`
}

// RelatedCategory represents a category of related beads
type RelatedCategory struct {
	Name  string   `json:"name"`
	Count int      `json:"count"`
	Beads []string `json:"beads"`
}

// ImpactNetworkResponse represents bead impact network from bv --robot-impact-network
type ImpactNetworkResponse struct {
	RootBead    string       `json:"root_bead,omitempty"`
	Nodes       []ImpactNode `json:"nodes"`
	Edges       []ImpactEdge `json:"edges"`
	TotalImpact int          `json:"total_impact"`
	Depth       int          `json:"depth"`
	GeneratedAt time.Time    `json:"generated_at"`
}

// ImpactNode represents a node in the impact network
type ImpactNode struct {
	BeadID       string `json:"bead_id"`
	Title        string `json:"title"`
	Type         string `json:"type"`
	Status       string `json:"status"`
	ImpactLevel  int    `json:"impact_level"`
	Dependencies int    `json:"dependencies"`
	Dependents   int    `json:"dependents"`
}

// ImpactEdge represents an edge in the impact network
type ImpactEdge struct {
	Source string  `json:"source"`
	Target string  `json:"target"`
	Type   string  `json:"type"`
	Weight float64 `json:"weight"`
}

// StatusResponse represents the overall BV client status
type StatusResponse struct {
	Healthy   bool      `json:"healthy"`
	Version   string    `json:"version,omitempty"`
	CheckedAt time.Time `json:"checked_at"`
	Cached    bool      `json:"cached"`
	Commands  []string  `json:"available_commands"`
}

// GraphMetricsResponse represents graph analytics for API responses
type GraphMetricsResponse struct {
	DataHash     string             `json:"data_hash"`
	Timestamp    time.Time          `json:"timestamp"`
	Project      string             `json:"project,omitempty"`
	TotalBeads   int                `json:"total_beads"`
	PageRank     map[string]float64 `json:"page_rank,omitempty"`
	Betweenness  map[string]float64 `json:"betweenness,omitempty"`
	HITS         *HITSMetrics       `json:"hits,omitempty"`
	Clustering   *ClusteringMetrics `json:"clustering,omitempty"`
	Cycles       []CycleInfo        `json:"cycles,omitempty"`
	CriticalPath []string           `json:"critical_path,omitempty"`
}

// HITSMetrics contains HITS algorithm results
type HITSMetrics struct {
	Hubs        map[string]float64 `json:"hubs,omitempty"`
	Authorities map[string]float64 `json:"authorities,omitempty"`
}

// ClusteringMetrics contains clustering coefficient metrics
type ClusteringMetrics struct {
	Global float64            `json:"global"`
	Local  map[string]float64 `json:"local,omitempty"`
}

// BottlenecksResponse represents the response for bottleneck beads
type BottlenecksResponse struct {
	DataHash    string              `json:"data_hash"`
	Timestamp   time.Time           `json:"timestamp"`
	Project     string              `json:"project,omitempty"`
	Bottlenecks []BottleneckDetails `json:"bottlenecks"`
	Count       int                 `json:"count"`
}

// BottleneckDetails represents detailed bottleneck information
type BottleneckDetails struct {
	BeadID          string   `json:"bead_id"`
	Title           string   `json:"title"`
	Betweenness     float64  `json:"betweenness"`
	BlockedCount    int      `json:"blocked_count"`
	BlockedBeads    []string `json:"blocked_beads,omitempty"`
	DependencyDepth int      `json:"dependency_depth"`
	Priority        int      `json:"priority"`
}

// HealthStatus represents the health check result
type HealthStatus struct {
	Healthy   bool      `json:"healthy"`
	CheckedAt time.Time `json:"checked_at"`
	Cached    bool      `json:"cached"`
	Version   string    `json:"version,omitempty"`
}
