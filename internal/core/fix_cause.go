package core

import (
	"bufio"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// beadIDTokenRe matches bead-ID-shaped tokens (e.g. hydra-5az8,
// srv-smart-engine-0n6ga_4e316b8f) so they can be scrubbed from failure
// signatures: two beads failing for the same reason must produce the same
// signature regardless of which bead it happened to.
var beadIDTokenRe = regexp.MustCompile(`([a-z0-9][a-z0-9_-]*/)?[a-z0-9][a-z0-9_-]*-[0-9a-z]{3,5}\b`)

// FailureSignature normalizes a close-failure into a cluster key:
// operation + error class with bead IDs scrubbed, lowercased and
// whitespace-collapsed (PRD FR-020). The operation is typically the
// integrate/commit stage ("integrate", "commit", "push", "worker-exit").
func FailureSignature(operation, cause string) string {
	s := strings.ToLower(operation + "|" + cause)
	s = beadIDTokenRe.ReplaceAllString(s, "<bead>")
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > 512 {
		s = s[:512]
	}
	return s
}

// SignatureLabel returns the durable bead label that carries a failure
// signature on both the meta fix bead and the parked originals, so clustering
// and release survive daemon restarts (labels are the only close/verdict
// storage the beads file has).
func SignatureLabel(sig string) string {
	sum := sha256.Sum256([]byte(sig))
	return fmt.Sprintf("fix-sig-%x", sum[:4])
}

// fixClusteredLabel marks a bead that failed and was parked against an open
// meta fix bead sharing its failure signature instead of getting its own
// per-bead fix bead. It is cleared when the meta bead closes (release).
const fixClusteredLabel = "fix-clustered"

// workersLogDir returns the directory where per-bead worker logs are stored.
// It mirrors the path construction in openWorkerLogFile so the two cannot drift.
func workersLogDir() string {
	base := os.Getenv("HYDRA_HOME")
	if base == "" {
		base = os.TempDir()
	}
	return filepath.Join(base, "logs", "workers")
}

// ExtractFailureCause distils a compact cause block from the newest worker log
// for the given bead. It is a pure log parser: no model calls, no network.
func ExtractFailureCause(logDir, beadID string, maxBytes int) string {
	if maxBytes <= 0 {
		return ""
	}

	f := newestWorkerLog(logDir, beadID)
	if f == nil {
		return ""
	}
	defer f.Close()

	var modelLines, failTestLines, failPkgLines, errLines, allLines []string

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		allLines = append(allLines, line)

		// The most valuable line is the model-failure marker, even when it is
		// embedded in worker-passthrough JSONL.
		if strings.Contains(line, "model call FAILED") {
			modelLines = append(modelLines, line)
			continue
		}

		// Skip raw worker-passthrough JSONL event noise.
		if strings.HasPrefix(payload(line), "{") {
			continue
		}

		// Anchor on line structure, not substring presence: a worker that
		// writes parser code echoes lines like `strings.Contains(line,
		// "--- FAIL:")` into its log, and a substring match harvests that
		// source code as a "cause" (run D3 shipped such a block). Real go
		// test output puts the marker at the start of the payload, and real
		// output never carries literal \n/\t/\" escape sequences — those
		// mark stringified source.
		pl := payload(line)
		if looksLikeEscapedSource(pl) {
			continue
		}
		trimmed := strings.TrimLeft(pl, " \t")
		if strings.HasPrefix(trimmed, "--- FAIL:") {
			failTestLines = append(failTestLines, line)
		} else if strings.HasPrefix(trimmed, "FAIL") && strings.Contains(pl, "\t") {
			failPkgLines = append(failPkgLines, line)
		} else if strings.Contains(pl, "Error:") || strings.Contains(pl, "error:") {
			errLines = append(errLines, line)
		}
	}

	var lines []string
	lines = append(lines, modelLines...)
	lines = append(lines, failTestLines...)
	lines = append(lines, failPkgLines...)
	lines = append(lines, errLines...)

	if len(lines) == 0 && len(allLines) > 0 {
		start := len(allLines) - 10
		if start < 0 {
			start = 0
		}
		lines = allLines[start:]
	}

	if len(lines) == 0 {
		return ""
	}

	block := strings.Join(lines, "\n")
	if len(block) <= maxBytes {
		return block
	}

	// Truncate: drop whole low-priority lines first, then keep the end of a
	// single remaining overlong line.
	parts := strings.Split(block, "\n")
	for len(parts) > 1 && len(strings.Join(parts, "\n")) > maxBytes {
		parts = parts[:len(parts)-1]
	}
	if len(parts) == 1 && len(parts[0]) > maxBytes {
		parts[0] = parts[0][len(parts[0])-maxBytes:]
	}
	return strings.Join(parts, "\n")
}

// ExtractRecordedVerdict returns the HYDRA_VERDICT block the original worker
// recorded in its final message, verbatim and bounded. Fix-bead prompts carry
// it so the fix worker knows what the previous attempt already claimed (and
// which ACs were already met) before redoing anything.
func ExtractRecordedVerdict(logDir, beadID string, maxBytes int) string {
	if maxBytes <= 0 {
		return ""
	}
	f := newestWorkerLog(logDir, beadID)
	if f == nil {
		return ""
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	var lines []string
	found := false
	for scanner.Scan() {
		line := scanner.Text()
		if !found && verdictPattern.FindStringIndex(payload(line)) != nil {
			found = true
		}
		if found {
			lines = append(lines, line)
		}
	}
	if !found || len(lines) == 0 {
		return ""
	}
	block := strings.Join(lines, "\n")
	if len(block) > maxBytes {
		block = block[:maxBytes]
	}
	return block
}

// newestWorkerLog opens the newest worker log for the given bead, or nil when
// none exists. Newest wins by modification time.
func newestWorkerLog(logDir, beadID string) *os.File {
	matches, err := filepath.Glob(filepath.Join(logDir, safeBeadID(beadID)+"-*.log"))
	if err != nil || len(matches) == 0 {
		return nil
	}
	sort.Slice(matches, func(i, j int) bool {
		fi, errI := os.Stat(matches[i])
		fj, errJ := os.Stat(matches[j])
		if errI != nil || errJ != nil {
			return matches[i] > matches[j]
		}
		return fi.ModTime().After(fj.ModTime())
	})
	f, err := os.Open(matches[0])
	if err != nil {
		return nil
	}
	return f
}

// safeBeadID mirrors the sanitisation openWorkerLogFile applies to bead IDs
// when constructing log file names.
func safeBeadID(beadID string) string {
	return strings.NewReplacer("/", "-", " ", "-").Replace(beadID)
}

// looksLikeEscapedSource reports whether a payload is stringified source code
// rather than real command output: literal backslash escape sequences only
// appear when code containing strings is echoed into the log.
func looksLikeEscapedSource(pl string) bool {
	return strings.Contains(pl, `\n`) || strings.Contains(pl, `\t`) || strings.Contains(pl, `\"`)
}

// payload returns the portion of a logged line after the worker timestamp/tag
// prefix. If no recognisable prefix is present, the whole line is returned.
func payload(line string) string {
	// Worker lines are written as: <RFC3339><tag> <content>
	// The tag is enclosed in brackets, e.g. [stderr]. The content begins after
	// the closing bracket and the following space.
	idx := strings.Index(line, "] ")
	if idx >= 0 {
		return line[idx+2:]
	}
	return line
}
