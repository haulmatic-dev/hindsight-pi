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
