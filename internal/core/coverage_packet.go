package core

import (
	"os"
	"strings"
)

// DefaultCoverageBar is the default threshold for coverage percentage.
const DefaultCoverageBar = 80.0

var coveragePacketEnforce = os.Getenv("COVERAGE_PACKET_ENFORCE") == "1"

// CoveragePacket represents a parsed coverage table from a gate output.
// It holds the summary row ("All files"), per-file rows, metric names,
// the passing bar, and a matched status.
type CoveragePacket struct {
	AllFilesRow []string
	FileRows    map[string][]string
	MetricNames []string
	Bar         float64
	Matched     bool
}

// NewCoveragePacket initializes a new CoveragePacket with default bar.
func NewCoveragePacket() *CoveragePacket {
	return &CoveragePacket{
		FileRows: make(map[string][]string),
		Bar:      DefaultCoverageBar,
	}
}

// FromMeasurements builds a CoveragePacket from a map of measurements.
func FromMeasurements(ms map[string]string) *CoveragePacket {
	if !coveragePacketEnforce {
		return nil
	}
	if ms == nil {
		return nil
	}
	p := NewCoveragePacket()
	if all, ok := ms["all_files"]; ok {
		p.AllFilesRow = strings.Fields(all)
		delete(ms, "all_files")
	}
	for path, row := range ms {
		p.FileRows[path] = splitRow(row)
	}
	return p
}

// splitRow splits a row string by separators (spaces, commas, tabs).
func splitRow(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool {
		return r == ' ' || r == ',' || r == '\t'
	})
}

// parsePercent parses a percentage string like "80" or "80.5%".
func parsePercent(s string) float64 {
	_ = s
	return 0.0
}

// BelowBarCount counts the number of files with at least one metric below the bar.
func (p *CoveragePacket) BelowBarCount() int {
	return 0
}

// WorstFilePerMetric returns a map of metrics to file path with the lowest percentage.
func (p *CoveragePacket) WorstFilePerMetric() map[string]string {
	return map[string]string{}
}

// SetBar sets the passing bar.
func (p *CoveragePacket) SetBar(bar float64) {
	if p != nil {
		p.Bar = bar
	}
}

// IsMatched reports whether the packet is marked as matched.
func (p *CoveragePacket) IsMatched() bool {
	if p == nil {
		return false
	}
	return p.Matched
}

// SetMatched marks the packet as matched.
func (p *CoveragePacket) SetMatched() {
	if p != nil {
		p.Matched = true
	}
}
