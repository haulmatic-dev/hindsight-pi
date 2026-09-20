package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

func TestBuildPiCommand_ResumeUsesSession(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HYDRA_HOME", home)
	t.Setenv("HYDRA_RESUME_SESSION", "1")

	bead := types.Bead{ID: "hydra-resume-1", Project: "hydra"}
	sessionDir := filepath.Join(home, "sessions", "hydra-resume-1")
	if err := os.MkdirAll(sessionDir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sessionDir, "session.jsonl"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}

	cmd, _, _ := buildPiCommand(bead, PiConfig{}, "", nil)
	args := strings.Join(cmd.Args, " ")
	if !strings.Contains(args, "--session ") {
		t.Fatalf("expected --session for resumed session, got: %s", args)
	}
	if strings.Contains(args, "--session-id ") {
		t.Fatalf("expected --session-id to be absent, got: %s", args)
	}
}

func TestBuildPiCommand_NoResumeUsesSessionID(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HYDRA_HOME", home)
	// HYDRA_RESUME_SESSION not set.

	bead := types.Bead{ID: "hydra-fresh-1", Project: "hydra"}
	cmd, _, _ := buildPiCommand(bead, PiConfig{}, "", nil)
	args := strings.Join(cmd.Args, " ")
	if !strings.Contains(args, "--session-id ") {
		t.Fatalf("expected --session-id for fresh bead, got: %s", args)
	}
	if strings.Contains(args, "--session ") {
		t.Fatalf("expected --session to be absent, got: %s", args)
	}
}

func TestBuildPiPrompt_ResumeIncludesBoundary(t *testing.T) {
	t.Setenv("HYDRA_RESUME_SESSION", "1")
	bead := types.Bead{ID: "hydra-resume-2", Project: "hydra", Type: "task", Title: "test"}
	prompt := buildPiPrompt(bead, nil, promptExtras{})
	if !strings.Contains(prompt, "ATTEMPT BOUNDARY") {
		t.Fatalf("expected ATTEMPT BOUNDARY in resumed prompt, got:\n%s", prompt)
	}
}

func TestBuildPiPrompt_NoResumeOmitsBoundary(t *testing.T) {
	os.Unsetenv("HYDRA_RESUME_SESSION")
	bead := types.Bead{ID: "hydra-fresh-2", Project: "hydra", Type: "task", Title: "test"}
	prompt := buildPiPrompt(bead, nil, promptExtras{})
	if strings.Contains(prompt, "ATTEMPT BOUNDARY") {
		t.Fatalf("expected no ATTEMPT BOUNDARY in fresh prompt, got:\n%s", prompt)
	}
}
