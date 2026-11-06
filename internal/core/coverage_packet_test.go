package core

import (
	"reflect"
	"testing"
)

func TestCoveragePacket_FromMeasurements_Nil(t *testing.T) {
	p := FromMeasurements(nil)
	if p != nil {
		t.Fatalf("expected nil packet for nil measurements, got %+v", p)
	}
}

func TestCoveragePacket_FromMeasurements_Empty(t *testing.T) {
	p := FromMeasurements(map[string]string{})
	if p == nil {
		t.Fatal("expected non-nil packet for empty map")
	}
	if len(p.FileRows) != 0 {
		t.Fatalf("expected no file rows, got %d", len(p.FileRows))
	}
	if len(p.AllFilesRow) != 0 {
		t.Fatalf("expected empty AllFilesRow, got %#v", p.AllFilesRow)
	}
}

func TestCoveragePacket_FromMeasurements_PopulatesFields(t *testing.T) {
	in := map[string]string{
		"all_files":  "85.5 90.1",
		"src/a.go":   "50.0 60.1",
		"src/b.go":   "95.1 92.3",
	}
	got := FromMeasurements(in)

	if got == nil {
		t.Fatal("expected non-nil packet for populated map")
	}
	if !reflect.DeepEqual(got.AllFilesRow, []string{"85.5", "90.1"}) {
		t.Fatalf("AllFilesRow = %#v, want [85.5 90.1]", got.AllFilesRow)
	}
	if len(got.FileRows) != 2 {
		t.Fatalf("FileRows len = %d, want 2", len(got.FileRows))
	}
	if got.Bar != DefaultCoverageBar {
		t.Fatalf("Bar = %v, want %v", got.Bar, DefaultCoverageBar)
	}
	if _, ok := got.FileRows["all_files"]; ok {
		t.Fatal("all_files should not be present as a file row")
	}
	if !reflect.DeepEqual(got.FileRows["src/a.go"], []string{"50.0", "60.1"}) {
		t.Fatalf("FileRows[src/a.go] = %#v, want [50.0 60.1]", got.FileRows["src/a.go"])
	}
	// FromMeasurements should not mutate its input beyond removing the
	// all_files key so the caller's map stays intact.
	if _, stillAll := in["all_files"]; stillAll {
		t.Fatal("all_files key should have been consumed from the input map")
	}
}

func TestCoveragePacket_SplitRow_Variants(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []string
	}{
		{"empty", "", nil},
		{"spaces", "1.2  3.4", []string{"1.2", "3.4"}},
		{"commas", "1.2,3.4", []string{"1.2", "3.4"}},
		{"tabs", "\t1.2\t3.4\t", []string{"1.2", "3.4"}},
		{"mixed", "1.2 , 3.4 \t 5.6", []string{"1.2", "3.4", "5.6"}},
		{"trailing-comma", "1.2,", []string{"1.2"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := splitRow(tt.in)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("splitRow(%q) = %#v, want %#v", tt.in, got, tt.want)
			}
		})
	}
}

func TestCoveragePacket_ParsePercent_Variants(t *testing.T) {
	tests := []struct {
		in  string
		want float64
		isNaN bool
	}{
		{"80", 80, false},
		{"80.5", 80.5, false},
		{"80%", 80, false},
		{" 72.3% ", 72.3, false},
		{"", 0, true},
		{"abc", 0, true},
		{"50.x%", 0, true},
	}
	for _, tt := range tests {
		got := parsePercent(tt.in)
		if tt.isNaN {
			if got == got { // NaN != NaN
				t.Fatalf("parsePercent(%q) = %v, want NaN", tt.in, got)
			}
		} else if got != tt.want {
			t.Fatalf("parsePercent(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

func TestCoveragePacket_BelowBarCount(t *testing.T) {
	tests := []struct {
		name string
		in   map[string]string
		want int
	}{
		{"nil-map", nil, 0},
		{"empty-map", map[string]string{}, 0},
		{
			"all-above",
			map[string]string{"a.go": "90", "b.go": "80"},
			0,
		},
		{
			"one-below",
			map[string]string{"a.go": "79.9", "b.go": "80"},
			1,
		},
		{
			"two-below-same-file",
			map[string]string{"a.go": "50 60", "b.go": "95"},
			1,
		},
		{
			"two-files-below",
			map[string]string{"a.go": "50 60", "b.go": "70 90"},
			2,
		},
		{
			"non-percent-cells-count-as-breach",
			map[string]string{"a.go": "not-a-pct", "b.go": "90"},
			1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := FromMeasurements(tt.in)
			got := p.BelowBarCount()
			if got != tt.want {
				t.Fatalf("BelowBarCount() = %d, want %d for %#v", got, tt.want, tt.in)
			}
		})
	}
}

func TestCoveragePacket_BelowBarCount_CustomBar(t *testing.T) {
	p := FromMeasurements(map[string]string{"a.go": "85", "b.go": "90"})
	p.SetBar(90)
	if got := p.BelowBarCount(); got != 1 {
		t.Fatalf("BelowBarCount() with bar=90 = %d, want 1 (85 < 90, 90 >= 90)", got)
	}
	p.SetBar(0)
	if got := p.BelowBarCount(); got != 0 {
		t.Fatalf("BelowBarCount() with bar<=0 should fall back to default, got %d", got)
	}
}

func TestCoveragePacket_WorstFilePerMetric(t *testing.T) {
	tests := []struct {
		name string
		in   map[string]string
		want map[string]string
	}{
		{"nil", nil, map[string]string{}},
		{"empty", map[string]string{}, map[string]string{}},
		{
			"single-file",
			map[string]string{"a.go": "70 60"},
			map[string]string{"metric_0": "a.go", "metric_1": "a.go"},
		},
		{
			"mixed",
			map[string]string{"a.go": "50 90", "b.go": "80 40", "c.go": "100 100"},
			// metric_0 lowest is a.go (50), metric_1 lowest is b.go (40)
			map[string]string{"metric_0": "a.go", "metric_1": "b.go"},
		},
		{
			"non-pct-scores-ignored",
			map[string]string{"a.go": "50 abc", "b.go": "34 80"},
			// metric_0 lowest is b.go (34) since a.go's second is NaN and a.go's first is 50 > 34.
			map[string]string{"metric_0": "b.go", "metric_1": "b.go"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got map[string]string
			if tt.in == nil {
				got = (&CoveragePacket{}).WorstFilePerMetric()
			} else {
				got = FromMeasurements(tt.in).WorstFilePerMetric()
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("WorstFilePerMetric() = %#v, want %#v for %#v", got, tt.want, tt.in)
			}
		})
	}
}

func TestCoveragePacket_MatchedFlag(t *testing.T) {
	p := NewCoveragePacket()
	if p.IsMatched() {
		t.Fatal("fresh packet should not be matched")
	}
	p.SetMatched()
	if !p.IsMatched() {
		t.Fatal("packet should be matched after SetMatched")
	}
	// nil receiver safety
	var q *CoveragePacket
	if q.IsMatched() {
		t.Fatal("nil packet should never report matched")
	}
}

func TestCoveragePacket_ZeroValueSafety(t *testing.T) {
	var p *CoveragePacket
	if got := p.BelowBarCount(); got != 0 {
		t.Fatalf("nil packet BelowBarCount should be 0, got %d", got)
	}
	worst := p.WorstFilePerMetric()
	if worst == nil || len(worst) != 0 {
		t.Fatalf("nil packet WorstFilePerMetric should be empty map, got %#v", worst)
	}
}
