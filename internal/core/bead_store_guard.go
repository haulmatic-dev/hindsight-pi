package core

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// BeadStoreGuard detects anomalous open-bead-count collapses in a project's
// .beads store and fails scheduling closed for that project until the store
// is restored from a .br_history snapshot.
//
// Motivation (srv-smart-engine, 2026-09-11): a raced concurrent `br` rebuild
// deleted issues.jsonl and left beads.db with 0 issues. Nothing detected it;
// scheduling silently went blind for hours. A drop to 0 or >80% of the open
// count in a single discovery tick is corruption, never a legitimate
// mid-campaign event (individual closes are one-at-a-time).
//
// On a signal the guard blocks the project, emits a typed event, and attempts
// one automatic restore (self-heal). If the restore cannot verify the open
// count recovered, the project stays blocked and further restores happen only
// via the explicit Restore call (Pan's repair-bead-store action) — fail-open
// on restore failure means "stay blocked and alert", never "unblock anyway".
type BeadStoreGuard struct {
	mu     sync.Mutex
	states map[string]*storeGuardState // key: project path
	events *EventSink                  // nil-safe

	// listOpen counts non-terminal beads for a project. Injectable for
	// tests; production wires it to BeadsClient.ListBeads.
	listOpen func(projectPath string) (int, error)
}

type storeGuardState struct {
	baseline             int // last known-good open count
	hasBaseline          bool
	blocked              bool
	blockedAt            time.Time
	blockedObserved      int // open count observed at block time
	autoRestoreAttempted bool
}

// storeGuardDropFraction is the fractional drop (vs baseline) that counts as
// corruption: curr < baseline * storeGuardDropFraction. 0.2 == ">80% drop".
const storeGuardDropFraction = 0.2

func NewBeadStoreGuard(listOpen func(projectPath string) (int, error)) *BeadStoreGuard {
	return &BeadStoreGuard{
		states:   make(map[string]*storeGuardState),
		listOpen: listOpen,
	}
}

// SetEventSink attaches the structured event stream. Nil stays a no-op.
func (g *BeadStoreGuard) SetEventSink(s *EventSink) { g.events = s }

// IsBlocked reports whether the project's store is excluded from scheduling.
func (g *BeadStoreGuard) IsBlocked(projectPath string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	st := g.states[projectPath]
	return st != nil && st.blocked
}

// Check feeds one discovery tick's listing for a project into the detector.
// beads is the result of a successful ListBeads for the project.
func (g *BeadStoreGuard) Check(project types.Project, beads []types.Bead) {
	open := 0
	for _, b := range beads {
		if !b.Status.IsTerminal() {
			open++
		}
	}
	g.checkCount(project, open, nil)
}

// NoteListFailure feeds a failed ListBeads for a project into the detector.
// A project that previously had open beads and now cannot be listed at all
// is treated as a wipe (issues.jsonl deleted made `br list` fail in the
// srv-smart-engine incident).
func (g *BeadStoreGuard) NoteListFailure(project types.Project, listErr error) {
	g.checkCount(project, 0, listErr)
}

func (g *BeadStoreGuard) checkCount(project types.Project, open int, listErr error) {
	g.mu.Lock()
	if g.states == nil {
		g.states = make(map[string]*storeGuardState)
	}
	st := g.states[project.Path]
	if st == nil {
		st = &storeGuardState{}
		g.states[project.Path] = st
	}
	if st.blocked {
		// Already blocked: stay blocked until a verified restore. No
		// re-trigger, no repeated auto-restore attempts.
		g.mu.Unlock()
		return
	}
	if !st.hasBaseline || st.baseline == 0 {
		// First observation, or the project has no open beads to protect
		// (no active campaign). Record and wait for a real baseline.
		st.baseline = open
		st.hasBaseline = true
		g.mu.Unlock()
		return
	}
	dropped := open == 0 || float64(open) < float64(st.baseline)*storeGuardDropFraction
	if !dropped {
		st.baseline = open
		g.mu.Unlock()
		return
	}
	st.blocked = true
	st.blockedAt = time.Now()
	st.blockedObserved = open
	attemptAuto := !st.autoRestoreAttempted
	st.autoRestoreAttempted = true
	prev := st.baseline
	g.mu.Unlock()

	if listErr != nil {
		log.Printf("🚨 [BeadStoreGuard] project %s (%s): open-bead listing failed (%v) after a healthy count of %d — treating as store corruption, FAIL-CLOSED: project excluded from scheduling until restored", project.Name, project.Path, listErr, prev)
	} else {
		log.Printf("🚨 [BeadStoreGuard] project %s (%s): open-bead count collapsed %d -> %d in one tick — store corruption, FAIL-CLOSED: project excluded from scheduling until restored", project.Name, project.Path, prev, open)
	}
	g.emit(Event{
		Type:    "bead_store_suspect",
		Project: project.Name,
		Reason:  "open_count_drop",
		Detail:  fmt.Sprintf("open-bead count for %s dropped from %d to %d in one discovery tick; store presumed corrupt, scheduling blocked", project.Name, prev, open),
		Fields: map[string]any{
			"project_path": project.Path,
			"previous":     prev,
			"current":      open,
			"list_error":   fmt.Sprint(listErr),
		},
	})

	if attemptAuto {
		// Self-heal: one automatic restore attempt per signal.
		if err := g.Restore(project.Path); err != nil {
			log.Printf("⚠️  [BeadStoreGuard] auto-restore for %s failed (project stays blocked): %v — Pan repair-bead-store or manual restore required", project.Name, err)
		}
	}
}

