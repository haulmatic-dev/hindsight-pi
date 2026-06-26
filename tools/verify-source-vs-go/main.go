// verify-source-vs-go compares reference source code (Java, Python,
// TypeScript, Ruby, etc.) against the current Go implementation and
// emits a structured gap report covering behavioural differences.
//
// This tool exists because the legacy compliance check (verify-exports,
// verify-routes, verify-us-ac, detect-placeholders, detect-phantom-files)
// is all *intra-Go* — it checks the Go code against the PRD. It cannot
// detect when the Go code is "different from the source in a way that
// matters" because it doesn't read the source.
//
// The 2026-06 srv-file-handler audit found 19 gaps where PRD ↔ Go
// differed, source ↔ Go differed, or Go silently diverged from the
// source (e.g., `http://` webhook URL instead of source's `https://`,
// default task queue `document-extraction-queue` instead of source's
// `document-ingestion-queue`, etc.). This tool closes that half of the
// comparison for **any source language** — not just Java.
//
// Usage:
//
//	verify-source-vs-go \
//	  --source-language java \
//	  --source-ref origin/development \
//	  --go-source . \
//	  --output gap-report.json
//
// Exit codes:
//
//	0 - zero gaps (source and Go match on every checked behaviour)
//	1 - gaps found (see output JSON)
//	2 - configuration error
//
// Language bundles:
//
//	java       - task queue, webhook scheme, retry attempts, error strings,
//	             workflow ID prefix (matches Spring @Value, Temporal
//	             RetryOptions, etc.)
//	python     - Celery queue name, Django URL patterns, retry decorator,
//	             exception messages, request ID format
//	typescript - BullMQ queue, Express error middleware, retry config,
//	             class-validator error strings, request ID
//	ruby       - Sidekiq queue, Rails routes, ActiveJob retry config,
//	             error messages, job ID format
//	auto       - detect from --source-ref file extensions (.java/.py/.ts/.rb)
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
	Passed     bool     `json:"passed"`
	SourceLang string   `json:"source_language"`
	SourceRef  string   `json:"source_ref"`
	GoSource   string   `json:"go_source"`
	PRDPath    string   `json:"prd_path"`
	Categories []string `json:"categories_checked"`
	Gaps       []Gap    `json:"gaps"`
	Summary    string   `json:"summary"`
}

// Gap describes a single source-vs-Go mismatch.
type Gap struct {
	ID            string   `json:"id"`
	Category      string   `json:"category"`
	Severity      string   `json:"severity"`
	Description   string   `json:"description"`
	SourceEvidence string  `json:"source_evidence"`
	GoEvidence    string   `json:"go_evidence"`
	PRDRef        string   `json:"prd_ref,omitempty"`
	TargetFiles   []string `json:"target_files,omitempty"`
	SuggestedBead string   `json:"suggested_bead,omitempty"`
}

type Config struct {
	SourceLang string
	SourceRef  string
	GoSource   string
	PRDPath    string
	Output     string
}

// behaviorCheck describes one specific source-vs-Go behavior we
// want to verify. SourceRe matches the source language's
// idiomatic patterns (e.g. Java @Value annotation); GoRe matches
// the Go counterpart. Both must match for the behavior to be
// considered consistent.
type behaviorCheck struct {
	Name     string
	SourceRe *regexp.Regexp
	GoRe     *regexp.Regexp
	DescFmt  string // format string with %s for source-evidence, %s for go-evidence
}

// languageBundle groups checks for one source language.
type languageBundle struct {
	Name   string
	Files  []string // file extensions to match (e.g. ".java", ".py", ".ts")
	Checks []behaviorCheck
}

