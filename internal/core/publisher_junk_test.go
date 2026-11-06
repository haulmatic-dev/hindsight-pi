package core

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Publish-path junk guard — RED contract tests (bead hydra-ai97h chain).
//
// Pinned behaviors (parent acceptance criteria):
//   AC1: branch whose diff contains one junk path → no push attempted,
//        exactly one visible publish_junk_blocked event.
//   AC2: third consecutive block → no further retries (escalated reason,
//        bounded backoff loop).
//   AC3: branch containing thread-cache.service.ts changes → pushes.
//   AC4: after junk is removed, the next publish attempt proceeds with no
//        block and a fresh counter.

const publishJunkEnforceEnv = "PUBLISH_JUNK_ENFORCE"

func publishJunkRequireImpl(t *testing.T) {
	t.Helper()
	if publishJunkImplemented {
		return
	}
	if os.Getenv(publishJunkEnforceEnv) == "1" {
		t.Fatal("RED: publish-path junk guard contract not yet implemented")
	}
	t.Skipf("RED: publish-path junk guard contract not yet implemented (set %s=1 to enforce)", publishJunkEnforceEnv)
}

// publishJunkAssertions returns a repo fixture: a bare remote + clone with
// the working branch one commit ahead of origin, as the publish tests do.
func publishJunkFixture(t *testing.T) (base, repo, bare string) {
	t.Helper()
	base = t.TempDir()
	bare = filepath.Join(base, "remote.git")
	repo = filepath.Join(base, "work")
	gitRun(t, base, "init", "--bare", bare)
	gitRun(t, base, "clone", bare, repo)
	gitRun(t, repo, "config", "user.email", "t@e.co")
	gitRun(t, repo, "config", "user.name", "T")
	return base, repo, bare
}

func publishJunkRemoteCommits(t *testing.T, bare string) []string {
	t.Helper()
	out, err := exec.Command("git", "-C", bare, "log", "--oneline", "--").CombinedOutput()
	if err != nil {
		// An empty bare remote (unborn HEAD) is a legitimate "nothing ever
		// pushed" state — exactly what AC1 expects after a blocked publish.
		if strings.Contains(string(out), "does not have any commits yet") {
			return nil
		}
		t.Fatalf("read remote log: %v\n%s", err, out)
	}
	var lines []string
	for _, l := range strings.Split(string(out), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			lines = append(lines, l)
		}
	}
	return lines
}

// TestPublisherJunk_Guard_JunkBranchNoPushOneEvent pins AC1: a branch whose
// diff contains one junk path (here .jest-cache/jx) must not be pushed —
// the publish is skipped with a visible, junk-naming reason and exactly one
// publish_junk_blocked event; the bare remote stays empty.
func TestPublisherJunk_Guard_JunkBranchNoPushOneEvent(t *testing.T) {
	publishJunkRequireImpl(t)
	_, repo, bare := publishJunkFixture(t)
	writeAndCommit(t, repo, ".jest-cache/jx", "junk\n")

	// Deterministic probe: the guard's diff seam reports exactly the junk
	// path (GREEN's production probe classifies the same committed diff).
	origProbe := publishJunkDiffProbe
	publishJunkDiffProbe = func(projectPath string) ([]string, error) {
		return []string{".jest-cache/jx"}, nil
	}
	defer func() { publishJunkDiffProbe = origProbe }()
	resetPublishJunkBlocks(repo)

	sinkDir := t.TempDir()
	origSink := publishEvents
	publishEvents = NewEventSink(sinkDir)
	defer func() { publishEvents = origSink }()

	pushed, reason := PublishRepo(NewWorktreeManager(), repo, "", false, nil, func(string) bool { return true })

	if pushed {
		t.Fatal("junk-diff branch must not be pushed, got pushed=true")
	}
	if !strings.Contains(reason, publishJunkReason) {
		t.Fatalf("reason must name the junk guard, got %q (want substring %q)", reason, publishJunkReason)
	}
	if got := publishJunkRemoteCommits(t, bare); len(got) != 0 {
		t.Fatalf("no push attempted: bare remote must stay empty, got: %v", got)
	}
	events := readEventsOfType(t, NewEventSink(sinkDir), publishJunkBlockedEvent)
	if len(events) != 1 {
		t.Fatalf("exactly one %s event expected, got %d", publishJunkBlockedEvent, len(events))
	}
	if !strings.Contains(events[0].Detail, ".jest-cache/jx") {
		t.Fatalf("blocked event must name the junk path verbatim, got: %+v", events[0])
	}
}

