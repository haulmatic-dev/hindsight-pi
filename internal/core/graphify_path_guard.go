package core

import (
	"os"
	"time"
)

const (
	// projectPathMissingEvent is emitted when a graphify refresh is requested
	// for a project path that no longer exists (moved, deleted, or misconfigured).
	projectPathMissingEvent = "project_path_missing"
	// projectPathMissingTTL rate-limits the event: a path that keeps missing
	// across refresh ticks reports once per TTL window instead of once per tick.
	projectPathMissingTTL = 24 * time.Hour
)

// pathExists reports whether path currently exists on disk (file or directory).
func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// clearMissingPath forgets a previously-noted missing path so a later miss
// after reappearance emits a fresh event.
func (g *GraphifyIndexer) clearMissingPath(path string) {
	if g == nil {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	delete(g.missingPaths, path)
}

// notePathMissing records the miss (rate-limited to one event per TTL window)
// and emits the typed event. Nil-safe on the sink and on the indexer. The gate
// check and the timestamp write share one lock acquisition, so concurrent
// misses cannot both pass the TTL gate.
func (g *GraphifyIndexer) notePathMissing(path string) {
	if g == nil {
		return
	}
	now := time.Now()
	g.mu.Lock()
	last, seen := g.missingPaths[path]
	if seen && now.Sub(last) < projectPathMissingTTL {
		g.mu.Unlock()
		return
	}
	g.missingPaths[path] = now
	g.mu.Unlock()
	g.events.Emit(Event{
		Type:   projectPathMissingEvent,
		Reason: "path_not_found",
		Detail: "graphify refresh skipped: project path does not exist",
		Fields: map[string]any{"path": path},
	})
}

// checkPathExists guards a refresh claim: an existing path clears any stale
// missing marker and allows the refresh; a missing one is noted (rate-limited)
// and the refresh is refused.
func (g *GraphifyIndexer) checkPathExists(abs string) bool {
	if pathExists(abs) {
		g.clearMissingPath(abs)
		return true
	}
	g.notePathMissing(abs)
	return false
}
