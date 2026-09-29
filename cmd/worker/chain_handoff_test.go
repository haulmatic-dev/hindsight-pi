package main

// Chain handoff artifact (2026-09-14, prefix-stability programme #2):
// a completed phase writes docs/hydra/handoff/<phase>.md into its worktree;
// integrate commits it to the integration branch; the next phase's worktree
// (created from that branch) carries it. The next worker embeds the frozen
// handoff in its prompt tail so it builds on the prior phase's verified
// outcome instead of re-deriving it via billed tool calls. Git is the frozen
// store: append-only, diffable, survives restarts.

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// Two prompts that differ only in the embedded chain context must still
// share the invariant doctrine head — the handoff block is tail content.
func TestChainContextPreservesStableHead(t *testing.T) {
	a := buildPiPrompt(types.Bead{ID: "x-1", Type: "green", Title: "GREEN: totals"}, nil, promptExtras{
		ChainContext: []string{"# red handoff\n- wrote src/totals_test.go\n- verified: go test ./src"},
	})
	b := buildPiPrompt(types.Bead{ID: "x-1", Type: "green", Title: "GREEN: totals"}, nil, promptExtras{})
	if head := commonPrefixLen(a, b); head < 400 {
		t.Errorf("shared head with vs without chain context = %d bytes, want >= 400 (context must be tail content)", head)
	}
	if !strings.Contains(a, "FROZEN CHAIN CONTEXT") {
		t.Error("prompt missing FROZEN CHAIN CONTEXT block")
	}
	if !strings.Contains(a, "src/totals_test.go") {
		t.Error("prompt missing handoff content")
	}
}

// Order of embedded contexts is the sorted order given (the caller sorts by
// filename), so identical files always yield identical prompt bytes.
func TestChainContextDeterministicOrder(t *testing.T) {
	bead := types.Bead{ID: "x-1", Type: "refactor", Title: "REFACTOR: totals"}
	p1 := buildPiPrompt(bead, nil, promptExtras{ChainContext: []string{"red content", "green content"}})
	p2 := buildPiPrompt(bead, nil, promptExtras{ChainContext: []string{"red content", "green content"}})
	if p1 != p2 {
		t.Error("identical chain context produced different prompts")
	}
	ir := strings.Index(p1, "red content")
	ig := strings.Index(p1, "green content")
	if ir < 0 || ig < 0 || ir > ig {
		t.Errorf("context order not preserved: red@%d green@%d", ir, ig)
	}
}

// loadChainContext reads docs/hydra/handoff/*.md from the worktree, sorted by
// filename, truncated per file. Missing dir → nil, never an error (fail open:
// the prompt simply carries no context).
func TestLoadChainContext(t *testing.T) {
	dir := t.TempDir()
	if got := loadChainContext(filepath.Join(dir, "nope")); got != nil {
		t.Errorf("missing dir: got %v, want nil", got)
	}
	hd := filepath.Join(dir, "docs", "hydra", "handoff")
	if err := os.MkdirAll(hd, 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(hd, "green.md"), []byte("green outcome"), 0o644)
	os.WriteFile(filepath.Join(hd, "red.md"), []byte("red outcome"), 0o644)
	got := loadChainContext(dir)
	if len(got) != 2 || got[0] != "red outcome" || got[1] != "green outcome" {
		t.Errorf("loadChainContext = %v, want [red green] sorted", got)
	}
	// Phase order beats alphabet: green.md sorts before red.md alphabetically
	// but must follow it. Unknown phases rank last.
	os.WriteFile(filepath.Join(hd, "aaa.md"), []byte("zzz latest"), 0o644)
	got = loadChainContext(dir)
	if len(got) != 3 || got[0] != "red outcome" || got[1] != "green outcome" || got[2] != "zzz latest" {
		t.Errorf("phase-order sort = %v, want [red green zzz]", got)
	}
	os.Remove(filepath.Join(hd, "aaa.md"))
	// Oversized file is truncated.
	big := strings.Repeat("y", chainContextMaxPerFile+500)
	os.WriteFile(filepath.Join(hd, "red.md"), []byte(big), 0o644)
	got = loadChainContext(dir)
	for _, g := range got {
		if len(g) > chainContextMaxPerFile {
			t.Errorf("entry len %d exceeds max %d", len(g), chainContextMaxPerFile)
		}
	}
	var _ = sort.Strings
}

// FR-001 (autonomous-resolution-layer): handoff fragments are per-bead
// (<phase>-<bead-id>.md), not per-phase, so parallel beads of the same
// phase never collide on one file at integrate time (discovery.md was the
// top conflict file: 568 conflicts / 570 integrate failures).
func TestChainHandoffFragmentName(t *testing.T) {
	tests := []struct {
		id, btype, want string
	}{
		{"hydra-5xvh", "discovery", filepath.Join("docs", "hydra", "handoff", "discovery-5xvh.md")},
		{"hydra-dogfood-w5ac", "green", filepath.Join("docs", "hydra", "handoff", "green-w5ac.md")},
		{"myproject-abc123", "implement", filepath.Join("docs", "hydra", "handoff", "implement-abc123.md")},
		{"x-1", "RED", filepath.Join("docs", "hydra", "handoff", "red-1.md")},   // phase lowercased
		{"x-2", "Weird Type!", filepath.Join("docs", "hydra", "handoff", "weirdtype-2.md")}, // sanitized
		{"x-3", "", filepath.Join("docs", "hydra", "handoff", "unknown-3.md")},  // no type
		{"bare", "test", filepath.Join("docs", "hydra", "handoff", "test-bare.md")}, // no suffix
	}
	for _, tc := range tests {
		got := chainHandoffFragmentName(types.Bead{ID: types.BeadID(tc.id), Type: tc.btype})
		if got != tc.want {
			t.Errorf("chainHandoffFragmentName(%q,%q) = %q, want %q", tc.id, tc.btype, got, tc.want)
		}
	}
}

// Fragment files rank with their phase, so green-5xvh.md follows red-abc.md
// in the embedded chain context regardless of alphabet.
func TestHandoffPhaseRankFragments(t *testing.T) {
	if handoffPhaseRank("green-5xvh.md") != 1 {
		t.Error("green-5xvh.md should rank as green (1)")
	}
	if handoffPhaseRank("red-abc.md") != 0 {
		t.Error("red-abc.md should rank as red (0)")
	}
	if handoffPhaseRank("discovery-5xvh.md") != 3 {
		t.Error("unknown-phase fragment should rank last (3)")
	}
	if handoffPhaseRank("greenhouse.md") != 3 {
		t.Error("greenhouse.md must not rank as green prefix")
	}
}

// loadChainContext orders per-bead fragments by phase then filename.
func TestLoadChainContextFragmentOrder(t *testing.T) {
	dir := t.TempDir()
	hd := filepath.Join(dir, "docs", "hydra", "handoff")
	if err := os.MkdirAll(hd, 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(hd, "green-5xvh.md"), []byte("green frag"), 0o644)
	os.WriteFile(filepath.Join(hd, "red-abc.md"), []byte("red frag"), 0o644)
	got := loadChainContext(dir)
	if len(got) != 2 || got[0] != "red frag" || got[1] != "green frag" {
		t.Errorf("fragment order = %v, want [red-abc green-5xvh]", got)
	}
}
