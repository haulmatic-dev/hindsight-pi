// verify-java-vs-go compares Java reference code (typically on the
// `development` branch of a black-box migration project) against
// the current Go implementation and emits a structured gap report
// covering behavioural differences.
//
// The tool exists because the legacy compliance check (verify-exports,
// verify-routes, verify-us-ac, detect-placeholders, detect-phantom-files)
// is all *intra-Go* — it checks the Go code against the PRD. It cannot
// detect when the Go code is "different from Java in a way that
// matters" because it doesn't read Java.
//
// Background: the 2026-06 srv-file-handler audit found 19 gaps where
// PRD ↔ Go differed, Java ↔ Go differed, or Go silently diverged
// from Java (e.g., `http://` webhook URL instead of Java's
// `https://`, default task queue `document-extraction-queue` instead
// of Java's `document-ingestion-queue`, etc.). 9 of those gaps were
// caught late by the validator; 5 were PRD-only gaps the validator
// cannot see. This tool closes the Java ↔ Go half of the comparison.
//
// Usage:
//
//	verify-java-vs-go \
//	  --java-ref origin/development \
//	  --go-source . \
//	  --prd docs/PRDs/srv-file-handler-go-migration-20260618.md \
//	  --output gap-report-java-vs-go.json
//
// Exit codes:
//
//	0 - zero gaps (Java and Go match on every checked behaviour)
//	1 - gaps found (see output JSON)
//	2 - configuration error
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// GapReport is the structured output. The shape matches
// hydra-compliance-analyzer's GapReport so the parent can
// concatenate this file's gaps with the parent's.
type GapReport struct {
	Passed     bool    `json:"passed"`
	JavaRef    string  `json:"java_ref"`
	GoSource   string  `json:"go_source"`
	PRDPath    string  `json:"prd_path"`
	Categories []string `json:"categories_checked"`
	Gaps       []Gap   `json:"gaps"`
	Summary    string  `json:"summary"`
}

// Gap describes a single Java-vs-Go mismatch.
type Gap struct {
	ID            string   `json:"id"`
	Category      string   `json:"category"`
	Severity      string   `json:"severity"`
	Description   string   `json:"description"`
	JavaEvidence  string   `json:"java_evidence"`
	GoEvidence    string   `json:"go_evidence"`
	PRDRef        string   `json:"prd_ref,omitempty"`
	TargetFiles   []string `json:"target_files,omitempty"`
	SuggestedBead string   `json:"suggested_bead,omitempty"`
}

type Config struct {
	JavaRef  string
	GoSource string
	PRDPath  string
	Output   string
}

// behaviorCheck describes one specific Java-vs-Go behavior we
// want to verify. The Check function receives the Java file
// contents, the Go file contents, and the PRD, and returns a
// list of gaps (empty if the behavior matches).
type behaviorCheck struct {
	Name     string
	JavaRe  *regexp.Regexp
	GoRe    *regexp.Regexp
	DescFmt string // format string with %s for java-evidence, %s for go-evidence
}