// TestPublisherJunk_Guard_ThirdConsecutiveBlockStopsRetries pins AC2: the
// consecutive-block counter is per project; after publishJunkMaxBlocks
// consecutive blocks the guard switches to the escalated reason (the
// publish backoff loop must stop retrying and surface to a human), and the
// guard stays blocked after that.
func TestPublisherJunk_Guard_ThirdConsecutiveBlockStopsRetries(t *testing.T) {
	publishJunkRequireImpl(t)
	project := t.TempDir()
	origProbe := publishJunkDiffProbe
	publishJunkDiffProbe = func(projectPath string) ([]string, error) {
		return []string{".tmp-x/junk.bin"}, nil
	}
	defer func() { publishJunkDiffProbe = origProbe }()
	resetPublishJunkBlocks(project)

	for i := 1; i <= publishJunkMaxBlocks; i++ {
		blocked, reason := publishJunkGuard(project)
		if !blocked {
			t.Fatalf("block %d: junk branch must be blocked", i)
		}
		if reason != publishJunkReason {
			t.Fatalf("block %d: reason=%q, want %q", i, reason, publishJunkReason)
		}
	}
	blocked, reason := publishJunkGuard(project)
	if !blocked {
		t.Fatal("after the cap, the guard must still refuse to retry the publish")
	}
	if reason != publishJunkEscalatedReason {
		t.Fatalf("after %d consecutive blocks reason=%q, want %q", publishJunkMaxBlocks, reason, publishJunkEscalatedReason)
	}
	// Other projects are unaffected by this project's counter: the guard may
	// still (correctly) refuse a junk diff for them, but their budget must be
	// fresh — the plain reason, never this project's escalated state.
	if _, reason := publishJunkGuard(project + "-other"); reason != publishJunkReason {
		t.Fatalf("counter must be per-project: other project reason=%q, want fresh %q", reason, publishJunkReason)
	}
}

// TestPublisherJunk_Guard_ThreadCacheChangesPush pins AC3: a branch containing
// only thread-cache.service.ts changes (a known non-junk path) must push —
// the default production probe classifies the real committed diff and
// nothing in it is junk.
func TestPublisherJunk_Guard_ThreadCacheChangesPush(t *testing.T) {
	publishJunkRequireImpl(t)
	_, repo, bare := publishJunkFixture(t)
	writeAndCommit(t, repo, "src/modules/agent/services/conversation/thread-cache.service.ts", "export class ThreadCache {}\n")
	resetPublishJunkBlocks(repo)

	sinkDir := t.TempDir()
	origSink := publishEvents
	publishEvents = NewEventSink(sinkDir)
	defer func() { publishEvents = origSink }()
	// No seam override: the production probe must run the real git diff.

	pushed, reason := PublishRepo(NewWorktreeManager(), repo, "", false, nil, func(string) bool { return true })
	if !pushed {
		t.Fatalf("thread-cache.service.ts branch must push, got (false, %q)", reason)
	}
	if got := publishJunkRemoteCommits(t, bare); len(got) == 0 {
		t.Fatal("commit did not land on the remote")
	}
	for _, ev := range readEventsOfType(t, NewEventSink(sinkDir), publishJunkBlockedEvent) {
		t.Errorf("no publish_junk_blocked event expected for a junk-free branch, got: %+v", ev)
	}
}

