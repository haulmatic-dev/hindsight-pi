// detect-phantom-files finds files on disk not listed in the PRD.
//
// Usage:
//
//	go run tools/detect-phantom-files/main.go --prd docs/PRDs/feature.md --source .
//
// Exit codes:
//
//	0 - no phantom files, all expected files present
//	1 - phantom files detected
//	2 - expected files missing
//	3 - both issues
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
	Path           string `json:"path"`
	SizeBytes      int64  `json:"size_bytes"`
	Issue          string `json:"issue"` // "not_in_prd" | "prd_specified_but_not_found"
	Recommendation string `json:"recommendation,omitempty"`
}

type Result struct {
	Passed               bool        `json:"passed"`
	PhantomFiles         []Violation `json:"phantom_files,omitempty"`
	ExpectedFilesMissing []Violation `json:"expected_files_missing,omitempty"`
}

var defaultIgnore = []string{
	".git", "node_modules", "vendor", ".beads", ".pi", ".tldr",
	"*.log", "bin/", "dist/", "build/", ".env", "*.env",
	".gitignore", ".gitattributes", ".DS_Store",
	"go.sum", "go.mod", "package-lock.json", "yarn.lock", "pnpm-lock.yaml",
	".github", ".vscode", ".idea",
	// Build artifacts
	"*.out", "*.exe", "*.dll", "*.so", "*.dylib",
	// Generated graph index
	".gograph/",
	"graphify-out/",
	// Go binary — NOTE: only file-level ignore, not dir, to avoid skipping cmd/outreach/
	// "outreach",
	// Generated files
	"*.db", "*.sqlite", "*.sqlite3",
	// Build binary
	"outreach",
	// Docs outside PRD scope
	"docs/sales-and-marketing/", "docs/auth-quick-reference.txt", "docs/quick-reference.md",
	"docs/cloudflare-access-setup.md", "docs/cloudflare-zero-trust-path.txt",
	"docs/session-knowledge.md", "docs/plans/",
	"docs/research-ai-outreach-2026.md", "docs/research-hubspot-ai-crm-2026.md",
	"docs/architecture-ascii.md",
	// The PRD file itself (and old versions)
	"docs/PRDs/outreach-go-migration-20260521.md",
	"docs/PRDs/outreach-go-migration-20260521-v2.md",
	// Tools directory
	"tools/",
	// Common project config files not typically in PRD
	".golangci.yml", "Makefile", ".editorconfig", ".pre-commit-config.yaml",
}

