package beads

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIsWorkspace(t *testing.T) {
	t.Run("missing", func(t *testing.T) {
		dir := t.TempDir()
		if IsWorkspace(dir) {
			t.Error("expected false for dir without .beads")
		}
	})
	t.Run("file", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, ".beads"), []byte{}, 0644); err != nil {
			t.Fatal(err)
		}
		if IsWorkspace(dir) {
			t.Error("expected false when .beads is a file")
		}
	})
	t.Run("workspace", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.Mkdir(filepath.Join(dir, ".beads"), 0755); err != nil {
			t.Fatal(err)
		}
		if !IsWorkspace(dir) {
			t.Error("expected true when .beads directory exists")
		}
	})
	t.Run("empty", func(t *testing.T) {
		if IsWorkspace("") {
			t.Error("expected false for empty dir")
		}
	})
}
