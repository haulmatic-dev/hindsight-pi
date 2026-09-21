package main

// Live prompt-tail verification: with LIVE_PACK_DIR + LIVE_PACK_BEAD set,
// fetches the bead from the real br store, parses it through the real beads
// parser, and asserts buildPiPrompt renders the CONTEXT PACK section in the
// tail. Skipped unless the env vars are set (normally driven by
// TestLiveVerify_ContextPackEndToEnd's project).

import (
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/haulmatic-dev/hydra/internal/beads"
	"github.com/haulmatic-dev/hydra/pkg/types"
)

func TestLiveVerify_PackInPromptTail(t *testing.T) {
	dir := os.Getenv("LIVE_PACK_DIR")
	beadID := os.Getenv("LIVE_PACK_BEAD")
	if dir == "" || beadID == "" {
		t.Skip("set LIVE_PACK_DIR and LIVE_PACK_BEAD for the live prompt verification")
	}
	cmd := exec.Command("br", "show", beadID, "--json")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("br show: %v", err)
	}
	b, err := beads.NewParser().ParseBeadDetail(out, beads.BeadContext{Project: "live", Repo: ""})
	if err != nil {
		t.Fatalf("parse bead detail: %v", err)
	}
	if b.AgentContext == "" {
		t.Fatal("live bead has no agent_context")
	}
	t.Setenv("HYDRA_DAEMON_GATES", "")
	prompt := buildPiPrompt(types.Bead{
		ID:           b.ID,
		Type:         b.Type,
		Title:        b.Title,
		Description:  b.Description,
		AgentContext: b.AgentContext,
	}, nil, promptExtras{})

	if !strings.Contains(prompt, "CONTEXT PACK") {
		t.Error("live bead prompt has no CONTEXT PACK section")
	}
	headIdx := strings.Index(prompt, "CLOSURE PROTOCOL")
	packIdx := strings.Index(prompt, "CONTEXT PACK")
	if headIdx < 0 || packIdx < headIdx {
		t.Errorf("pack at %d, head boundary at %d — must be tail", packIdx, headIdx)
	}
	t.Logf("LIVE VERIFY OK: prompt tail contains pack section (%d bytes) for bead %s", packIdx, beadID)
}
