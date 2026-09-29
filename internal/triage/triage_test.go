package triage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestGroupByOverlap(t *testing.T) {
	prs := []PR{
		{Number: 1, Files: []string{"a.go", "b.go"}},
		{Number: 2, Files: []string{"b.go"}},
		{Number: 3, Files: []string{"c.go"}},
		{Number: 4, Files: []string{"a.go", "d.go"}},
	}
	groups := GroupByOverlap(prs)
	if len(groups) != 1 {
		t.Fatalf("want 1 group, got %d: %+v", len(groups), groups)
	}
	g := groups[0]
	if len(g.PRs) != 3 {
		t.Fatalf("want 3 PRs in group, got %+v", g.PRs)
	}
	// PR #3 (c.go only) must be excluded (group <2).
	if g.PRs[0].Number != 1 || g.PRs[1].Number != 2 || g.PRs[2].Number != 4 {
		t.Fatalf("unexpected order/content: %+v", g.PRs)
	}
	shared := map[string]bool{}
	for _, f := range g.Files {
		shared[f] = true
	}
	if !shared["a.go"] || !shared["b.go"] || shared["d.go"] || shared["c.go"] {
		t.Fatalf("shared files wrong: %v", g.Files)
	}
}

func TestGroupByOverlapTwoGroupsAndSinglesDropped(t *testing.T) {
	prs := []PR{
		{Number: 1, Files: []string{"x.go"}},
		{Number: 2, Files: []string{"x.go"}},
		{Number: 3, Files: []string{"y.go"}},
		{Number: 4, Files: []string{"y.go", "z.go"}},
		{Number: 5, Files: []string{"solo.go"}},
	}
	groups := GroupByOverlap(prs)
	if len(groups) != 2 {
		t.Fatalf("want 2 groups, got %+v", groups)
	}
	if groups[0].PRs[0].Number != 1 || groups[1].PRs[0].Number != 3 {
		t.Fatalf("group order wrong: %+v", groups)
	}
}

func TestGroupByOverlapEmptyAndSingle(t *testing.T) {
	if got := GroupByOverlap(nil); len(got) != 0 {
		t.Fatalf("want 0 groups, got %+v", got)
	}
	if got := GroupByOverlap([]PR{{Number: 1, Files: []string{"a.go"}}}); len(got) != 0 {
		t.Fatalf("single PR must not group, got %+v", got)
	}
}

// fake gh: pr list returns fixed entries; pr view returns per-number files.
func fakeRunner(list []ghPRListEntry, files map[int][]string, failNumbers map[int]bool) GhRunner {
	return func(ctx context.Context, dir string, args ...string) ([]byte, error) {
		if args[0] == "pr" && args[1] == "list" {
			return json.Marshal(list)
		}
		if args[0] == "pr" && args[1] == "view" {
			var n int
			fmt.Sscanf(args[2], "%d", &n)
			if failNumbers[n] {
				return nil, errors.New("gh pr view: boom")
			}
			fl := []ghPRFile{}
			for _, f := range files[n] {
				fl = append(fl, ghPRFile{Path: f})
			}
			return json.Marshal(ghPRListEntry{Files: fl})
		}
		if args[0] == "pr" && args[1] == "comment" {
			return []byte("https://example.invalid/comment"), nil
		}
		if args[0] == "pr" && args[1] == "edit" {
			return []byte(""), nil
		}
		return nil, fmt.Errorf("unexpected args %v", args)
	}
}

func TestCollectOpenDogfoodPRs(t *testing.T) {
	list := []ghPRListEntry{
		{Number: 10, Title: "t1", HeadRefName: "dogfood/hydra-abc", URL: "u1"},
		{Number: 11, Title: "t2", HeadRefName: "feature/other"}, // non-dogfood, skipped
		{Number: 12, Title: "t3", HeadRefName: "dogfood/hydra-xyz"},
	}
	prs, err := CollectOpenDogfoodPRs(context.Background(), fakeRunner(list, map[int][]string{
		10: {"a.go"},
		12: {"a.go"},
	}, nil), "/repo")
	if err != nil {
		t.Fatal(err)
	}
	if len(prs) != 2 {
		t.Fatalf("want 2 dogfood PRs, got %+v", prs)
	}
	if prs[0].Number != 10 || prs[1].Number != 12 {
		t.Fatalf("wrong PRs: %+v", prs)
	}
}

func TestCollectOpenDogfoodPRsFailOpenPerPR(t *testing.T) {
	list := []ghPRListEntry{
		{Number: 10, HeadRefName: "dogfood/x"},
		{Number: 12, HeadRefName: "dogfood/y"},
	}
	// #10 file list fails → dropped, #12 survives.
	prs, err := CollectOpenDogfoodPRs(context.Background(), fakeRunner(list, map[int][]string{
		12: {"a.go"},
	}, map[int]bool{10: true}), "/repo")
	if err != nil {
		t.Fatal(err)
	}
	if len(prs) != 1 || prs[0].Number != 12 {
		t.Fatalf("fail-open per PR broken: %+v", prs)
	}
}

