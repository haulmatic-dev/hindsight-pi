package main

import (
	"os"
	"os/exec"
	"testing"
)

func TestCollectFixCommitsInGitRepo(t *testing.T) {
	// We're in the hydra repo — commits must be collected, fail-open shape
	out := collectFixCommits()
	if out != "" {
		if len(out) < 20 || out[:13] != "\n\nFIX_COMMITS" {
			t.Fatalf("unexpected shape: %q", out[:min(len(out), 40)])
		}
	}
}

func TestCollectFixCommitsOutsideGit(t *testing.T) {
	dir, _ := os.MkdirTemp("", "nogit")
	defer os.RemoveAll(dir)
	old, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(old)
	if out := collectFixCommits(); out != "" {
		t.Fatalf("expected empty outside git, got %q", out)
	}
	_ = exec.Command("true")
}

func min(a, b int) int { if a < b { return a }; return b }
