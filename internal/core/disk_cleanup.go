package core

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"sync"
	"time"
)

// diskCleanupGuard: when the host disk watermark is breached, run a safe
// systemd-tmpfiles cleanup pass once per cooldown window instead of the
// digest's previous advice-only anomaly (an operator had to notice and act
// before br started failing with I/O error 28). Fail-open throughout: every
// execution error is logged only; the guard never blocks the digest.
type diskCleanupGuard struct {
	mu          sync.Mutex
	lastCleanup time.Time
	minInterval time.Duration
	// exec runs one cleanup command; injectable for tests (nil → exec.Command).
	exec func(name string, args ...string) error
	// events, when non-nil, receives a typed disk_cleanup_run event.
	events *EventSink
}

// defaultDiskCleanupInterval: at most one cleanup pass per cooldown window;
// tmpfiles cleanup is cheap but not free (fs scan) and disk pressure recovers
// slowly, so firing hourly is plenty.
const defaultDiskCleanupInterval = time.Hour

// diskCleanupCommands: conservative, host-sanctioned reclaim commands only.
// systemd-tmpfiles --clean applies age-based rules from tmpfiles.d; no rm is
// ever run by the guard.
var diskCleanupCommands = []struct {
	name string
	args []string
}{
	{"systemd-tmpfiles", []string{"--clean"}},
}

func (g *diskCleanupGuard) maybeCleanup(path string, usedPct, watermark int) {
	if g == nil || watermark <= 0 || usedPct < watermark {
		return
	}
	g.mu.Lock()
	if !g.lastCleanup.IsZero() && time.Since(g.lastCleanup) < g.minInterval {
		g.mu.Unlock()
		return
	}
	g.lastCleanup = time.Now()
	exec := g.exec
	events := g.events
	g.mu.Unlock()

	for _, c := range diskCleanupCommands {
		var err error
		if exec != nil {
			err = exec(c.name, c.args...)
		} else {
			err = runDiskCleanupCommand(c.name, c.args)
		}
		if err != nil {
			log.Printf("⚠️  disk cleanup: %s %v failed: %v (fail-open)", c.name, c.args, err)
		}
	}
	log.Printf("🧹 Disk cleanup pass run: %d%% used on %s (watermark %d%%)", usedPct, path, watermark)
	if events != nil {
		events.EmitRepo("", Event{
			Type:   "disk_cleanup_run",
			Detail: fmt.Sprintf("used=%d%% path=%s watermark=%d%%", usedPct, path, watermark),
		})
	}
}

// runDiskCleanupCommand executes one cleanup command with a short timeout;
// a hung tmpfiles must never wedge the digest loop.
func runDiskCleanupCommand(name string, args []string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return exec.CommandContext(ctx, name, args...).Run()
}
