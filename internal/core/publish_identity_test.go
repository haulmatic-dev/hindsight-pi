package core

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

func TestReadOwnerPATFile(t *testing.T) {
	dir := t.TempDir()
	if got := readOwnerPATFile(dir); got != "" {
		t.Fatalf("missing file: got %q", got)
	}
	hydra := filepath.Join(dir, ".hydra")
	if err := os.MkdirAll(hydra, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(hydra, ".github-token"), []byte("  ghp_owner  \n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := readOwnerPATFile(dir); got != "ghp_owner" {
		t.Fatalf("got %q, want ghp_owner", got)
	}
}

func TestResolvePublishIdentity_OwnerPATFailClosed(t *testing.T) {
	uc := &UserContext{Username: "ajantha", HomeDir: t.TempDir()} // no PAT file
	_, err := ResolvePublishIdentity("/home/ajantha/proj", uc)
	if !errors.Is(err, ErrPublishIdentityMissing) {
		t.Fatalf("err = %v, want ErrPublishIdentityMissing", err)
	}

	hydra := filepath.Join(uc.HomeDir, ".hydra")
	os.MkdirAll(hydra, 0o700)
	os.WriteFile(filepath.Join(hydra, ".github-token"), []byte("ghp_ajantha"), 0o600)
	id, err := ResolvePublishIdentity("/home/ajantha/proj", uc)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if id.Owner != "ajantha" || id.Token != "ghp_ajantha" || id.Source != "owner-pat-file" {
		t.Fatalf("identity = %+v", id)
	}
}

func TestResolveDaemonPublishIdentity_GhTokenEnv(t *testing.T) {
	t.Setenv("GH_TOKEN", "ghp_daemon")
	id, err := ResolveDaemonPublishIdentity()
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if id.Owner != "hydra" || id.Token != "ghp_daemon" || id.Source != "gh-token-env" {
		t.Fatalf("identity = %+v", id)
	}
}

func TestPublishViaPR_IdentityMissingFailsClosed(t *testing.T) {
	orig := publishIdentityResolver
	publishIdentityResolver = func(string) (PublishIdentity, error) {
		return PublishIdentity{Owner: "ajantha"}, ErrPublishIdentityMissing
	}
	defer func() { publishIdentityResolver = orig }()

	_, err := PublishViaPR("/tmp/x", "main", "dogfood/x", "t", "")
	if !errors.Is(err, ErrPublishIdentityMissing) {
		t.Fatalf("err = %v, want ErrPublishIdentityMissing", err)
	}
}

func TestResolveGitAuthorFor(t *testing.T) {
	orig := publishGitAuthorResolver
	defer func() { publishGitAuthorResolver = orig }()
	publishGitAuthorResolver = nil
	name, email := resolveGitAuthorFor("/tmp/x")
	if name != "hydra" || email != "hydra@users.noreply.github.com" {
		t.Fatalf("got %s <%s>", name, email)
	}
	publishGitAuthorResolver = func(string) (string, string, bool) {
		return "Ajantha", "ajantha@users.noreply.github.com", true
	}
	name, email = resolveGitAuthorFor("/tmp/x")
	if name != "Ajantha" {
		t.Fatalf("got %s", name)
	}
}

func TestWorkerEnvBuilderGitIdentityEnv(t *testing.T) {
	orig := publishGitAuthorResolver
	publishGitAuthorResolver = func(string) (string, string, bool) {
		return "Ajantha", "ajantha@users.noreply.github.com", true
	}
	defer func() { publishGitAuthorResolver = orig }()

	b := NewWorkerEnvBuilder(&Config{})
	env := b.Build(types.Bead{ID: "hydra-x", Project: "p"}, "/tmp/proj", "standard", 0)
	want := map[string]string{
		"GIT_AUTHOR_NAME":     "Ajantha",
		"GIT_AUTHOR_EMAIL":    "ajantha@users.noreply.github.com",
		"GIT_COMMITTER_NAME":  "Ajantha",
		"GIT_COMMITTER_EMAIL": "ajantha@users.noreply.github.com",
	}
	for k, v := range want {
		found := false
		for _, e := range env {
			if e == k+"="+v {
				found = true
			}
		}
		if !found {
			t.Fatalf("env missing %s=%s", k, v)
		}
	}
}
