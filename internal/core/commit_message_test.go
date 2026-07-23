package core

import (
	"strings"
	"testing"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

func TestBuildCommitMessage(t *testing.T) {
	t.Parallel()

	longTitle := strings.Repeat("a", 200)

	tests := []struct {
		name                string
		bead                types.Bead
		wantFirstLine       string
		wantContains        []string
		wantNotContains     []string
		wantMaxFirstLineLen int
	}{
		{
			name: "GREEN prefix wins over implement type",
			bead: types.Bead{
				ID:    "p-1",
				Title: "GREEN: Implement the resolver.",
				Type:  "implement",
			},
			wantFirstLine: "feat: implement the resolver",
			wantContains:  []string{"Bead: p-1"},
		},
		{
			name: "FIX prefix maps to fix",
			bead: types.Bead{
				Title: "FIX: Handle nil client",
				Type:  "fix",
			},
			wantFirstLine: "fix: handle nil client",
		},
		{
			name: "REFACTOR prefix wins over task type",
			bead: types.Bead{
				Title: "REFACTOR: Extract helper",
				Type:  "task",
			},
			wantFirstLine: "refactor: extract helper",
		},
		{
			name: "unknown SYNTAX prefix falls back to type",
			bead: types.Bead{
				Title: "SYNTAX: Check build",
				Type:  "task",
			},
			wantFirstLine: "chore: check build",
		},
		{
			name: "description and prd path are included in body",
			bead: types.Bead{
				ID:          "hydra-xyz",
				Title:       "GREEN: Add feature",
				Type:        "implement",
				Description: "Adds X.",
				Metadata:    map[string]string{"prd_path": "docs/PRDs/foo.md"},
			},
			wantFirstLine: "feat: add feature",
			wantContains:  []string{"Adds X.", "PRD: docs/PRDs/foo.md", "Bead: hydra-xyz"},
		},
		{
			name: "missing prd path produces no PRD line",
			bead: types.Bead{
				Title:       "GREEN: Add feature",
				Type:        "implement",
				Description: "Adds X.",
			},
			wantFirstLine:   "feat: add feature",
			wantContains:    []string{"Adds X."},
			wantNotContains: []string{"PRD:"},
		},
		{
			name: "long title is truncated to 72 characters",
			bead: types.Bead{
				Title: longTitle,
				Type:  "task",
			},
			wantMaxFirstLineLen: 72,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := BuildCommitMessage(tt.bead)
			lines := strings.Split(got, "\n")
			first := lines[0]

			if tt.wantFirstLine != "" && first != tt.wantFirstLine {
				t.Errorf("first line = %q, want %q", first, tt.wantFirstLine)
			}
			if tt.wantMaxFirstLineLen > 0 && len(first) > tt.wantMaxFirstLineLen {
				t.Errorf("first line length = %d, want at most %d\nline: %q", len(first), tt.wantMaxFirstLineLen, first)
			}
			for _, want := range tt.wantContains {
				if !strings.Contains(got, want) {
					t.Errorf("message missing %q\nmessage:\n%s", want, got)
				}
			}
			for _, notWant := range tt.wantNotContains {
				if strings.Contains(got, notWant) {
					t.Errorf("message should not contain %q\nmessage:\n%s", notWant, got)
				}
			}
		})
	}
}
