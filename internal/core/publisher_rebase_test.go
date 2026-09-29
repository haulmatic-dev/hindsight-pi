package core

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func withTestRebaseVars(t *testing.T, rebaseCalls *int, rebaseErr *error) {
	t.Helper()
	origCheckout := rebaseCheckoutOntoBase
	origBranch := rebaseBranchOntoBase
	origSink := publishEvents
	rebaseCheckoutOntoBase = func(projectPath, base string) error {
		*rebaseCalls++
		if rebaseErr != nil {
			return *rebaseErr
		}
		return nil
	}
	rebaseBranchOntoBase = func(projectPath, branch, base string) error {
		*rebaseCalls++
		if rebaseErr != nil {
			return *rebaseErr
		}
		return nil
	}
	publishEvents = NewEventSink("")
	t.Cleanup(func() {
		rebaseCheckoutOntoBase = origCheckout
		rebaseBranchOntoBase = origBranch
		publishEvents = origSink
	})
}

func TestPRConflictErrorDetection(t *testing.T) {
	cases := map[string]bool{
		"pr merge: merge head: gh pr merge: exit 1\nPull request is not mergeable": true,
		"branch has conflicts that must be resolved":                               true,
		"the merge state is unstable":                                              false, // not a conflict phrase
		"gh pr create: exit 4":                                                     false,
	}
	// fix the "mergeable state" case explicitly
	cases["pr merge: merge head: mergeable state is unstable"] = true
	for msg, want := range cases {
		var err error
		if msg != "nil" {
			err = errors.New(msg)
		}
		if got := prConflictError(err); got != want {
			t.Errorf("prConflictError(%q) = %v, want %v", msg, got, want)
		}
	}
	if prConflictError(nil) {
		t.Error("prConflictError(nil) = true, want false")
	}
}

func TestPublishWithRebaseRetry_ConflictRebasesAndRetries(t *testing.T) {
	attempts := 0
	rebaseCalls := 0
	url, err := publishWithRebaseRetry(
		func() (string, error) {
			attempts++
			if attempts == 1 {
				return "", errors.New("pr merge: merge head: Pull request is not mergeable")
			}
			return "https://example/pr/1", nil
		},
		func() error { rebaseCalls++; return nil },
		"/tmp/proj", "main")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if url != "https://example/pr/1" {
		t.Errorf("url = %q", url)
	}
	if attempts != 2 || rebaseCalls != 1 {
		t.Errorf("attempts=%d rebaseCalls=%d, want 2/1", attempts, rebaseCalls)
	}
}

func TestPublishWithRebaseRetry_CapsAtTwoThenHuman(t *testing.T) {
	attempts := 0
	rebaseCalls := 0
	_, err := publishWithRebaseRetry(
		func() (string, error) {
			attempts++
			return "", fmt.Errorf("pr merge: merge head: gh pr merge: conflicts")
		},
		func() error { rebaseCalls++; return nil },
		"/tmp/proj", "main")
	if err == nil {
		t.Fatal("expected error after cap")
	}
	if !strings.Contains(err.Error(), "human review needed") {
		t.Errorf("error missing human escalation: %v", err)
	}
	if rebaseCalls != MaxPRRebaseAttempts {
		t.Errorf("rebaseCalls=%d, want %d", rebaseCalls, MaxPRRebaseAttempts)
	}
	if attempts != MaxPRRebaseAttempts+1 {
		t.Errorf("attempts=%d, want %d", attempts, MaxPRRebaseAttempts+1)
	}
}

func TestPublishWithRebaseRetry_FailedRebaseEscalatesImmediately(t *testing.T) {
	attempts := 0
	rebaseCalls := 0
	_, err := publishWithRebaseRetry(
		func() (string, error) {
			attempts++
			return "", errors.New("pr merge: merge head: not mergeable: conflicts")
		},
		func() error { rebaseCalls++; return errors.New("rebase main onto origin/main: CONFLICT (content)") },
		"/tmp/proj", "main")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "human review needed") {
		t.Errorf("error missing human escalation: %v", err)
	}
	if attempts != 1 || rebaseCalls != 1 {
		t.Errorf("attempts=%d rebaseCalls=%d, want 1/1 (no retry after failed rebase)", attempts, rebaseCalls)
	}
}

func TestPublishWithRebaseRetry_NonConflictErrorNotRetried(t *testing.T) {
	attempts := 0
	rebaseCalls := 0
	_, err := publishWithRebaseRetry(
		func() (string, error) {
			attempts++
			return "", errors.New("gh pr create: exit 4")
		},
		func() error { rebaseCalls++; return nil },
		"/tmp/proj", "main")
	if err == nil || strings.Contains(err.Error(), "human review") {
		t.Fatalf("non-conflict error must propagate unchanged, got: %v", err)
	}
	if attempts != 1 || rebaseCalls != 0 {
		t.Errorf("attempts=%d rebaseCalls=%d, want 1/0", attempts, rebaseCalls)
	}
}

