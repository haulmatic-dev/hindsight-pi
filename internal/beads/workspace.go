package beads

import (
	"os"
	"path/filepath"
)

// IsWorkspace reports whether dir is the root of a beads workspace.
// A workspace is identified by the presence of a .beads directory.
func IsWorkspace(dir string) bool {
	if dir == "" {
		return false
	}
	info, err := os.Stat(filepath.Join(dir, ".beads"))
	if err != nil {
		return false
	}
	return info.IsDir()
}
