package main

import (
	"strings"
	"testing"

	"github.com/haulmatic-dev/hydra/agents"
	"github.com/haulmatic-dev/hydra/internal/hindsight"
	"github.com/haulmatic-dev/hydra/pkg/types"
)

// TestBuildPiPrompt_DeterministicTail verifies the cache-hardening contract:
// the Hindsight memory block and the CASS bullet block are ordered by content
// hash, so two builds of the same bead spec with the same underlying memory
// and bullet sets produce byte-identical prompts even when the server-ranked
// input order differs between calls (2026-09-06).
func TestBuildPiPrompt_DeterministicTail(t *testing.T) {
	bead := types.Bead{ID: "b1", Type: "implement", Title: "cache hardening", Description: "d"}

	enrichment := func(order []int) *agents.ContextEnrichment {
		all := []hindsight.Memory{
			{Content: "memory alpha about workers"},
			{Content: "memory beta about prompts"},
			{Content: "memory gamma about caches"},
			{Content: "memory delta about sessions"},
		}
		mems := make([]hindsight.Memory, 0, len(order))
		for _, i := range order {
			mems = append(mems, all[i])
		}
		return &agents.ContextEnrichment{Memories: mems}
	}
	extras := func(order []int) promptExtras {
		all := []string{
			"bullet one: nil-guard injected deps",
			"bullet two: keep stdout alive in coverage runs",
			"bullet three: check files exist before agent verification",
		}
		bullets := make([]string, 0, len(order))
		for _, i := range order {
			bullets = append(bullets, all[i])
		}
		return promptExtras{CassBullets: bullets}
	}

	p1 := buildPiPrompt(bead, enrichment([]int{0, 1, 2, 3}), extras([]int{0, 1, 2}))
	p2 := buildPiPrompt(bead, enrichment([]int{3, 1, 0, 2}), extras([]int{2, 0, 1}))
	p3 := buildPiPrompt(bead, enrichment([]int{2, 3, 1, 0}), extras([]int{1, 2, 0}))

	if p1 != p2 || p2 != p3 {
		t.Errorf("prompts differ across input permutations of the same set")
	}

	// Same bead, different memory set must change the prompt (guard against a
	// degenerate test where the tail is always empty).
	changed := buildPiPrompt(bead, enrichment([]int{0, 1, 2}), extras([]int{0, 1, 2}))
	if changed == p1 {
		t.Errorf("prompt must change when the memory set changes")
	}
	for _, want := range []string{"memory alpha", "memory beta", "memory gamma", "memory delta",
		"bullet one", "bullet two", "bullet three"} {
		if !strings.Contains(p1, want) {
			t.Errorf("prompt missing %q", want)
		}
	}
}
