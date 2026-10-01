package core

// Owner-identity publish (OPERATOR RULE 2026-09-29, fail-closed):
//
// Every publish-path git/GitHub operation — commit authorship, push, PR
// create, PR merge — must run under the bead owner's identity. The owner is
// resolved bead -> project -> owning user (multi-user discovery). The owner's
// PAT lives at /home/<owner>/.hydra/.github-token. There are NO fallbacks for
// non-daemon-owned projects: an unresolved owner PAT blocks the publish with
// a typed publish_identity_missing event. The daemon service account (owner
// "hydra") owns the hydra repo's beads itself, so its own credential —
// GH_TOKEN or the daemon ~/.git-credentials store — is a legitimate source.

import (
	"errors"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strings"
)

// ErrPublishIdentityMissing is returned when a publish identity cannot be
// resolved. Callers must fail the publish closed (errors.Is).
var ErrPublishIdentityMissing = errors.New("publish identity missing")

// daemonOwnerName is the service-account username that owns daemon-managed
// repos (hydra itself). Its own credential is the only fallback allowed.
const daemonOwnerName = "hydra"

// PublishIdentity is the resolved bead-owner identity for one publish-path
// operation.
type PublishIdentity struct {
	Owner  string // owning username; "hydra" = daemon service account
	Token  string // resolved PAT ("" only alongside an error)
	Source string // token source, e.g. "owner-pat-file", "gh-token-env"
}

// publishIdentityResolver resolves the publish identity for a project path.
// Set by the overseer; nil means every project is daemon-owned.
var publishIdentityResolver func(projectPath string) (PublishIdentity, error)

// SetPublishIdentityResolver installs the project->owner identity resolver.
// Used by tests and the overseer wiring.
func SetPublishIdentityResolver(fn func(projectPath string) (PublishIdentity, error)) {
	publishIdentityResolver = fn
}

// resolvePublishIdentityFor resolves the identity for projectPath. Without a
// resolver (unit tests, standalone tools) everything is daemon-owned.
func resolvePublishIdentityFor(projectPath string) (PublishIdentity, error) {
	if publishIdentityResolver != nil {
		return publishIdentityResolver(projectPath)
	}
	return ResolveDaemonPublishIdentity()
}

// ResolvePublishIdentity maps a project path to its owning user and that
// user's PAT. uc is the owner's user context from multi-user discovery
// (nil = daemon-owned project, resolved to the service account).
//
// Non-daemon owner: the ONLY token source is the owner's own
// ~/.hydra/.github-token. Missing/unreadable -> ErrPublishIdentityMissing
// (fail closed, no GH_TOKEN and no daemon-credential fallback).
func ResolvePublishIdentity(projectPath string, uc *UserContext) (PublishIdentity, error) {
	if uc != nil && uc.Username != "" && uc.Username != daemonOwnerName {
		home := uc.HomeDir
		if home == "" {
			home = "/home/" + uc.Username
		}
		tok := readOwnerPATFile(home)
		if tok == "" {
			return PublishIdentity{Owner: uc.Username}, fmt.Errorf(
				"%w: owner %q has no readable PAT at %s (fail-closed: no fallbacks)",
				ErrPublishIdentityMissing, uc.Username, filepath.Join(home, ".hydra", ".github-token"))
		}
		return PublishIdentity{Owner: uc.Username, Token: tok, Source: "owner-pat-file"}, nil
	}
	return ResolveDaemonPublishIdentity()
}

// data, err := os.ReadFile(filepath.Join(home, ".hydra", ".github-token"))
//
// daemonTokenOwnerUser (output.github_token_owner_user) names the human whose
// PAT file the daemon borrows — 2026-10-01: the daemon's private
// ~/.git-credentials held a STALE token while the working PAT sat in the
// owning user's file, grounding the fleet 7h. When set, resolution reads that
// file and STOPS: no GH_TOKEN, no daemon ~/.git-credentials (one source of
// truth, fail-closed).
var daemonTokenOwnerUser string

// SetDaemonTokenOwnerUser installs the token-owner username for daemon
// credential resolution. Called by the overseer from output.github_token_owner_user.
func SetDaemonTokenOwnerUser(username string) { daemonTokenOwnerUser = username }

// DaemonTokenOwnerUser returns the configured token owner ("" if unset).
func DaemonTokenOwnerUser() string { return daemonTokenOwnerUser }

