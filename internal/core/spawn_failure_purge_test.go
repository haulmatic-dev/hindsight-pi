package core

import (
	"testing"
	"time"
)

// staleSpawnFailure gates the digest-time purge of phantom spawn-failure
// entries (fmsfe-npa 2026-10-02): purge only on positive evidence — a live
// registry worker, or age past the TTL with no live worker. Nil registry
// store (zero-value spawner) makes HasLiveRegistryWorker false, so the TTL
// path is what purges; a nil spawner fails open (entry stays visible).
func TestStaleSpawnFailure(t *testing.T) {
	if staleSpawnFailure(nil, "fmsfe-x", time.Now()) {
		t.Fatal("nil spawner must fail open (entry stays visible)")
	}
	sp := &WorkerSpawner{} // registryStore nil → HasLiveRegistryWorker false
	fresh := time.Now()
	if staleSpawnFailure(sp, "fmsfe-x", fresh) {
		t.Fatal("fresh entry with no live worker must not be stale")
	}
	old := time.Now().Add(-2 * spawnFailureTTL)
	if !staleSpawnFailure(sp, "fmsfe-x", old) {
		t.Fatal("entry older than TTL must be stale")
	}
	if staleSpawnFailure(sp, "fmsfe-x", time.Time{}) {
		t.Fatal("zero timestamp must fail open")
	}
}
