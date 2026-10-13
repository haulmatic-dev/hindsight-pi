// prd-lint enforces a mechanical subset of the create-prd compiler contract.
//
// Usage:
//
//	prd-lint <prd.md> [--json]
//
// Exit codes:
//
//	0 - clean
//	1 - lint violations found
//	2 - configuration error (missing/unreadable file, bad arguments)
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"regexp"
	"sort"
	"strings"
)

// Violation describes a single lint failure.
type Violation struct {
	Rule    string `json:"rule"`
	ID      string `json:"id"`
	Line    int    `json:"line"`
	Message string `json:"message"`
}

// LintResult is the machine-readable output of a lint run.
type LintResult struct {
	Passed     bool        `json:"passed"`
	Violations []Violation `json:"violations"`
}

var (
	headingRe = regexp.MustCompile(`^#{2,3}\s+((?:FR|US|NFR)[-A-Za-z0-9]*-?\d*)\b`)
	idRe      = regexp.MustCompile(`\b((?:FR|US|NFR)[-A-Za-z0-9]*-?\d*)\b`)
)

const usage = "Usage: prd-lint <prd.md> [--json]"

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	path, jsonOut, ok := parseArgs(args, stderr)
	if !ok {
		return 2
	}

	data, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintf(stderr, "Error reading %s: %v\n", path, err)
		return 2
	}

	result := lintContent(path, string(data))

	if jsonOut {
		out, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			fmt.Fprintf(stderr, "Error marshaling JSON: %v\n", err)
			return 2
		}
		fmt.Fprintln(stdout, string(out))
	} else {
		for _, v := range result.Violations {
			fmt.Fprintf(stdout, "%s:%d %s %s: %s\n", path, v.Line, v.Rule, v.ID, v.Message)
		}
	}

	if result.Passed {
		return 0
	}
	return 1
}

func parseArgs(args []string, stderr io.Writer) (path string, jsonOut bool, ok bool) {
	for _, a := range args {
		if a == "--json" {
			jsonOut = true
			continue
		}
		if path == "" {
			path = a
			continue
		}
		fmt.Fprintln(stderr, usage)
		return "", false, false
	}
	if path == "" {
		fmt.Fprintln(stderr, usage)
		return "", false, false
	}
	return path, jsonOut, true
}

type line struct {
	text string
	num  int
}

type reqBlock struct {
	id          string
	headingLine int
	lines       []line
}

func lintContent(name string, content string) LintResult {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	rawLines := strings.Split(content, "\n")
	activeLines := filterCodeBlocks(rawLines)
	blocks := parseBlocks(activeLines)

	violations := make([]Violation, 0)

	if len(blocks) == 0 {
		violations = append(violations, Violation{
			Rule:    "R0-empty",
			ID:      "",
			Line:    0,
			Message: "no requirement headings found",
		})
		return LintResult{Passed: false, Violations: violations}
	}

	violations = append(violations, checkUniqueIDs(blocks)...)

	allIDs := make(map[string]struct{}, len(blocks))
	for _, b := range blocks {
		allIDs[b.id] = struct{}{}
	}

	for _, b := range blocks {
		violations = append(violations, checkBlock(b)...)
	}

	violations = append(violations, checkReferences(blocks, allIDs)...)

	sortViolations(violations)

	return LintResult{
		Passed:     len(violations) == 0,
		Violations: violations,
	}
}

func filterCodeBlocks(rawLines []string) []line {
	var active []line
	inside := false
	for i, raw := range rawLines {
		trimmed := strings.TrimSpace(raw)
		if strings.HasPrefix(trimmed, "```") {
			inside = !inside
			continue
		}
		if !inside {
			active = append(active, line{text: raw, num: i + 1})
		}
	}
	return active
}

func parseBlocks(activeLines []line) []reqBlock {
	var blocks []reqBlock
	var current *reqBlock
	for _, l := range activeLines {
		m := headingRe.FindStringSubmatch(l.text)
		if m != nil {
			id := m[1]
			if current != nil {
				blocks = append(blocks, *current)
			}
			current = &reqBlock{
				id:          id,
				headingLine: l.num,
				lines:       []line{},
			}
			continue
		}
		if current != nil {
			current.lines = append(current.lines, l)
		}
	}
	if current != nil {
		blocks = append(blocks, *current)
	}
	return blocks
}

func checkUniqueIDs(blocks []reqBlock) []Violation {
	var violations []Violation
	seen := make(map[string]int)
	reported := make(map[string]bool)
	for _, b := range blocks {
		if first, ok := seen[b.id]; ok && !reported[b.id] {
			violations = append(violations, Violation{
				Rule:    "R5-unique-id",
				ID:      b.id,
				Line:    b.headingLine,
				Message: fmt.Sprintf("duplicate requirement ID %s (first at line %d)", b.id, first),
			})
			reported[b.id] = true
		} else {
			seen[b.id] = b.headingLine
		}
	}
	return violations
}

