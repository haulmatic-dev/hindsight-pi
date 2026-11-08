package validation

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// beadEvidenceRequireImpl skips the contract tests loudly while the RED
// phase pins the contract (beadEvidenceImplemented == false). Setting
// BEAD_EVIDENCE_ENFORCE=1 turns the skips into failures so the RED gate
// proves the suite detects the missing contract.
func beadEvidenceRequireImpl(t *testing.T) {
	t.Helper()
	if beadEvidenceImplemented {
		return
	}
	if os.Getenv("BEAD_EVIDENCE_ENFORCE") == "1" {
		t.Fatalf("RED: per-bead evidence contract not yet implemented (BeadEvidence/ValidateTDDChainEvidence are inert stubs)")
	}
	t.Skip("RED: per-bead evidence contract not yet implemented (set BEAD_EVIDENCE_ENFORCE=1 to enforce)")
}

// initEvidenceRepo creates a temp git repo with an initial commit and
// returns its root.
func initEvidenceRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com",
			"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v failed: %v\n%s", args, err, out)
		}
	}
	run("init", "-q")
	run("config", "user.name", "test")
	run("config", "user.email", "test@example.com")
	return dir
}

// commitFiles stages and commits the given files with the given message.
func commitFiles(t *testing.T, dir, msg string, files map[string]string) {
	t.Helper()
	for rel, content := range files {
		abs := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com",
			"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v failed: %v\n%s", args, err, out)
		}
	}
	run("add", "-A")
	run("commit", "-q", "-m", msg)
}

func evidenceHasFile(t *testing.T, ev Evidence, rel string) bool {
	t.Helper()
	for _, f := range ev.Files {
		if f == rel {
			return true
		}
	}
	return false
}

func TestBeadEvidence_CommitSubjectMatchesBeadID(t *testing.T) {
	beadEvidenceRequireImpl(t)
	dir := initEvidenceRepo(t)
	commitFiles(t, dir, "test: x (hydra-ab12)", map[string]string{"pkg/a_test.go": "package pkg\n"})
	ev, err := BeadEvidence(dir, "hydra-ab12", false)
	if err != nil {
		t.Fatalf("BeadEvidence: %v", err)
	}
	if !evidenceHasFile(t, ev, "pkg/a_test.go") {
		t.Fatalf("commit subject (hydra-ab12) must be evidence for hydra-ab12; got files %v", ev.Files)
	}
}

func TestBeadEvidence_CommitBodyBeadLine(t *testing.T) {
	beadEvidenceRequireImpl(t)
	dir := initEvidenceRepo(t)
	commitFiles(t, dir, "fix: thing\n\nBead: hydra-ab12", map[string]string{"pkg/b.go": "package pkg\n"})
	ev, err := BeadEvidence(dir, "hydra-ab12", false)
	if err != nil {
		t.Fatalf("BeadEvidence: %v", err)
	}
	if !evidenceHasFile(t, ev, "pkg/b.go") {
		t.Fatalf("commit body `Bead: hydra-ab12` must be evidence for hydra-ab12; got files %v", ev.Files)
	}
}

func TestBeadEvidence_UnrelatedCommitNotEvidence(t *testing.T) {
	beadEvidenceRequireImpl(t)
	dir := initEvidenceRepo(t)
	commitFiles(t, dir, "chore: unrelated", map[string]string{"pkg/c.go": "package pkg\n"})
	ev, err := BeadEvidence(dir, "hydra-ab12", false)
	if err != nil {
		t.Fatalf("BeadEvidence: %v", err)
	}
	if evidenceHasFile(t, ev, "pkg/c.go") {
		t.Fatalf("unrelated commit must not be evidence for hydra-ab12; got files %v", ev.Files)
	}
}

func TestBeadEvidence_NoPrefixBleed(t *testing.T) {
	beadEvidenceRequireImpl(t)
	dir := initEvidenceRepo(t)
	commitFiles(t, dir, "test: x (hydra-ab123)", map[string]string{"pkg/d.go": "package pkg\n"})
	ev, err := BeadEvidence(dir, "hydra-ab12", false)
	if err != nil {
		t.Fatalf("BeadEvidence: %v", err)
	}
	if evidenceHasFile(t, ev, "pkg/d.go") {
		t.Fatalf("commit mentioning hydra-ab123 must NOT be evidence for hydra-ab12 (whole-word match); got files %v", ev.Files)
	}
}

