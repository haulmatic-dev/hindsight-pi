package core

// spawn_preflight.go — fail-closed spawn preflight (bead fmsfe-a2u, 2026-09-30
// P0 incident). After the 17:00 UTC daemon restart, reconstructed beads
// carried an empty Type (and empty Project), so every scheduler type filter
// (approval-gate / approval / epic / hold) silently no-opped: the approval
// gate fmsfe-a2u was SPAWNED as work 4x, its worker exit was treated as
// completion, and the DAG released its dependents while the br store still
// showed the gate open.
//
// Root fix: every spawn path funnels through Overseer.spawnWorker, which now
// (1) re-reads Type/Project from the store immediately before spawning and
// (2) fails CLOSED when the effective Type is empty or a non-executable
// (human-gate/container) type — a bead that cannot be executed as work must
// never reach a worker.

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/haulmatic-dev/hydra/internal/logx"
	"github.com/haulmatic-dev/hydra/pkg/types"
)

// nonExecutableBeadTypes are bead types that exist as gates or containers,
// never as worker-executable work. Case-insensitive (fmsfe stores emitted
// mixed-case Type values after reconstruction).
var nonExecutableBeadTypes = map[string]bool{
	"approval-gate": true,
	"approval":      true,
	"epic":          true,
	"hold":          true,
}

// isNonExecutableBeadType reports whether t (case-insensitive, trimmed) is a
// human-gate or container type that must never spawn a worker.
func isNonExecutableBeadType(t string) bool {
	return nonExecutableBeadTypes[strings.ToLower(strings.TrimSpace(t))]
}

// spawnPreflightBead runs the fail-closed pre-spawn assertions on the
// effective bead (post store-refresh). Returns a *SpawnSuppressedError so the
// scheduler treats the block as "not a failure" — the bead stays open and is
// re-checked next tick (auto-resume), with no retry budget burned.
func (o *Overseer) spawnPreflightBead(bead types.Bead, projectPath string) error {
	beadIDStr := bead.ID.String()

	// FAIL-CLOSED: an empty Type means the bead was reconstructed without its
	// metadata. Scheduling it would bypass every type filter — refuse.
	if strings.TrimSpace(bead.Type) == "" {
		o.emitSpawnPreflightBlocked(projectPath, "spawn_blocked_type_missing", bead,
			"bead has empty Type (cannot verify it is executable work)")
		return &SpawnSuppressedError{BeadID: beadIDStr,
			Reason: "empty Type (fail-closed: type cannot be verified)"}
	}

	// Human gates and containers must never spawn, even when the in-memory
	// copy's Type was empty and the store refresh just restored it.
	if isNonExecutableBeadType(bead.Type) || isHoldBead(bead) {
		o.emitSpawnPreflightBlocked(projectPath, "spawn_blocked_human_gate", bead,
			fmt.Sprintf("bead Type %q is a human gate/container, not executable work", bead.Type))
		return &SpawnSuppressedError{BeadID: beadIDStr,
			Reason: fmt.Sprintf("non-executable type %q", bead.Type)}
	}

	// pan-filed beads are human-triage filings from Pan's file-bead actuator
	// (hydra-bldo): a worker must never execute them.
	if hasBeadLabel(bead.Labels, panFiledLabel) {
		o.emitSpawnPreflightBlocked(projectPath, "spawn_blocked_human_triage", bead,
			"bead is pan-filed (human triage), not worker-executable work")
		return &SpawnSuppressedError{BeadID: beadIDStr,
			Reason: "pan-filed human-triage bead"}
	}

	return nil
}

// spawnPreflightStoreOwnership blocks the spawn funnel when the project's
// .beads store is owned by a uid other than the daemon's (bug hydra-6g4u).
// br's fsqlite WAL layer uid-locks its namespace sidecars: a store written by
// the daemon (uid 999) can never be opened interactively by the repo owner
// ("fsqlite refused its namespace sidecar ... owned by uid 999"), and vice
// versa. Remedy per the bead's option (a): the daemon stops managing
// user-owned repos — it refuses to spawn work into them (fail-closed,
// auto-resume if ownership is later corrected) instead of deepening the
// cross-uid churn. The probe itself fails open: a missing store or an
// unreadable path must not stall the fleet.
var daemonUID = func() int { return os.Geteuid() }