// behaviorChecks is the list of behaviors verified-java-vs-go
// scans for. Each entry describes a Java pattern and the matching
// Go pattern; if both are present, the behavior is consistent
// and the check passes. If Java has it but Go doesn't, or vice
// versa, the check emits a Gap.
//
// The list is intentionally small and specific: it covers the
// 19-gap categories from the 2026-06-22 srv-file-handler audit.
// Adding new checks is a deliberate code change so the tool's
// coverage stays auditable.
var behaviorChecks = []behaviorCheck{
	{
		// Java uses Spring `@Value("${temporal.task-queue:document-ingestion-queue}")`
		// which puts the default after the colon inside braces. The Go worker uses
		// a constant `defaultTaskQueue = "document-extraction-queue"`. This is
		// the migration cutover risk: workflows started by Java listener route to
		// document-ingestion-queue, but Go worker polls document-extraction-queue.
		Name:    "task-queue-default",
		JavaRe: regexp.MustCompile(`temporal\.task-queue[:\}]+\s*([a-z][a-z0-9-]+)`),
		GoRe:   regexp.MustCompile(`defaultTaskQueue[^=]*=\s*"([a-z][a-z0-9-]+)"`),
		DescFmt: "Default Temporal task queue mismatch: Java uses '%s', Go uses '%s'. PRD does not pin a default, so the worker and the workflow starter route to different queues across migration cutover.",
	},
	{
		// Both files have an http(s)://srv-invoice URL; the regex captures the scheme
		// portion. Java uses http:// (legacy dev cluster); production requires https://.
		Name:    "webhook-scheme",
		JavaRe: regexp.MustCompile(`(https?)://srv-invoice`),
		GoRe:   regexp.MustCompile(`(https?)://srv-invoice`),
		DescFmt: "Webhook URL scheme differs. Java: '%s'; Go: '%s'. Production requires TLS; the consumer rejects plain http.",
	},
	{
		// Error message strings drift between throw strings and Go error
		// constructor strings. PRD ACs require exact literal strings.
		Name:    "error-message-quoted",
		JavaRe: regexp.MustCompile(`throw new RuntimeException\(\"([^\"]+)\"\)`),
		GoRe:   regexp.MustCompile(`(?:errors\.New|fmt\.Errorf)\(\"([^\"]+)\"`),
		DescFmt: "Error message string mismatch detected between Java throw and Go error constructor. PRD ACs require exact literal strings; drift means the consumer's regex matchers stop working. Java: '%s'; Go: '%s'.",
	},
	{
		// Java: io.temporal.common.RetryOptions.setMaximumAttempts(3). Go: temporal.RetryPolicy.MaximumAttempts: 3.
		Name:    "retry-attempts",
		JavaRe: regexp.MustCompile(`setMaximumAttempts\((\d+)\)`),
		GoRe:   regexp.MustCompile(`MaximumAttempts:\s*(\d+)`),
		DescFmt: "Activity retry attempts mismatch. Java: %s; Go: %s. Workflow retry budget differs across the cutover.",
	},
	{
		// Java @Value with default = "iso8601" or similar; Go const decl.
		// This catches the workflow ID format mismatch (Java: doc-ingestion-{fileId},
		// Go: doc-ingestion-{fileId}). The PRD AC for FR-013 says the format must be
		// doc-ingestion-{fileId}; the Java code is the reference.
		Name:    "workflow-id-prefix",
		JavaRe: regexp.MustCompile(`doc-ingestion-([a-zA-Z0-9_-]+)`),
		GoRe:   regexp.MustCompile(`doc-ingestion-([a-zA-Z0-9_-]+)`),
		DescFmt: "Workflow ID prefix/suffix mismatch. Java: 'doc-ingestion-%s'; Go: 'doc-ingestion-%s'. Cross-cutover workflows started by Java listener won't be observable to Go.",
	},
}

// findJavaFiles walks the Java ref tree (via `git show`) and
// returns the paths to .java files under the conventional
// `src/main/java/` layout. The javaRef is interpreted as a path
// to a git working tree OR a git refspec; we resolve to the
// working tree root so `git show` can find the ref.
func findJavaFiles(javaRef string) ([]string, error) {
	// Resolve to a git working tree. If the path is a refspec
	// (like "origin/development") we cannot use `git -C <ref>` —
	// we have to be inside a working tree that has the ref.
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}

	// Try a sequence of strategies:
	// 1) `git ls-tree -r --name-only <refspec>` from cwd
	// 2) `git ls-tree -r --name-only HEAD:src/main/java` from cwd
	// 3) `git -C <path> ls-tree ...` if javaRef is a path

	tryLs := func(args ...string) ([]byte, error) {
		cmd := exec.Command("git", args...)
		cmd.Dir = cwd
		return cmd.Output()
	}

	var out []byte
	for _, args := range [][]string{
		{"ls-tree", "-r", "--name-only", javaRef},
		{"ls-tree", "-r", "--name-only", "HEAD:src/main/java"},
	} {
		out, err = tryLs(args...)
		if err == nil {
			break
		}
	}
	if err != nil {
		// 3) Maybe javaRef is a path to a working tree.
		if info, statErr := os.Stat(javaRef); statErr == nil && info.IsDir() {
			cmd := exec.Command("git", "-C", javaRef, "ls-tree", "-r", "--name-only", "HEAD")
			out, err = cmd.Output()
		}
		if err != nil {
			return nil, fmt.Errorf("git ls-tree failed for %s: %w", javaRef, err)
		}
	}

	var files []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if strings.HasSuffix(line, ".java") {
			files = append(files, line)
		}
	}
	return files, nil
}

// findGoFiles walks the Go source tree (locally) and returns
// paths to .go files. We exclude test files and vendored
// dependencies.
func findGoFiles(root string) ([]string, error) {
	var files []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			switch info.Name() {
			case "vendor", ".git", "node_modules", "bin":
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(path, "_test.go") {
			return nil
		}
		if strings.HasSuffix(path, ".go") {
			// Skip the very-thin cmd entrypoints to avoid noise;
			// the verifier focuses on internal package behavior.
			if strings.HasPrefix(path, "cmd/") {
				return nil
			}
			files = append(files, path)
		}
		return nil
	})
	return files, err
}

