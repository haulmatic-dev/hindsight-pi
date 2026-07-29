package core

// Hand-finished implementation of PRD shepherd-a2 FR-001. The dogfood run
// produced the FR-002 tests but left ExtractFailureCause as a stub through
// multiple fix attempts — failing in precisely the way this PRD exists to fix:
// its fix beads carried no cause. This file supersedes the stub at harvest.

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// causePriority orders which lines earn a place in the cause block. Lower is
// more valuable.
func causePriority(line string) int {
	switch {
	case strings.Contains(line, "model call FAILED"):
		return 0
	case strings.Contains(line, "--- FAIL:"):
		return 1
	case strings.Contains(line, "FAIL\t"):
		return 2
	case strings.Contains(line, "Error:") || strings.Contains(line, "error:"):
		return 3
	default:
		return -1 // not a cause line
	}
}

// isPassthroughJSONL reports whether a worker-log line is raw event JSONL
// (model output mirrored into the log). Those lines are noise for a cause
// block — except the one that names the model failure, which the caller
// handles before consulting this.
func isPassthroughJSONL(line string) bool {
	// Worker log lines are "<RFC3339> [stderr] <payload>" or bare payload.
	idx := strings.Index(line, "] ")
	payload := line
	if idx >= 0 {
		payload = line[idx+2:]
	}
	payload = strings.TrimSpace(payload)
	return strings.HasPrefix(payload, "{")
}

// extractFailureCause distils a compact cause block from the newest worker log
// for beadID under logDir. Pure log parsing: no model, no network, no
// subprocess. Missing logs yield "" — a fix bead without a cause block beats a
// close path that errors.
func extractFailureCause(logDir, beadID string, maxBytes int) string {
	if logDir == "" || beadID == "" || maxBytes <= 0 {
		return ""
	}
	matches, err := filepath.Glob(filepath.Join(logDir, beadID+"-*.log"))
	if err != nil || len(matches) == 0 {
		return ""
	}
	// Newest by mtime: a bead can have several attempts, and the last one's
	// failure is the one the fix bead must address.
	sort.Slice(matches, func(i, j int) bool {
		fi, ei := os.Stat(matches[i])
		fj, ej := os.Stat(matches[j])
		if ei != nil || ej != nil {
			return matches[i] < matches[j]
		}
		return fi.ModTime().After(fj.ModTime())
	})
	data, err := os.ReadFile(matches[0])
	if err != nil {
		return ""
	}
	lines := strings.Split(string(data), "\n")

	// Collect cause lines grouped by priority so truncation drops the least
	// valuable first.
	byPrio := map[int][]string{}
	for _, line := range lines {
		p := causePriority(line)
		if p < 0 {
			continue
		}
		// Passthrough JSONL is excluded except the model-failure marker: raw
		// event noise must not fill the block, but the one line naming the
		// model failure is the most valuable line in the file.
		if isPassthroughJSONL(line) && !strings.Contains(line, "model call FAILED") {
			continue
		}
		byPrio[p] = append(byPrio[p], strings.TrimSpace(line))
	}

	var picked []string
	for p := 0; p <= 3; p++ {
		picked = append(picked, byPrio[p]...)
	}

	// Fallback: nothing matched — take the last non-empty, non-passthrough
	// lines so the block is never empty when a log exists.
	if len(picked) == 0 {
		for i := len(lines) - 1; i >= 0 && len(picked) < 10; i-- {
			l := strings.TrimSpace(lines[i])
			if l == "" || isPassthroughJSONL(l) {
				continue
			}
			picked = append([]string{l}, picked...)
		}
	}
	if len(picked) == 0 {
		return ""
	}

	// Assemble under the byte budget, keeping highest-priority lines; truncate
	// a single overlong line from the front (the end of a long error line is
	// usually the informative part).
	var b strings.Builder
	for _, l := range picked {
		// Room left after prior lines (each costs its bytes plus one newline,
		// trimmed on the final line). "…" is 3 bytes.
		room := maxBytes - b.Len()
		if room <= 4 {
			break
		}
		if len(l) > room-1 {
			l = "…" + l[len(l)-(room-4):]
		}
		b.WriteString(l)
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}
