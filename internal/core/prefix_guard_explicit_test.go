package core

import (
	"os"
	"path/filepath"
	"testing"
)

// srv-jobfile regression (2026-10-05): PrefixGuard renamed a mid-life store's
// issue_prefix from "srv-jobfile" to "ajantha-srv-jobfile" because the current
// value merely equaled the repo basename — indistinguishable from br's default
// via `br config get`. Every bead filed after the rename (including Pan's
// wedge triage beads) carried the new prefix while 38 established beads kept
// the old one, splitting the store's ID namespace.

func guardTestDir(t *testing.T) (string, *fakeBrRunner, *PrefixGuard) {
	t.Helper()
	base := t.TempDir()
	dir := filepath.Join(base, "srv-jobfile") // basename drives defaultPrefix
	if err := os.MkdirAll(filepath.Join(dir, ".beads"), 0755); err != nil {
		t.Fatal(err)
	}
	runner := newFakeBrRunner()
	g := NewPrefixGuard()
	g.runner = runner
	return dir, runner, g
}

func TestPrefixGuard_ExplicitPrefixMatchingBasenamePreserved(t *testing.T) {
	dir, runner, g := guardTestDir(t)
	cfg := "# Beads Project Configuration\nissue_prefix: srv-jobfile\n"
	if err := os.WriteFile(filepath.Join(dir, ".beads", "config.yaml"), []byte(cfg), 0644); err != nil {
		t.Fatal(err)
	}
	runner.values[dir] = "srv-jobfile" // br default == explicit value

	g.guardRepo(dir, "ajantha", "srv-jobfile", "")

	if len(runner.setCalls) != 0 {
		t.Errorf("explicit prefix was renamed mid-store; sets = %v", runner.sets)
	}
}

func TestPrefixGuard_CommentedDefaultPrefixRenamed(t *testing.T) {
	// br writes defaults as a commented line (nipuna.j webapp store) — that is
	// a true default and safe to namespace.
	dir, runner, g := guardTestDir(t)
	cfg := "# Beads Project Configuration\n# issue_prefix: srv-jobfile\n# default_type: task\n"
	if err := os.WriteFile(filepath.Join(dir, ".beads", "config.yaml"), []byte(cfg), 0644); err != nil {
		t.Fatal(err)
	}
	runner.values[dir] = "srv-jobfile"

	g.guardRepo(dir, "ajantha", "srv-jobfile", "")

	if got := runner.sets[dir]; got != "ajantha-srv-jobfile" {
		t.Errorf("set issue_prefix = %q, want ajantha-srv-jobfile", got)
	}
}

func TestPrefixGuard_MissingConfigStillRenamesDefault(t *testing.T) {
	dir, runner, g := guardTestDir(t)
	runner.values[dir] = "srv-jobfile"

	g.guardRepo(dir, "ajantha", "srv-jobfile", "")

	if got := runner.sets[dir]; got != "ajantha-srv-jobfile" {
		t.Errorf("set issue_prefix = %q, want ajantha-srv-jobfile", got)
	}
}

func TestPrefixGuard_UnreadableConfigPreservesEstablishedPrefix(t *testing.T) {
	// Broken store (WAL poison made `br config get` flap): never gamble a
	// mid-life rename on unreadable state.
	dir, runner, g := guardTestDir(t)
	if err := os.MkdirAll(filepath.Join(dir, ".beads", "config.yaml"), 0755); err != nil {
		t.Fatal(err)
	}
	runner.values[dir] = "srv-jobfile"

	g.guardRepo(dir, "ajantha", "srv-jobfile", "")

	if len(runner.setCalls) != 0 {
		t.Errorf("unreadable config must preserve the prefix; sets = %v", runner.sets)
	}
}

func TestStorePrefixExplicit(t *testing.T) {
	dir := t.TempDir()
	g := NewPrefixGuard()
	read := func(content string, want bool, wantErr bool) {
		t.Helper()
		g.rawConfig = func(string) ([]byte, error) {
			if wantErr {
				return nil, os.ErrPermission
			}
			return []byte(content), nil
		}
		got, err := g.storePrefixExplicit(dir)
		if wantErr {
			if err == nil {
				t.Fatalf("expected error for %q", content)
			}
			return
		}
		if err != nil {
			t.Fatalf("unexpected error for %q: %v", content, err)
		}
		if got != want {
			t.Errorf("storePrefixExplicit(%q) = %v, want %v", content, got, want)
		}
	}
	read("issue_prefix: foo\n", true, false)
	read("  issue_prefix:   foo\n", true, false)
	read("# issue_prefix: foo\n", false, false)
	read("default_type: task\n", false, false)
	read("", false, false)
	read("", false, true)
}