// languageBundles is the registry of per-language behavior
// bundles. Adding a new language is a deliberate code change:
// extend this map with the source-language regexes.
var languageBundles = map[string]languageBundle{
	"java": {
		Name:  "java",
		Files: []string{".java"},
		Checks: []behaviorCheck{
			{
				Name:    "task-queue-default",
				SourceRe: regexp.MustCompile(`temporal\.task-queue[:\}]+\s*([a-z][a-z0-9-]+)`),
				GoRe:     regexp.MustCompile(`defaultTaskQueue[^=]*=\s*"([a-z][a-z0-9-]+)"`),
				DescFmt:  "Default Temporal task queue mismatch: Java uses '%s', Go uses '%s'. PRD does not pin a default, so the worker and the workflow starter route to different queues across migration cutover.",
			},
			{
				Name:    "webhook-scheme",
				SourceRe: regexp.MustCompile(`(https?)://srv-invoice`),
				GoRe:     regexp.MustCompile(`(https?)://srv-invoice`),
				DescFmt:  "Webhook URL scheme differs. Java: '%s'; Go: '%s'. Production requires TLS; the consumer rejects plain http.",
			},
			{
				Name:    "error-message-quoted",
				SourceRe: regexp.MustCompile(`throw new RuntimeException\(\"([^\"]+)\"\)`),
				GoRe:     regexp.MustCompile(`(?:errors\.New|fmt\.Errorf)\(\"([^\"]+)\"`),
				DescFmt:  "Error message string mismatch detected between Java throw and Go error constructor. PRD ACs require exact literal strings; drift means the consumer's regex matchers stop working. Java: '%s'; Go: '%s'.",
			},
			{
				Name:    "retry-attempts",
				SourceRe: regexp.MustCompile(`setMaximumAttempts\((\d+)\)`),
				GoRe:     regexp.MustCompile(`MaximumAttempts:\s*(\d+)`),
				DescFmt:  "Activity retry attempts mismatch. Java: %s; Go: %s. Workflow retry budget differs across the cutover.",
			},
			{
				Name:    "workflow-id-prefix",
				SourceRe: regexp.MustCompile(`doc-ingestion-([a-zA-Z0-9_-]+)`),
				GoRe:     regexp.MustCompile(`doc-ingestion-([a-zA-Z0-9_-]+)`),
				DescFmt:  "Workflow ID prefix/suffix mismatch. Java: 'doc-ingestion-%s'; Go: 'doc-ingestion-%s'. Cross-cutover workflows started by Java listener won't be observable to Go.",
			},
		},
	},
	"python": {
		Name:  "python",
		Files: []string{".py"},
		Checks: []behaviorCheck{
			{
				Name:    "celery-queue",
				SourceRe: regexp.MustCompile(`task_default_queue\s*=\s*['"]([a-z][a-z0-9_-]+)['"]`),
				GoRe:     regexp.MustCompile(`DefaultQueue\s*=\s*"([a-z][a-z0-9_-]+)"`),
				DescFmt:  "Default Celery task queue mismatch: Python uses '%s', Go uses '%s'. Cross-cutover workers route to different queues.",
			},
			{
				Name:    "webhook-scheme",
				SourceRe: regexp.MustCompile(`(https?)://[a-z0-9.-]+/webhook`),
				GoRe:     regexp.MustCompile(`(https?)://[a-z0-9.-]+/webhook`),
				DescFmt:  "Webhook URL scheme differs. Python: '%s'; Go: '%s'. Production requires TLS.",
			},
			{
				Name:    "error-message-quoted",
				SourceRe: regexp.MustCompile(`(?:raise\s+\w+\(['"]|Exception\(['"])([^'"]+)['"]\)`),
				GoRe:     regexp.MustCompile(`(?:errors\.New|fmt\.Errorf)\(\"([^\"]+)\"`),
				DescFmt:  "Error message string mismatch between Python raise and Go error constructor. Python: '%s'; Go: '%s'.",
			},
			{
				Name:    "retry-attempts",
				SourceRe: regexp.MustCompile(`autoretry_for=\(\s*[^,]+,\s*max_retries=(\d+)`),
				GoRe:     regexp.MustCompile(`MaxRetries:\s*(\d+)`),
				DescFmt:  "Task retry count mismatch. Python: %s; Go: %s.",
			},
			{
				Name:    "task-id-format",
				SourceRe: regexp.MustCompile(`task_id\s*=\s*f?['"]doc-ingestion-([a-zA-Z0-9_-]+)['"]`),
				GoRe:     regexp.MustCompile(`doc-ingestion-([a-zA-Z0-9_-]+)`),
				DescFmt:  "Task/job ID format mismatch. Python: 'doc-ingestion-%s'; Go: 'doc-ingestion-%s'.",
			},
		},
	},
	"typescript": {
		Name:  "typescript",
		Files: []string{".ts", ".tsx", ".js", ".mjs"},
		Checks: []behaviorCheck{
			{
				Name:    "bullmq-queue",
				SourceRe: regexp.MustCompile(`defaultJobOptions\s*[:=]\s*\{\s*[\s\S]*?queueName\s*:\s*['"]([a-z][a-z0-9-]+)['"]`),
				GoRe:     regexp.MustCompile(`DefaultQueue\s*=\s*"([a-z][a-z0-9_-]+)"`),
				DescFmt:  "Default BullMQ queue mismatch: TypeScript uses '%s', Go uses '%s'.",
			},
			{
				Name:    "webhook-scheme",
				SourceRe: regexp.MustCompile(`(https?)://[a-z0-9.-]+/webhook`),
				GoRe:     regexp.MustCompile(`(https?)://[a-z0-9.-]+/webhook`),
				DescFmt:  "Webhook URL scheme differs. TypeScript: '%s'; Go: '%s'. Production requires TLS.",
			},
			{
				Name:    "error-message-quoted",
				SourceRe: regexp.MustCompile(`throw new Error\(['"]([^'"]+)['"]\)`),
				GoRe:     regexp.MustCompile(`(?:errors\.New|fmt\.Errorf)\(\"([^\"]+)\"`),
				DescFmt:  "Error message string mismatch between TypeScript throw and Go error constructor. TypeScript: '%s'; Go: '%s'.",
			},
			{
				Name:    "retry-attempts",
				SourceRe: regexp.MustCompile(`attempts:\s*(\d+)`),
				GoRe:     regexp.MustCompile(`MaxAttempts:\s*(\d+)`),
				DescFmt:  "Job retry count mismatch. TypeScript: %s; Go: %s.",
			},
		},
	},
	"ruby": {
		Name:  "ruby",
		Files: []string{".rb"},
		Checks: []behaviorCheck{
			{
				Name:    "sidekiq-queue",
				SourceRe: regexp.MustCompile(`sidekiq_options\s+queue:\s*([a-z][a-z0-9_]+)`),
				GoRe:     regexp.MustCompile(`DefaultQueue\s*=\s*"([a-z][a-z0-9_-]+)"`),
				DescFmt:  "Default Sidekiq queue mismatch: Ruby uses '%s', Go uses '%s'.",
			},
			{
				Name:    "webhook-scheme",
				SourceRe: regexp.MustCompile(`(https?)://[a-z0-9.-]+/webhook`),
				GoRe:     regexp.MustCompile(`(https?)://[a-z0-9.-]+/webhook`),
				DescFmt:  "Webhook URL scheme differs. Ruby: '%s'; Go: '%s'. Production requires TLS.",
			},
			{
				Name:    "error-message-quoted",
				SourceRe: regexp.MustCompile(`raise\s+['"]([^'"]+)['"]`),
				GoRe:     regexp.MustCompile(`(?:errors\.New|fmt\.Errorf)\(\"([^\"]+)\"`),
				DescFmt:  "Error message string mismatch between Ruby raise and Go error constructor. Ruby: '%s'; Go: '%s'.",
			},
		},
	},
}