// BlockedProjects returns the paths of all currently blocked projects.
func (g *BeadStoreGuard) BlockedProjects() []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	var out []string
	for path, st := range g.states {
		if st.blocked {
			out = append(out, path)
		}
	}
	sort.Strings(out)
	return out
}

// Restore rebuilds .beads/issues.jsonl from the newest .br_history snapshot,
// then verifies the effect — not the command — by re-listing the open count.
// The project is unblocked only if the count recovered; on any failure it
// stays blocked and a bead_store_restore_failed event is emitted.
func (g *BeadStoreGuard) Restore(projectPath string) error {
	snapshot, err := newestSnapshot(projectPath)
	if err != nil {
		g.restoreFailed(projectPath, "no_snapshot", err, nil)
		return err
	}

	if err := copyAtomic(snapshot, filepath.Join(projectPath, ".beads", "issues.jsonl")); err != nil {
		g.restoreFailed(projectPath, "copy_failed", err, map[string]any{"snapshot": snapshot})
		return err
	}
	log.Printf("[BeadStoreGuard] restored %s from snapshot %s — verifying open-count recovery", projectPath, snapshot)

	open, err := g.listOpen(projectPath)
	if err != nil {
		g.restoreFailed(projectPath, "verify_list_failed", err, map[string]any{"snapshot": snapshot})
		return fmt.Errorf("verify: re-list after restore: %w", err)
	}

	g.mu.Lock()
	st := g.states[projectPath]
	recovered := open > 0
	if st != nil && st.hasBaseline {
		// Recovered means back to at least half the pre-corruption
		// baseline; a snapshot slightly behind the wipe is fine.
		if min := st.baseline / 2; recovered && open < min && min > 0 {
			recovered = false
		}
	}
	var prev int
	if st != nil {
		prev = st.baseline
	}
	if recovered {
		if st == nil {
			st = &storeGuardState{}
			g.states[projectPath] = st
		}
		st.baseline = open
		st.hasBaseline = true
		st.blocked = false
		st.blockedObserved = 0
		st.autoRestoreAttempted = false
	}
	g.mu.Unlock()

	if !recovered {
		err := fmt.Errorf("open count %d did not recover (baseline %d)", open, prev)
		g.restoreFailed(projectPath, "count_not_recovered", err, map[string]any{
			"snapshot": snapshot,
			"open":     open,
			"baseline": prev,
		})
		return err
	}

	log.Printf("✅ [BeadStoreGuard] %s recovered: open count %d (baseline %d) — scheduling unblocked", projectPath, open, prev)
	g.emit(Event{
		Type:   "bead_store_restored",
		Reason: "snapshot_restored",
		Detail: fmt.Sprintf("restored .beads store for %s from snapshot; open count recovered to %d", projectPath, open),
		Fields: map[string]any{
			"project_path": projectPath,
			"snapshot":     snapshot,
			"open":         open,
			"baseline":     prev,
		},
	})
	return nil
}

func (g *BeadStoreGuard) restoreFailed(projectPath, reason string, err error, fields map[string]any) {
	log.Printf("🚨 [BeadStoreGuard] restore failed for %s (%s): %v — project stays blocked", projectPath, reason, err)
	if fields == nil {
		fields = map[string]any{}
	}
	fields["project_path"] = projectPath
	fields["error"] = err.Error()
	g.emit(Event{
		Type:   "bead_store_restore_failed",
		Reason: reason,
		Detail: fmt.Sprintf("restore of .beads store for %s failed (%s): %v; project stays blocked", projectPath, reason, err),
		Fields: fields,
	})
}

func (g *BeadStoreGuard) emit(ev Event) {
	if g.events != nil {
		g.events.Emit(ev)
	}
}

// newestSnapshot returns the newest issues.*.jsonl file under
// <projectPath>/.beads/.br_history, excluding *.meta.json sidecars.
func newestSnapshot(projectPath string) (string, error) {
	histDir := filepath.Join(projectPath, ".beads", ".br_history")
	entries, err := os.ReadDir(histDir)
	if err != nil {
		return "", fmt.Errorf("read snapshot dir %s: %w", histDir, err)
	}
	type cand struct {
		name string
		mod  time.Time
	}
	var cands []cand
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		cands = append(cands, cand{name: e.Name(), mod: info.ModTime()})
	}
	if len(cands) == 0 {
		return "", fmt.Errorf("no issues.*.jsonl snapshots in %s", histDir)
	}
	sort.Slice(cands, func(i, j int) bool {
		if !cands[i].mod.Equal(cands[j].mod) {
			return cands[i].mod.After(cands[j].mod)
		}
		return cands[i].name > cands[j].name
	})
	return filepath.Join(histDir, cands[0].name), nil
}

// copyAtomic copies src to dst via a temp file in dst's directory + rename,
// so a crash mid-copy never leaves a torn issues.jsonl.
func copyAtomic(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("read snapshot: %w", err)
	}
	tmp := dst + ".hydra-restore-tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, dst); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename %s -> %s: %w", tmp, dst, err)
	}
	return nil
}