// storeOwnerUID reports the owning uid of <projectPath>/.beads.
// missing=false signals the store does not exist (nothing to guard).
func storeOwnerUID(projectPath string) (uid int, missing bool, err error) {
	storeDir := filepath.Join(projectPath, ".beads")
	fi, err := os.Stat(storeDir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, true, nil
		}
		return 0, false, err
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, false, fmt.Errorf("store dir %s: not a unix stat_t", storeDir)
	}
	return int(st.Uid), false, nil
}

// maxOffendingStorePaths caps how many foreign-owned paths the preflight
// warning names (FR-005: name actual offending paths instead of a generic
// store-wide string).
const maxOffendingStorePaths = 3

// offendingStorePathsDetail returns " [.beads/x: uid N ...]" naming up to
// maxOffendingStorePaths paths under <projectPath>/.beads owned by a uid
// different from the daemon's. Errors are swallowed (fail-open): the
// generic store-wide detail already carries the diagnosis.
func offendingStorePathsDetail(projectPath string) string {
	daemon := daemonUID()
	storeDir := filepath.Join(projectPath, ".beads")
	entries, err := os.ReadDir(storeDir)
	if err != nil {
		return ""
	}
	parts := make([]string, 0, maxOffendingStorePaths)
	for _, e := range entries {
		if len(parts) >= maxOffendingStorePaths {
			break
		}
		fi, err := e.Info()
		if err != nil {
			continue
		}
		st, ok := fi.Sys().(*syscall.Stat_t)
		if !ok || int(st.Uid) == daemon {
			continue
		}
		parts = append(parts, fmt.Sprintf(".beads/%s: uid %d", e.Name(), int(st.Uid)))
	}
	if len(parts) == 0 {
		return ""
	}
	return " [offending: " + strings.Join(parts, ", ") + "]"
}

func (o *Overseer) spawnPreflightStoreOwnership(bead types.Bead, projectPath string) error {
	if projectPath == "" {
		return nil
	}
	uid, missing, err := storeOwnerUID(projectPath)
	if err != nil {
		// Probe error ≠ proof of foreign ownership: fail open.
		log.Printf("[Spawn] %s store-ownership preflight probe error (fail-open, spawning): %v", bead.ID, err)
		return nil
	}
	if missing || uid == daemonUID() {
		return nil
	}
	// 2026-10-13: downgraded from fail-closed to fail-open. The uid-mismatch
	// heuristic blocked every spawn into user-owned repos (event
	// spawn_blocked_store_ownership, srv-smart-engine wedge) even though the
	// daemon demonstrably reads foreign stores fine (br list/show as hydra on
	// ajantha's store) and workers already execute as the repo owner via su.
	// The hydra-6g4u sidecar uid-lock is a WRITE hazard; gating all spawns on
	// ownership alone was a false-positive sensor. Keep the typed event for
	// visibility; let real write failures surface at the write site.

	// Store-ownership preflight precision: run a br probe to see if the
	// daemon can actually read the foreign store. A successful probe (exit 0)
	// means cross-uid reads work, so the warning is downgraded to debug-only
	// (no typed event, no spawn churn). A failing probe keeps today's warn +
	// event, naming the actual offending paths (up to 3).
	probeErr := brListProbe(projectPath)
	detail := fmt.Sprintf(".beads store owned by uid %d, daemon runs as uid %d — cross-uid store; writes may hit fsqlite sidecar uid-lock (hydra-6g4u)%s", uid, daemonUID(), offendingStorePathsDetail(projectPath))
	if probeErr == nil {
		logx.Debugf("[Spawn] %s store-ownership preflight (fail-open, spawning): %s", bead.ID, detail)
		return nil
	}
	log.Printf("[Spawn] %s store-ownership preflight WARN (fail-open, spawning): %s", bead.ID, detail)
	o.emitSpawnPreflightBlocked(projectPath, "spawn_warn_store_ownership", bead, detail)
	return nil
}

