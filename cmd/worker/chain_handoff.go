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

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// chainContextMaxPerFile bounds a single embedded handoff; the block is tail
// content, but unbounded tails still re-bill on every retry.
const chainContextMaxPerFile = 1500

// handoffPhaseRank orders handoffs by chain phase, not alphabet (green.md
// would sort before red.md). FR-001: per-bead fragments (<phase>-<bead>.md)
// rank with their phase via prefix match, so green-5xvh.md orders with green.
// Unknown phases rank last, alphabetically.
func handoffPhaseRank(name string) int {
	base := strings.TrimSuffix(name, ".md")
	for _, p := range []struct {
		prefix string
		rank   int
	}{{"red", 0}, {"test", 0}, {"green", 1}, {"refactor", 2}} {
		if base == p.prefix || strings.HasPrefix(base, p.prefix+"-") {
			return p.rank
		}
	}
	return 3
}

// chainHandoffFragmentName returns the per-bead handoff path for a bead:
// docs/hydra/handoff/<phase>-<bead-id>.md (FR-001). Parallel beads of the
// same phase each write their own fragment — no shared discovery.md to
// collide on at integrate time. The bead id is sanitized to its numeric
// suffix, lowercased, filename-safe.
func chainHandoffFragmentName(bead types.Bead) string {
	phase := strings.ToLower(strings.TrimSpace(bead.Type))
	if phase == "" {
		phase = "unknown"
	}
	phase = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			return r
		}
		return -1
	}, phase)
	id := strings.ToLower(string(bead.ID))
	if i := strings.LastIndex(id, "-"); i >= 0 && i < len(id)-1 {
		id = id[i+1:]
	}
	id = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			return r
		}
		return -1
	}, id)
	if id == "" {
		id = "bead"
	}
	return filepath.Join("docs", "hydra", "handoff", phase+"-"+id+".md")
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
