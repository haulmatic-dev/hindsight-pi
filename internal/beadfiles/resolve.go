// Package beadfiles resolves the target files a bead declares. It is the
// single shared resolver for validators and the close path; it imports only
// the standard library and pkg/types so it stays cycle-free.
package beadfiles

import (
	"encoding/json"
	"path/filepath"
	"strings"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// resolveImplemented reports whether Resolve and IsTestFile carry real
// behavior. The contract tests consult it to decide whether to skip.
const resolveImplemented = true

var (
	// metadataKeys are the bead metadata keys holding a file list, in
	// precedence order.
	metadataKeys = []string{"files", "target_files"}
	// testFileSuffixes are base-name suffixes that mark a test file.
	testFileSuffixes = []string{"_test.go", ".spec.ts", ".test.ts", ".spec.js", ".test.js", "_test.py"}
	// filesLinePrefixes are the lower-cased description line prefixes that
	// introduce a file list; longer prefixes come first.
	filesLinePrefixes = []string{"**target files:**", "target files:", "files:"}
)

// Resolve returns the bead's declared target files: repo-relative,
// de-duplicated, order-preserving. The first non-empty source wins: metadata
// "files", metadata "target_files", the target.files array of the first
// fenced json block after a "## Context" heading, then a "Files:" or
// "**Target Files:**" description line.
func Resolve(bead types.Bead) []string {
	for _, key := range metadataKeys {
		if v, ok := bead.Metadata[key]; ok {
			if files := normalize(splitFileList(v)); len(files) > 0 {
				return files
			}
		}
	}
	if files := normalize(contextJSONFiles(bead.Description)); len(files) > 0 {
		return files
	}
	return normalize(filesLine(bead.Description))
}

// IsTestFile reports whether path names a test file (_test.go, .spec.ts,
// .test.ts, .spec.js, .test.js, test_*.py, *_test.py).
func IsTestFile(path string) bool {
	base := filepath.Base(path)
	for _, suffix := range testFileSuffixes {
		if strings.HasSuffix(base, suffix) {
			return true
		}
	}
	return strings.HasPrefix(base, "test_") && strings.HasSuffix(base, ".py")
}

// splitFileList splits a file list on comma, semicolon, space and tab,
// dropping "TBD" and stripping backticks.
func splitFileList(s string) []string {
	var out []string
	for _, p := range strings.FieldsFunc(s, func(r rune) bool {
		return r == ',' || r == ';' || r == ' ' || r == '\t'
	}) {
		p = strings.TrimSpace(strings.Trim(strings.TrimSpace(p), "`"))
		if p != "" && !strings.EqualFold(p, "TBD") {
			out = append(out, p)
		}
	}
	return out
}

// normalize strips ":symbol" suffixes and de-duplicates, preserving order.
func normalize(in []string) []string {
	var out []string
	seen := make(map[string]bool, len(in))
	for _, p := range in {
		p = strings.TrimSpace(p)
		if i := strings.Index(p, ":"); i > 0 {
			p = p[:i]
		}
		if p == "" {
			continue
		}
		p = filepath.Clean(p)
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	return out
}

// contextJSONFiles returns target.files from the first fenced json block
// following a "## Context" heading. Malformed JSON or a missing block yields
// nil.
func contextJSONFiles(desc string) []string {
	block, ok := contextJSONBlock(desc)
	if !ok {
		return nil
	}
	var doc struct {
		Target struct {
			Files []string `json:"files"`
		} `json:"target"`
	}
	if err := json.Unmarshal([]byte(block), &doc); err != nil {
		return nil
	}
	return doc.Target.Files
}

// contextJSONBlock returns the body of the first fenced json block that
// follows a "## Context" heading line.
func contextJSONBlock(desc string) (string, bool) {
	lines := strings.Split(desc, "\n")
	i := 0
	for ; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "## Context" {
			break
		}
	}
	for ; i < len(lines); i++ {
		if strings.EqualFold(strings.TrimSpace(lines[i]), "```json") {
			break
		}
	}
	if i >= len(lines) {
		return "", false
	}
	var block []string
	for i++; i < len(lines); i++ {
		if strings.HasPrefix(strings.TrimSpace(lines[i]), "```") {
			break
		}
		block = append(block, lines[i])
	}
	return strings.Join(block, "\n"), true
}

// filesLine returns the list on the first "Files:" or "**Target Files:**"
// description line.
func filesLine(desc string) []string {
	for _, line := range strings.Split(desc, "\n") {
		l := strings.TrimSpace(line)
		lower := strings.ToLower(l)
		for _, prefix := range filesLinePrefixes {
			if strings.HasPrefix(lower, prefix) {
				return splitFileList(l[len(prefix):])
			}
		}
	}
	return nil
}