// brListProbe runs `br list --json --limit 1` inside projectPath to verify the
// daemon can actually READ the store before warning about cross-uid writes.
// A successful probe (exit 0) means cross-uid reads work; a failure (or a
// missing br binary, which is not proof of a broken store) returns the error.
// The probe is stub-able in tests via a fake `br` on PATH. It fails open by
// contract: callers treat probe errors as "unverified", never as blockers.
func brListProbe(projectPath string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "br", "list", "--json", "--limit", "1")
	cmd.Dir = projectPath
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("br list probe: %v (%s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// spawnPreflightPublishIdentity blocks the spawn funnel when the bead owner's
// publish identity cannot be resolved (fail-closed, auto-resume: the bead is
// re-checked next tick and spawns once the PAT lands). Transient resolution
// errors fail open — a broken identity service must not stall the fleet.
func (o *Overseer) spawnPreflightPublishIdentity(bead types.Bead, projectPath string) error {
	if projectPath == "" {
		return nil
	}
	uc := o.getUserContextForProject(projectPath)
	if uc == nil || uc.Username == "" || uc.Username == daemonOwnerName {
		// Daemon-owned project: the service account's own credential always
		// resolves; nothing to preflight.
		return nil
	}
	_, err := ResolvePublishIdentity(projectPath, uc)
	if err == nil {
		return nil
	}
	if !errors.Is(err, ErrPublishIdentityMissing) {
		log.Printf("[Spawn] %s identity preflight transient error (fail-open, spawning): %v", bead.ID, err)
		return nil
	}
	o.emitSpawnPreflightBlocked(projectPath, "spawn_blocked_identity_missing", bead, err.Error())
	return &SpawnSuppressedError{BeadID: bead.ID.String(),
		Reason: "owner publish identity unresolved (auto-resumes when PAT lands)"}
}

// spawnPreflightGitAccess blocks the spawn funnel when the daemon user
// (hydra) cannot run git in the project repo because git rejects the
// repository's ownership (exit 128 "dubious ownership"). Fail-closed with
// auto-resume: the bead is re-checked next tick once a human fixes trust.
func (o *Overseer) spawnPreflightGitAccess(bead types.Bead, projectPath string) error {
	if projectPath == "" || !isGitWorktree(projectPath) {
		return nil
	}
	if err := gitAccessProbe(projectPath); err == nil {
		return nil
	} else if !isDubiousOwnershipError(err) {
		log.Printf("[Spawn] %s git access probe failed open: %v", bead.ID, err)
		return nil
	}
	o.emitSpawnPreflightBlocked(projectPath, "spawn_blocked_git_ownership", bead,
		gitOwnershipRemediation(projectPath))
	return &SpawnSuppressedError{BeadID: bead.ID.String(),
		Reason: "git ownership rejected for project repo (human must set safe.directory)"}
}

// emitSpawnPreflightBlocked writes a typed, repo-mirrored event whenever the
// spawn funnel refuses a bead.
func (o *Overseer) emitSpawnPreflightBlocked(projectPath, evType string, bead types.Bead, detail string) {
	ev := Event{
		Type:    evType,
		Reason:  "preflight_fail_closed",
		Project: beadProject(&bead),
		Bead:    bead.ID.String(),
		Detail:  fmt.Sprintf("Spawn blocked for %s: %s", bead.ID, detail),
	}
	if o.beadCloser != nil && o.beadCloser.events != nil && projectPath != "" {
		o.beadCloser.events.EmitRepo(projectPath, ev)
	} else {
		log.Printf("[Spawn] %s: %s", ev.Type, ev.Detail)
	}
}
