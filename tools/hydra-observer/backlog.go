package main

import (
	"crypto/sha1"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Entry is one finding to be persisted in the backlog.
type Entry struct {
	Kind     string
	Key      string
	Severity string
	Evidence string
}

// Fingerprint returns a stable 16-character hex identifier for e.
// It ignores Evidence so that the same defect observed on different runs
// collapses to a single backlog item. Identity fields are normalized before
// hashing so trivial whitespace or casing differences do not create duplicates.
func Fingerprint(e Entry) string {
	id := fmt.Sprintf("%s|%s|%s", normalizeIdentifier(e.Kind), normalizeIdentifier(e.Key), normalizeIdentifier(e.Severity))
	sum := sha1.Sum([]byte(id))
	return fmt.Sprintf("%x", sum[:8])
}

// normalizeIdentifier returns a trimmed, lowercased string for stable comparisons.
func normalizeIdentifier(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

// WriteBacklog persists entries as markdown files in dir, skipping any whose
// fingerprint file already exists or has already been written in this batch.
// It returns the number of files written.
func WriteBacklog(dir string, entries []Entry) (int, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return 0, err
	}

	if len(entries) == 0 {
		return 0, nil
	}

	existing, err := existingMarkdownFiles(dir)
	if err != nil {
		return 0, err
	}

	sorted := make([]Entry, len(entries))
	copy(sorted, entries)
	sortBySeverityThenKey(sorted)

	known := existing
	written := 0
	for _, entry := range sorted {
		fp := Fingerprint(entry)
		name := filenameFor(fp)
		if _, exists := known[name]; exists {
			continue
		}

		if err := writeEntryFile(dir, name, entry, fp); err != nil {
			return written, err
		}
		known[name] = struct{}{}
		written++
	}
	return written, nil
}

// sortBySeverityThenKey sorts entries so critical items come first, and ties
// are broken by key. It mutates the slice in place.
func sortBySeverityThenKey(entries []Entry) {
	sort.Slice(entries, func(i, j int) bool {
		rankI := severityRank(entries[i].Severity)
		rankJ := severityRank(entries[j].Severity)
		if rankI != rankJ {
			return rankI < rankJ
		}
		return entries[i].Key < entries[j].Key
	})
}

// severityRank maps known severity strings to a stable sort order. Unknown
// severities are ordered after the supported ones.
func severityRank(s string) int {
	switch s {
	case "critical":
		return 0
	case "warning":
		return 1
	default:
		return 2
	}
}

// existingMarkdownFiles returns the set of .md file names already present in dir.
// Building the set once avoids repeated stat calls when deduplicating a large
// backlog against an existing directory.
func existingMarkdownFiles(dir string) (map[string]struct{}, error) {
	items, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	files := make(map[string]struct{}, len(items))
	for _, item := range items {
		if item.IsDir() {
			continue
		}
		name := item.Name()
		if strings.HasSuffix(name, ".md") {
			files[name] = struct{}{}
		}
	}
	return files, nil
}

// filenameFor returns the markdown file name for a fingerprint.
func filenameFor(fingerprint string) string {
	return fingerprint + ".md"
}

// writeEntryFile writes a single backlog entry as markdown.
func writeEntryFile(dir, filename string, entry Entry, fingerprint string) error {
	path := filepath.Join(dir, filename)
	return os.WriteFile(path, []byte(formatEntry(entry, fingerprint)), 0644)
}

// formatEntry returns the markdown content for a backlog entry.
func formatEntry(entry Entry, fingerprint string) string {
	return fmt.Sprintf(
		"# Observer: %s (%s)\n\nEvidence:\n```\n%s\n```\n\nFingerprint: %s\n",
		entry.Key,
		entry.Severity,
		entry.Evidence,
		fingerprint,
	)
}
