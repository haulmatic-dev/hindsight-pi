package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/haulmatic-dev/hydra/internal/registry"
	"github.com/haulmatic-dev/hydra/pkg/types"
)

func TestBuildPiSession_DeterministicAndSafe(t *testing.T) {
	bead := types.Bead{ID: "hydra/zx-abc123"}
	hydraHome := t.TempDir()
	sid, sdir, err := buildPiSession(bead, hydraHome)
	if err != nil {
		t.Fatalf("buildPiSession: %v", err)
	}
	if !strings.Contains(sid, "hydra-zx-abc123") {
		t.Errorf("session id should be derived from bead id, got %q", sid)
	}
	if strings.Contains(sid, "/") {
		t.Errorf("session id must be a valid file name, got slash: %q", sid)
	}
	wantDir := filepath.Join(hydraHome, "sessions", sid)
	if sdir != wantDir {
		t.Errorf("session dir = %q, want %q", sdir, wantDir)
	}

	// Second call with the same bead must produce the same id/dir so a
	// respawned worker can resume the same session files.
	sid2, sdir2, err := buildPiSession(bead, hydraHome)
	if err != nil {
		t.Fatalf("buildPiSession second: %v", err)
	}
	if sid2 != sid || sdir2 != sdir {
		t.Errorf("session not deterministic: %q/%q vs %q/%q", sid2, sdir2, sid, sdir)
	}
}

func TestBuildPiCommand_UsesNamedSession(t *testing.T) {
	bead := types.Bead{ID: "hydra-zx-abc123"}
	piConfig := PiConfig{Enabled: true, Provider: "kimi-coding", Model: "kimi-for-coding"}
	hydraHome := t.TempDir()
	t.Setenv("HYDRA_HOME", hydraHome)

	oldReporter := activeReporter
	activeReporter = newStateReporter(registry.NewClient("http://127.0.0.1:8089"), "", "")
	defer func() { activeReporter = oldReporter }()

	cmd, sid, sdir := buildPiCommand(bead, piConfig, "", nil)
	if sid == "" || sdir == "" {
		t.Fatalf("expected session id/dir, got %q/%q", sid, sdir)
	}
	got := strings.Join(cmd.Args, " ")
	for _, want := range []string{"--session-id", "--session-dir", sid, sdir} {
		if !strings.Contains(got, want) {
			t.Errorf("command missing %q; got %q", want, got)
		}
	}
	// --session resumes an EXISTING session and pi exits 1 with
	// "No session found matching" for a fresh bead. --session-id creates the
	// named session when missing. Assert the exact token, not a substring:
	// "--session-id" itself contains "--session".
	for _, a := range cmd.Args {
		if a == "--session" {
			t.Errorf("must not use --session (resume-only); use --session-id so fresh beads create the session")
		}
	}
	if strings.Contains(got, "--no-session") {
		t.Errorf("command should not use --no-session; got %q", got)
	}
	if _, err := os.Stat(sdir); err != nil {
		t.Errorf("session dir not created: %v", err)
	}
	envOK := false
	for _, e := range cmd.Env {
		if e == "PI_CODING_AGENT_SESSION_DIR="+sdir {
			envOK = true
			break
		}
	}
	if !envOK {
		t.Errorf("PI_CODING_AGENT_SESSION_DIR not set correctly in env")
	}
	if activeReporter.rep.PiSessionID != sid {
		t.Errorf("reporter PiSessionID = %q, want %q", activeReporter.rep.PiSessionID, sid)
	}
}