func checkBlock(b reqBlock) []Violation {
	var violations []Violation

	typ := blockType(b)
	if !isValidType(typ) {
		violations = append(violations, Violation{
			Rule:    "R1-type",
			ID:      b.id,
			Line:    b.headingLine,
			Message: fmt.Sprintf("invalid or missing Type: %q", typ),
		})
	}

	if !hasPriority(b.lines) {
		violations = append(violations, Violation{
			Rule:    "R2-priority",
			ID:      b.id,
			Line:    b.headingLine,
			Message: "missing Priority",
		})
	}

	if requiresTargetFiles(typ) {
		if v, ok := checkTargetFiles(b); ok {
			violations = append(violations, v)
		}
	}

	if !hasAcceptanceCriteria(b.lines) {
		violations = append(violations, Violation{
			Rule:    "R4-acceptance",
			ID:      b.id,
			Line:    b.headingLine,
			Message: "missing Acceptance Criteria with at least one bullet",
		})
	}

	if st, ok := systemType(b.lines); ok && !isValidSystemType(st) {
		violations = append(violations, Violation{
			Rule:    "R9-system-type",
			ID:      b.id,
			Line:    b.headingLine,
			Message: fmt.Sprintf("invalid System Type %q: must be one of library, cli, api, application, ops", st),
		})
	}

	if strings.HasPrefix(typ, "us-") && !hasAggregatesSection(b.lines) {
		violations = append(violations, Violation{
			Rule:    "R6-aggregates",
			ID:      b.id,
			Line:    b.headingLine,
			Message: "missing Aggregates section",
		})
	}

	if typ == "nfr-config" {
		if !hasCommand(b.lines, "Test Command") {
			violations = append(violations, Violation{
				Rule:    "R8-commands",
				ID:      b.id,
				Line:    b.headingLine,
				Message: "missing Test Command",
			})
		}
		if !hasCommand(b.lines, "Build Command") {
			violations = append(violations, Violation{
				Rule:    "R8-commands",
				ID:      b.id,
				Line:    b.headingLine,
				Message: "missing Build Command",
			})
		}
	}

	return violations
}

func isValidSystemType(v string) bool {
	switch v {
	case "library", "cli", "api", "application", "ops":
		return true
	}
	return false
}

// systemType extracts the optional **System Type:** metadata line value.
// The value may carry a parenthetical qualifier ("library (internal Go)"),
// so only the first whitespace-separated token is the enum value.
func systemType(lines []line) (string, bool) {
	re := regexp.MustCompile(`^\*\*System Type:\*\*\s*(\S+)`)
	for _, l := range lines {
		m := re.FindStringSubmatch(l.text)
		if m != nil {
			return strings.TrimSpace(m[1]), true
		}
	}
	return "", false
}

func blockType(b reqBlock) string {
	re := regexp.MustCompile(`^\*\*Type:\*\*\s*(.*)$`)
	for _, l := range b.lines {
		m := re.FindStringSubmatch(l.text)
		if m != nil {
			return strings.Trim(strings.TrimSpace(m[1]), "`")
		}
	}
	if strings.HasPrefix(b.id, "FR-") {
		return "fr"
	}
	if strings.HasPrefix(b.id, "US-") {
		return "us"
	}
	if strings.HasPrefix(b.id, "NFR-") {
		return "nfr"
	}
	return ""
}

func isValidType(typ string) bool {
	switch typ {
	case "fr", "us-light", "us-heavy", "us-integration", "nfr-verification", "nfr-config":
		return true
	}
	return false
}

func requiresTargetFiles(typ string) bool {
	switch typ {
	case "fr", "nfr-config", "us-light", "us-heavy":
		return true
	}
	return false
}

func hasPriority(lines []line) bool {
	re := regexp.MustCompile(`^\*\*Priority:\*\*\s*(?i)P[0-3]\b`)
	for _, l := range lines {
		if re.MatchString(l.text) {
			return true
		}
	}
	return false
}