// readJavaFile uses `git show` to read a file from the Java ref.
// The javaRef is either a refspec (run from cwd) or a path to a
// working tree (run from there).
func readJavaFile(javaRef, path string) (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	cmd := exec.Command("git", "show", fmt.Sprintf("%s:%s", javaRef, path))
	cmd.Dir = cwd
	out, err := cmd.Output()
	if err != nil {
		// Maybe javaRef is a path; try from there.
		if info, statErr := os.Stat(javaRef); statErr == nil && info.IsDir() {
			cmd = exec.Command("git", "-C", javaRef, "show", fmt.Sprintf("HEAD:%s", path))
			out, err = cmd.Output()
		}
		if err != nil {
			return "", err
		}
	}
	return string(out), nil
}

// matchInFile scans a body for the first regex match.
func matchInFile(re *regexp.Regexp, body string) (string, bool) {
	if re == nil {
		return "", false
	}
	m := re.FindStringSubmatch(body)
	if m == nil {
		return "", false
	}
	if len(m) > 1 {
		return m[1], true
	}
	return m[0], true
}

// joinAllJavaBodies reads every Java file in the Java ref and
// concatenates them with file-path headers. This is used for
// project-wide regex checks (e.g., task queue default) where the
// setting may live in any of several files.
func joinAllJavaBodies(javaRef string, paths []string) string {
	var sb strings.Builder
	for _, p := range paths {
		body, err := readJavaFile(javaRef, p)
		if err != nil {
			continue
		}
		sb.WriteString("--- " + p + " ---\n")
		sb.WriteString(body)
		sb.WriteString("\n")
	}
	return sb.String()
}

// joinAllGoBodies reads every Go file from disk and concatenates
// them with file-path headers.
func joinAllGoBodies(paths []string) string {
	var sb strings.Builder
	for _, p := range paths {
		body, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		sb.WriteString("--- " + p + " ---\n")
		sb.Write(body)
		sb.WriteString("\n")
	}
	return sb.String()
}

// checkBehavior compares the Java and Go files for one behavior.
// If only one side has the pattern, or the patterns disagree, a
// Gap is returned.
func checkBehavior(check behaviorCheck, javaBody, goBody, javaFile, goFile string) []Gap {
	javaHit, javaOK := matchInFile(check.JavaRe, javaBody)
	goHit, goOK := matchInFile(check.GoRe, goBody)

	switch {
	case javaOK && !goOK:
		// Java has the behavior but Go doesn't. Gap.
		return []Gap{{
			ID:            fmt.Sprintf("java-vs-go-%s-%s", check.Name, filepath.Base(goFile)),
			Category:      check.Name,
			Severity:      "critical",
			Description:   fmt.Sprintf(check.DescFmt, javaHit, "(not found)"),
			JavaEvidence:  fmt.Sprintf("%s:%s", javaFile, javaHit),
			GoEvidence:    fmt.Sprintf("%s:(no match)", goFile),
			TargetFiles:   []string{goFile},
			SuggestedBead: fmt.Sprintf("Add Java's %s behavior to %s (Java has: %s)", check.Name, goFile, javaHit),
		}}
	case !javaOK && goOK:
		// Go has the behavior but Java doesn't. Informational;
		// Java may be older. Surface as info so operators see it.
		return []Gap{{
			ID:            fmt.Sprintf("java-vs-go-%s-%s", check.Name, filepath.Base(goFile)),
			Category:      check.Name,
			Severity:      "info",
			Description:   fmt.Sprintf(check.DescFmt, "(not found)", goHit),
			JavaEvidence:  fmt.Sprintf("%s:(no match)", javaFile),
			GoEvidence:    fmt.Sprintf("%s:%s", goFile, goHit),
			TargetFiles:   []string{goFile},
			SuggestedBead: fmt.Sprintf("Verify %s is intentional in Go (%s)", check.Name, goHit),
		}}
	case javaOK && goOK && javaHit != goHit:
		// Both have the behavior but they disagree. Critical gap.
		return []Gap{{
			ID:            fmt.Sprintf("java-vs-go-%s-%s", check.Name, filepath.Base(goFile)),
			Category:      check.Name,
			Severity:      "critical",
			Description:   fmt.Sprintf(check.DescFmt, javaHit, goHit),
			JavaEvidence:  fmt.Sprintf("%s:%s", javaFile, javaHit),
			GoEvidence:    fmt.Sprintf("%s:%s", goFile, goHit),
			TargetFiles:   []string{goFile},
			SuggestedBead: fmt.Sprintf("Reconcile %s: Java=%s, Go=%s", check.Name, javaHit, goHit),
		}}
	}
	return nil
}

