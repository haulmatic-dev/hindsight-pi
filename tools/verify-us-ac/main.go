// verify-us-ac cross-references PRD User Story acceptance criteria
// against closed beads to detect untested/unimplemented ACs.
//
// Usage:
//
//	go run tools/verify-us-ac --prd docs/PRDs/feature.md [--beads-cmd "br list --all --json"]
//
// Exit codes:
//
//	0 - all ACs satisfied
//	1 - one or more ACs missing
//	2 - PRD parse error
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
)

type AC struct {
	USID        string `json:"us_id"`
	USTitle     string `json:"us_title"`
	Text        string `json:"text"`
	Satisfied   bool   `json:"satisfied"`
	SatisfiedBy string `json:"satisfied_by,omitempty"`
}

type Violation struct {
	USID    string `json:"us_id"`
	USTitle string `json:"us_title"`
	AC      string `json:"ac"`
	Issue   string `json:"issue"`
}

type Result struct {
	Passed      bool        `json:"passed"`
	TotalACs    int         `json:"total_acs"`
	Satisfied   int         `json:"satisfied"`
	Missing     []Violation `json:"missing,omitempty"`
	Unsatisfied []AC        `json:"unsatisfied,omitempty"`
}

type Bead struct {
	ID          string   `json:"id"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Status      string   `json:"status"`
	Labels      []string `json:"labels"`
}

func main() {
	prdPath := flag.String("prd", "", "Path to PRD markdown file")
	beadsCmd := flag.String("beads-cmd", "br list --all --json", "Command to list all beads")
	flag.Parse()

	if *prdPath == "" {
		fmt.Fprintln(os.Stderr, "Usage: verify-us-ac --prd <prd.md> [--beads-cmd <cmd>]")
		os.Exit(2)
	}

	prdBytes, err := os.ReadFile(*prdPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading PRD: %v\n", err)
		os.Exit(2)
	}

	acs := extractACs(string(prdBytes))
	beads := fetchBeads(*beadsCmd)

	var missing []Violation
	var unsatisfied []AC
	satisfiedCount := 0

	for i := range acs {
		beadID, ok := findSatisfyingBead(acs[i], beads)
		acs[i].Satisfied = ok
		if ok {
			acs[i].SatisfiedBy = beadID
			satisfiedCount++
		} else {
			unsatisfied = append(unsatisfied, acs[i])
			missing = append(missing, Violation{
				USID:    acs[i].USID,
				USTitle: acs[i].USTitle,
				AC:      acs[i].Text,
				Issue:   "no closed bead claims to satisfy this acceptance criterion",
			})
		}
	}

	result := Result{
		Passed:      len(missing) == 0,
		TotalACs:    len(acs),
		Satisfied:   satisfiedCount,
		Missing:     missing,
		Unsatisfied: unsatisfied,
	}

	out, _ := json.MarshalIndent(result, "", "  ")
	fmt.Println(string(out))

	if len(missing) > 0 {
		os.Exit(1)
	}
}

// extractACs parses User Stories and their acceptance criteria from the PRD.
func extractACs(prd string) []AC {
	var acs []AC

	// Find each User Story section
	usRe := regexp.MustCompile(`(?m)^#{2,4}\s+US-\d+:\s*(.+)$`)
	acRe := regexp.MustCompile(`(?m)^#{2,4}\s+Acceptance Criteria`)

	usMatches := usRe.FindAllStringIndex(prd, -1)

	for i, usMatch := range usMatches {
		start := usMatch[0]
		end := len(prd)
		if i+1 < len(usMatches) {
			end = usMatches[i+1][0]
		}

		usSection := prd[start:end]
		usTitle := strings.TrimSpace(usRe.FindStringSubmatch(usSection)[1])
		usID := extractUSID(usSection)

		// Find Acceptance Criteria within this US
		acMatch := acRe.FindStringIndex(usSection)
		if acMatch == nil {
			continue
		}

		acSection := usSection[acMatch[1]:]
		// Stop at next section
		nextSection := regexp.MustCompile(`(?m)^#{2,4}\s+`).FindStringIndex(acSection)
		if nextSection != nil {
			acSection = acSection[:nextSection[0]]
		}

		// Extract bullet points
		bulletRe := regexp.MustCompile(`(?m)^\s*[-*]\s+(.+)$`)
		bullets := bulletRe.FindAllStringSubmatch(acSection, -1)

		for _, b := range bullets {
			acs = append(acs, AC{
				USID:    usID,
				USTitle: usTitle,
				Text:    strings.TrimSpace(b[1]),
			})
		}
	}

	return acs
}

func extractUSID(section string) string {
	re := regexp.MustCompile(`US-\d+`)
	m := re.FindString(section)
	if m != "" {
		return m
	}
	return "US-???"
}

// fetchBeads runs the beads command and parses the output.
func fetchBeads(cmdStr string) []Bead {
	parts := strings.Fields(cmdStr)
	if len(parts) == 0 {
		return nil
	}

	cmd := exec.Command(parts[0], parts[1:]...)
	out, err := cmd.Output()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: beads command failed: %v\n", err)
		return nil
	}

	// br list --json returns {"issues":[...],"total":N,...}
	var result struct {
		Issues []Bead `json:"issues"`
	}
	if err := json.Unmarshal(out, &result); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to parse beads JSON: %v\n", err)
		return nil
	}

	return result.Issues
}

// findSatisfyingBead checks if any closed bead's title or description
// mentions the acceptance criterion text (or key terms from it).
func findSatisfyingBead(ac AC, beads []Bead) (string, bool) {
	keywords := extractKeywords(ac.Text)
	acLower := strings.ToLower(ac.Text)

	for _, bead := range beads {
		if bead.Status != "closed" && bead.Status != "completed" {
			continue
		}

		beadText := strings.ToLower(bead.Title + " " + bead.Description)

		// Direct substring match
		if strings.Contains(beadText, acLower) {
			return bead.ID, true
		}

		// Keyword match: all keywords must appear in the bead
		allMatch := true
		for _, kw := range keywords {
			if !strings.Contains(beadText, kw) {
				allMatch = false
				break
			}
		}
		if allMatch && len(keywords) > 0 {
			return bead.ID, true
		}
	}

	return "", false
}

// extractKeywords pulls important terms from an AC string.
func extractKeywords(text string) []string {
	// Normalize
	text = strings.ToLower(text)
	// Remove punctuation except hyphens and slashes
	text = regexp.MustCompile(`[^a-z0-9\s/-]`).ReplaceAllString(text, " ")
	// Split
	words := strings.Fields(text)

	// Filter out common stop words
	stopWords := map[string]bool{
		"the": true, "a": true, "an": true, "and": true, "or": true,
		"to": true, "of": true, "in": true, "for": true, "with": true,
		"on": true, "at": true, "by": true, "is": true, "are": true,
		"be": true, "that": true, "it": true, "from": true, "as": true,
		"returns": true, "should": true, "must": true, "can": true,
		"have": true, "has": true, "using": true, "via": true,
	}

	var keywords []string
	seen := make(map[string]bool)
	for _, w := range words {
		if len(w) < 3 {
			continue
		}
		if stopWords[w] {
			continue
		}
		if !seen[w] {
			seen[w] = true
			keywords = append(keywords, w)
		}
	}

	return keywords
}
