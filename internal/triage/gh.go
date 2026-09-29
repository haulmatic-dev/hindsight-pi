package triage

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

// GhRunner executes a gh command in a repo dir and returns stdout.
// Injectable for tests. Collection uses `gh api` (REST) rather than
// `gh pr list` (GraphQL): fine-grained PATs on this box return HTTP 401 on
// the GraphQL endpoint while REST works, and the REST pulls response carries
// mergeable_state — the failure context the judge prompt needs.
type GhRunner func(ctx context.Context, dir string, args ...string) ([]byte, error)

// DefaultGhRunner is the production runner: fail-closed on non-zero exit
// (collection must not silently produce an empty PR set and report "no
// overlap"); callers decide whether an error fails open for the tick.
func DefaultGhRunner(ctx context.Context, dir string, args ...string) ([]byte, error) {
	// args[0] is the program ("gh" or "git"); the rest are its arguments.
	c := exec.CommandContext(ctx, args[0], args[1:]...)
	c.Dir = dir
	var stderr strings.Builder
	c.Stderr = &stderr
	out, err := c.Output()
	if err != nil {
		return nil, fmt.Errorf("gh %s: %w (%s)", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return out, nil
}

// GhCommentRunner posts a PR comment via `gh pr comment` (GraphQL upstream).
// Kept separate from GhRunner so tests (and GraphQL-broken environments) can
// swap or observe posting independently.
type GhCommentRunner func(ctx context.Context, dir string, number int, body string) error

// ghRepoSlug extracts "owner/repo" from the repo's origin remote URL.
func ghRepoSlug(ctx context.Context, run GhRunner, dir string) (string, error) {
	out, err := run(ctx, dir, "git", "remote", "get-url", "origin")
	if err != nil {
		return "", fmt.Errorf("git remote get-url origin: %w", err)
	}
	return SlugFromRemoteURL(strings.TrimSpace(string(out)))
}

// SlugFromRemoteURL parses owner/repo from an https or ssh GitHub remote URL.
func SlugFromRemoteURL(url string) (string, error) {
	u := strings.TrimSuffix(strings.TrimSpace(url), ".git")
	if i := strings.Index(u, "github.com"); i >= 0 {
		rest := strings.TrimPrefix(u[i+len("github.com"):], ":")
		rest = strings.TrimPrefix(rest, "/")
		parts := strings.Split(rest, "/")
		if len(parts) >= 2 && parts[0] != "" && parts[1] != "" {
			return parts[0] + "/" + parts[1], nil
		}
	}
	return "", fmt.Errorf("cannot parse owner/repo from remote URL %q", url)
}

// restPull is the JSON shape of GET /repos/{owner}/{repo}/pulls (subset).
type restPull struct {
	Number         int    `json:"number"`
	Title          string `json:"title"`
	HTMLURL        string `json:"html_url"`
	MergeableState string `json:"mergeable_state"`
	Head           struct {
		Ref string `json:"ref"`
	} `json:"head"`
}

// restPullFile is one entry of GET /repos/{owner}/{repo}/pulls/{n}/files.
type restPullFile struct {
	Filename string `json:"filename"`
}

// CollectOpenDogfoodPRs returns all open dogfood/* PRs for the repo at dir,
// with their target files and merge state (mergeable_state = failure
// context). PRs whose file list cannot be read are skipped (fail-open per
// PR): without files they cannot group, and a recommendation without overlap
// evidence is noise.
func CollectOpenDogfoodPRs(ctx context.Context, run GhRunner, dir string) ([]PR, error) {
	slug, err := ghRepoSlug(ctx, run, dir)
	if err != nil {
		return nil, err
	}
	out, err := run(ctx, dir, "gh", "api", "--paginate",
		"repos/"+slug+"/pulls?state=open&per_page=100")
	if err != nil {
		return nil, err
	}
	var entries []restPull
	if err := json.Unmarshal(out, &entries); err != nil {
		return nil, fmt.Errorf("parse pulls response: %w", err)
	}
	var prs []PR
	for _, e := range entries {
		if !strings.HasPrefix(e.Head.Ref, "dogfood/") {
			continue
		}
		p := PR{Number: e.Number, Title: e.Title, Branch: e.Head.Ref,
			URL: e.HTMLURL, MergeableState: e.MergeableState}
		files, err := collectPRFiles(ctx, run, dir, slug, e.Number)
		if err != nil {
			// Fail-open per PR: one unreadable file list must not kill the scan.
			continue
		}
		p.Files = files
		prs = append(prs, p)
	}
	return prs, nil
}

func collectPRFiles(ctx context.Context, run GhRunner, dir, slug string, number int) ([]string, error) {
	out, err := run(ctx, dir, "gh", "api",
		fmt.Sprintf("repos/%s/pulls/%d/files?per_page=100", slug, number))
	if err != nil {
		return nil, err
	}
	var files []restPullFile
	if err := json.Unmarshal(out, &files); err != nil {
		return nil, fmt.Errorf("parse pull files response: %w", err)
	}
	paths := make([]string, 0, len(files))
	for _, f := range files {
		paths = append(paths, f.Filename)
	}
	return paths, nil
}

// PostComment posts the recommendation comment on a PR via `gh pr comment`.
// Mechanical act, explicitly safe: a comment only.
func PostComment(ctx context.Context, run GhRunner, dir string, number int, body string) error {
	_, err := run(ctx, dir, "gh", "pr", "comment", fmt.Sprintf("%d", number), "--body", body)
	return err
}

// AddLabel adds a triage label via REST (`gh api`), avoiding the GraphQL
// endpoint. Fail-open by caller: repos without the label pre-created return
// an error which the service logs and continues.
func AddLabel(ctx context.Context, run GhRunner, dir string, number int, label string) error {
	slug, err := ghRepoSlug(ctx, run, dir)
	if err != nil {
		return err
	}
	_, err = run(ctx, dir, "gh", "api", "--method", "POST",
		fmt.Sprintf("repos/%s/issues/%d/labels", slug, number),
		"-f", "labels[]="+label)
	return err
}
