package intelligence

import (
	"testing"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

func newVerdictExtractor(t *testing.T) *BeadPatternExtractor {
	t.Helper()
	cfg := DefaultPatternExtractorConfig()
	cfg.StoragePath = t.TempDir()
	ext, err := NewBeadPatternExtractor(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return ext
}

func TestExtractVerdict_DoneStoresPatterns(t *testing.T) {
	ext := newVerdictExtractor(t)
	beadID := types.BeadID("proj-abc123")

	err := ext.ExtractVerdict(beadID, VerdictPatternInput{
		Done:         true,
		ACMet:        []string{"AC1", "AC2"},
		FilesChanged: []string{"main.go", "main_test.go"},
		Summary:      "implemented feature",
	})
	if err != nil {
		t.Fatalf("ExtractVerdict: %v", err)
	}

	patterns, err := ext.GetPatternsForBead(beadID)
	if err != nil {
		t.Fatal(err)
	}
	if len(patterns) != 2 {
		t.Fatalf("patterns = %d, want 2 (outcome + files)", len(patterns))
	}
	for _, p := range patterns {
		if p.Confidence < 0.7 {
			t.Errorf("pattern %s below confidence bar: %f", p.ID, p.Confidence)
		}
	}
}

func TestExtractVerdict_NotDoneStoresNothing(t *testing.T) {
	ext := newVerdictExtractor(t)
	beadID := types.BeadID("proj-abc123")

	// Abandoned work: done=false drops every pattern below the 0.7 bar.
	err := ext.ExtractVerdict(beadID, VerdictPatternInput{
		Done:         false,
		ACMet:        []string{},
		FilesChanged: []string{"main.go"},
		Summary:      "abandoned: out of scope",
	})
	if err != nil {
		t.Fatalf("ExtractVerdict: %v", err)
	}

	patterns, err := ext.GetPatternsForBead(beadID)
	if err != nil {
		t.Fatal(err)
	}
	if len(patterns) != 0 {
		t.Fatalf("patterns = %d, want 0 for abandoned work", len(patterns))
	}
}

func TestExtractVerdict_DisabledIsNoOp(t *testing.T) {
	cfg := DefaultPatternExtractorConfig()
	cfg.StoragePath = t.TempDir()
	cfg.Enabled = false
	ext, err := NewBeadPatternExtractor(cfg)
	if err != nil {
		t.Fatal(err)
	}

	err = ext.ExtractVerdict(types.BeadID("proj-abc123"), VerdictPatternInput{Done: true, ACMet: []string{"AC1"}})
	if err != nil {
		t.Fatal(err)
	}
	patterns, err := ext.GetPatternsForBead(types.BeadID("proj-abc123"))
	if err != nil {
		t.Fatal(err)
	}
	if len(patterns) != 0 {
		t.Errorf("disabled extractor stored %d patterns", len(patterns))
	}
}
