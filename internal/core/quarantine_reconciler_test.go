package core

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// setupReconcilerRepo builds a temp git repo with a main branch, a
// hydra/quarantine/<key> branch carrying one extra commit, and a
// .beads/issues.jsonl with root + member beads (member depends on root).
// Returns the project path and the member-labels mutator helpers.
func setupReconcilerRepo(t *testing.T, key string, memberLabels []string) (string, *BeadFileReader) {
	t.Helper()
	dir := t.TempDir()
	runGitVC(t, dir, "init", "-b", "main")
	runGitVC(t, dir, "config", "user.email", "test@hydra")
	runGitVC(t, dir, "config", "user.name", "hydra-test")
	if err := os.WriteFile(filepath.Join(dir, "base.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitVC(t, dir, "add", ".")
	runGitVC(t, dir, "commit", "-m", "base")

	// Quarantine branch with chain work.
	runGitVC(t, dir, "checkout", "-b", "hydra/quarantine/"+key)
	if err := os.WriteFile(filepath.Join(dir, "chain-work.txt"), []byte("132 beads of work\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitVC(t, dir, "add", ".")
	runGitVC(t, dir, "commit", "-m", "chain work")
	runGitVC(t, dir, "checkout", "main")

	// Beads store: root + member (member depends on root).
	beadsDir := filepath.Join(dir, ".beads")
	if err := os.MkdirAll(beadsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	labelsJSON := "[]"
	if len(memberLabels) > 0 {
		quoted := make([]string, len(memberLabels))
		for i, l := range memberLabels {
			quoted[i] = fmt.Sprintf("%q", l)
		}
		labelsJSON = "[" + strings.Join(quoted, ",") + "]"
	}
	jsonl := fmt.Sprintf(`{"id":"%s","title":"root","status":"closed","type":"feature"}`+"\n"+`{"id":"member-1","title":"member","status":"closed","type":"task","labels":%s,"dependencies":[{"issue_id":"member-1","depends_on_id":"%s","type":"blocks"}]}`+"\n",
		key, labelsJSON, key)
	if err := os.WriteFile(filepath.Join(beadsDir, "issues.jsonl"), []byte(jsonl), 0o644); err != nil {
		t.Fatal(err)
	}

	reader := NewBeadFileReader([]types.Project{{Name: "test", Path: dir}})
	return dir, reader
}

func newTestReconciler(t *testing.T, projectPath string, reader *BeadFileReader) *QuarantineReconciler {
	t.Helper()
	r := NewQuarantineReconciler(
		filepath.Join(t.TempDir(), "quarantined-chains.json"),
		NewWorktreeManager(), reader, nil,
		func() []types.Project { return []types.Project{{Name: "test", Path: projectPath}} })
	return r
}

func TestQuarantineReconciler_TaintedChainStaysQuarantined(t *testing.T) {
	key := "test-chain-tainted"
	dir, reader := setupReconcilerRepo(t, key, []string{closedFailedLabel})
	r := newTestReconciler(t, dir, reader)
	r.Record(dir, "test", key, "hydra/quarantine/"+key, []types.BeadID{types.BeadID(key), "member-1"})

	r.Reconcile()

	if !branchExists(dir, "hydra/quarantine/"+key) {
		t.Fatal("tainted chain: quarantine branch must survive")
	}
	if _, err := os.Stat(filepath.Join(dir, "chain-work.txt")); !os.IsNotExist(err) {
		t.Fatal("tainted chain: work must NOT land on main")
	}
	if len(r.entries) != 1 {
		t.Fatalf("entry must remain for retry, got %d", len(r.entries))
	}
}

func TestQuarantineReconciler_CleanChainReintegrates(t *testing.T) {
	key := "test-chain-clean"
	dir, reader := setupReconcilerRepo(t, key, nil)
	r := newTestReconciler(t, dir, reader)
	r.Record(dir, "test", key, "hydra/quarantine/"+key, []types.BeadID{types.BeadID(key), "member-1"})

	r.Reconcile()

	if branchExists(dir, "hydra/quarantine/"+key) {
		t.Fatal("clean chain: quarantine branch must be removed after re-integration")
	}
	if _, err := os.Stat(filepath.Join(dir, "chain-work.txt")); err != nil {
		t.Fatalf("clean chain: chain work must land on main: %v", err)
	}
	if len(r.entries) != 0 {
		t.Fatalf("entry must be dropped after re-integration, got %d", len(r.entries))
	}
	// Registry persisted as empty.
	data, err := os.ReadFile(r.registryPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), key) {
		t.Fatal("registry must not retain the re-integrated chain")
	}
}

func TestQuarantineReconciler_RevalidationScrubsStaleLabel(t *testing.T) {
	key := "test-chain-reval"
	dir, reader := setupReconcilerRepo(t, key, []string{closedFailedLabel})
	r := newTestReconciler(t, dir, reader)

	// Label ops mutate issues.jsonl so the reader sees the scrub.
	scrub := func(projectPath string, beadID types.BeadID, labels []string) error {
		p := filepath.Join(projectPath, ".beads", "issues.jsonl")
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		out := strings.ReplaceAll(string(data), `"labels":["closed-failed"]`, `"labels":[]`)
		return os.WriteFile(p, []byte(out), 0o644)
	}
	gateRan := false
	r.SetRevalidation(
		func(bead *types.Bead, projectPath, worktreePath string) (bool, bool) {
			gateRan = true
			// The gate must run in the scratch worktree at the quarantine
			// branch tip — that tree contains the chain's work.
			if worktreePath == "" {
				t.Error("revalidation gate must receive a scratch worktree path")
			}
			if _, err := os.Stat(filepath.Join(worktreePath, "chain-work.txt")); err != nil {
				t.Errorf("scratch worktree lacks the chain's work: %v", err)
			}
			return true, true
		},
		scrub,
		func(projectPath string, beadID types.BeadID, labels []string) error { return nil })
	r.Record(dir, "test", key, "hydra/quarantine/"+key, []types.BeadID{types.BeadID(key), "member-1"})

	r.Reconcile()

	if !gateRan {
		t.Fatal("revalidation gate never ran for the tainted member")
	}
	if branchExists(dir, "hydra/quarantine/"+key) {
		t.Fatal("revalidated chain: quarantine branch must be removed after re-integration")
	}
	if _, err := os.Stat(filepath.Join(dir, "chain-work.txt")); err != nil {
		t.Fatalf("revalidated chain: work must land on main: %v", err)
	}
}

func TestQuarantineReconciler_LegacyBootstrapReconstructsMembers(t *testing.T) {
	key := "test-chain-legacy"
	dir, reader := setupReconcilerRepo(t, key, nil)
	r := newTestReconciler(t, dir, reader)
	// No Record call — bootstrap must discover the branch from the repo scan.

	r.Reconcile()

	if branchExists(dir, "hydra/quarantine/"+key) {
		t.Fatal("legacy quarantine: clean chain must re-integrate after membership reconstruction")
	}
	if _, err := os.Stat(filepath.Join(dir, "chain-work.txt")); err != nil {
		t.Fatalf("legacy quarantine: work must land on main: %v", err)
	}
}