func TestJudgePromptAndParse(t *testing.T) {
	g := Group{Files: []string{"a.go"}, PRs: []PR{
		{Number: 1, Title: "one", Branch: "dogfood/a", Files: []string{"a.go"}},
		{Number: 2, Title: "two", Branch: "dogfood/b", Files: []string{"a.go"}},
	}}
	prompt := JudgePrompt(g)
	for _, want := range []string{"PR #1", "merge_order", "duplicates", "a.go", "dogfood/a"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt missing %q", want)
		}
	}
	out := "```json\n{\"merge_order\":[1,2],\"duplicates\":[{\"pr\":2,\"of\":1,\"reason\":\"same change\"}],\"needs_human\":[],\"reasoning\":\"r\"}\n```"
	rec, err := ParseRecommendation(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(rec.MergeOrder) != 2 || rec.MergeOrder[0] != 1 {
		t.Fatalf("merge order wrong: %+v", rec)
	}
	if len(rec.Duplicates) != 1 || rec.Duplicates[0].Of != 1 {
		t.Fatalf("duplicates wrong: %+v", rec.Duplicates)
	}
	if _, err := ParseRecommendation("garbage"); err == nil {
		t.Fatal("want parse error on garbage")
	}
}

func TestCommentBodyAdviceOnly(t *testing.T) {
	g := Group{Files: []string{"a.go"}, PRs: []PR{{Number: 1}, {Number: 2}}}
	body := CommentBody(g, Recommendation{MergeOrder: []int{1, 2},
		Duplicates: []DuplicateFlag{{PR: 2, Of: 1, Reason: "same"}}, Reasoning: "r"})
	for _, want := range []string{"no action taken", "#1 →", "duplicate of #1", "Never auto-merged"} {
		if !strings.Contains(body, want) {
			t.Errorf("comment missing %q", want)
		}
	}
}

func TestRunOnceGroupsCommentsAndRateLimits(t *testing.T) {
	dir := t.TempDir()
	state := filepath.Join(dir, "state.json")
	var comments []string
	svc := New(time.Hour, JudgeConfig{}, func() []Project {
		return []Project{{Name: "p", Path: "/repo"}}
	}, state)
	svc.SetRunner(func(ctx context.Context, dir string, args ...string) ([]byte, error) {
		switch {
		case args[1] == "list":
			return mustJSON(t, []ghPRListEntry{
				{Number: 1, HeadRefName: "dogfood/a"},
				{Number: 2, HeadRefName: "dogfood/b"},
			}), nil
		case args[1] == "view":
			return mustJSON(t, ghPRListEntry{Files: []ghPRFile{{Path: "a.go"}}}), nil
		case args[1] == "comment":
			comments = append(comments, strings.Join(args, " "))
			return []byte("ok"), nil
		case args[1] == "edit":
			return []byte("ok"), nil
		}
		return nil, fmt.Errorf("unexpected %v", args)
	})
	svc.SetPiRunner(func(ctx context.Context, cfg JudgeConfig, prompt string) (string, error) {
		return `{"merge_order":[1,2],"duplicates":[{"pr":2,"of":1,"reason":"same change"}],"needs_human":[],"reasoning":"overlap"}`, nil
	})
	svc.RunOnce()
	if len(comments) != 2 {
		t.Fatalf("want 2 comments (one per PR in group), got %d: %v", len(comments), comments)
	}
	// Second run within 24h: rate-limited, no new comments.
	svc.RunOnce()
	if len(comments) != 2 {
		t.Fatalf("rate limit failed: got %d comments", len(comments))
	}
	// State persisted.
	b, err := os.ReadFile(state)
	if err != nil || !strings.Contains(string(b), "#1") {
		t.Fatalf("state file missing/empty: %v %s", err, b)
	}
}

func TestRunOnceNoOverlapNoComment(t *testing.T) {
	var comments []string
	svc := New(time.Hour, JudgeConfig{}, func() []Project {
		return []Project{{Name: "p", Path: "/repo"}}
	}, filepath.Join(t.TempDir(), "state.json"))
	svc.SetRunner(func(ctx context.Context, dir string, args ...string) ([]byte, error) {
		switch {
		case args[1] == "list":
			return mustJSON(t, []ghPRListEntry{{Number: 1, HeadRefName: "dogfood/a"}}), nil
		case args[1] == "view":
			return mustJSON(t, ghPRListEntry{Files: []ghPRFile{{Path: "a.go"}}}), nil
		case args[1] == "comment":
			comments = append(comments, "x")
			return []byte("ok"), nil
		}
		return nil, fmt.Errorf("unexpected %v", args)
	})
	svc.RunOnce()
	if len(comments) != 0 {
		t.Fatalf("single PR must not trigger judge/comment, got %d", len(comments))
	}
}

func TestCommentedRecentlyStateReload(t *testing.T) {
	dir := t.TempDir()
	state := filepath.Join(dir, "sub", "state.json")
	svc := New(time.Hour, JudgeConfig{}, nil, state)
	svc.markCommented("/repo", 7)
	svc2 := New(time.Hour, JudgeConfig{}, nil, state)
	if !svc2.commentedRecently("/repo", 7) {
		t.Fatal("rate limit must survive restart via state file")
	}
	if svc2.commentedRecently("/repo", 8) {
		t.Fatal("different PR must not be rate limited")
	}
	_ = time.Now
}