func TestBeadEvidence_DirtySetOnlyWithIncludeDirty(t *testing.T) {
	beadEvidenceRequireImpl(t)
	dir := initEvidenceRepo(t)
	commitFiles(t, dir, "init", map[string]string{"pkg/tracked.go": "package pkg\n"})
	// Dirty: modified tracked file + untracked file inside a new directory.
	if err := os.WriteFile(filepath.Join(dir, "pkg/tracked.go"), []byte("package pkg // changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "pkg/newdir"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "pkg/newdir/untracked.go"), []byte("package newdir\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	clean, err := BeadEvidence(dir, "hydra-ab12", false)
	if err != nil {
		t.Fatalf("BeadEvidence(includeDirty=false): %v", err)
	}
	if evidenceHasFile(t, clean, "pkg/tracked.go") || evidenceHasFile(t, clean, "pkg/newdir/untracked.go") {
		t.Fatalf("dirty files must be excluded with includeDirty=false; got %v", clean.Files)
	}

	dirty, err := BeadEvidence(dir, "hydra-ab12", true)
	if err != nil {
		t.Fatalf("BeadEvidence(includeDirty=true): %v", err)
	}
	if !evidenceHasFile(t, dirty, "pkg/tracked.go") {
		t.Fatalf("modified tracked file missing from dirty evidence: %v", dirty.Files)
	}
	if !evidenceHasFile(t, dirty, "pkg/newdir/untracked.go") {
		t.Fatalf("untracked file in new directory missing from dirty evidence: %v", dirty.Files)
	}
}

func TestBeadEvidence_NonRepoYieldsEmptyEvidenceNilError(t *testing.T) {
	beadEvidenceRequireImpl(t)
	dir := t.TempDir() // no git init
	ev, err := BeadEvidence(dir, "hydra-ab12", true)
	if err != nil {
		t.Fatalf("non-repo dir must yield nil error, got %v", err)
	}
	if len(ev.Files) != 0 {
		t.Fatalf("non-repo dir must yield empty evidence, got %v", ev.Files)
	}
}

func TestBeadEvidence_FingerprintDeterministic(t *testing.T) {
	beadEvidenceRequireImpl(t)
	dir := initEvidenceRepo(t)
	commitFiles(t, dir, "test: x (hydra-ab12)", map[string]string{"pkg/a_test.go": "package pkg\n"})

	ev1, err := BeadEvidence(dir, "hydra-ab12", false)
	if err != nil {
		t.Fatalf("BeadEvidence: %v", err)
	}
	if len(ev1.Fingerprint) != 64 {
		t.Fatalf("fingerprint must be sha256 hex (64 chars), got %q", ev1.Fingerprint)
	}
	ev2, err := BeadEvidence(dir, "hydra-ab12", false)
	if err != nil {
		t.Fatalf("BeadEvidence second call: %v", err)
	}
	if ev1.Fingerprint != ev2.Fingerprint {
		t.Fatalf("fingerprint not deterministic: %q vs %q", ev1.Fingerprint, ev2.Fingerprint)
	}

	commitFiles(t, dir, "test: x (hydra-ab12)", map[string]string{"pkg/a_test.go": "package pkg // v2\n"})
	ev3, err := BeadEvidence(dir, "hydra-ab12", false)
	if err != nil {
		t.Fatalf("BeadEvidence after change: %v", err)
	}
	if ev3.Fingerprint == ev1.Fingerprint {
		t.Fatalf("fingerprint must change when committed content changes")
	}
}

// grepWholeWord mirrors the whole-word matching contract: the bead id must be
// followed by a non-alphanumeric or end of line so hydra-ab12 never matches
// hydra-ab123. It is asserted via the regex contract used by BeadEvidence.
func TestBeadEvidence_WholeWordRegexShape(t *testing.T) {
	beadEvidenceRequireImpl(t)
	re, err := regexp.Compile(regexp.QuoteMeta("hydra-ab12") + `([^[:alnum:]]|$)`)
	if err != nil {
		t.Fatal(err)
	}
	if !re.MatchString("test: x (hydra-ab12)\n") {
		t.Fatal("subject form must match")
	}
	if !re.MatchString("Bead: hydra-ab12\n") {
		t.Fatal("body form must match")
	}
	if re.MatchString("test: x (hydra-ab123)") {
		t.Fatal("prefix bleed must not match")
	}
	if strings.Contains("", "never") {
		t.Fatal("unreachable")
	}
}
