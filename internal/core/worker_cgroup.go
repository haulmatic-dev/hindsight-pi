package core

import (
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

// worker_cgroup.go — per-worker memory containment (hydra-3ocq).
//
// Motivation: on 2026-10-15 the fleet OOM-killed the whole daemon multiple
// times in one night (22.1G peak on a 22G box, swap full). Workers share the
// daemon's cgroup, so the OOM killer's victim selection can take the daemon
// down with the worker — every in-flight bead then redoes npm ci + LLM turns
// + jest from scratch (~20min and the tokens wasted per bead).
//
// Confining each worker (and its children — pi, jest, npm all inherit the
// worker's cgroup) to its own cgroup with memory.max turns a runaway into a
// single-worker failure the retry manager can absorb, instead of a
// fleet-wide crash.
//
// Requirements: cgroup v2 + `Delegate=yes` on hydra.service (see
// docs/operations/host-setup.md). Without delegation the writes fail; we log
// a warning and continue — containment is a hardening layer, never a spawn
// blocker (fail-open).

var cgroupSetupOnce sync.Once
var cgroupSetupOK bool

// ensureCgroupDelegated prepares the delegated service cgroup for per-worker
// children. The kernel's no-internal-process constraint forbids enabling a
// domain controller (memory) on a cgroup that directly contains processes,
// so the daemon first moves ITSELF into a `daemon/` leaf (the containerd
// pattern), then enables +memory on the service cgroup. Idempotent via
// sync.Once. Fail-open: any error leaves containment disabled with a warning.
func ensureCgroupDelegated(root string) {
	cgroupSetupOnce.Do(func() {
		daemonDir := filepath.Join(root, "daemon")
		if err := os.Mkdir(daemonDir, 0o755); err != nil && !os.IsExist(err) {
			log.Printf("[Worker] cgroup setup: mkdir daemon: %v — containment disabled", err)
			return
		}
		self := strconv.Itoa(os.Getpid())
		if err := os.WriteFile(filepath.Join(daemonDir, "cgroup.procs"), []byte(self), 0o644); err != nil {
			log.Printf("[Worker] cgroup setup: move daemon to leaf: %v — containment disabled", err)
			return
		}
		if err := os.WriteFile(filepath.Join(root, "cgroup.subtree_control"), []byte("+memory"), 0o644); err != nil {
			log.Printf("[Worker] cgroup setup: enable +memory: %v — containment disabled", err)
			return
		}
		cgroupSetupOK = true
		log.Printf("[Worker] cgroup delegation ready: daemon confined to %s, +memory enabled for worker cgroups", daemonDir)
	})
}

// confineWorkerToCgroup moves pid into a dedicated child cgroup with
// memory.max set from config. Overseer.WorkerMemoryMaxMB <= 0 disables.
// All failures are logged and swallowed: a missing cgroup setup must never
// prevent a spawn.
func (s *WorkerSpawner) confineWorkerToCgroup(pid int, beadID string) {
	maxMB := 0
	if s.config != nil {
		maxMB = s.config.Overseer.WorkerMemoryMaxMB
	}
	if maxMB <= 0 || pid <= 0 {
		return
	}
	rel := parseCgroupV2Path(readSelfCgroup())
	if rel == "" {
		return // cgroup v1 or unreadable — nothing to do
	}
	root := filepath.Join("/sys/fs/cgroup", rel)
	ensureCgroupDelegated(root)
	if !cgroupSetupOK {
		return
	}
	dir := filepath.Join(root, "hydra-worker-"+strconv.Itoa(pid))
	if err := os.Mkdir(dir, 0o755); err != nil {
		log.Printf("[Worker] cgroup containment unavailable for %s (mkdir %s: %v) — running uncapped", beadID, dir, err)
		return
	}
	maxBytes := int64(maxMB) * 1024 * 1024
	if err := os.WriteFile(filepath.Join(dir, "memory.max"), []byte(strconv.FormatInt(maxBytes, 10)), 0o644); err != nil {
		log.Printf("[Worker] cgroup containment unavailable for %s (memory.max: %v) — running uncapped", beadID, err)
		return
	}
	if err := os.WriteFile(filepath.Join(dir, "cgroup.procs"), []byte(strconv.Itoa(pid)), 0o644); err != nil {
		log.Printf("[Worker] cgroup containment unavailable for %s (cgroup.procs: %v) — running uncapped", beadID, err)
		return
	}
	log.Printf("[Worker] bead %s confined to %s (memory.max=%dMB)", beadID, dir, maxMB)
}

func readSelfCgroup() string {
	data, err := os.ReadFile("/proc/self/cgroup")
	if err != nil {
		return ""
	}
	return string(data)
}

// parseCgroupV2Path extracts the unified-hierarchy path from
// /proc/self/cgroup content (the "0::<path>" line). Returns "" on cgroup v1
// or malformed content.
func parseCgroupV2Path(content string) string {
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(line, "0::") {
			return strings.TrimSpace(strings.TrimPrefix(line, "0::"))
		}
	}
	return ""
}
