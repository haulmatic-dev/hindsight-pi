// Package triage implements the periodic LLM PR-triage pass for product-repo
// dogfood PRs (bead hydra-muvh, PRD autonomous-resolution-layer FR-030).
//
// Design principle (operator, 2026-09-29): intelligence for classification
// and strategy, mechanical for contracts and safety. This pass is strategy
// advice ONLY: it groups overlapping dogfood/* PRs by target files, asks the
// cheap LLM judge for a merge order / duplicate / needs-human verdict, and
// posts a recommendation comment. It NEVER merges and NEVER closes — Tier 2
// merge authority stays human.
package triage

import (
	"fmt"
	"sort"
	"strings"
)

// PR is one open dogfood PR collected from a product repo.
type PR struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
	Branch string `json:"branch"` // headRefName
	URL    string `json:"url"`
	Files  []string `json:"files"` // target file paths
}

// Key identifies the PR for rate limiting ("#12" within one repo scan).
func (p PR) Key() string { return fmt.Sprintf("#%d", p.Number) }

// Group is a set of PRs that touch at least one common target file.
type Group struct {
	// Files are the shared target files that caused the grouping.
	Files []string
	PRs   []PR
}

// Recommendation is the LLM's verdict for one group.
type Recommendation struct {
	MergeOrder []int           `json:"merge_order"`   // PR numbers, merge first → last
	Duplicates []DuplicateFlag `json:"duplicates"`    // close-as-duplicate candidates
	NeedsHuman []HumanFlag     `json:"needs_human"`   // judgment calls for Tier 2
	Reasoning  string          `json:"reasoning"`     // one-line rationale
}

// DuplicateFlag marks a PR as an overlap duplicate of another.
type DuplicateFlag struct {
	PR     int    `json:"pr"`
	Of     int    `json:"of"`
	Reason string `json:"reason"`
}

// HumanFlag marks a PR as needing human judgment.
type HumanFlag struct {
	PR     int    `json:"pr"`
	Reason string `json:"reason"`
}

// LabelDuplicate / LabelMergeFirst are the optional triage labels the pass
// may attach. Never auto-merge, never auto-close.
const (
	LabelDuplicate = "triage:duplicate"
	LabelMergeFirst = "triage:merge-first"
)

// GroupByOverlap groups PRs into connected components of file overlap: two
// PRs sharing at least one target file land in the same group. Groups with a
// single PR are dropped (nothing to triage). Deterministic ordering: groups
// sorted by first PR number, PRs sorted by number within a group.
func GroupByOverlap(prs []PR) []Group {
	n := len(prs)
	parent := make([]int, n)
	for i := range parent {
		parent[i] = i
	}
	var find func(int) int
	find = func(x int) int {
		for parent[x] != x {
			parent[x] = parent[parent[x]]
			x = parent[x]
		}
		return x
	}
	union := func(a, b int) {
		ra, rb := find(a), find(b)
		if ra != rb {
			parent[rb] = ra
		}
	}
	// file → PR indices touching it
	byFile := make(map[string][]int)
	for i, p := range prs {
		for _, f := range p.Files {
			byFile[f] = append(byFile[f], i)
		}
	}
	for _, idxs := range byFile {
		for _, j := range idxs[1:] {
			union(idxs[0], j)
		}
	}
	comps := make(map[int][]int)
	for i := range prs {
		r := find(i)
		comps[r] = append(comps[r], i)
	}
	var groups []Group
	for _, idxs := range comps {
		if len(idxs) < 2 {
			continue
		}
		prsIn := make([]PR, 0, len(idxs))
		for _, i := range idxs {
			prsIn = append(prsIn, prs[i])
		}
		sort.Slice(prsIn, func(a, b int) bool { return prsIn[a].Number < prsIn[b].Number })
		// Shared files = files touched by ≥2 PRs in the group.
		count := make(map[string]int)
		for _, p := range prsIn {
			for _, f := range p.Files {
				count[f]++
			}
		}
		var shared []string
		for f, c := range count {
			if c >= 2 {
				shared = append(shared, f)
			}
		}
		sort.Strings(shared)
		groups = append(groups, Group{Files: shared, PRs: prsIn})
	}
	sort.Slice(groups, func(a, b int) bool { return groups[a].PRs[0].Number < groups[b].PRs[0].Number })
	return groups
}

// CommentBody renders the PR comment for a group's recommendation. The
// wording makes explicit that this is advice only.
func CommentBody(g Group, r Recommendation) string {
	var b strings.Builder
	b.WriteString("🤖 **PR triage (automated recommendation — no action taken)**\n\n")
	b.WriteString(fmt.Sprintf("Overlap group on %d shared file(s): %s\n\n",
		len(g.Files), strings.Join(g.Files, ", ")))
	if len(r.MergeOrder) > 0 {
		b.WriteString("**Suggested merge order:**")
		for _, n := range r.MergeOrder {
			b.WriteString(fmt.Sprintf(" #%d →", n))
		}
		b.WriteString(" done\n")
	}
	if len(r.Duplicates) > 0 {
		b.WriteString("\n**Duplicate candidates** (consider closing; human decides):\n")
		for _, d := range r.Duplicates {
			b.WriteString(fmt.Sprintf("- #%d — duplicate of #%d: %s\n", d.PR, d.Of, d.Reason))
		}
	}
	if len(r.NeedsHuman) > 0 {
		b.WriteString("\n**Needs human judgment:**\n")
		for _, h := range r.NeedsHuman {
			b.WriteString(fmt.Sprintf("- #%d: %s\n", h.PR, h.Reason))
		}
	}
	if r.Reasoning != "" {
		b.WriteString("\n" + r.Reasoning + "\n")
	}
	b.WriteString("\n_Never auto-merged, never auto-closed. Tier 2 merger decides._")
	return b.String()
}
