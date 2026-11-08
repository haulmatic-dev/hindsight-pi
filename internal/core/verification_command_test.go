package core

import (
	"strings"
	"testing"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// t55vDesc mirrors the real task-breakdown bead description shape: a prose
// instruction line precedes the fenced Verification Command block.
func t55vDesc(prose, fenced string) string {
	var b strings.Builder
	b.WriteString("Implement the change.\n\n")
	if prose != "" {
		b.WriteString(prose + "\n\n")
	}
	b.WriteString("**Verification Command:**\n\n```sh\n" + fenced + "\n```\n")
	return b.String()
}

func TestVerificationCommandFor_FencedBlockAfterMarkerLine(t *testing.T) {
	desc := t55vDesc("- Run the exact test command: npm test", "npm test")
	bead := &types.Bead{ID: "t55v-1", Description: desc}
	cmd, ok := verificationCommandFor(bead)
	if !ok || cmd != "npm test" {
		t.Fatalf("got cmd=%q ok=%v, want \"npm test\" (fenced block must win over prose)", cmd, ok)
	}
}

func TestVerificationCommandFor_RejectsProseExtraction(t *testing.T) {
	desc := "Do the work.\n- Run the exact test command: npm test\n(no fenced block follows)"
	bead := &types.Bead{ID: "t55v-2", Description: desc}
	if cmd, ok := verificationCommandFor(bead); ok {
		t.Fatalf("got cmd=%q ok=%v, want no contract — prose with sentence markers must not be extracted", cmd, ok)
	}
}

func TestVerificationCommandFor_RejectsDashLedExtraction(t *testing.T) {
	desc := "Task.\nTest Command: some prose sentence with the word the inside"
	bead := &types.Bead{ID: "t55v-3", Description: desc}
	if cmd, ok := verificationCommandFor(bead); ok {
		t.Fatalf("got cmd=%q ok=%v, want no contract for prose", cmd, ok)
	}
}

func TestVerificationCommandFor_MetadataStillWins(t *testing.T) {
	desc := t55vDesc("- prose line", "npm run stale-test")
	bead := &types.Bead{ID: "t55v-4", Description: desc,
		Metadata: map[string]string{"test_command": "npm test"}}
	cmd, ok := verificationCommandFor(bead)
	if !ok || cmd != "npm test" {
		t.Fatalf("got cmd=%q ok=%v, want metadata test_command", cmd, ok)
	}
}

func TestVerificationCommandFor_BacktickedSameLine(t *testing.T) {
	desc := "Task.\nTest Command: `npm test`"
	bead := &types.Bead{ID: "t55v-5", Description: desc}
	cmd, ok := verificationCommandFor(bead)
	if !ok || cmd != "npm test" {
		t.Fatalf("got cmd=%q ok=%v, want npm test", cmd, ok)
	}
}

func TestVerificationCommandFor_EmptyDescriptionNoContract(t *testing.T) {
	bead := &types.Bead{ID: "t55v-6", Description: ""}
	if cmd, ok := verificationCommandFor(bead); ok {
		t.Fatalf("got cmd=%q ok=%v, want none", cmd, ok)
	}
}