// TestPublisherJunk_Guard_JunkRemovedPublishProceeds pins AC4: after a block,
// once the junk is gone from the branch (probe returns nothing) the guard
// allows the publish, resets the counter, and a subsequent single block
// reports the plain (non-escalated) reason — a fixed branch gets a fresh
// guard budget instead of an inherited escalated state.
func TestPublisherJunk_Guard_JunkRemovedPublishProceeds(t *testing.T) {
	publishJunkRequireImpl(t)
	project := "/proj/fix-junk"
	origProbe := publishJunkDiffProbe
	publishJunkDiffProbe = func(projectPath string) ([]string, error) {
		if projectPath != project {
			return nil, nil
		}
		return []string{".jest-cache/lake"}, nil
	}
	defer func() { publishJunkDiffProbe = origProbe }()
	resetPublishJunkBlocks(project)

	if b, reason := publishJunkGuard(project); !b || reason != publishJunkReason {
		t.Fatalf("first block: got (%v,%q)", b, reason)
	}
	// Junk removed from the branch — publish must proceed.
	publishJunkDiffProbe = func(projectPath string) ([]string, error) { return nil, nil }
	if b, reason := publishJunkGuard(project); b {
		t.Fatalf("clean branch after junk removal must publish, got reason=%q", reason)
	}
	// Counter reset: one more junk generation is still a plain block.
	publishJunkDiffProbe = func(projectPath string) ([]string, error) { return []string{".jest-cache/new"}, nil }
	if _, reason := publishJunkGuard(project); reason != publishJunkReason {
		t.Fatalf("after junk removal the counter must reset: got reason=%q, want %q", reason, publishJunkReason)
	}
}

// TestPublisherJunk_Guard_CleanPathFailOpenCoverage is unconditional (not
// require-gated): gives the guard wiring measurable coverage on a junk-free
// project in both RED (inert stub) and GREEN (real guard) states. Also
// pins that a probe tooling failure fails open (never blocks a publish).
func TestPublisherJunk_Guard_CleanPathFailOpenCoverage(t *testing.T) {
	origProbe := publishJunkDiffProbe
	defer func() { publishJunkDiffProbe = origProbe }()
	resetPublishJunkBlocks("/proj/failopen")

	publishJunkDiffProbe = func(projectPath string) ([]string, error) { return nil, nil }
	if b, reason := publishJunkGuard("/proj/failopen"); b {
		t.Fatalf("clean project must not be blocked, got reason=%q", reason)
	}
	publishJunkDiffProbe = func(projectPath string) ([]string, error) { return nil, fmt.Errorf("git probe failed") }
	if b, reason := publishJunkGuard("/proj/failopen"); b {
		t.Fatalf("probe failure must fail open, got reason=%q", reason)
	}
}

// TestPublisherJunk_RepoGuardEventBounded pins the event-budget half of AC2:
// repeated publish ticks against a persistently junk branch emit at most
// publishJunkMaxBlocks publish_junk_blocked events (the escalated state
// stops the visible-event churn).
func TestPublisherJunk_RepoGuardEventBounded(t *testing.T) {
	publishJunkRequireImpl(t)
	_, repo, _ := publishJunkFixture(t)
	writeAndCommit(t, repo, ".jest-cache/deep", "junk\n")

	origProbe := publishJunkDiffProbe
	publishJunkDiffProbe = func(projectPath string) ([]string, error) {
		return []string{".jest-cache/deep"}, nil
	}
	defer func() { publishJunkDiffProbe = origProbe }()
	resetPublishJunkBlocks(repo)

	sinkDir := t.TempDir()
	origSink := publishEvents
	publishEvents = NewEventSink(sinkDir)
	defer func() { publishEvents = origSink }()

	for i := 0; i < publishJunkMaxBlocks+3; i++ {
		PublishRepo(NewWorktreeManager(), repo, "", false, nil, func(string) bool { return true })
	}
	events := readEventsOfType(t, NewEventSink(sinkDir), publishJunkBlockedEvent)
	if len(events) == 0 {
		t.Fatal("at least one publish_junk_blocked event expected for a junk branch")
	}
	if len(events) > publishJunkMaxBlocks {
		t.Fatalf("event churn bounded by %d consecutive blocks, got %d events", publishJunkMaxBlocks, len(events))
	}
}
