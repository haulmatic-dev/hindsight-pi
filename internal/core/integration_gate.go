package core

import (
	"log"
	"path/filepath"
	"sync"
	"time"
)

// integration_gate.go serializes re-integration attempts per repository.
//
// Motivation (2026-09-29 telematics wedge): four beads hard-parked with
// integration-exhausted on the same repo within one campaign. Each close
// raced the others to merge into the same base branch — parallel
// CHANGELOG.md/.gitignore edits conflicting within seconds (cd8h/yc2/tg7g).
// The per-bead backoff in failIntegration spaces one bead's retries, but
// nothing spaces *sibling* beads: bead B can start integrating one second
// after bead A failed on the same base, hit the same conflict, and burn an
// exhaustion-cap attempt that the shared wedge, not B's work, caused.
//
// Contract:
//   - integrationGate(repoPath, fn) holds a per-repo mutex for the whole
//     integrate/PR step, so at most one bead integrates against a repo at a
//     time.
//   - After a failure, the repo enters a short cooldown; the next bead's
//     gated attempt waits it out before touching the base branch. This is
//     fail-closed by design (product gate: it protects the integration
//     path) but bounded — integrationRepoCooldown — and a success clears
//     the cooldown immediately.
//   - The gate never swallows errors: fn's result is returned verbatim.

var (
	integrationRepoLocksMu sync.Mutex
	integrationRepoLocks   = map[string]*sync.Mutex{}
	integrationRepoLast    sync.Map // repoPath -> time.Time of last gated failure
)

// integrationRepoCooldown: after a gated integration fails, sibling beads on
// the same repo wait this long before starting their own attempt. Matches
// integrationBackoffBase so the first sibling wait mirrors the failing
// bead's own first backoff.
var integrationRepoCooldown = 30 * time.Second

func integrationRepoMutex(repoPath string) *sync.Mutex {
	integrationRepoLocksMu.Lock()
	defer integrationRepoLocksMu.Unlock()
	mu, ok := integrationRepoLocks[repoPath]
	if !ok {
		mu = &sync.Mutex{}
		integrationRepoLocks[repoPath] = mu
	}
	return mu
}

// integrationGate runs fn while holding the repo's integration lock,
// waiting out any outstanding repo cooldown first, and records a fresh
// cooldown when fn fails.
func integrationGate(repoPath string, fn func() error) error {
	mu := integrationRepoMutex(repoPath)
	mu.Lock()
	defer mu.Unlock()
	if v, ok := integrationRepoLast.Load(repoPath); ok {
		if last, ok := v.(time.Time); ok {
			if elapsed := time.Since(last); elapsed < integrationRepoCooldown {
				wait := integrationRepoCooldown - elapsed
				log.Printf("[Integrate] %s: waiting %v (repo cooldown after sibling integration failure)",
					filepath.Base(repoPath), wait.Round(time.Second))
				time.Sleep(wait)
			}
		}
	}
	err := fn()
	if err != nil {
		integrationRepoLast.Store(repoPath, time.Now())
	}
	return err
}

// resetIntegrationCooldown clears the repo's cooldown after a successful
// integration so a later failure starts from a clean slate.
func resetIntegrationCooldown(repoPath string) {
	integrationRepoLast.Delete(repoPath)
}

// errStage preserves the failing stage (commit/integrate/push) through the
// gate so failIntegration reports the same stage it did before gating.
type errStage struct {
	stage string
	err   error
}

func (e errStage) Error() string { return e.stage + ": " + e.err.Error() }