func checkTargetFiles(b reqBlock) (Violation, bool) {
	for i, l := range b.lines {
		trimmed := strings.TrimSpace(l.text)
		if !strings.HasPrefix(trimmed, "**Target Files:**") {
			continue
		}
		rest := strings.TrimSpace(strings.TrimPrefix(trimmed, "**Target Files:**"))
		if rest != "" {
			return Violation{}, false
		}
		for j := i + 1; j < len(b.lines); j++ {
			next := strings.TrimSpace(b.lines[j].text)
			if next == "" {
				continue
			}
			if strings.HasPrefix(next, "- ") {
				return Violation{
					Rule:    "R3-target-files",
					ID:      b.id,
					Line:    l.num,
					Message: "Target Files must be same-line (bullet lists are not parsed)",
				}, true
			}
			break
		}
		return Violation{
			Rule:    "R3-target-files",
			ID:      b.id,
			Line:    b.headingLine,
			Message: "missing Target Files",
		}, true
	}
	return Violation{
		Rule:    "R3-target-files",
		ID:      b.id,
		Line:    b.headingLine,
		Message: "missing Target Files",
	}, true
}

func hasAcceptanceCriteria(lines []line) bool {
	inSection := false
	for _, l := range lines {
		if strings.HasPrefix(strings.TrimSpace(l.text), "**Acceptance Criteria:**") {
			inSection = true
			continue
		}
		if inSection && sectionLabel(l.text) != "" {
			inSection = false
			continue
		}
		if inSection && strings.HasPrefix(strings.TrimSpace(l.text), "- ") {
			return true
		}
	}
	return false
}

func hasAggregatesSection(lines []line) bool {
	for _, l := range lines {
		if strings.HasPrefix(strings.TrimSpace(l.text), "**Aggregates:**") {
			return true
		}
	}
	return false
}

func hasCommand(lines []line, label string) bool {
	prefix := "**" + label + ":**"
	re := regexp.MustCompile(`^\*\*` + regexp.QuoteMeta(label) + `:\*\*\s+\S`)
	for _, l := range lines {
		if re.MatchString(l.text) {
			return true
		}
		if strings.HasPrefix(l.text, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(l.text, prefix)) != ""
		}
	}
	return false
}

type reference struct {
	sourceID string
	targetID string
	line     int
}

func checkReferences(blocks []reqBlock, allIDs map[string]struct{}) []Violation {
	var violations []Violation
	refs := collectReferences(blocks)
	reported := make(map[string]bool)
	for _, ref := range refs {
		key := ref.sourceID + "->" + ref.targetID
		if _, ok := allIDs[ref.targetID]; !ok && !reported[key] {
			violations = append(violations, Violation{
				Rule:    "R7-refs",
				ID:      ref.sourceID,
				Line:    ref.line,
				Message: fmt.Sprintf("referenced requirement %s does not exist", ref.targetID),
			})
			reported[key] = true
		}
	}
	return violations
}

func collectReferences(blocks []reqBlock) []reference {
	sectionRe := regexp.MustCompile(`^\*\*(Depends On|Aggregates|Verifies|Reads From):\*\*`)
	var refs []reference
	for _, b := range blocks {
		currentSection := ""
		for _, l := range b.lines {
			if isHeadingLine(l.text) {
				currentSection = ""
				continue
			}
			m := sectionRe.FindStringSubmatch(l.text)
			if m != nil {
				currentSection = m[1]
				for _, id := range extractIDs(l.text) {
					refs = append(refs, reference{sourceID: b.id, targetID: id, line: l.num})
				}
				continue
			}
			if sectionLabel(l.text) != "" {
				currentSection = ""
				continue
			}
			if currentSection != "" && strings.HasPrefix(strings.TrimSpace(l.text), "- ") {
				for _, id := range extractIDs(l.text) {
					refs = append(refs, reference{sourceID: b.id, targetID: id, line: l.num})
				}
			}
		}
	}
	return refs
}

func isHeadingLine(text string) bool {
	trimmed := strings.TrimSpace(text)
	for i := 0; i < len(trimmed); i++ {
		if trimmed[i] != '#' {
			return i > 0 && i < len(trimmed) && trimmed[i] == ' '
		}
	}
	return false
}

func extractIDs(text string) []string {
	seen := make(map[string]bool)
	var ids []string
	for _, m := range idRe.FindAllStringSubmatch(text, -1) {
		id := m[1]
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	return ids
}

func sectionLabel(text string) string {
	re := regexp.MustCompile(`^\*\*([^*:]+):\*\*`)
	m := re.FindStringSubmatch(text)
	if m != nil {
		return m[1]
	}
	return ""
}

func sortViolations(vs []Violation) {
	sort.Slice(vs, func(i, j int) bool {
		if vs[i].Line != vs[j].Line {
			return vs[i].Line < vs[j].Line
		}
		if vs[i].Rule != vs[j].Rule {
			return vs[i].Rule < vs[j].Rule
		}
		if vs[i].ID != vs[j].ID {
			return vs[i].ID < vs[j].ID
		}
		return vs[i].Message < vs[j].Message
	})
}
