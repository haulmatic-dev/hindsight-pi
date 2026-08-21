package beads

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// TestGetBead_EnrichesMetadataFromJSONL verifies that target_files stored in
// .beads/issues.jsonl metadata/bead_metadata are visible to validators and
// workers even when the `br show` response omits the metadata object.
func TestGetBead_EnrichesMetadataFromJSONL(t *testing.T) {
	dir := t.TempDir()
	beadsDir := filepath.Join(dir, ".beads")
	if err := os.MkdirAll(beadsDir, 0755); err != nil {
		t.Fatal(err)
	}

	jsonl := `{"id":"hydra-meta1","title":"Meta bead","description":"d","status":"open","priority":2,"issue_type":"task","labels":[],"created_at":"2026-08-21T00:00:00Z","updated_at":"2026-08-21T00:00:00Z","metadata":{"target_files":"a.go,b.go"},"bead_metadata":{"extra":"x"}}`
	if err := os.WriteFile(filepath.Join(beadsDir, "issues.jsonl"), []byte(jsonl+"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	mockExec := NewMockExecutor()
	// br show response deliberately lacks metadata.
	mockExec.AddResponse("br", []string{"show", "hydra-meta1", "--json"}, []byte(`[{"id":"hydra-meta1","title":"Meta bead","description":"d","status":"open","priority":2,"issue_type":"task","labels":[],"created_at":"2026-08-21T00:00:00Z","updated_at":"2026-08-21T00:00:00Z"}]`), nil)

	client := NewClientWithExecutor(mockExec)
	bead, err := client.GetBead(dir, "", types.BeadID("hydra-meta1"))
	if err != nil {
		t.Fatal(err)
	}

	if bead.Metadata == nil {
		t.Fatal("expected non-nil metadata")
	}
	if got := bead.Metadata["target_files"]; got != "a.go,b.go" {
		t.Errorf("target_files: want a.go,b.go, got %q", got)
	}
	if got := bead.Metadata["extra"]; got != "x" {
		t.Errorf("extra: want x, got %q", got)
	}
}

// TestGetReadyBeads_EnrichesMetadataFromJSONL verifies the ready-beads path
// also picks up JSONL metadata so workers receive target_files.
func TestGetReadyBeads_EnrichesMetadataFromJSONL(t *testing.T) {
	dir := t.TempDir()
	beadsDir := filepath.Join(dir, ".beads")
	if err := os.MkdirAll(beadsDir, 0755); err != nil {
		t.Fatal(err)
	}

	jsonl := `{"id":"hydra-ready1","title":"Ready bead","description":"d","status":"open","priority":2,"issue_type":"implement","labels":[],"created_at":"2026-08-21T00:00:00Z","updated_at":"2026-08-21T00:00:00Z","bead_metadata":{"target_files":"impl.go"}}`
	if err := os.WriteFile(filepath.Join(beadsDir, "issues.jsonl"), []byte(jsonl+"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	mockExec := NewMockExecutor()
	mockExec.AddResponse("br", []string{"ready", "--json"}, []byte(`[{"id":"hydra-ready1","title":"Ready bead","description":"d","status":"open","priority":2,"issue_type":"implement","labels":[],"created_at":"2026-08-21T00:00:00Z","updated_at":"2026-08-21T00:00:00Z"}]`), nil)

	client := NewClientWithExecutor(mockExec)
	beads, err := client.GetReadyBeads("test", dir, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(beads) != 1 {
		t.Fatalf("expected 1 ready bead, got %d", len(beads))
	}
	if got := beads[0].Metadata["target_files"]; got != "impl.go" {
		t.Errorf("target_files: want impl.go, got %q", got)
	}
}
