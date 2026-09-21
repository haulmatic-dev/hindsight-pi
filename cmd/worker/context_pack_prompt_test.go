package main

// Context pack prompt-injection tests: the pack must land in the per-bead
// variable TAIL (after the invariant doctrine head), and only when the bead
// actually carries an agent_context pack.

import (
	"strings"
	"testing"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

func packBead() types.Bead {
	return types.Bead{
		ID:          "x-9",
		Type:        "implement",
		Title:       "GREEN: build handler",
		Description: "Implement the handler.\n\n## Target Files\n- handler.go",
		AgentContext: `{"version":1,"base_sha":"abc123","gate_commands":["go test ./..."],` +
			`"spec_pattern":"*_test.go (colocated next to source)","target_dirs":["internal"],` +
			`"files":[{"requirement":"Handler","path":"handler.go","confidence":1}],` +
			`"provenance":"hydra-context-pack"}`,
	}
}

// The pack section must appear in the tail — strictly after the invariant
// head blocks (CLOSURE PROTOCOL is the last head block before per-bead
// content) — never before them.
func TestBuildPiPrompt_ContextPackLandsInTail(t *testing.T) {
	t.Setenv("HYDRA_DAEMON_GATES", "")
	p := buildPiPrompt(packBead(), nil, promptExtras{})

	packIdx := strings.Index(p, "CONTEXT PACK")
	if packIdx < 0 {
		t.Fatal("prompt has no CONTEXT PACK section")
	}
	headIdx := strings.Index(p, "CLOSURE PROTOCOL")
	if headIdx < 0 || packIdx < headIdx {
		t.Errorf("CONTEXT PACK at %d, CLOSURE PROTOCOL at %d — pack must be in the tail, after the invariant head", packIdx, headIdx)
	}
	for _, want := range []string{
		"Gate commands", "go test ./...",
		"Spec convention", "*_test.go",
		"Repo dirs", "internal",
		"Handler → handler.go",
		"NO APPROVAL-SEEKING", "PARALLEL TOOL CALLS", "FAILURE PROTOCOL",
	} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt missing %q", want)
		}
	}
}

// A bead without agent_context gets no pack section: the pack is an
// optimization, unguided beads keep the plain prompt.
func TestBuildPiPrompt_NoPackNoSection(t *testing.T) {
	t.Setenv("HYDRA_DAEMON_GATES", "")
	b := packBead()
	b.AgentContext = ""
	p := buildPiPrompt(b, nil, promptExtras{})
	if strings.Contains(p, "CONTEXT PACK") {
		t.Error("unguided bead must not get a CONTEXT PACK section")
	}
}

// Human-written (non-pack) agent_context must not render as a pack either.
func TestBuildPiPrompt_NonPackAgentContextIgnored(t *testing.T) {
	t.Setenv("HYDRA_DAEMON_GATES", "")
	b := packBead()
	b.AgentContext = `{"custom":"human instructions"}`
	p := buildPiPrompt(b, nil, promptExtras{})
	if strings.Contains(p, "CONTEXT PACK") {
		t.Error("non-pack agent_context must not render a CONTEXT PACK section")
	}
}

// The pack must not shrink the invariant head shared with pack-less beads:
// everything before the per-bead title block stays byte-identical.
func TestBuildPiPrompt_PackKeepsStableHead(t *testing.T) {
	t.Setenv("HYDRA_DAEMON_GATES", "")
	withPack := buildPiPrompt(packBead(), nil, promptExtras{})
	plain := types.Bead{ID: "x-9", Type: "implement", Title: "GREEN: build handler",
		Description: "Implement the handler.\n\n## Target Files\n- handler.go"}
	without := buildPiPrompt(plain, nil, promptExtras{})

	// The shared head ends where per-bead title begins; the pack sits after
	// that, so the heads of both prompts must be identical up to the title.
	title := "implement: GREEN: build handler"
	h1 := strings.Index(withPack, title)
	h2 := strings.Index(without, title)
	if h1 < 0 || h2 < 0 {
		t.Fatalf("title not found: %d %d", h1, h2)
	}
	if withPack[:h1] != without[:h2] {
		t.Error("pack injection moved content into the invariant head — cache-stability regression")
	}
}