// pairJavaToGo finds Go files whose basename matches the Java
// file's basename (e.g., DocumentExtractionActivitiesImpl.java
// matches activities.go). Returns the Go file path or empty if
// no match. The mapping is heuristic; for black-box migrations
// the Go port usually preserves the class basename in lowercase
// snake_case form.
//
// We also consider the Java file's parent directory: a
// `controller/BulkOrderController.java` may have been ported to
// `internal/handler/bulk_order.go` (handler/ instead of
// controller/). When the directory names align in obvious ways
// (controller→handler, service→service, dto→dto) we use them as
// a fallback.
func pairJavaToGo(javaFile string, goFiles []string) (string, bool) {
	base := strings.TrimSuffix(filepath.Base(javaFile), ".java")
	// Strip common Java suffixes that don't map to Go file basenames.
	base = strings.TrimSuffix(base, "Impl")
	base = strings.TrimSuffix(base, "Controller")
	base = strings.TrimSuffix(base, "Service")
	base = strings.TrimSuffix(base, "Activities")
	lower := strings.ToLower(base)
	snake := camelToSnake(base)

	// Directory mapping: many migrations rename the package
	// (controller/ → handler/, dto/ → domain/, etc.). When the
	// Java file is under one of these, accept any Go file
	// under the corresponding Go directory.
	javaDir := filepath.Dir(javaFile)
	dirAliases := map[string][]string{
		"controller": {"handler", "controllers"},
		"dto":        {"domain", "dto"},
		"service":    {"service", "services"},
		"activity":   {"extraction", "activities"},
		"workflow":   {"extraction", "workflows"},
	}
	lastDir := filepath.Base(javaDir)

	for _, goFile := range goFiles {
		gb := strings.ToLower(strings.TrimSuffix(filepath.Base(goFile), ".go"))
		gDir := filepath.Dir(goFile)

		// Exact basename match (camelCase → snake_case).
		if gb == snake+".go" {
			return goFile, true
		}
		// Substring match (e.g., "DocumentExtractionActivitiesImpl"
		// contains "activities").
		if strings.Contains(gb, snake) {
			return goFile, true
		}
		// Last-ditch case-insensitive contains.
		if strings.Contains(gb, lower) {
			return goFile, true
		}
		// Directory alias match: when the Java file is under
		// controller/ and the Go file is under handler/ (or any
		// alias), consider basename match without the directory
		// alignment.
		for fromDir, toDirs := range dirAliases {
			if lastDir == fromDir {
				for _, toDir := range toDirs {
					if strings.Contains(gDir, toDir) {
						if gb == snake+".go" || strings.Contains(gb, snake) {
							return goFile, true
						}
					}
				}
			}
		}
	}
	return "", false
}

// camelToSnake converts a CamelCase identifier to snake_case.
// E.g., "DocumentExtractionWorkflow" -> "document_extraction_workflow".
func camelToSnake(s string) string {
	var out []rune
	for i, r := range s {
		if i > 0 && r >= 'A' && r <= 'Z' {
			out = append(out, '_')
		}
		if r >= 'A' && r <= 'Z' {
			out = append(out, rune(r+('a'-'A')))
		} else {
			out = append(out, r)
		}
	}
	return string(out)
}

