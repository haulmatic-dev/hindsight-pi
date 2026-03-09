package ubs

// Bug represents a single bug found by UBS
type Bug struct {
	Rule     string `json:"rule"`
	Severity string `json:"severity"` // critical, warning, info
	File     string `json:"file"`
	Line     int    `json:"line"`
	Column   int    `json:"column"`
	Message  string `json:"message"`
}

// UBSScanResult represents the complete scan result from UBS
type UBSScanResult struct {
	Project      string  `json:"project"`
	FilesScanned int     `json:"files_scanned"`
	BugsFound    int     `json:"bugs_found"`
	Bugs         []Bug   `json:"bugs"`
	Score        float64 `json:"score"` // Calculated: 100 - penalties
}

// ScanOptions provides options for scanning files
type ScanOptions struct {
	Staged      bool     // Scan only staged files
	Diff        bool     // Scan changed files vs HEAD
	MinSeverity string   // Filter by minimum severity (critical, warning, info)
	Files       []string // Specific files to scan
}

// QualityThreshold represents quality gate configuration
type QualityThreshold struct {
	MinScore       float64 // Minimum score to pass (default: 95)
	FailOnAnyBug   bool    // Fail if any bug found regardless of score
	CriticalWeight int     // Weight for critical bugs (default: 10)
	WarningWeight  int     // Weight for warning bugs (default: 1)
	InfoWeight     int     // Weight for info bugs (default: 0)
}

// HealthStatus represents the health check result
type HealthStatus struct {
	Healthy   bool   `json:"healthy"`
	CheckedAt int64  `json:"checked_at"`
	Version   string `json:"version,omitempty"`
	Cached    bool   `json:"cached"`
}

// Severity levels
const (
	SeverityCritical = "critical"
	SeverityWarning  = "warning"
	SeverityInfo     = "info"
)

// DefaultQualityThreshold returns the default quality threshold configuration
func DefaultQualityThreshold() QualityThreshold {
	return QualityThreshold{
		MinScore:       95.0,
		FailOnAnyBug:   false,
		CriticalWeight: 10,
		WarningWeight:  1,
		InfoWeight:     0,
	}
}

// CalculateScore computes the quality score based on bug severities
func (r *UBSScanResult) CalculateScore(threshold QualityThreshold) float64 {
	if r == nil || len(r.Bugs) == 0 {
		return 100.0
	}

	penalty := 0
	for _, bug := range r.Bugs {
		switch bug.Severity {
		case SeverityCritical:
			penalty += threshold.CriticalWeight
		case SeverityWarning:
			penalty += threshold.WarningWeight
		case SeverityInfo:
			penalty += threshold.InfoWeight
		}
	}

	score := 100.0 - float64(penalty)
	if score < 0 {
		return 0.0
	}
	return score
}

// PassesQualityGate checks if the scan result passes the quality threshold
func (r *UBSScanResult) PassesQualityGate(threshold QualityThreshold) bool {
	if r == nil {
		return true // No scan = pass
	}

	if threshold.FailOnAnyBug && r.BugsFound > 0 {
		return false
	}

	score := r.CalculateScore(threshold)
	return score >= threshold.MinScore
}

// GetBugsBySeverity returns bugs filtered by severity
func (r *UBSScanResult) GetBugsBySeverity(severity string) []Bug {
	if r == nil {
		return nil
	}

	var filtered []Bug
	for _, bug := range r.Bugs {
		if bug.Severity == severity {
			filtered = append(filtered, bug)
		}
	}
	return filtered
}

// CountBugsBySeverity counts bugs of a specific severity
func (r *UBSScanResult) CountBugsBySeverity(severity string) int {
	return len(r.GetBugsBySeverity(severity))
}