func main() {
	var (
		prdPath   = flag.String("prd", "", "Path to PRD markdown file")
		source    = flag.String("source", ".", "Source root directory")
		ignoreStr = flag.String("ignore", strings.Join(defaultIgnore, ","), "Comma-separated ignore patterns")
		jsonOut   = flag.Bool("json", true, "Output JSON")
		verbose   = flag.Bool("verbose", false, "Print scan progress")
	)
	flag.Parse()

	if *prdPath == "" {
		fmt.Fprintln(os.Stderr, "Usage: detect-phantom-files --prd <prd.md> [--source .]")
		os.Exit(2)
	}

	prdContent, err := os.ReadFile(*prdPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading PRD: %v\n", err)
		os.Exit(2)
	}

	ignorePatterns := splitComma(*ignoreStr)

	// Parse PRD for expected files
	expectedFiles := parsePRDFiles(string(prdContent))
	expectedSet := make(map[string]bool)
	for _, f := range expectedFiles {
		expectedSet[f] = true
	}

	// Scan disk for actual files
	diskFiles := make(map[string]int64) // path -> size
	err = filepath.WalkDir(*source, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if shouldIgnoreDir(path, ignorePatterns) {
				return filepath.SkipDir
			}
			return nil
		}

		relPath, _ := filepath.Rel(*source, path)
		relPath = filepath.ToSlash(relPath) // normalize to forward slashes

		if shouldIgnoreFile(relPath, ignorePatterns) {
			return nil
		}

		info, err := d.Info()
		if err != nil {
			return nil
		}

		diskFiles[relPath] = info.Size()
		if strings.Contains(relPath, "main.go") || strings.Contains(relPath, "test.go") {
		}
		return nil
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Walk error: %v\n", err)
		os.Exit(2)
	}

	result := Result{
		Passed:               true,
		PhantomFiles:         []Violation{},
		ExpectedFilesMissing: []Violation{},
	}

	// Find phantom files: on disk but NOT in PRD
	for f, size := range diskFiles {
		if !expectedSet[f] {
			result.PhantomFiles = append(result.PhantomFiles, Violation{
				Path:           f,
				SizeBytes:      size,
				Issue:          "not_in_prd",
				Recommendation: "Remove or add to PRD Target Files / Project Structure",
			})
			result.Passed = false
			if *verbose {
				fmt.Fprintf(os.Stderr, "PHANTOM: %s\n", f)
			}
		}
	}

	// Find expected files missing: in PRD but NOT on disk
	// Skip bare filenames (no slashes) — they likely came from tree parsing
	// and lost their directory context. We can't accurately verify them.
	for f := range expectedSet {
		if _, ok := diskFiles[f]; !ok {
			if !strings.Contains(f, "/") {
				continue // skip bare filenames from tree parsing
			}
			result.ExpectedFilesMissing = append(result.ExpectedFilesMissing, Violation{
				Path:  f,
				Issue: "prd_specified_but_not_found",
			})
			result.Passed = false
			if *verbose {
				fmt.Fprintf(os.Stderr, "MISSING: %s\n", f)
			}
		}
	}

	if *jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		enc.Encode(result)
	} else {
		if result.Passed {
			fmt.Printf("✓ All files accounted for (%d on disk, %d in PRD)\n", len(diskFiles), len(expectedSet))
		} else {
			if len(result.PhantomFiles) > 0 {
				fmt.Printf("✗ %d phantom files (on disk but not in PRD):\n", len(result.PhantomFiles))
				for _, v := range result.PhantomFiles {
					fmt.Printf("  %s\n", v.Path)
				}
			}
			if len(result.ExpectedFilesMissing) > 0 {
				fmt.Printf("✗ %d expected files missing (in PRD but not on disk):\n", len(result.ExpectedFilesMissing))
				for _, v := range result.ExpectedFilesMissing {
					fmt.Printf("  %s\n", v.Path)
				}
			}
		}
	}

	exitCode := 0
	if len(result.PhantomFiles) > 0 {
		exitCode |= 1
	}
	if len(result.ExpectedFilesMissing) > 0 {
		exitCode |= 2
	}
	if exitCode != 0 {
		os.Exit(exitCode)
	}
}

// parsePRDFiles extracts all file paths from PRD markdown.
// Looks for:
//
//	**Target Files:**
//	- `path/to/file.go`
//	Project Structure sections with code blocks showing file trees
func parsePRDFiles(content string) []string {
	var files []string
	lines := strings.Split(content, "\n")

	var inTargetFiles bool
	var inProjectStructure bool
	var inCodeBlock bool

	reBacktickPath := regexp.MustCompile("`([^`]+\\.[a-zA-Z0-9]+)`")
	reTreeFile := regexp.MustCompile(`^[\s│├└──]*([a-zA-Z][a-zA-Z0-9_./-]*\.[a-zA-Z0-9]+)$`)
	reTargetFilesHeader := regexp.MustCompile(`(?i)^\s*\*\*Target Files:\*\*\s*$`)
	reProjectStructureHeader := regexp.MustCompile(`(?i)^#{1,4}\s+.*Project Structure`)

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Code block fences
		if strings.HasPrefix(trimmed, "```") {
			inCodeBlock = !inCodeBlock
			if !inCodeBlock {
				inProjectStructure = false
			}
			continue
		}

		// Target Files section
		if reTargetFilesHeader.MatchString(line) {
			inTargetFiles = true
			continue
		}
		if inTargetFiles {
			if strings.HasPrefix(trimmed, "-") || strings.HasPrefix(trimmed, "*") {
				matches := reBacktickPath.FindAllStringSubmatch(line, -1)
				for _, m := range matches {
					f := strings.TrimSpace(m[1])
					if isValidFilePath(f) {
						files = append(files, f)
					}
				}
				continue
			}
			if trimmed != "" && !strings.HasPrefix(trimmed, "-") && !strings.HasPrefix(trimmed, "*") {
				inTargetFiles = false
			}
		}

		// Project Structure section - look for code blocks with tree or file list
		if reProjectStructureHeader.MatchString(line) {
			inProjectStructure = true
			continue
		}
		if inProjectStructure && inCodeBlock {
			// Tree-style: "├── file.go" or "    └── file.go"
			if m := reTreeFile.FindStringSubmatch(trimmed); m != nil {
				files = append(files, m[1])
				continue
			}
			// Simple list: "file.go" or "path/to/file.go"
			if !strings.Contains(trimmed, " ") && strings.Contains(trimmed, ".") {
				// Heuristic: looks like a file path
				if isLikelyFilePath(trimmed) {
					files = append(files, trimmed)
				}
			}
		}
	}

	// Deduplicate
	seen := make(map[string]bool)
	var unique []string
	for _, f := range files {
		if !seen[f] {
			seen[f] = true
			unique = append(unique, f)
		}
	}
	return unique
}

