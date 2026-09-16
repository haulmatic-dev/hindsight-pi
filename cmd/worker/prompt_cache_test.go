package main

// Prompt cache stability (2026-09-14, dogfood of the prefix-stability
// programme): Kimi caches server-side by prefix hash, so cross-bead and
// cross-retry savings require the invariant doctrine blocks to precede all
// per-bead variable content in the worker prompt. These tests pin the
// ordering and the digest we log for measurement.

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// commonPrefixLen returns the length of the shared byte prefix of a and b.
func commonPrefixLen(a, b string) int {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	i := 0
	for i < n && a[i] == b[i] {
		i++
	}
	return i
}

// Two beads that differ only in per-bead fields (type, title, description,
// target files) must share a long byte-identical head: the invariant
// doctrine blocks come first, per-bead content last. This is the property
// that makes provider-side prefix caching pay across beads of a project.
func TestBuildPiPromptStableHeadPrefix(t *testing.T) {
	t.Setenv("HYDRA_DAEMON_GATES", "")
	a := buildPiPrompt(types.Bead{ID: "x-1", Type: "test", Title: "RED: compute totals",
		Description: "Write failing tests for computeTotals."}, nil, promptExtras{})
	b := buildPiPrompt(types.Bead{ID: "x-2", Type: "green", Title: "GREEN: implement totals",
		Description: "Implement computeTotals to satisfy the tests.\nFiles: src/totals.ts"}, nil, promptExtras{})

	head := commonPrefixLen(a, b)
	if head < 400 {
		t.Errorf("shared invariant head = %d bytes, want >= 400 — per-bead content leaks before the doctrine blocks", head)
	}
}

// Identical bead + identical enrichment must yield byte-identical prompts,
// so a retry (same bead, same context) is a full cache hit.
func TestBuildPiPromptByteDeterministic(t *testing.T) {
	bead := types.Bead{ID: "x-1", Type: "test", Title: "RED: t", Description: "d"}
	p1 := buildPiPrompt(bead, nil, promptExtras{})
	p2 := buildPiPrompt(bead, nil, promptExtras{})
	if p1 != p2 {
		t.Errorf("same input produced different prompts:\n%q\n---\n%q", p1, p2)
	}
	f1, h1 := promptPrefixDigest(p1)
	f2, h2 := promptPrefixDigest(p2)
	if f1 != f2 || h1 != h2 {
		t.Errorf("digests differ for identical prompts: (%s,%s) vs (%s,%s)", f1, h1, f2, h2)
	}
}

// promptPrefixDigest must be sha256 over the whole prompt and over the first
// 8KiB head. Short prompts hash head == full.
func TestPromptPrefixDigest(t *testing.T) {
	short := "hello prompt"
	full, head := promptPrefixDigest(short)
	sum := sha256.Sum256([]byte(short))
	wantFull := hex.EncodeToString(sum[:])
	if full != wantFull {
		t.Errorf("full digest = %s, want %s", full, wantFull)
	}
	if head != full {
		t.Errorf("short-prompt head digest %s != full digest %s", head, full)
	}

	long := strings.Repeat("x", 9000)
	_, headLong := promptPrefixDigest(long)
	hs := sha256.Sum256([]byte(long[:8192]))
	wantHead := hex.EncodeToString(hs[:])
	if headLong != wantHead {
		t.Errorf("8KiB head digest = %s, want %s", headLong, wantHead)
	}
}

// Regression (2026-09-16, nipuna campaign incident): beads of type test/red
// completed with EMPTY diffs because the deliverable contract only covered
// implementation types. A RED bead whose whole purpose is writing failing
// specs must carry the same no-plan-only contract.
func TestDeliverableContractCoversTestAndRedTypes(t *testing.T) {
	for _, typ := range []string{"test", "red"} {
		p := buildPiPrompt(types.Bead{ID: "x-1", Type: typ, Title: "RED: specs", Description: "d"}, nil, promptExtras{})
		if !strings.Contains(p, "DELIVERABLE CONTRACT") {
			t.Errorf("type %q: prompt missing DELIVERABLE CONTRACT (empty-diff completions accepted)", typ)
		}
	}
	// Non-edit types stay contract-free (discovery is analysis-shaped).
	p := buildPiPrompt(types.Bead{ID: "x-1", Type: "discovery", Title: "Discovery", Description: "d"}, nil, promptExtras{})
	if strings.Contains(p, "DELIVERABLE CONTRACT") {
		t.Error("type discovery must NOT carry the deliverable contract")
	}
}
