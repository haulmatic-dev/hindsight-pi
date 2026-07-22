package core

import (
	"strings"
	"unicode"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// prefixType maps a leading uppercase title prefix to a conventional commit type.
// Prefixes in the bead title win over the bead Type field.
var prefixType = map[string]string{
	"GREEN":    "feat",
	"RED":      "test",
	"FIX":      "fix",
	"REFACTOR": "refactor",
	"DOCS":     "docs",
	"STYLE":    "style",
	"PERF":     "perf",
	"TEST":     "test",
	"BUILD":    "build",
	"CI":       "ci",
	"CHORE":    "chore",
	"REVERT":   "revert",
}

// beadType maps a bead type to a conventional commit type. Unmapped types fall
// back to "chore".
var beadType = map[string]string{
	"implement":     "feat",
	"fix":           "fix",
	"bug":           "fix",
	"refactor":      "refactor",
	"test":          "test",
	"test-validate": "test",
	"requirements":  "feat",
	"review":        "chore",
	"deploy":        "chore",
	"task":          "chore",
	"approval-gate": "chore",
}

// BuildCommitMessage composes a conventional commit message from a bead.
// The summary line is limited to 72 characters. The body contains the bead
// description, an optional PRD reference, and the bead ID footer.
func BuildCommitMessage(bead types.Bead) string {
	commitType, subject := extractCommitType(bead)
	subject = normalizeSubject(subject)
	subject = truncateSubject(commitType, subject)

	summary := commitType + ": " + subject

	var body []string
	if d := strings.TrimSpace(bead.Description); d != "" {
		body = append(body, d)
	}
	if prd, ok := bead.Metadata["prd_path"]; ok && prd != "" {
		body = append(body, "PRD: "+prd)
	}
	if bead.ID != "" {
		body = append(body, "Bead: "+bead.ID.String())
	}

	if len(body) == 0 {
		return summary
	}
	return summary + "\n\n" + strings.Join(body, "\n")
}

func extractCommitType(bead types.Bead) (string, string) {
	title := bead.Title
	if colonIdx := strings.Index(title, ":"); colonIdx > 0 {
		prefix := title[:colonIdx]
		if isUpperWord(prefix) {
			rest := strings.TrimSpace(title[colonIdx+1:])
			if t, ok := prefixType[strings.ToUpper(prefix)]; ok {
				return t, rest
			}
			return mapBeadType(bead.Type), rest
		}
	}
	return mapBeadType(bead.Type), title
}

func mapBeadType(t string) string {
	if c, ok := beadType[t]; ok {
		return c
	}
	return "chore"
}

func normalizeSubject(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return s
	}
	runes := []rune(s)
	runes[0] = unicode.ToLower(runes[0])
	s = string(runes)
	s = strings.TrimSuffix(s, ".")
	return strings.TrimSpace(s)
}

func truncateSubject(commitType, subject string) string {
	const maxLine = 72
	typeLen := len(commitType) + len(": ")
	maxSubject := maxLine - typeLen
	if maxSubject <= 0 {
		return subject
	}
	if len(subject) <= maxSubject {
		return subject
	}
	cut := subject[:maxSubject]
	if idx := strings.LastIndexByte(cut, ' '); idx > 0 {
		cut = cut[:idx]
	}
	return strings.TrimSpace(cut)
}

func isUpperWord(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !unicode.IsUpper(r) && !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}
