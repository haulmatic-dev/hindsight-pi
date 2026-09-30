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

	// integrationRepoFailStreak counts consecutive gated integration
	// failures per repo. Each failure doubles the next sibling's cooldown
	// (AIMD-style: reset on success), so a burst of beads exhausting on the
	// same base — hydra-mhk2/xxq5/5ob8, three hard-parks within six minutes
	// (2026-09-29) — is staggered increasingly far apart instead of each
	// re-racing after the same fixed 30s window.
	integrationRepoFailStreak sync.Map // repoPath -> int

	// integrationOpenNotifier, when set, is invoked whenever a repo's
	// integration path opens (successful gated integration). The retest
	// stage registers itself here so parked siblings are absorbed fast
	// while the window is open instead of waiting out their hourly
	// retry interval (2026-09-29 spike: five beads parked within minutes,
	// then converged back only one-per-hour via the regular retest tick).
	integrationOpenNotifierMu sync.Mutex
	integrationOpenNotifier   func(projectPath string)
)

// SetIntegrationOpenNotifier registers a callback fired (async-safe, in a
// goroutine owned by the notifier) after each successful repo integration.
// Nil clears. Fail-open: the close path never depends on the callback.
func SetIntegrationOpenNotifier(fn func(projectPath string)) {
	integrationOpenNotifierMu.Lock()
	integrationOpenNotifier = fn
	integrationOpenNotifierMu.Unlock()
}

// integrationRepoCooldown: after a gated integration fails, sibling beads on
// the same repo wait this long before starting their own attempt. Matches
// integrationBackoffBase so the first sibling wait mirrors the failing
// bead's own first backoff. Consecutive failures escalate the effective
// cooldown exponentially (see integrationRepoFailStreak).
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
	var streak int
	if v, ok := integrationRepoFailStreak.Load(repoPath); ok {
		streak, _ = v.(int)
	}
	if v, ok := integrationRepoLast.Load(repoPath); ok {
		if last, ok := v.(time.Time); ok {
			// Adaptive stagger: each consecutive failure doubles the wait
			// (30s→1m→2m→4m→8m capped) so concurrent exhausting beads queue
			// behind an increasingly long backoff instead of re-racing the
			// same base branch. First failure keeps the base cooldown.
			mult := 0
			if streak > 1 {
				mult = min(streak-1, 4)
			}
			cooldown := integrationRepoCooldown << uint(mult)
			if cooldown > integrationBackoffMax {
				cooldown = integrationBackoffMax
			}
			if elapsed := time.Since(last); elapsed < cooldown {
				wait := cooldown - elapsed
				log.Printf("[Integrate] %s: waiting %v (repo cooldown after %d consecutive sibling integration failure(s))",
					filepath.Base(repoPath), wait.Round(time.Second), streak)
				time.Sleep(wait)
			}
		}
	}
	err := fn()
	if err != nil {
		integrationRepoLast.Store(repoPath, time.Now())
		integrationRepoFailStreak.Store(repoPath, streak+1)
	} else {
		// In-gate success clears the streak so the next failure starts from
		// the base cooldown. The close path's resetIntegrationCooldown already
		// handles the full reset + notifier; this is the belt to that suspenders.
		integrationRepoFailStreak.Delete(repoPath)
	}
	return err
}

// resetIntegrationCooldown clears the repo's cooldown after a successful
// integration so a later failure starts from a clean slate.
func resetIntegrationCooldown(repoPath string) {
	integrationRepoLast.Delete(repoPath)
	integrationRepoFailStreak.Delete(repoPath)
	integrationOpenNotifierMu.Lock()
	fn := integrationOpenNotifier
	integrationOpenNotifierMu.Unlock()
	if fn != nil {
		fn(repoPath)
	}
}

// errStage preserves the failing stage (commit/integrate/push) through the
// gate so failIntegration reports the same stage it did before gating.
type errStage struct {
	stage string
	err   error
}

func (e errStage) Error() string { return e.stage + ": " + e.err.Error() }
