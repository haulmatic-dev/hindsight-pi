package core

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// AppendActivity writes a per-repo activity event to repoPath/.hydra/activity.log
// and ensures repoPath/.gitignore contains an ignore rule for .hydra/.
// It is fail-open: I/O errors do not panic.
func AppendActivity(repoPath, beadID, event string) {
	if repoPath == "" || beadID == "" {
		return
	}

	hydraDir := filepath.Join(repoPath, ".hydra")
	if err := os.MkdirAll(hydraDir, 0o755); err != nil {
		return
	}

	logPath := filepath.Join(hydraDir, "activity.log")
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()

	if _, err := fmt.Fprintf(f, "%s %s\n", beadID, event); err != nil {
		return
	}

	// Ensure .gitignore ignores the .hydra directory.
	gitignorePath := filepath.Join(repoPath, ".gitignore")
	ensureGitignoreLine(gitignorePath, ".hydra/")
}

func ensureGitignoreLine(path, line string) {
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return
	}

	exists := false
	if err == nil {
		s := bufio.NewScanner(strings.NewReader(string(data)))
		for s.Scan() {
			if strings.TrimSpace(s.Text()) == line {
				exists = true
				break
			}
		}
	}
	if exists {
		return
	}

	gf, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer gf.Close()

	if len(data) > 0 && !strings.HasSuffix(string(data), "\n") {
		if _, err := gf.WriteString("\n"); err != nil {
			return
		}
	}
	if _, err := gf.WriteString(line + "\n"); err != nil {
		return
	}
}