// detectLanguage inspects the source-ref tree and returns the
// language key matching the dominant file extension. Defaults to
// "java" when no clear winner is detected (the legacy tool's
// default; preserved for backward compat).
func detectLanguage(sourceRef string) string {
	cmd := exec.Command("git", "ls-tree", "-r", "--name-only", sourceRef)
	out, err := cmd.Output()
	if err != nil {
		return "java"
	}
	counts := map[string]int{}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		ext := filepath.Ext(line)
		if ext == "" {
			continue
		}
		for _, bundle := range languageBundles {
			for _, bundleExt := range bundle.Files {
				if ext == bundleExt {
					counts[bundle.Name]++
				}
			}
		}
	}
	maxCount := 0
	maxLang := "java"
	for lang, count := range counts {
		if count > maxCount {
			maxCount = count
			maxLang = lang
		}
	}
	return maxLang
}

// findSourceFiles walks the source ref tree (via `git show`) and
// returns the paths to files with extensions matching the
// language bundle. The sourceRef is interpreted as a git refspec
// (run from cwd) or a path to a working tree.
func findSourceFiles(sourceRef, sourceLang string) ([]string, error) {
	bundle, ok := languageBundles[sourceLang]
	if !ok {
		return nil, fmt.Errorf("unsupported source language %q (supported: java, python, typescript, ruby)", sourceLang)
	}
	extSet := make(map[string]bool)
	for _, ext := range bundle.Files {
		extSet[ext] = true
	}

	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}

	var out []byte
	// Try several strategies; the cwd-relative ls-tree usually works.
	tryLs := func(args ...string) error {
		cmd := exec.Command("git", args...)
		cmd.Dir = cwd
		outb, err := cmd.Output()
		if err == nil {
			out = outb
			return nil
		}
		return err
	}

	if err := tryLs("ls-tree", "-r", "--name-only", sourceRef); err != nil {
		if err := tryLs("ls-tree", "-r", "--name-only", "HEAD:src/main/java"); err != nil {
			if info, statErr := os.Stat(sourceRef); statErr == nil && info.IsDir() {
				cmd := exec.Command("git", "-C", sourceRef, "ls-tree", "-r", "--name-only", "HEAD")
				outb, err := cmd.Output()
				if err != nil {
					return nil, fmt.Errorf("git ls-tree failed for %s: %w", sourceRef, err)
				}
				out = outb
			} else {
				return nil, fmt.Errorf("git ls-tree failed for %s: %w", sourceRef, err)
			}
		}
	}

	var files []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		ext := filepath.Ext(line)
		if extSet[ext] {
			files = append(files, line)
		}
	}
	return files, nil
}