func main() {
	cfg := parseFlags()

	javaFiles, err := findJavaFiles(cfg.JavaRef)
	if err != nil {
		fmt.Fprintf(os.Stderr, "findJavaFiles: %v\n", err)
		os.Exit(2)
	}
	goFiles, err := findGoFiles(cfg.GoSource)
	if err != nil {
		fmt.Fprintf(os.Stderr, "findGoFiles: %v\n", err)
		os.Exit(2)
	}

	var gaps []Gap
	categoriesChecked := map[string]bool{}

	for _, jf := range javaFiles {
		jBody, err := readJavaFile(cfg.JavaRef, jf)
		if err != nil {
			// File deleted on Java ref, skip.
			continue
		}
		gf, ok := pairJavaToGo(jf, goFiles)
		if !ok {
			continue // no Go counterpart; skip
		}
		gBody, err := os.ReadFile(gf)
		if err != nil {
			continue
		}
		for _, check := range behaviorChecks {
			categoriesChecked[check.Name] = true
			gaps = append(gaps, checkBehavior(check, jBody, string(gBody), jf, gf)...)
		}
	}

	// Phase 2: project-wide checks. Some behaviors (task queue
	// default, workflow ID prefix) are configured at the
	// application bootstrap level — they may live in
	// main.go / config.go rather than the per-class
	// counterpart. Run the same regexes against the whole
	// Go project and report any mismatch with the whole
	// Java project.
	jRoot := joinAllJavaBodies(cfg.JavaRef, javaFiles)
	gRoot := joinAllGoBodies(goFiles)
	for _, check := range behaviorChecks {
		categoriesChecked[check.Name] = true
		// Skip checks that already found a paired-file match
		// (those gaps are already reported). Run the remaining
		// checks at project scope.
		jHits := check.JavaRe.FindAllStringSubmatch(jRoot, -1)
		gHits := check.GoRe.FindAllStringSubmatch(gRoot, -1)
		// Extract capture-group values (or full match if no group).
		jVals := make([]string, 0, len(jHits))
		for _, m := range jHits {
			if len(m) > 1 {
				jVals = append(jVals, m[1])
			} else {
				jVals = append(jVals, m[0])
			}
		}
		gVals := make([]string, 0, len(gHits))
		for _, m := range gHits {
			if len(m) > 1 {
				gVals = append(gVals, m[1])
			} else {
				gVals = append(gVals, m[0])
			}
		}
		// If Java has multiple values, report each as a separate gap
		// (Java internal inconsistency is a gap too).
		jOK := len(jVals) > 0
		gOK := len(gVals) > 0
		// For comparison: report each Java value that disagrees with
		// the FIRST Go value.
		if jOK {
			for _, jv := range jVals {
				if !gOK || jv != gVals[0] {
					// Filter out: only report if not already reported
					// by the per-file scan.
					alreadyReported := false
					for _, g := range gaps {
						if g.Category == check.Name {
							alreadyReported = true
							break
						}
					}
					if !alreadyReported {
						gv := "(not found in Go project)"
						if gOK {
							gv = gVals[0]
						}
						severity := "critical"
						if !gOK {
							severity = "critical"
						} else {
							severity = "warning" // both have value, just different
						}
						gaps = append(gaps, Gap{
							ID:            fmt.Sprintf("java-vs-go-%s-project", check.Name),
							Category:      check.Name,
							Severity:      severity,
							Description:   fmt.Sprintf(check.DescFmt, jv, gv),
							JavaEvidence:  fmt.Sprintf("(project-wide Java match: %s)", jv),
							GoEvidence:    fmt.Sprintf("(project-wide Go match: %s)", gv),
							SuggestedBead: fmt.Sprintf("Reconcile %s: Java=%s, Go=%s", check.Name, jv, gv),
						})
						break // only report first non-matching Java value to avoid spam
					}
				}
			}
		}
	}

	report := GapReport{
		Passed:  len(gaps) == 0,
		JavaRef: cfg.JavaRef,
		GoSource: cfg.GoSource,
		PRDPath: cfg.PRDPath,
	}
	for c := range categoriesChecked {
		report.Categories = append(report.Categories, c)
	}
	report.Gaps = gaps
	if len(gaps) == 0 {
		report.Summary = "Java and Go match on all checked behaviors."
	} else {
		critical := 0
		for _, g := range gaps {
			if g.Severity == "critical" {
				critical++
			}
		}
		report.Summary = fmt.Sprintf("%d gap(s) found, %d critical.", len(gaps), critical)
	}

	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "marshal: %v\n", err)
		os.Exit(2)
	}
	if cfg.Output != "" {
		if err := os.WriteFile(cfg.Output, data, 0644); err != nil {
			fmt.Fprintf(os.Stderr, "write %s: %v\n", cfg.Output, err)
			os.Exit(2)
		}
		fmt.Fprintf(os.Stderr, "Wrote %d gap(s) to %s\n", len(gaps), cfg.Output)
	} else {
		fmt.Println(string(data))
	}

	if len(gaps) == 0 {
		os.Exit(0)
	}
	os.Exit(1)
}

func parseFlags() Config {
	cfg := Config{}
	flag.StringVar(&cfg.JavaRef, "java-ref", "origin/development", "git ref containing the Java reference (e.g. origin/development)")
	flag.StringVar(&cfg.GoSource, "go-source", ".", "path to the Go source root")
	flag.StringVar(&cfg.PRDPath, "prd", "", "optional: path to PRD for cross-reference")
	flag.StringVar(&cfg.Output, "output", "", "optional: write report to file")
	flag.Parse()
	if cfg.JavaRef == "" {
		fmt.Fprintln(os.Stderr, "--java-ref is required")
		os.Exit(2)
	}
	return cfg
}