func isValidFilePath(s string) bool {
	// Must have a file extension (at least 2 chars after dot)
	if !strings.Contains(s, ".") {
		return false
	}
	extIdx := strings.LastIndex(s, ".")
	if extIdx == -1 || extIdx == len(s)-1 || len(s)-extIdx < 3 {
		return false
	}
	ext := strings.ToLower(s[extIdx:])
	// Known code/doc extensions
	validExts := map[string]bool{
		".go": true, ".ts": true, ".js": true, ".py": true, ".rs": true,
		".java": true, ".c": true, ".cpp": true, ".h": true, ".hpp": true,
		".swift": true, ".kt": true, ".scala": true, ".php": true, ".rb": true,
		".cs": true, ".fs": true, ".ex": true, ".erl": true, ".lua": true,
		".r": true, ".m": true, ".mm": true, ".dart": true, ".groovy": true,
		".clj": true, ".cljs": true, ".coffee": true, ".elm": true,
		".sql": true, ".md": true, ".txt": true, ".yaml": true, ".yml": true,
		".json": true, ".toml": true, ".xml": true, ".html": true, ".css": true,
		".scss": true, ".sass": true, ".less": true, ".sh": true, ".bash": true,
		".zsh": true, ".fish": true, ".ps1": true, ".bat": true, ".cmd": true,
		".dockerfile": true, ".makefile": true, ".mk": true, ".env": true,
	}
	if !validExts[ext] {
		return false
	}
	// Must not be a URL
	if strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://") {
		return false
	}
	// Must not be an absolute path
	if strings.HasPrefix(s, "/") {
		return false
	}
	// Must not be just a number like "0.95"
	if isNumber(s) {
		return false
	}
	// Must not be a Go identifier like "types.ApolloContact" or "os.Stderr"
	// Pattern: lowercase.word.uppercase.word — this is a type reference, not a file
	if isGoIdentifier(s) {
		return false
	}
	// Must not be a domain name like "swyftflo.com"
	if isDomainName(s) {
		return false
	}
	// Must not contain spaces
	if strings.Contains(s, " ") {
		return false
	}
	// Must not be a comment
	if strings.HasPrefix(s, "#") || strings.HasPrefix(s, "//") {
		return false
	}
	return true
}

func isNumber(s string) bool {
	// Match things like "0.95", "123", "0.5", "0.0"
	return regexp.MustCompile(`^\d+(\.\d+)?$`).MatchString(s)
}

func isGoIdentifier(s string) bool {
	// Matches: package.Type, package.function, os.Stderr, types.ApolloContact
	// But NOT: cmd/outreach/main.go (has slashes)
	if strings.Contains(s, "/") {
		return false
	}
	parts := strings.Split(s, ".")
	if len(parts) != 2 {
		return false
	}
	// package.Type: first part lowercase, second part starts with uppercase
	first := parts[0]
	second := parts[1]
	if len(first) == 0 || len(second) == 0 {
		return false
	}
	if first[0] >= 'a' && first[0] <= 'z' && second[0] >= 'A' && second[0] <= 'Z' {
		return true
	}
	return false
}

func isDomainName(s string) bool {
	// Simple check: has dots but no slashes, ends with known TLD
	if strings.Contains(s, "/") {
		return false
	}
	tlds := []string{".com", ".net", ".org", ".io", ".dev", ".app", ".co", ".ai"}
	for _, tld := range tlds {
		if strings.HasSuffix(strings.ToLower(s), tld) {
			return true
		}
	}
	return false
}

func isLikelyFilePath(s string) bool {
	// Reuse the stricter validation
	return isValidFilePath(s)
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
		// Skip file-only patterns (no slash, not ending with /) in dir checks
		if !strings.Contains(p, "/") && !strings.HasSuffix(p, "/") {
			continue
		}
		if base == p || strings.HasSuffix(path, "/"+p) {
			return true
		}
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
