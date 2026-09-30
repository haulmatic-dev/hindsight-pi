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
	"errors"
	"fmt"
	"log"
	"strings"

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
