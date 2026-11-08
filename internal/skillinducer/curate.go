package skillinducer

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"
)

// skillIndexEntry is a flattened existing skill for dedup comparison.
type skillIndexEntry struct {
	Name        string
	Description string
}

// loadSkillIndex reads every <dir>/<name>/SKILL.md, extracting name and
// description from YAML frontmatter (falling back to the directory name).
// Missing dir returns an empty index, not an error.
func loadSkillIndex(dir string) ([]skillIndexEntry, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var idx []skillIndexEntry
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		skillPath := filepath.Join(dir, e.Name(), "SKILL.md")
		data, err := os.ReadFile(skillPath)
		if err != nil {
			continue // not a skill dir; harmless
		}
		name, desc := parseFrontmatter(string(data))
		if name == "" {
			name = e.Name()
		}
		idx = append(idx, skillIndexEntry{Name: name, Description: desc})
	}
	return idx, nil
}

// parseFrontmatter pulls name and description out of leading `---` YAML
// frontmatter. Only simple `key: value` lines are handled — enough for the
// SKILL.md files hydra and the shared config write.
func parseFrontmatter(content string) (name, description string) {
	lines := strings.Split(content, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return "", ""
	}
	for _, line := range lines[1:] {
		trimmed := strings.TrimSpace(line)
		if trimmed == "---" {
			break
		}
		switch {
		case strings.HasPrefix(trimmed, "name:"):
			name = unquote(strings.TrimSpace(strings.TrimPrefix(trimmed, "name:")))
		case strings.HasPrefix(trimmed, "description:"):
			description = unquote(strings.TrimSpace(strings.TrimPrefix(trimmed, "description:")))
		}
	}
	return name, description
}

func unquote(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}

// normalizeSkillName maps an LLM-proposed name to a valid kebab-case skill
// directory name. Returns "" when nothing usable remains.
func normalizeSkillName(name string) string {
	s := strings.ToLower(strings.TrimSpace(name))
	s = strings.ReplaceAll(s, " ", "-")
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	out := b.String()
	for strings.Contains(out, "--") {
		out = strings.ReplaceAll(out, "--", "-")
	}
	out = strings.Trim(out, "-")
	if len(out) > 64 {
		out = strings.Trim(out[:64], "-")
	}
	if len(out) < 3 {
		return ""
	}
	return out
}

// tokenize splits text into lowercase alphanumeric tokens.
func tokenize(s string) map[string]struct{} {
	tokens := map[string]struct{}{}
	var cur strings.Builder
	flush := func() {
		if cur.Len() > 0 {
			tokens[cur.String()] = struct{}{}
			cur.Reset()
		}
	}
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			cur.WriteRune(r)
		} else {
			flush()
		}
	}
	flush()
	return tokens
}

// jaccard returns |A∩B| / |A∪B| over token sets.
func jaccard(a, b map[string]struct{}) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	inter := 0
	for t := range a {
		if _, ok := b[t]; ok {
			inter++
		}
	}
	union := len(a) + len(b) - inter
	if union == 0 {
		return 0
	}
	return float64(inter) / float64(union)
}

// containment returns |A∩B| / min(|A|,|B|): how much of the smaller token
// set is covered by the larger. Variant-name sprawl ("verify-first-close" vs
// "verify-first-close-in-shared-worktree") scores low on Jaccard (3/6) but
// 1.0 on containment — live 2026-11-08: 30+ same-family variants survived
// Jaccard-only dedup and needed a manual consolidation pass.
func containment(a, b map[string]struct{}) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	inter := 0
	for t := range a {
		if _, ok := b[t]; ok {
			inter++
		}
	}
	min := len(a)
	if len(b) < min {
		min = len(b)
	}
	return float64(inter) / float64(min)
}

// isDuplicate compares a candidate (normalized name + description) against
// the skill index. It returns the best score and the closest entry. The
// score mirrors the consolidation pass: max(nameScore, 0.6*name+0.4*all)
// where nameScore = max(Jaccard, 0.9*containment) over name tokens — so both
// exact renames and variant-name sprawl of an existing skill register as
// duplicates. Exact name equality short-circuits at 1.0.
func isDuplicate(name, description string, idx []skillIndexEntry) (float64, skillIndexEntry) {
	best := 0.0
	var bestEntry skillIndexEntry
	candNameTokens := tokenize(name)
	candAllTokens := tokenize(name + " " + description)
	for _, e := range idx {
		if e.Name == name {
			return 1.0, e
		}
		eNameTokens := tokenize(e.Name)
		nameScore := math.Max(jaccard(candNameTokens, eNameTokens), 0.9*containment(candNameTokens, eNameTokens))
		allScore := jaccard(candAllTokens, tokenize(e.Name+" "+e.Description))
		score := math.Max(nameScore, 0.6*nameScore+0.4*allScore)
		if score > best {
			best = score
			bestEntry = e
		}
	}
	return best, bestEntry
}

// candidateMetadata is written next to each candidate SKILL.md for review
// provenance.
type candidateMetadata struct {
	Name        string    `json:"name"`
	BeadID      string    `json:"bead_id"`
	SourceFile  string    `json:"source_file"`
	AgentType   string    `json:"agent_type"`
	Model       string    `json:"model"`
	InducedAt   time.Time `json:"induced_at"`
	Description string    `json:"description"`
}

// writeCandidate materializes one candidate skill directory:
//
//	<candDir>/SKILL.md       — frontmatter + body + provenance footer
//	<candDir>/metadata.json  — machine-readable provenance
//
// SKILL.md is shaped exactly like the skills the shared Pi config ships so a
// human promotion is a plain `git mv`-style copy, no editing required.
func writeCandidate(candDir, name string, ind *induction, rec *ExecutionRecord, sourceFile, model string) error {
	if err := os.MkdirAll(candDir, 0o755); err != nil {
		return err
	}

	var front strings.Builder
	front.WriteString("---\n")
	fmt.Fprintf(&front, "name: %s\n", name)
	fmt.Fprintf(&front, "description: %q\n", singleLine(ind.Description))
	if len(ind.Triggers) > 0 {
		fmt.Fprintf(&front, "triggers:\n")
		for _, t := range ind.Triggers {
			fmt.Fprintf(&front, "  - %q\n", singleLine(t))
		}
	}
	front.WriteString("---\n\n")

	body := strings.TrimSpace(ind.Body)
	if body == "" {
		body = fmt.Sprintf("# %s\n\n%s", name, ind.Description)
	}

	var b strings.Builder
	b.WriteString(front.String())
	b.WriteString(body)
	b.WriteString("\n\n## Provenance\n\n")
	fmt.Fprintf(&b, "- Induced from worker execution `%s` (file `%s`, agent `%s`)\n", rec.BeadID, sourceFile, rec.AgentType)
	fmt.Fprintf(&b, "- Model: %s at %s\n", model, time.Now().UTC().Format(time.RFC3339))
	b.WriteString("- MACHINE-EXTRACTED CANDIDATE: review before promoting into the shared skill library.\n")

	if err := os.WriteFile(filepath.Join(candDir, "SKILL.md"), []byte(b.String()), 0o644); err != nil {
		return err
	}

	meta := candidateMetadata{
		Name:        name,
		BeadID:      rec.BeadID,
		SourceFile:  sourceFile,
		AgentType:   rec.AgentType,
		Model:       model,
		InducedAt:   time.Now().UTC(),
		Description: ind.Description,
	}
	metaData, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(candDir, "metadata.json"), metaData, 0o644)
}

// singleLine collapses newlines/tabs so frontmatter values stay parseable.
func singleLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
