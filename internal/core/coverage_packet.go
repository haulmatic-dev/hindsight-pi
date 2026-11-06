package core

import (
	"math"
	"strconv"
	"strings"
)

// DefaultCoverageBar is the default threshold for coverage percentage.
const DefaultCoverageBar = 80.0

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
// The "all_files" key is consumed into AllFilesRow; every other entry
// becomes a per-file row keyed by path.
func FromMeasurements(ms map[string]string) *CoveragePacket {
	if ms == nil {
		return nil
	}
	p := NewCoveragePacket()
	if all, ok := ms["all_files"]; ok {
		p.AllFilesRow = splitRow(all)
		delete(ms, "all_files")
	}
	p.MetricNames = make([]string, len(p.AllFilesRow))
	for i := range p.MetricNames {
		p.MetricNames[i] = metricName(i)
	}
	for path, row := range ms {
		p.FileRows[path] = splitRow(row)
	}
	return p
}

// metricName returns the stable key used for coverage metric columns,
// derived from the zero-based column index (e.g. "metric_0").
func metricName(i int) string {
	return "metric_" + strconv.Itoa(i)
}

// splitRow splits a row string on spaces, commas and tabs.
func splitRow(s string) []string {
	fields := strings.FieldsFunc(s, func(r rune) bool {
		return r == ' ' || r == ',' || r == '\t'
	})
	if len(fields) == 0 {
		return nil
	}
	return fields
}

// nan returns IEEE NaN for "not a percentage cell".
func nan() float64 {
	return math.NaN()
}

// isNaN reports whether f is NaN.
func isNaN(f float64) bool {
	return f != f
}

// parsePercent parses a percentage string like "80", "80.5" or "80%".
// Unparseable values (empty, non-numeric) return NaN, which callers must
// treat as "not a percentage cell".
func parsePercent(s string) float64 {
	s = strings.TrimSpace(s)
	s = strings.TrimSuffix(s, "%")
	s = strings.TrimSpace(s)
	if s == "" {
		return nan()
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return nan()
	}
	return f
}

// effectiveBar returns the packet bar, falling back to the default when
// the configured bar is zero or negative.
func (p *CoveragePacket) effectiveBar() float64 {
	if p != nil && p.Bar > 0 {
		return p.Bar
	}
	return DefaultCoverageBar
}

// isBreachingCell reports whether a single coverage cell violates the bar.
// A cell that is not a valid percentage (NaN) counts as a breach to avoid
// silently ignoring malformed measurements.
func isBreachingCell(cell string, bar float64) bool {
	v := parsePercent(cell)
	return isNaN(v) || v < bar
}

// BelowBarCount counts the number of files with at least one metric below
// the bar. Non-percentage cells count as a breach. A nil packet counts 0.
func (p *CoveragePacket) BelowBarCount() int {
	if p == nil {
		return 0
	}
	bar := p.effectiveBar()
	count := 0
	for _, row := range p.FileRows {
		if anyCellBreaches(row, bar) {
			count++
		}
	}
	return count
}

func anyCellBreaches(row []string, bar float64) bool {
	for _, cell := range row {
		if isBreachingCell(cell, bar) {
			return true
		}
	}
	return false
}

// WorstFilePerMetric returns, for each metric index ("metric_N"), the file
// path with the lowest percentage for that metric. NaN cells are ignored.
// A nil packet returns an empty map.
func (p *CoveragePacket) WorstFilePerMetric() map[string]string {
	worst := map[string]string{}
	if p == nil {
		return worst
	}
	worstVal := map[string]float64{}
	for path, row := range p.FileRows {
		for i, cell := range row {
			v := parsePercent(cell)
			if isNaN(v) {
				continue
			}
			key := metricName(i)
			w, ok := worstVal[key]
			if !ok || v < w {
				worstVal[key] = v
				worst[key] = path
			}
		}
	}
	return worst
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
