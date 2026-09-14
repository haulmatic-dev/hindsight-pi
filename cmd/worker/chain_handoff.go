package main

// Chain handoff artifact (2026-09-14, prefix-stability programme #2):
// a completed phase writes docs/hydra/handoff/<phase>.md into its worktree;
// integrate commits it to the integration branch; the next phase's worktree
// (created from that branch) carries it. The next worker embeds the frozen
// handoff in its prompt tail so it builds on the prior phase's verified
// outcome instead of re-deriving it via billed tool calls. Git is the frozen
// store: append-only, diffable, survives restarts.
//
// docs/hydra/handoff/ rather than .hydra/: srv-smart-engine gitignores
// .hydra/, and docs/ is already scope-gate allowlisted.

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// chainContextMaxPerFile bounds a single embedded handoff; the block is tail
// content, but unbounded tails still re-bill on every retry.
const chainContextMaxPerFile = 1500

// handoffPhaseRank orders handoffs by chain phase, not alphabet (green.md
// would sort before red.md). Unknown phases rank last, alphabetically.
func handoffPhaseRank(name string) int {
	switch strings.TrimSuffix(name, ".md") {
	case "red", "test":
		return 0
	case "green":
		return 1
	case "refactor":
		return 2
	}
	return 3
}

// loadChainContext reads docs/hydra/handoff/*.md from the worktree, ordered
// by chain phase then filename, each truncated to chainContextMaxPerFile.
// Missing dir or unreadable files yield nil — fail open: the prompt simply
// carries no chain context.
func loadChainContext(worktree string) []string {
	entries, err := os.ReadDir(filepath.Join(worktree, "docs", "hydra", "handoff"))
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
			names = append(names, e.Name())
		}
	}
	if len(names) == 0 {
		return nil
	}
	sort.Slice(names, func(i, j int) bool {
		ri, rj := handoffPhaseRank(names[i]), handoffPhaseRank(names[j])
		if ri != rj {
			return ri < rj
		}
		return names[i] < names[j]
	})
	out := make([]string, 0, len(names))
	for _, n := range names {
		b, err := os.ReadFile(filepath.Join(worktree, "docs", "hydra", "handoff", n))
		if err != nil {
			continue
		}
		out = append(out, truncateString(string(b), chainContextMaxPerFile))
	}
	return out
}
