// detect-placeholders finds empty files, stub-only files, and placeholder comments.
//
// Usage:
//
//	go run tools/detect-placeholders/main.go --source . --min-bytes 50
//
// Exit codes:
//
//	0 - no placeholders found
//	1 - placeholders detected
//	2 - config error
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type Violation struct {
	File           string `json:"file"`
	SizeBytes      int64  `json:"size_bytes,omitempty"`
	Issue          string `json:"issue"` // "file_too_small" | "placeholder_comment" | "only_comments" | "empty_file"
	Line           int    `json:"line,omitempty"`
	Match          string `json:"match,omitempty"`
	ContentPreview string `json:"content_preview,omitempty"`
}

type Result struct {
	Passed     bool        `json:"passed"`
	Violations []Violation `json:"violations"`
	Clean      []string    `json:"clean,omitempty"`
}

var defaultForbidden = []string{
	"TODO", "FIXME", "stub", "implement me",
	"not implemented", "not yet implemented",
}

var defaultIgnore = []string{
	".git", "node_modules", "vendor", ".beads", ".pi", ".tldr",
	"*.log", "bin/", "dist/", "build/", ".env", "go.sum", "go.mod",
	".gitignore", ".gitattributes", "tools/",
}

func main() {
	var (
		source       = flag.String("source", ".", "Source root directory")
		minBytes     = flag.Int("min-bytes", 50, "Minimum file size in bytes")
		forbiddenStr = flag.String("forbidden", strings.Join(defaultForbidden, ","), "Comma-separated forbidden patterns")
		ignoreStr    = flag.String("ignore", strings.Join(defaultIgnore, ","), "Comma-separated ignore patterns")
		exts         = flag.String("exts", ".go,.ts,.js,.py,.rs,.java,.c,.cpp,.h,.hpp,.swift,.kt,.scala,.php,.rb,.cs,.fs,.ex,.erl,.lua,.r,.m,.mm,.dart,.groovy,.clj,.cljs,.coffee,.elm", "Comma-separated file extensions to scan")
		jsonOut      = flag.Bool("json", true, "Output JSON")
		verbose      = flag.Bool("verbose", false, "Print scan progress")
	)
	flag.Parse()

	forbidden := splitComma(*forbiddenStr)
	ignorePatterns := splitComma(*ignoreStr)
	extensions := splitComma(*exts)
	extSet := make(map[string]bool)
	for _, e := range extensions {
		extSet[strings.TrimSpace(e)] = true
	}

	result := Result{
		Passed:     true,
		Violations: []Violation{},
		Clean:      []string{},
	}

	// Build forbidden regex
	var forbiddenRe *regexp.Regexp
	if len(forbidden) > 0 {
		escaped := make([]string, len(forbidden))
		for i, f := range forbidden {
			escaped[i] = regexp.QuoteMeta(f)
		}
		pattern := `(?i)\b(` + strings.Join(escaped, "|") + `)\b`
		forbiddenRe = regexp.MustCompile(pattern)
	}

	err := filepath.WalkDir(*source, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable
		}
		if d.IsDir() {
			if shouldIgnoreDir(path, ignorePatterns) {
				return filepath.SkipDir
			}
			return nil
		}

		// Skip test files (they legitimately mention stubs/placeholders)
		if strings.HasSuffix(filepath.Base(path), "_test.go") ||
			strings.HasSuffix(filepath.Base(path), ".test.ts") ||
			strings.HasSuffix(filepath.Base(path), ".spec.ts") ||
			strings.HasSuffix(filepath.Base(path), ".test.js") ||
			strings.HasSuffix(filepath.Base(path), ".spec.js") ||
			strings.HasSuffix(filepath.Base(path), "_test.py") ||
			strings.HasSuffix(filepath.Base(path), "_test.rs") {
			return nil
		}

		// Check extension
		ext := filepath.Ext(path)
		if !extSet[ext] {
			return nil
		}

		// Check ignore patterns
		relPath, _ := filepath.Rel(*source, path)
		if shouldIgnoreFile(relPath, ignorePatterns) {
			return nil
		}

		if *verbose {
			fmt.Fprintln(os.Stderr, "Scanning:", relPath)
		}

		info, err := d.Info()
		if err != nil {
			return nil
		}

		// Check 1: File too small
		if info.Size() < int64(*minBytes) {
			preview := readPreview(path, 80)
			result.Violations = append(result.Violations, Violation{
				File:           relPath,
				SizeBytes:      info.Size(),
				Issue:          "file_too_small",
				ContentPreview: preview,
			})
			result.Passed = false
			return nil
		}

		// Check 2: Placeholder comments and comment-only files
		content, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		contentStr := string(content)

		// Check for forbidden patterns
		if forbiddenRe != nil {
			matches := findAllMatches(forbiddenRe, contentStr)
			for _, m := range matches {
				result.Violations = append(result.Violations, Violation{
					File:      relPath,
					SizeBytes: info.Size(),
					Issue:     "placeholder_comment",
					Line:      m.line,
					Match:     m.text,
				})
				result.Passed = false
			}
		}

		// Check 3: File contains only comments / whitespace
		if isCommentOnly(contentStr, ext) {
			result.Violations = append(result.Violations, Violation{
				File:      relPath,
				SizeBytes: info.Size(),
				Issue:     "only_comments",
			})
			result.Passed = false
			return nil
		}

		result.Clean = append(result.Clean, relPath)
		return nil
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Walk error: %v\n", err)
		os.Exit(2)
	}

	if *jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		enc.Encode(result)
	} else {
		if result.Passed {
			fmt.Printf("✓ All %d files clean\n", len(result.Clean))
		} else {
			fmt.Printf("✗ %d violations in %d files\n", len(result.Violations), len(result.Violations))
			for _, v := range result.Violations {
				fmt.Printf("  [%s] %s", v.Issue, v.File)
				if v.Line > 0 {
					fmt.Printf(":%d", v.Line)
				}
				if v.Match != "" {
					fmt.Printf(" (match: %q)", v.Match)
				}
				if v.ContentPreview != "" {
					fmt.Printf(" preview: %q", v.ContentPreview)
				}
				fmt.Println()
			}
		}
	}

	if !result.Passed {
		os.Exit(1)
	}
}

