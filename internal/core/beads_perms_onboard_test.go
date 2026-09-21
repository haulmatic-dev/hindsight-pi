package core

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// requireBeadsGroup skips the test on hosts without the beads group or when
// the test user cannot chgrp to it (both hold on the hydra daemon host).
func requireBeadsGroup(t *testing.T) {
	t.Helper()
	if beadsGroupGID < 0 {
		t.Skip("beads group not available on this host")
	}
	if err := os.Chown(t.TempDir(), -1, beadsGroupGID); err != nil {
		t.Skipf("cannot chgrp to beads group: %v", err)
	}
}

func statGid(t *testing.T, path string) int {
	t.Helper()
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		t.Fatalf("no syscall.Stat_t for %s", path)
	}
	return int(st.Gid)
}

func TestOnboardBeadsPerms_FreshRepoGetsPerms(t *testing.T) {
	requireBeadsGroup(t)
	repo := t.TempDir()
	beadsDir := filepath.Join(repo, ".beads")
	// Simulate a fresh `br init` in a user-owned repo: locked-down .beads and
	// a 0600 issues.jsonl (br init's default), primary group = user's own.
	if err := os.Mkdir(beadsDir, 0o700); err != nil {
		t.Fatal(err)
	}
	jsonl := filepath.Join(beadsDir, "issues.jsonl")
	if err := os.WriteFile(jsonl, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := onboardBeadsPerms(repo); err != nil {
		t.Fatalf("onboardBeadsPerms: %v", err)
	}

	fi, err := os.Stat(beadsDir)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode()&os.ModeSetgid == 0 {
		t.Errorf(".beads mode %o: setgid bit missing", fi.Mode().Perm())
	}
	if perm := fi.Mode().Perm(); perm&0o070 != 0o070 || perm&0o007 != 0 {
		t.Errorf(".beads perm = %o, want group rwx, other none", perm)
	}
	if gid := statGid(t, beadsDir); gid != beadsGroupGID {
		t.Errorf(".beads gid = %d, want beads group %d", gid, beadsGroupGID)
	}

	jfi, err := os.Stat(jsonl)
	if err != nil {
		t.Fatal(err)
	}
	if perm := jfi.Mode().Perm(); perm&0o060 != 0o060 || perm&0o007 != 0 {
		t.Errorf("issues.jsonl perm = %o, want group rw, other none", perm)
	}
	if gid := statGid(t, jsonl); gid != beadsGroupGID {
		t.Errorf("issues.jsonl gid = %d, want beads group %d", gid, beadsGroupGID)
	}
}

func TestOnboardBeadsPerms_AlreadyCorrectRepoUntouched(t *testing.T) {
	requireBeadsGroup(t)
	repo := t.TempDir()
	beadsDir := filepath.Join(repo, ".beads")
	if err := os.Mkdir(beadsDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(beadsDir, 0o770|os.ModeSetgid); err != nil {
		t.Fatal(err)
	}
	if err := os.Chown(beadsDir, -1, beadsGroupGID); err != nil {
		t.Fatal(err)
	}
	jsonl := filepath.Join(beadsDir, "issues.jsonl")
	if err := os.WriteFile(jsonl, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(jsonl, 0o0660); err != nil {
		t.Fatal(err)
	}
	if err := os.Chown(jsonl, -1, beadsGroupGID); err != nil {
		t.Fatal(err)
	}

	beforeDir, _ := os.Stat(beadsDir)
	beforeJSONL, _ := os.Stat(jsonl)

	if err := onboardBeadsPerms(repo); err != nil {
		t.Fatalf("onboardBeadsPerms: %v", err)
	}

	afterDir, _ := os.Stat(beadsDir)
	afterJSONL, _ := os.Stat(jsonl)
	if !afterDir.ModTime().Equal(beforeDir.ModTime()) {
		t.Errorf("conforming .beads was mutated (mtime changed)")
	}
	if !afterJSONL.ModTime().Equal(beforeJSONL.ModTime()) {
		t.Errorf("conforming issues.jsonl was mutated (mtime changed)")
	}
	if afterDir.Mode() != beforeDir.Mode() {
		t.Errorf(".beads mode changed: %v -> %v", beforeDir.Mode(), afterDir.Mode())
	}
}

func TestOnboardBeadsPerms_RepairsBrInit0600JSONL(t *testing.T) {
	requireBeadsGroup(t)
	repo := t.TempDir()
	beadsDir := filepath.Join(repo, ".beads")
	// Correct dir, but issues.jsonl re-created 0600 by a fresh `br init`
	// (chmod 0600 zeroes the ACL mask, so group access silently dies).
	if err := os.Mkdir(beadsDir, 0o770|os.ModeSetgid); err != nil {
		t.Fatal(err)
	}
	if err := os.Chown(beadsDir, -1, beadsGroupGID); err != nil {
		t.Fatal(err)
	}
	jsonl := filepath.Join(beadsDir, "issues.jsonl")
	if err := os.WriteFile(jsonl, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := onboardBeadsPerms(repo); err != nil {
		t.Fatalf("onboardBeadsPerms: %v", err)
	}

	jfi, err := os.Stat(jsonl)
	if err != nil {
		t.Fatal(err)
	}
	if perm := jfi.Mode().Perm(); perm&0o060 != 0o060 {
		t.Errorf("issues.jsonl perm = %o, want group rw", perm)
	}
	if gid := statGid(t, jsonl); gid != beadsGroupGID {
		t.Errorf("issues.jsonl gid = %d, want beads group %d", gid, beadsGroupGID)
	}
}

func TestOnboardBeadsPerms_NoBeadsStoreIsNoOp(t *testing.T) {
	repo := t.TempDir() // no .beads at all
	if err := onboardBeadsPerms(repo); err != nil {
		t.Fatalf("onboardBeadsPerms on repo without .beads: %v", err)
	}
}

func TestMultiUserDiscovery_DiscoveryOnboardsPerms(t *testing.T) {
	requireBeadsGroup(t)
	dir := t.TempDir()
	projectPath := filepath.Join(dir, "my-platform")
	repoDir := filepath.Join(projectPath, "backend")
	beadsDir := filepath.Join(repoDir, ".beads")
	if err := os.MkdirAll(beadsDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(beadsDir, "issues.jsonl"), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	ctx := &UserContext{Username: "alice", HomeDir: dir}
	d := NewMultiUserDiscovery()
	d.prefixGuard.runner = newFakeBrRunner()

	project := d.convertProject(UserProject{
		Name: "my-platform",
		Path: projectPath,
		Type: "multi",
	}, ctx)
	if project == nil {
		t.Fatal("project conversion returned nil")
	}

	fi, err := os.Stat(beadsDir)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode()&os.ModeSetgid == 0 {
		t.Errorf("discovered repo .beads mode %o: setgid missing after discovery", fi.Mode().Perm())
	}
	if gid := statGid(t, beadsDir); gid != beadsGroupGID {
		t.Errorf("discovered repo .beads gid = %d, want beads group %d", gid, beadsGroupGID)
	}
	jfi, err := os.Stat(filepath.Join(beadsDir, "issues.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if perm := jfi.Mode().Perm(); perm&0o060 != 0o060 {
		t.Errorf("discovered repo issues.jsonl perm = %o, want group rw", perm)
	}
}
