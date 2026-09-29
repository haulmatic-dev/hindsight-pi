package core

import (
	"errors"
	"strings"
	"testing"
)

// Regression coverage for the 2026-09-29 integration-exhausted storm (49
// srv-telematics beads hard-parked on `a pull request for branch ...
// already exists`). HEAD carries the resolveOrCreatePR implementation from
// the publisher PR-reuse reconciliation (hydra-6zwl); these tests pin the
// contract: create failure with "already exists" resolves the existing PR
// instead of looping to integration-exhausted.

func TestIsPRAlreadyExists(t *testing.T) {
	if !isPRAlreadyExists(errors.New(`a pull request for branch "dogfood/x" into branch "development" already exists: https://...`)) {
		t.Fatal("already-exists phrasing not detected")
	}
	for _, s := range []string{"", "No commits between", "gh pr create: exit status 1: other"} {
		if isPRAlreadyExists(errOrNil(s)) {
			t.Fatalf("false positive on %q", s)
		}
	}
	if isPRAlreadyExists(nil) {
		t.Fatal("nil error must not match")
	}
}

func errOrNil(s string) error {
	if s == "" {
		return nil
	}
	return errors.New(s)
}

func TestPrURLFromError(t *testing.T) {
	u := prURLFromError(errors.New(`a pull request for branch "dogfood/x" into branch "development" already exists:
https://github.com/o/r/pull/42`))
	if u != "https://github.com/o/r/pull/42" {
		t.Fatalf("prURLFromError = %q, want the embedded URL", u)
	}
	if prURLFromError(nil) != "" {
		t.Fatal("nil error must yield empty URL")
	}
}

func TestResolveOrCreatePR_AlreadyExistsReusesExistingPR(t *testing.T) {
	origCreate, origView := autoPRGhCreate, ghPRView
	defer func() { autoPRGhCreate, ghPRView = origCreate, origView }()

	autoPRGhCreate = func(string, string, string, string, string, string) (string, error) {
		return "", errors.New(`gh pr create: exit status 1: a pull request for branch "dogfood/x" into branch "development" already exists:
https://example/pr/1`)
	}
	viewCalled := false
	ghPRView = func(string, string, string) (string, error) {
		viewCalled = true
		return "https://example/pr/1 OPEN", nil
	}

	url, err := resolveOrCreatePR("/repo", "development", "dogfood/x", "t", "body", "tok")
	if err != nil {
		t.Fatalf("resolveOrCreatePR err = %v, want nil", err)
	}
	if url != "https://example/pr/1" {
		t.Fatalf("url = %q, want existing PR URL reused", url)
	}
	if viewCalled {
		t.Fatal("URL from create error must be used without a gh pr view round trip")
	}
}

func TestResolveOrCreatePR_LookupFailsClosed(t *testing.T) {
	origCreate, origView := autoPRGhCreate, ghPRView
	defer func() { autoPRGhCreate, ghPRView = origCreate, origView }()

	autoPRGhCreate = func(string, string, string, string, string, string) (string, error) {
		return "", errors.New("a pull request ... already exists")
	}
	ghPRView = func(string, string, string) (string, error) {
		return "", errors.New("gh pr view: not found")
	}

	_, err := resolveOrCreatePR("/repo", "development", "dogfood/x", "t", "body", "tok")
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("err = %v, want original already-exists error passthrough when lookup fails", err)
	}
}

func TestResolveOrCreatePR_ViewFallbackResolves(t *testing.T) {
	origCreate, origView := autoPRGhCreate, ghPRView
	defer func() { autoPRGhCreate, ghPRView = origCreate, origView }()

	// No URL embedded in the create error → fallback via ghPRView.
	autoPRGhCreate = func(string, string, string, string, string, string) (string, error) {
		return "", errors.New("gh pr create: exit status 1: a pull request for branch \"dogfood/x\" into branch \"development\" already exists")
	}
	ghPRView = func(string, string, string) (string, error) {
		return "https://example/pr/2 MERGED", nil
	}

	url, err := resolveOrCreatePR("/repo", "development", "dogfood/x", "t", "body", "tok")
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if url != "https://example/pr/2" {
		t.Fatalf("url = %q, want fallback-resolved PR URL", url)
	}
}
