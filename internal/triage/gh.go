package triage

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

// GhRunner executes a gh/git command in a repo dir and returns stdout.
// Injectable for tests. Mirrors the exec pattern used by pan.RunPi.
type GhRunner func(ctx context.Context, dir string, args ...string) ([]byte, error)

// DefaultGhRunner is the production runner: fail-closed on non-zero exit
// (collection must not silently produce an empty PR set and report "no
// overlap"); callers decide whether an error fails open for the tick.
func DefaultGhRunner(ctx context.Context, dir string, args ...string) ([]byte, error) {
	c := exec.CommandContext(ctx, "gh", args...)
	c.Dir = dir
	var stderr strings.Builder
	c.Stderr = &stderr
	out, err := c.Output()
	if err != nil {
		return nil, fmt.Errorf("gh %s: %w (%s)", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return out, nil
}

// ghPRList is the JSON shape of `gh pr list --json ...`.
type ghPRListEntry struct {
	Number        int    `json:"number"`
	Title         string `json:"title"`
	HeadRefName   string `json:"headRefName"`
	URL           string `json:"url"`
	Files         []ghPRFile `json:"files"`
}

type ghPRFile struct {
	Path string `json:"path"`
}

// CollectOpenDogfoodPRs returns all open dogfood/* PRs for the repo at dir,
// with their target files. PRs with no resolvable file list are kept with an
// empty file set (they will not group, which is the safe direction: no
// recommendation, no comment).
func CollectOpenDogfoodPRs(ctx context.Context, run GhRunner, dir string) ([]PR, error) {
	out, err := run(ctx, dir, "pr", "list", "--state", "open",
		"--json", "number,title,headRefName,url")
	if err != nil {
		return nil, err
	}
	var entries []ghPRListEntry
	if err := json.Unmarshal(out, &entries); err != nil {
		return nil, fmt.Errorf("parse gh pr list: %w", err)
	}
	var prs []PR
	for _, e := range entries {
		if !strings.HasPrefix(e.HeadRefName, "dogfood/") {
			continue
		}
		p := PR{Number: e.Number, Title: e.Title, Branch: e.HeadRefName, URL: e.URL}
		files, err := collectPRFiles(ctx, run, dir, e.Number)
		if err != nil {
			// Fail-open per PR: one unreadable file list must not kill the scan.
			continue
		}
		p.Files = files
		prs = append(prs, p)
	}
	return prs, nil
}

func collectPRFiles(ctx context.Context, run GhRunner, dir string, number int) ([]string, error) {
	out, err := run(ctx, dir, "pr", "view", fmt.Sprintf("%d", number), "--json", "files")
	if err != nil {
		return nil, err
	}
	var e ghPRListEntry
	if err := json.Unmarshal(out, &e); err != nil {
		return nil, fmt.Errorf("parse gh pr view files: %w", err)
	}
	files := make([]string, 0, len(e.Files))
	for _, f := range e.Files {
		files = append(files, f.Path)
	}
	return files, nil
}

// PostComment posts the recommendation comment on a PR. Returns the comment
// URL (or error). Mechanical act, explicitly safe: a comment only.
func PostComment(ctx context.Context, run GhRunner, dir string, number int, body string) error {
	_, err := run(ctx, dir, "pr", "comment", fmt.Sprintf("%d", number), "--body", body)
	return err
}

// AddLabel adds a triage label. Fail-open by caller: missing labels on the
// repo return a gh error which the service logs and continues.
func AddLabel(ctx context.Context, run GhRunner, dir string, number int, label string) error {
	_, err := run(ctx, dir, "pr", "edit", fmt.Sprintf("%d", number), "--add-label", label)
	return err
}