// readSourceFile uses `git show` to read a file from the source ref.
func readSourceFile(sourceRef, path string) (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	cmd := exec.Command("git", "show", fmt.Sprintf("%s:%s", sourceRef, path))
	cmd.Dir = cwd
	out, err := cmd.Output()
	if err != nil {
		if info, statErr := os.Stat(sourceRef); statErr == nil && info.IsDir() {
			cmd = exec.Command("git", "-C", sourceRef, "show", fmt.Sprintf("HEAD:%s", path))
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

// checkBehavior compares the source and Go files for one behavior.
func checkBehavior(check behaviorCheck, sourceBody, goBody, sourceFile, goFile string) []Gap {
	sourceHit, sourceOK := matchInFile(check.SourceRe, sourceBody)
	goHit, goOK := matchInFile(check.GoRe, goBody)

	switch {
	case sourceOK && !goOK:
		return []Gap{{
			ID:             fmt.Sprintf("source-vs-go-%s-%s", check.Name, filepath.Base(goFile)),
			Category:       check.Name,
			Severity:       "critical",
			Description:    fmt.Sprintf(check.DescFmt, sourceHit, "(not found)"),
			SourceEvidence: fmt.Sprintf("%s:%s", sourceFile, sourceHit),
			GoEvidence:     fmt.Sprintf("%s:(no match)", goFile),
			TargetFiles:    []string{goFile},
			SuggestedBead:  fmt.Sprintf("Add source's %s behavior to %s (source has: %s)", check.Name, goFile, sourceHit),
		}}
	case !sourceOK && goOK:
		return []Gap{{
			ID:             fmt.Sprintf("source-vs-go-%s-%s", check.Name, filepath.Base(goFile)),
			Category:       check.Name,
			Severity:       "info",
			Description:    fmt.Sprintf(check.DescFmt, "(not found)", goHit),
			SourceEvidence: fmt.Sprintf("%s:(no match)", sourceFile),
			GoEvidence:     fmt.Sprintf("%s:%s", goFile, goHit),
			TargetFiles:    []string{goFile},
			SuggestedBead:  fmt.Sprintf("Verify %s is intentional in Go (%s)", check.Name, goHit),
		}}
	case sourceOK && goOK && sourceHit != goHit:
		return []Gap{{
			ID:             fmt.Sprintf("source-vs-go-%s-%s", check.Name, filepath.Base(goFile)),
			Category:       check.Name,
			Severity:       "critical",
			Description:    fmt.Sprintf(check.DescFmt, sourceHit, goHit),
			SourceEvidence: fmt.Sprintf("%s:%s", sourceFile, sourceHit),
			GoEvidence:     fmt.Sprintf("%s:%s", goFile, goHit),
			TargetFiles:    []string{goFile},
			SuggestedBead:  fmt.Sprintf("Reconcile %s: source=%s, Go=%s", check.Name, sourceHit, goHit),
		}}
	}
	return nil
}

// pairSourceToGo finds Go files whose basename matches the source
// file's basename (e.g., DocumentExtractionActivitiesImpl.java
// matches activities.go). Returns the Go file path or empty if
// no match. The mapping is heuristic; for black-box migrations
// the Go port usually preserves the class basename in lowercase
// snake_case form.
func pairSourceToGo(sourceFile string, goFiles []string) (string, bool) {
	base := strings.TrimSuffix(filepath.Base(sourceFile), filepath.Ext(sourceFile))
	// Strip common Java suffixes that don't map to Go file basenames.
	for _, suffix := range []string{"Impl", "Controller", "Service", "Activities"} {
		base = strings.TrimSuffix(base, suffix)
	}
	lower := strings.ToLower(base)
	snake := camelToSnake(base)

	// Directory mapping: many migrations rename the package
	// (controller/ → handler/, dto/ → domain/, etc.). When the
	// source file is under one of these, accept any Go file
	// under the corresponding Go directory.
	sourceDir := filepath.Dir(sourceFile)
	dirAliases := map[string][]string{
		"controller":  {"handler", "controllers"},
		"dto":         {"domain", "dto"},
		"service":     {"service", "services"},
		"activity":    {"extraction", "activities"},
		"workflow":    {"extraction", "workflows"},
		"api":         {"handler", "api"},
		"tasks":       {"worker", "tasks"},
		"jobs":        {"worker", "jobs"},
		"webhooks":    {"webhook"},
	}
	lastDir := filepath.Base(sourceDir)

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
		// Directory alias match.
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

// findGoFiles walks the Go source tree (locally) and returns
// paths to .go files. Excludes tests and vendored deps.
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
			if strings.HasPrefix(path, "cmd/") {
				return nil
			}
			files = append(files, path)
		}
		return nil
	})
	return files, err
}