func TestPublishBranchViaPR_RebaseRetryOnConflict(t *testing.T) {
	rebaseCalls := 0
	withTestRebaseVars(t, &rebaseCalls, nil)

	origPush := prMergeBranchPush
	origCreate := autoPRGhCreate
	origMerge := autoPRGhMerge
	origToken := prMergeResolveToken
	attempts := 0
	prMergeBranchPush = func(repoPath, localRef, ref, token string) error { return nil }
	autoPRGhCreate = func(repoPath, base, head, title, body, token string) (string, error) {
		return "https://example/pr/2", nil
	}
	autoPRGhMerge = func(repoPath, head, token string) error {
		attempts++
		if attempts == 1 {
			return fmt.Errorf("gh pr merge: Pull request is not mergeable")
		}
		return nil
	}
	prMergeResolveToken = func() string { return "" }
	t.Cleanup(func() {
		prMergeBranchPush = origPush
		autoPRGhCreate = origCreate
		autoPRGhMerge = origMerge
		prMergeResolveToken = origToken
	})

	url, err := PublishBranchViaPR("/tmp/proj", "main", "hydra/wt-abc", "dogfood/x", "title", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if url != "https://example/pr/2" {
		t.Errorf("url = %q", url)
	}
	if rebaseCalls != 1 {
		t.Errorf("rebaseCalls=%d, want 1", rebaseCalls)
	}
}

func TestPublishBranchViaPR_RebaseExhaustedEscalates(t *testing.T) {
	rebaseCalls := 0
	withTestRebaseVars(t, &rebaseCalls, nil)

	origPush := prMergeBranchPush
	origCreate := autoPRGhCreate
	origMerge := autoPRGhMerge
	prMergeBranchPush = func(repoPath, localRef, ref, token string) error { return nil }
	autoPRGhCreate = func(repoPath, base, head, title, body, token string) (string, error) {
		return "https://example/pr/3", nil
	}
	autoPRGhMerge = func(repoPath, head, token string) error {
		return errors.New("gh pr merge: Pull request is not mergeable")
	}
	t.Cleanup(func() {
		prMergeBranchPush = origPush
		autoPRGhCreate = origCreate
		autoPRGhMerge = origMerge
	})

	_, err := PublishBranchViaPR("/tmp/proj", "main", "hydra/wt-abc", "dogfood/x", "title", "")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "human review needed") {
		t.Errorf("error missing human escalation: %v", err)
	}
	if rebaseCalls != MaxPRRebaseAttempts {
		t.Errorf("rebaseCalls=%d, want %d", rebaseCalls, MaxPRRebaseAttempts)
	}
}

func TestDefaultRebaseBranchOntoBase_RealGit(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	run := func(args ...string) string {
		t.Helper()
		out, err := gitOut(dir, args...)
		if err != nil {
			t.Fatalf("git %v: %v", args, err)
		}
		return out
	}
	run("init", "-q", "-b", "main", ".")
	run("config", "user.email", "t@t")
	run("config", "user.name", "t")
	writeFile := func(name, content string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writeFile("f.txt", "base\n")
	run("add", "-A")
	run("commit", "-qm", "base")
	// remote: advance main
	run("clone", "-q", ".", dir+"/../origin-clone")
	writeFile("g.txt", "other\n")
	run("add", "-A")
	run("commit", "-qm", "other")
	// branch with its own commit, diverging
	run("checkout", "-qb", "feat")
	writeFile("f.txt", "base+feat\n")
	run("commit", "-qam", "feat")
	// add origin pointing at the clone's main
	if _, err := gitOut(dir, "remote", "add", "origin", filepath.Join(dir, "..", "origin-clone")); err != nil {
		t.Fatal(err)
	}
	if _, err := gitOut(dir, "fetch", "-q", "origin"); err != nil {
		t.Fatal(err)
	}
	if _, err := gitOut(filepath.Join(dir, "..", "origin-clone"), "config", "user.email", "t@t"); err != nil {
		t.Fatal(err)
	}
	if _, err := gitOut(filepath.Join(dir, "..", "origin-clone"), "config", "user.name", "t"); err != nil {
		t.Fatal(err)
	}

	// clean rebase: origin/main has no overlapping change to f.txt yet
	if err := defaultRebaseBranchOntoBase(dir, "feat", "main"); err != nil {
		t.Fatalf("clean rebase failed: %v", err)
	}
	// branch must now contain origin/main
	if !isAncestorOf(dir, "origin/main", "feat") {
		t.Errorf("feat does not contain origin/main after rebase (origin/main=%s feat=%s)",
			strings.TrimSpace(run("rev-parse", "origin/main")), strings.TrimSpace(run("rev-parse", "feat")))
	}
}
