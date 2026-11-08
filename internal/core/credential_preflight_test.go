package core

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

func TestClassifyCredentialError(t *testing.T) {
	cases := []struct {
		msg  string
		want bool
	}{
		{"fatal: Authentication failed for 'https://github.com/o/r.git'", true},
		{"ERROR: Repository not found", true},
		{"fatal: could not read Username for 'https://github.com': terminal prompts disabled", true},
		{"fatal: unable to access 'https://github.com/o/r.git/': Could not resolve host: github.com", false},
		{"", false},
	}
	for _, c := range cases {
		if got := classifyCredentialError(c.msg); got != c.want {
			t.Errorf("classify(%q) = %v, want %v", c.msg, got, c.want)
		}
	}
}

// sinkHasEvent reads the sink's central stream for today and reports whether
// an event of the given type was emitted.
func sinkHasEvent(sink *EventSink, typ string) bool {
	path := filepath.Join(sink.centralDir, time.Now().UTC().Format("2006-01-02")+".jsonl")
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	return strings.Contains(string(data), "\"type\":\""+typ+"\"")
}

// Overseer preflight: identity failure and stale probe emit the typed event;
// network failure fails open. registryStore nil => park skipped (logged).
func TestPreflightOneRepo(t *testing.T) {
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".git", "objects"), 0o755); err != nil {
		t.Fatal(err)
	}
	project := types.Project{Name: "p", Path: repo, Type: types.RepoTypeSingle}

	origProbe := credentialProbe
	defer func() { credentialProbe = origProbe }()
	origResolver := publishIdentityResolver
	defer func() { publishIdentityResolver = origResolver }()

	t.Run("identity unresolved emits credential_stale", func(t *testing.T) {
		publishIdentityResolver = func(string) (PublishIdentity, error) {
			return PublishIdentity{}, errors.New("publish identity missing: boom")
		}
		sink := NewEventSink(t.TempDir())
		o := &Overseer{eventSink: sink}
		o.preflightOneRepo(project, repo)
		if !sinkHasEvent(sink, "credential_stale") {
			t.Fatal("expected credential_stale event")
		}
	})

	t.Run("stale probe emits credential_stale", func(t *testing.T) {
		publishIdentityResolver = func(string) (PublishIdentity, error) {
			return PublishIdentity{Owner: "hydra", Token: "tok", Source: "owner-pat-file"}, nil
		}
		credentialProbe = func(repoPath, token string) error {
			return errors.New("ls-remote: fatal: Repository not found")
		}
		sink := NewEventSink(t.TempDir())
		o := &Overseer{eventSink: sink}
		o.preflightOneRepo(project, repo)
		if !sinkHasEvent(sink, "credential_stale") {
			t.Fatal("expected credential_stale event")
		}
	})

	t.Run("network probe fails open", func(t *testing.T) {
		publishIdentityResolver = func(string) (PublishIdentity, error) {
			return PublishIdentity{Owner: "hydra", Token: "tok"}, nil
		}
		credentialProbe = func(repoPath, token string) error {
			return errors.New("ls-remote: Could not resolve host")
		}
		sink := NewEventSink(t.TempDir())
		o := &Overseer{eventSink: sink}
		o.preflightOneRepo(project, repo)
		if sinkHasEvent(sink, "credential_stale") {
			t.Fatal("network failure must fail open (no credential_stale)")
		}
	})
}

func TestRunCredentialPreflight_SkipsNonGitDir(t *testing.T) {
	o := &Overseer{config: &Config{Projects: []ProjectConfig{{Name: "p", Path: t.TempDir()}}}}
	o.RunCredentialPreflight() // must not panic; nothing probed
}