// ResolveDaemonPublishIdentity resolves the daemon service account's own
// credential. With a token owner configured: the daemon's own PAT file,
// then the token-owner user's PAT file — failing both is fail-closed (the
// private ~/.git-credentials copy is deliberately NOT consulted). Without
// one: legacy fallback daemon PAT file -> GH_TOKEN -> ~/.git-credentials.
func ResolveDaemonPublishIdentity() (PublishIdentity, error) {
	if u, err := user.Lookup(daemonOwnerName); err == nil {
		if tok := readOwnerPATFile(u.HomeDir); tok != "" {
			return PublishIdentity{Owner: daemonOwnerName, Token: tok, Source: "owner-pat-file"}, nil
		}
	}
	if owner := daemonTokenOwnerUser; owner != "" {
		if u, err := user.Lookup(owner); err == nil {
			if tok := readOwnerPATFile(u.HomeDir); tok != "" {
				return PublishIdentity{Owner: owner, Token: tok, Source: "owner-pat-file"}, nil
			}
		}
		return PublishIdentity{Owner: owner}, fmt.Errorf(
			"%w: token owner %q has no readable PAT at ~%s/.hydra/.github-token (fail-closed: output.github_token_owner_user is set, no GH_TOKEN or ~/.git-credentials fallback)",
			ErrPublishIdentityMissing, owner, owner)
	}
	if tok := os.Getenv("GH_TOKEN"); tok != "" {
		return PublishIdentity{Owner: daemonOwnerName, Token: tok, Source: "gh-token-env"}, nil
	}
	if tok := parseGitCredentialsToken(); tok != "" {
		return PublishIdentity{Owner: daemonOwnerName, Token: tok, Source: "daemon-git-credentials"}, nil
	}
	return PublishIdentity{Owner: daemonOwnerName}, fmt.Errorf(
		"%w: service account %q has no credential (PAT file, GH_TOKEN, or ~/.git-credentials)",
		ErrPublishIdentityMissing, daemonOwnerName)
}

// readOwnerPATFile reads <home>/.hydra/.github-token, trimmed. Any read
// failure or empty file yields "".
func readOwnerPATFile(home string) string {
	data, err := os.ReadFile(filepath.Join(home, ".hydra", ".github-token"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// parseGitCredentialsToken extracts the x-access-token from the daemon
// user's ~/.git-credentials store. Legacy daemon-credential parsing — the
// ONLY sanctioned use is the daemon service account's own store.
func parseGitCredentialsToken() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	data, err := os.ReadFile(filepath.Join(home, ".git-credentials"))
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		idx := strings.Index(line, "x-access-token:")
		if idx < 0 {
			continue
		}
		rest := line[idx+len("x-access-token:"):]
		end := strings.IndexAny(rest, "@ \t")
		if end >= 0 {
			return rest[:end]
		}
		return strings.TrimSpace(rest)
	}
	return ""
}

// autoPRResolveToken resolves the GitHub PAT for a bead owner. Non-daemon
// owners resolve ONLY from their own ~/.hydra/.github-token (fail-closed
// otherwise); the daemon service account may use its own GH_TOKEN or
// ~/.git-credentials. Empty owner means daemon-owned.
func autoPRResolveToken(owner string) (string, error) {
	if owner != "" && owner != daemonOwnerName {
		home := "/home/" + owner
		if u, err := user.Lookup(owner); err == nil && u.HomeDir != "" {
			home = u.HomeDir
		}
		if tok := readOwnerPATFile(home); tok != "" {
			return tok, nil
		}
		return "", fmt.Errorf("%w: owner %q has no readable PAT at %s",
			ErrPublishIdentityMissing, owner, filepath.Join(home, ".hydra", ".github-token"))
	}
	id, err := ResolveDaemonPublishIdentity()
	return id.Token, err
}

// publishGitAuthorResolver resolves the commit author (git user.name /
// user.email) for a project path. Set by the overseer; nil means the daemon
// service account identity.
var publishGitAuthorResolver func(projectPath string) (name, email string, ok bool)

// SetPublishGitAuthorResolver installs the project->git-author resolver.
func SetPublishGitAuthorResolver(fn func(projectPath string) (name, email string, ok bool)) {
	publishGitAuthorResolver = fn
}

// resolveGitAuthorFor resolves the commit author identity for projectPath.
// Always succeeds: the daemon service account identity is the default.
func resolveGitAuthorFor(projectPath string) (string, string) {
	if publishGitAuthorResolver != nil {
		if name, email, ok := publishGitAuthorResolver(projectPath); ok && name != "" {
			return name, email
		}
	}
	return daemonOwnerName, fmt.Sprintf("%s@users.noreply.github.com", daemonOwnerName)
}
