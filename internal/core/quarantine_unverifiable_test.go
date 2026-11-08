package core

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// Contract tests for the unverifiable-scar exit of the quarantine
// reconciler (bounded attempts, operator-visible verdict). All tests gate on
// unverifiableImplemented (RED pattern: skip loudly by default,
// UNVERIFIABLE_ENFORCE=1 fails them).
//
// Parent ACs covered:
//   AC1: runner faults never increment the streak; only no-command does.
//   AC2: three consecutive no-command outcomes stamp closed-failed-unverifiable
//        + one quarantine_unverifiable event; stamped member skipped in `who`.
//   AC3: streak survives daemon restart via the registry file; the per-tick
//        sweep resets streaks for members no longer carrying closed-failed.
//   AC4: quarantine holds for unverifiable members (fail-closed); operator
//        label removal resets the streak and re-enables re-verdict.

// setupUnverifiableRepo builds a temp git repo with a quarantine branch
// (root + two members depending on it) and the labels fixture.
func setupUnverifiableRepo(t *testing.T, key string, labels map[string][]string) (string, *BeadFileReader) {
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
	runGitVC(t, dir, "checkout", "-b", "hydra/quarantine/"+key)
	if err := os.WriteFile(filepath.Join(dir, "chain-work.txt"), []byte("work\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitVC(t, dir, "add", ".")
	runGitVC(t, dir, "commit", "-m", "chain work")
	runGitVC(t, dir, "checkout", "main")

	if err := os.MkdirAll(filepath.Join(dir, ".beads"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeUnverifiableJSONL(t, dir, key, labels)
	reader := NewBeadFileReader([]types.Project{{Name: "test", Path: dir}})
	return dir, reader
}

// writeUnverifiableJSONL writes the fixture store: root + member-1/member-2
// (both depending on the root, as the quarantined chain's members do).
func writeUnverifiableJSONL(t *testing.T, dir, key string, labels map[string][]string) {
	t.Helper()
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf(`{"id":"%s","title":"root","status":"closed","type":"feature"}`+"\n", key))
	for _, m := range []string{"member-1", "member-2"} {
		lbl := "[]"
		if ls := labels[m]; len(ls) > 0 {
			quoted := make([]string, len(ls))
			for i, l := range ls {
				quoted[i] = fmt.Sprintf("%q", l)
			}
			lbl = "[" + strings.Join(quoted, ",") + "]"
		}
		sb.WriteString(fmt.Sprintf(`{"id":"%s","title":"%s","status":"closed","type":"task","labels":%s,"dependencies":[{"issue_id":"%s","depends_on_id":"%s","type":"blocks"}]}`+"\n", m, m, lbl, m, key))
	}
	if err := os.WriteFile(filepath.Join(dir, ".beads", "issues.jsonl"), []byte(sb.String()), 0o644); err != nil {
		t.Fatal(err)
	}
}

// setMemberLabels mutates the store like a br label op / green scrub would.
func setMemberLabels(t *testing.T, dir, key, id string, labels map[string][]string) {
	t.Helper()
	labels[id] = append([]string(nil), labels[id]...)
	writeUnverifiableJSONL(t, dir, key, labels)
}

func newUnverifiableReconciler(t *testing.T, dir, sinkDir string) *QuarantineReconciler {
	t.Helper()
	return NewQuarantineReconciler(
		filepath.Join(t.TempDir(), "quarantined-chains.json"),
		NewWorktreeManager(), NewBeadFileReader([]types.Project{{Name: "test", Path: dir}}),
		NewEventSink(sinkDir),
		func() []types.Project { return []types.Project{{Name: "test", Path: dir}} })
}

func recordUnverifiableEntry(r *QuarantineReconciler, dir, key string) *QuarantinedChainEntry {
	r.Record(dir, "test", key, "hydra/quarantine/"+key, []types.BeadID{types.BeadID(key), "member-1", "member-2"})
	return r.entries[registryKey(dir, "hydra/quarantine/"+key)]
}

// registryStreak reads the persisted streak for a member from the registry
// file itself (the daemon-restart mechanism must be the file, not process
// state).
func registryStreak(t *testing.T, path, member string) int {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("registry read: %v", err)
	}
	var list []QuarantinedChainEntry
	if err := json.Unmarshal(data, &list); err != nil {
		t.Fatalf("registry parse: %v", err)
	}
	for _, e := range list {
		if n, ok := e.RanFalseStreaks[member]; ok {
			return n
		}
	}
	return 0
}

func TestUnverifiableStreak_OnlyNoCommandIncrements(t *testing.T) {
	requireUnverifiableImpl(t)
	dir, _ := setupUnverifiableRepo(t, "uv-class", map[string][]string{
		"member-1": {closedFailedLabel}, "member-2": nil,
	})
	r := newUnverifiableReconciler(t, dir, t.TempDir())
	e := recordUnverifiableEntry(r, dir, "uv-class")
	streak := func() int { return e.RanFalseStreaks["member-1"] }

	// Runner fault: the declared command existed but execution faulted —
	// retryable, never streaked (parent AC 1).
	for i := 0; i < 2; i++ {
		r.noteRevalidateOutcome(e, "member-1", RevalidateVerdict{Ran: false, NoCommand: false})
	}
	if streak() != 0 {
		t.Fatalf("runner fault incremented the streak: got %d, want 0", streak())
	}
	// Real red run: also never streaked.
	r.noteRevalidateOutcome(e, "member-1", RevalidateVerdict{Passed: false, Ran: true})
	if streak() != 0 {
		t.Fatalf("red run incremented the streak: got %d, want 0", streak())
	}
	// Only a no-command outcome increments.
	r.noteRevalidateOutcome(e, "member-1", RevalidateVerdict{Ran: false, NoCommand: true})
	if streak() != 1 {
		t.Fatalf("no-command outcome did not increment the streak: got %d, want 1", streak())
	}
}

func TestUnverifiableStreak_ThirdNoCommandStampsAndEmits(t *testing.T) {
	requireUnverifiableImpl(t)
	dir, _ := setupUnverifiableRepo(t, "uv-stamp", map[string][]string{
		"member-1": {closedFailedLabel}, "member-2": nil,
	})
	sinkDir := t.TempDir()
	r := newUnverifiableReconciler(t, dir, sinkDir)
	r.classifiedAdd = func(projectPath string, beadID types.BeadID, labels []string) error {
		if len(labels) != 1 || labels[0] != closedFailedUnverifiableLabel {
			t.Errorf("stamping labels = %v, want [%s]", labels, closedFailedUnverifiableLabel)
		}
		if beadID.String() != "member-1" {
			t.Errorf("stamping bead = %s, want member-1", beadID)
		}
		return nil
	}
	e := recordUnverifiableEntry(r, dir, "uv-stamp")

	stamped := false
	for i := 1; i <= 3; i++ {
		if r.noteRevalidateOutcome(e, "member-1", RevalidateVerdict{Ran: false, NoCommand: true}) {
			stamped = true
			if i != 3 {
				t.Fatalf("stamped on outcome %d, want the third", i)
			}
		}
	}
	if !stamped {
		t.Fatal("three consecutive no-command outcomes did not stamp closed-failed-unverifiable")
	}
	// The stamp resets the member's streak (a fresh bounded reign starts).
	if n := e.RanFalseStreaks["member-1"]; n != 0 {
		t.Fatalf("streak not reset after stamping: got %d", n)
	}
	events := readEventsOfType(t, NewEventSink(sinkDir), quarantineUnverifiableEventType)
	if len(events) != 1 {
		t.Fatalf("expected exactly one %s event, got %d", quarantineUnverifiableEventType, len(events))
	}
	if events[0].Bead != "member-1" {
		t.Errorf("event bead = %q, want member-1", events[0].Bead)
	}
}

func TestUnverifiable_ThreeTicksStampAndWhoSkipsMember(t *testing.T) {
	requireUnverifiableImpl(t)
	dir, _ := setupUnverifiableRepo(t, "uv-ticks", map[string][]string{
		"member-1": {closedFailedLabel}, "member-2": {closedFailedLabel},
	})
	r := newUnverifiableReconciler(t, dir, t.TempDir())

	var gateIDs []string
	var addedLabels []string
	r.SetRevalidationClassified(
		func(bead *types.Bead, projectPath, worktreePath string) RevalidateVerdict {
			gateIDs = append(gateIDs, bead.ID.String())
			if bead.ID.String() == "member-1" {
				return RevalidateVerdict{Ran: false, NoCommand: true} // no command declared
			}
			return RevalidateVerdict{Ran: false, NoCommand: false} // member-2: runner fault
		},
		func(projectPath string, beadID types.BeadID, labels []string) error { return nil },
		func(projectPath string, beadID types.BeadID, labels []string) error {
			addedLabels = append(addedLabels, beadID.String()+":"+strings.Join(labels, ","))
			return nil
		})
	recordUnverifiableEntry(r, dir, "uv-ticks")

	// No-command outcomes bypass the revalidation cooldown (no rerun budget
	// is spent) so three full reconcile passes reach the bounded cap.
	for i := 0; i < 3; i++ {
		r.Reconcile()
	}
	if !branchExists(dir, "hydra/quarantine/uv-ticks") {
		t.Fatal("quarantine must hold (fail-closed) for the unverifiable member")
	}
	if len(addedLabels) == 0 || !strings.Contains(addedLabels[0], closedFailedUnverifiableLabel) {
		t.Fatalf("member-1 not stamped with the operator-visible verdict: %v", addedLabels)
	}
	// closed-failed stays on the stamped member (fail-closed): the verdict is
	// only annotated as unverifiable, never reversed.
	// After stamping, `who` selection skips member-1: the next revalidation
	// target must be member-2 (its sibling keeps getting re-validated).
	for i := 0; i < 2; i++ {
		r.Reconcile()
	}
	if len(gateIDs) == 0 {
		t.Fatal("classified gate never ran")
	}
	if gateIDs[0] != "member-1" {
		t.Fatalf("first revalidation target = %s, want member-1", gateIDs[0])
	}
	if last := gateIDs[len(gateIDs)-1]; last != "member-2" {
		t.Fatalf("after stamping member-1, revalidation target = %s, want member-2 (stamped member skipped in who)", last)
	}
}

func TestUnverifiable_StreakSurvivesRestartViaRegistry(t *testing.T) {
	requireUnverifiableImpl(t)
	dir, _ := setupUnverifiableRepo(t, "uv-restart", map[string][]string{
		"member-1": {closedFailedLabel}, "member-2": nil,
	})
	regPath := filepath.Join(t.TempDir(), "quarantined-chains.json")
	r := NewQuarantineReconciler(regPath, NewWorktreeManager(),
		NewBeadFileReader([]types.Project{{Name: "test", Path: dir}}), nil,
		func() []types.Project { return []types.Project{{Name: "test", Path: dir}} })
	e := recordUnverifiableEntry(r, dir, "uv-restart")

	// Two bounded no-command outcomes, then the process "dies".
	r.noteRevalidateOutcome(e, "member-1", RevalidateVerdict{Ran: false, NoCommand: true})
	r.noteRevalidateOutcome(e, "member-1", RevalidateVerdict{Ran: false, NoCommand: true})
	r.Reconcile() // persists the registry

	if got := registryStreak(t, regPath, "member-1"); got != 2 {
		t.Fatalf("registry file must carry the streak: got %d, want 2", got)
	}

	// New reconciler over the same registry file (daemon restart).
	r2 := NewQuarantineReconciler(regPath, NewWorktreeManager(),
		NewBeadFileReader([]types.Project{{Name: "test", Path: dir}}), nil,
		func() []types.Project { return []types.Project{{Name: "test", Path: dir}} })
	r2.classifiedAdd = func(projectPath string, beadID types.BeadID, labels []string) error { return nil }
	r2.Reconcile() // loads the registry
	e2 := r2.entries[registryKey(dir, "hydra/quarantine/uv-restart")]
	if e2 == nil {
		t.Fatal("restarted reconciler lost the entry")
	}
	// The third bounded attempt must stamp (streak carried over, not reset).
	if !r2.noteRevalidateOutcome(e2, "member-1", RevalidateVerdict{Ran: false, NoCommand: true}) {
		t.Fatal("restart must not reset the streak: third bounded attempt did not stamp")
	}
}

func TestUnverifiable_SweepResetsStreakWhenLabelGone(t *testing.T) {
	requireUnverifiableImpl(t)
	dir, _ := setupUnverifiableRepo(t, "uv-sweep", map[string][]string{
		"member-1": {closedFailedLabel}, "member-2": nil,
	})
	r := newUnverifiableReconciler(t, dir, t.TempDir())
	e := recordUnverifiableEntry(r, dir, "uv-sweep")

	r.noteRevalidateOutcome(e, "member-1", RevalidateVerdict{Ran: false, NoCommand: true})
	r.noteRevalidateOutcome(e, "member-1", RevalidateVerdict{Ran: false, NoCommand: true})
	if e.RanFalseStreaks["member-1"] != 2 {
		t.Fatalf("seed streak = %d, want 2", e.RanFalseStreaks["member-1"])
	}

	// A green scrub (post-close sweep / M1) removes closed-failed — the
	// per-tick sweep must reset the member's streak.
	setMemberLabels(t, dir, "uv-sweep", "member-1",
		map[string][]string{"member-1": {closedFailedRevalidatedLabel}, "member-2": nil})
	r.Reconcile()
	if n := e.RanFalseStreaks["member-1"]; n != 0 {
		t.Fatalf("sweep must reset the streak for a member no longer carrying closed-failed: got %d", n)
	}
}

func TestUnverifiable_OperatorRemovalResetsAndReEnables(t *testing.T) {
	requireUnverifiableImpl(t)
	dir, _ := setupUnverifiableRepo(t, "uv-operator", map[string][]string{
		"member-1": {closedFailedLabel}, "member-2": nil,
	})
	r := newUnverifiableReconciler(t, dir, t.TempDir())

	var gateIDs []string
	r.SetRevalidationClassified(
		func(bead *types.Bead, projectPath, worktreePath string) RevalidateVerdict {
			gateIDs = append(gateIDs, bead.ID.String())
			return RevalidateVerdict{Ran: false, NoCommand: true}
		},
		func(projectPath string, beadID types.BeadID, labels []string) error { return nil },
		func(projectPath string, beadID types.BeadID, labels []string) error { return nil })
	e := recordUnverifiableEntry(r, dir, "uv-operator")

	for i := 0; i < 3; i++ {
		r.Reconcile() // stamping tick
	}
	if !branchExists(dir, "hydra/quarantine/uv-operator") {
		t.Fatal("quarantine must hold (fail-closed) for the unverifiable member")
	}
	// Operator removes the scar.
	setMemberLabels(t, dir, "uv-operator", "member-1",
		map[string][]string{"member-1": {closedFailedLabel}, "member-2": nil})
	r.Reconcile() // sweep + re-verdict re-enabled
	e = r.entries[registryKey(dir, "hydra/quarantine/uv-operator")]
	if e == nil {
		t.Fatal("entry lost after operator reset")
	}
	if len(gateIDs) < 4 {
		t.Fatalf("operator removal must re-enable re-verdicting: gate calls = %v (want member-1 again after removal)", gateIDs)
	}
	if last := gateIDs[len(gateIDs)-1]; last != "member-1" {
		t.Fatalf("after operator label removal the member must be re-verdicted: last target = %s, want member-1", last)
	}
}