// joinAllSourceBodies reads every source file in the source ref and
// concatenates them with file-path headers. Used for project-wide
// regex checks (e.g., task queue default) where the setting may
// live in any of several files.
func joinAllSourceBodies(sourceRef string, paths []string) string {
	var sb strings.Builder
	for _, p := range paths {
		body, err := readSourceFile(sourceRef, p)
		if err != nil {
			continue
		}
		sb.WriteString("--- " + p + " ---\n")
		sb.WriteString(body)
		sb.WriteString("\n")
	}
	return sb.String()
}

// joinAllGoBodies reads every Go file from disk and concatenates them.
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

func main() {
	cfg := parseFlags()

	// Auto-detect language if not specified.
	if cfg.SourceLang == "" || cfg.SourceLang == "auto" {
		cfg.SourceLang = detectLanguage(cfg.SourceRef)
		fmt.Fprintf(os.Stderr, "Auto-detected source language: %s\n", cfg.SourceLang)
	}

	bundle, ok := languageBundles[cfg.SourceLang]
	if !ok {
		fmt.Fprintf(os.Stderr, "Unsupported source language %q. Supported: java, python, typescript, ruby, auto.\n", cfg.SourceLang)
		os.Exit(2)
	}

	sourceFiles, err := findSourceFiles(cfg.SourceRef, cfg.SourceLang)
	if err != nil {
		fmt.Fprintf(os.Stderr, "findSourceFiles: %v\n", err)
		os.Exit(2)
	}
	goFiles, err := findGoFiles(cfg.GoSource)
	if err != nil {
		fmt.Fprintf(os.Stderr, "findGoFiles: %v\n", err)
		os.Exit(2)
	}

	var gaps []Gap
	categoriesChecked := map[string]bool{}

	// Phase 1: per-file pairing (each source file → its Go counterpart).
	for _, sf := range sourceFiles {
		sBody, err := readSourceFile(cfg.SourceRef, sf)
		if err != nil {
			continue
		}
		gf, ok := pairSourceToGo(sf, goFiles)
		if !ok {
			continue
		}
		gBody, err := os.ReadFile(gf)
		if err != nil {
			continue
		}
		for _, check := range bundle.Checks {
			categoriesChecked[check.Name] = true
			gaps = append(gaps, checkBehavior(check, sBody, string(gBody), sf, gf)...)
		}
	}

	// Phase 2: project-wide scan (catches config-level mismatches
	// in main.go / config.go that don't pair with a single source file).
	sRoot := joinAllSourceBodies(cfg.SourceRef, sourceFiles)
	gRoot := joinAllGoBodies(goFiles)
	for _, check := range bundle.Checks {
		categoriesChecked[check.Name] = true
		sHits := check.SourceRe.FindAllStringSubmatch(sRoot, -1)
		gHits := check.GoRe.FindAllStringSubmatch(gRoot, -1)

		sVals := make([]string, 0, len(sHits))
		for _, m := range sHits {
			if len(m) > 1 {
				sVals = append(sVals, m[1])
			} else {
				sVals = append(sVals, m[0])
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
		sOK := len(sVals) > 0
		gOK := len(gVals) > 0

		if sOK {
			for _, sv := range sVals {
				if !gOK || sv != gVals[0] {
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
							severity = "warning"
						}
						gaps = append(gaps, Gap{
							ID:             fmt.Sprintf("source-vs-go-%s-project", check.Name),
							Category:       check.Name,
							Severity:       severity,
							Description:    fmt.Sprintf(check.DescFmt, sv, gv),
							SourceEvidence: fmt.Sprintf("(project-wide source match: %s)", sv),
							GoEvidence:     fmt.Sprintf("(project-wide Go match: %s)", gv),
							SuggestedBead:  fmt.Sprintf("Reconcile %s: source=%s, Go=%s", check.Name, sv, gv),
						})
						break
					}
				}
			}
		}
	}

	report := GapReport{
		Passed:     len(gaps) == 0,
		SourceLang: cfg.SourceLang,
		SourceRef:  cfg.SourceRef,
		GoSource:   cfg.GoSource,
		PRDPath:    cfg.PRDPath,
	}
	for c := range categoriesChecked {
		report.Categories = append(report.Categories, c)
	}
	report.Gaps = gaps
	if len(gaps) == 0 {
		report.Summary = fmt.Sprintf("Source (%s) and Go match on all checked behaviors.", cfg.SourceLang)
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
	flag.StringVar(&cfg.SourceLang, "source-language", "auto", "source language (java, python, typescript, ruby, auto). 'auto' detects from --source-ref file extensions.")
	flag.StringVar(&cfg.SourceRef, "source-ref", "origin/development", "git ref containing the source reference (e.g. origin/development)")
	flag.StringVar(&cfg.GoSource, "go-source", ".", "path to the Go source root")
	flag.StringVar(&cfg.PRDPath, "prd", "", "optional: path to PRD for cross-reference")
	flag.StringVar(&cfg.Output, "output", "", "optional: write report to file")
	flag.Parse()
	if cfg.SourceRef == "" {
		fmt.Fprintln(os.Stderr, "--source-ref is required")
		os.Exit(2)
	}
	return cfg
}