func splitComma(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func shouldIgnoreDir(path string, patterns []string) bool {
	base := filepath.Base(path)
	for _, p := range patterns {
		if base == p || strings.HasSuffix(path, "/"+p) {
			return true
		}
		// Match glob suffix like "dist/"
		if strings.HasSuffix(p, "/") && base == strings.TrimSuffix(p, "/") {
			return true
		}
	}
	return false
}

func shouldIgnoreFile(relPath string, patterns []string) bool {
	for _, p := range patterns {
		if matched, _ := filepath.Match(p, relPath); matched {
			return true
		}
		if matched, _ := filepath.Match(p, filepath.Base(relPath)); matched {
			return true
		}
		if strings.HasPrefix(relPath, p) {
			return true
		}
	}
	return false
}

type matchInfo struct {
	line int
	text string
}

func findAllMatches(re *regexp.Regexp, content string) []matchInfo {
	var matches []matchInfo
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		if re.MatchString(line) {
			matches = append(matches, matchInfo{
				line: i + 1,
				text: strings.TrimSpace(line),
			})
		}
	}
	return matches
}

func readPreview(path string, maxLen int) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	preview := string(data)
	if len(preview) > maxLen {
		preview = preview[:maxLen] + "..."
	}
	preview = strings.ReplaceAll(preview, "\n", "\\n")
	return preview
}

// isCommentOnly checks if file has no executable code (only comments/whitespace).
// Language-aware: strips comments for known languages.
func isCommentOnly(content string, ext string) bool {
	switch ext {
	case ".go", ".c", ".cpp", ".h", ".hpp", ".java", ".js", ".ts", ".rs", ".swift", ".kt", ".scala", ".dart", ".php", ".cs":
		return isCStyleCommentOnly(content)
	case ".py", ".rb", ".sh", ".yaml", ".yml", ".toml", ".dockerfile":
		return isHashCommentOnly(content)
	default:
		return false // conservative — don't flag unknown languages
	}
}

func isCStyleCommentOnly(content string) bool {
	var out strings.Builder
	i := 0
	for i < len(content) {
		// Single-line comment
		if i+1 < len(content) && content[i] == '/' && content[i+1] == '/' {
			for i < len(content) && content[i] != '\n' {
				i++
			}
			continue
		}
		// Multi-line comment
		if i+1 < len(content) && content[i] == '/' && content[i+1] == '*' {
			i += 2
			for i < len(content)-1 {
				if content[i] == '*' && content[i+1] == '/' {
					i += 2
					break
				}
				i++
			}
			continue
		}
		out.WriteByte(content[i])
		i++
	}
	stripped := strings.TrimSpace(out.String())
	return stripped == ""
}

func isHashCommentOnly(content string) bool {
	var out strings.Builder
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		out.WriteString(line)
		out.WriteByte('\n')
	}
	stripped := strings.TrimSpace(out.String())
	return stripped == ""
}
