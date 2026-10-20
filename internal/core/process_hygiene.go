package core

import (
	"errors"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/haulmatic-dev/hydra/internal/registry"
)

// Process-hygiene sensors (live incident 2026-10-20 07:08–07:26 UTC):
//
// (A) Zero-token stall — every active worker sat at tokens_so_far=0 while
// heartbeats stayed fresh, so heartbeat-based liveness (Charon stuck checks)
// read the fleet as healthy and nothing recovered it. A worker whose session
// has EVER produced tokens has tokens_so_far>0 from an earlier turn; workers
// legitimately long inside tool calls are therefore NOT flagged. The precise
// signature is: state=working AND tokens_so_far==0 AND age>15m — the session
// never completed a turn.
//
// (B) Rogue pi — a bare `pi` interactive launch outside hydra.service (live
// fixture: PID 3002553, root session scope, 1.6d old) burns a core through
// every daemon restart: nothing owns it. The scanner walks /proc for pi
// processes whose cgroup is not under hydra.service and whose PID is absent
// from the tracked-workers set, with etime>2h.
//
// Both findings feed the Pan digest (host anomalies) and both have
// whitelisted repair verbs: repair:kill-stalled-worker (explicit PID only —
// NEVER by PGID, gates share the daemon PGID) and repair:kill-rogue-pi.

const (
	// zeroTokenStallThreshold: how long a working row may sit at zero
	// ever-tokens before it is a stall. Long tool calls are fine (tokens>0);
	// a never-completed turn is not.
	zeroTokenStallThreshold = 15 * time.Minute
	// roguePiMinAge: bare-pi processes younger than this are ignored (test
	// shells, the operator's own tooling).
	roguePiMinAge = 2 * time.Hour
	// clockTicks is the Linux USER_HZ used to convert /proc/<pid>/stat field
	// 22 (starttime) into seconds. Linux exports HZ=100 for userspace.
	clockTicks = 100.0
	// roguePiCmdline is the process name (basename of argv[0]) the rogue
	// scanner looks for.
	roguePiCmdline = "pi"
)

// RoguePi describes one detected out-of-ownership pi process.
type RoguePi struct {
	PID     int
	Cmdline string
	Cgroup  string
	Age     time.Duration
}

// processHygiene holds the latest sensor findings between Charon ticks and
// exposes them to the Pan digest collector and the repair handlers.
// Findings are a snapshot: a stale finding is not dangerous (repairs kill
// explicit PIDs recorded here, and a dead PID kill is a no-op), so no TTL
// machinery is warranted.
type processHygiene struct {
	mu sync.Mutex
	// stalls maps beadID → zero-token working row (latest scan snapshot).
	stalls map[string]registry.WorkerRecord
	// rogues is the latest rogue-pi snapshot.
	rogues []RoguePi
	// rogueEmitted dedupes rogue_pi_detected events per PID per daemon life.
	rogueEmitted map[int]bool
	events       *EventSink
}

// detectZeroTokenStalls returns the working rows that match the stall
// signature (tokens_so_far==0, age>15m) and refreshes the digest snapshot.
// Fail-open: registry read errors return nil without poisoning the snapshot.
func (j *Charon) detectZeroTokenStalls() []registry.WorkerRecord {
	if j.hygiene == nil {
		j.hygiene = &processHygiene{rogueEmitted: map[int]bool{}}
	}
	if j.registryStore == nil {
		return nil
	}
	rows, err := j.registryStore.ListWorkers(registry.StateWorking)
	if err != nil {
		log.Printf("[Charon] zero-token stall probe warning: %v", err)
		return nil
	}
	now := time.Now()
	var stalls []registry.WorkerRecord
	for _, r := range rows {
		if r.TokensSoFar == 0 && now.Sub(r.StartedAt) > zeroTokenStallThreshold {
			stalls = append(stalls, r)
		}
	}
	j.hygiene.mu.Lock()
	j.hygiene.stalls = map[string]registry.WorkerRecord{}
	for _, r := range stalls {
		j.hygiene.stalls[r.BeadID] = r
	}
	j.hygiene.mu.Unlock()
	for _, r := range stalls {
		if j.events != nil {
			j.events.Emit(Event{Type: "zero_token_stall_detected",
				Reason: "working-with-zero-ever-tokens",
				Detail: fmt.Sprintf("worker for bead %s (PID %d) is %v into its run with tokens_so_far=0: the session never completed a turn", r.BeadID, r.PID, now.Sub(r.StartedAt).Round(time.Minute)),
				Fields: map[string]any{"bead": r.BeadID, "pid": r.PID, "age_minutes": int(now.Sub(r.StartedAt).Minutes())}})
		}
		log.Printf("[Charon] 🧯 zero-token stall: bead %s (PID %d) working with tokens_so_far=0 for %v", r.BeadID, r.PID, now.Sub(r.StartedAt).Round(time.Minute))
	}
	return stalls
}

// ZeroTokenStallReport returns the latest zero-token stall snapshot for the
// Pan digest collector.
func (j *Charon) ZeroTokenStallReport() []registry.WorkerRecord {
	if j.hygiene == nil {
		j.hygiene = &processHygiene{rogueEmitted: map[int]bool{}}
	}
	j.hygiene.mu.Lock()
	defer j.hygiene.mu.Unlock()
	out := make([]registry.WorkerRecord, 0, len(j.hygiene.stalls))
	for _, r := range j.hygiene.stalls {
		out = append(out, r)
	}
	return out
}

// KillStalledWorker implements repair:kill-stalled-worker: kills the stalled
// worker by its EXPLICIT registry PID (never by PGID — close gates share the
// daemon's process group and a PGID kill would take the daemon down with it),
// then marks the registry row dead so the bead reopens for respawn.
func (j *Charon) KillStalledWorker(beadID string) error {
	if j.hygiene == nil {
		j.hygiene = &processHygiene{rogueEmitted: map[int]bool{}}
	}
	if j.registryStore == nil {
		return fmt.Errorf("kill-stalled-worker: no registry store")
	}
	rec, ok, err := j.registryStore.GetWorker(beadID)
	if err != nil {
		return fmt.Errorf("kill-stalled-worker: GetWorker %s: %w", beadID, err)
	}
	if !ok {
		return fmt.Errorf("kill-stalled-worker: no worker row for %s", beadID)
	}
	if rec.PID <= 0 {
		return fmt.Errorf("kill-stalled-worker: worker row for %s has no usable PID", beadID)
	}
	if err := j.killPID(rec.PID); err != nil {
		return fmt.Errorf("kill-stalled-worker: kill PID %d (bead %s): %w", rec.PID, beadID, err)
	}
	if err := j.registryStore.MarkCompletedWithReason(beadID, -1, "zero-token-stall-killed"); err != nil {
		return fmt.Errorf("kill-stalled-worker: mark dead %s: %w", beadID, err)
	}
	log.Printf("[PanRepair] kill-stalled-worker: killed PID %d for bead %s; row marked dead, bead reopens for respawn", rec.PID, beadID)
	j.hygiene.mu.Lock()
	delete(j.hygiene.stalls, beadID)
	j.hygiene.mu.Unlock()
	return nil
}

// scanRoguePis walks procRoot (default /proc; injectable for tests) for
// rogue pi processes and refreshes the digest snapshot. A process is rogue
// when: argv[0] basename is "pi", its cgroup is NOT under hydra.service, its
// PID is absent from the tracked-workers set, and it is older than
// roguePiMinAge. Fail-open: unreadable per-pid entries are skipped.
func (j *Charon) scanRoguePis() []RoguePi {
	return j.scanRoguePisIn("/proc", j.trackedWorkerPIDs())
}

// trackedWorkerPIDs unions the PID-tracker's live workers with registry rows
// whose heartbeat is still fresh — both are daemon-owned pi processes.
func (j *Charon) trackedWorkerPIDs() map[int]bool {
	tracked := map[int]bool{}
	for pid := range j.workerTracker.GetRunningWorkers() {
		tracked[pid] = true
	}
	if j.registryStore != nil {
		if rows, err := j.registryStore.ListWorkers(); err == nil {
			fresh := time.Now().Add(-5 * time.Minute)
			for _, r := range rows {
				if r.PID > 0 && r.LastHeartbeat.After(fresh) {
					tracked[r.PID] = true
				}
			}
		}
	}
	return tracked
}

func (j *Charon) scanRoguePisIn(procRoot string, tracked map[int]bool) []RoguePi {
	if j.hygiene == nil {
		j.hygiene = &processHygiene{rogueEmitted: map[int]bool{}}
	}
	entries, err := os.ReadDir(procRoot)
	if err != nil {
		log.Printf("[Charon] rogue-pi probe warning: %v", err)
		return nil
	}
	var rogues []RoguePi
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil || pid <= 0 {
			continue
		}
		if pid == os.Getpid() || tracked[pid] {
			continue
		}
		cmdlineBytes, err := os.ReadFile(procRoot + "/" + e.Name() + "/cmdline")
		if err != nil {
			continue
		}
		cmdline := strings.ReplaceAll(string(cmdlineBytes), "\x00", " ")
		argv0 := strings.TrimSpace(strings.SplitN(cmdline, " ", 2)[0])
		if argv0 == "" || basenameOf(argv0) != roguePiCmdline {
			continue
		}
		cgroupBytes, err := os.ReadFile(procRoot + "/" + e.Name() + "/cgroup")
		if err != nil {
			continue
		}
		cgroup := strings.TrimSpace(string(cgroupBytes))
		if isHydraServiceCgroup(cgroup) {
			continue // daemon-owned pi is Charon's job
		}
		age, err := procStartAge(procRoot, procRoot+"/"+e.Name()+"/stat")
		if err != nil {
			continue
		}
		if age < roguePiMinAge {
			continue
		}
		rogues = append(rogues, RoguePi{PID: pid, Cmdline: cmdline, Cgroup: cgroup, Age: age})
	}

	j.hygiene.mu.Lock()
	j.hygiene.rogues = rogues
	var fresh []RoguePi
	for _, r := range rogues {
		if !j.hygiene.rogueEmitted[r.PID] {
			j.hygiene.rogueEmitted[r.PID] = true
			fresh = append(fresh, r)
		}
	}
	j.hygiene.mu.Unlock()

	for _, r := range fresh {
		if j.events != nil {
			j.events.Emit(Event{Type: "rogue_pi_detected",
				Reason: "pi-outside-hydra-ownership",
				Detail: fmt.Sprintf("rogue pi PID %d (%s, cgroup %q, age %v) is outside hydra.service and not tracked — repair:kill-rogue-pi", r.PID, r.Cmdline, r.Cgroup, r.Age.Round(time.Minute)),
				Fields: map[string]any{"pid": r.PID, "cmdline": r.Cmdline, "cgroup": r.Cgroup, "age_minutes": int(r.Age.Minutes())}})
		}
		log.Printf("[Charon] 🚨 rogue pi detected: PID %d (%s) outside hydra.service, age %v — nothing owns it; repair:kill-rogue-pi", r.PID, r.Cmdline, r.Age.Round(time.Hour))
	}
	return rogues
}

// RoguePiReport returns the latest rogue-pi snapshot for the Pan digest.
func (j *Charon) RoguePiReport() []RoguePi {
	if j.hygiene == nil {
		j.hygiene = &processHygiene{rogueEmitted: map[int]bool{}}
	}
	j.hygiene.mu.Lock()
	defer j.hygiene.mu.Unlock()
	return append([]RoguePi(nil), j.hygiene.rogues...)
}

// KillRoguePis implements repair:kill-rogue-pi: kills every recorded rogue by
// its EXPLICIT PID and clears the snapshot. Explicit PID only — these
// processes are by definition outside the daemon's process group.
func (j *Charon) KillRoguePis() (int, error) {
	if j.hygiene == nil {
		j.hygiene = &processHygiene{rogueEmitted: map[int]bool{}}
	}
	j.hygiene.mu.Lock()
	rogues := j.hygiene.rogues
	j.hygiene.rogues = nil
	j.hygiene.mu.Unlock()
	killed := 0
	var firstErr error
	for _, r := range rogues {
		if err := j.killPID(r.PID); err != nil {
			if isKillPermissionDenied(err) {
				// Root-owned rogue (live fixture 3002553: owner root, daemon
				// uid hydra): the repair CANNOT succeed from the daemon
				// account — fail closed, loud: typed needs-human event with
				// the exact remedy instead of silently dropping the finding.
				if j.events != nil {
					j.events.Emit(Event{Type: "rogue_pi_kill_denied",
						Reason: "permission-denied-root-owned-rogue",
						Detail: fmt.Sprintf("kill-rogue-pi: PID %d (%s) is owned by another user (root); daemon uid cannot signal it — ROOT MUST KILL PID %d (rogue pi outside hydra.service, age %v)", r.PID, r.Cmdline, r.PID, r.Age.Round(time.Minute)),
						Fields: map[string]any{"pid": r.PID, "cmdline": r.Cmdline}})
				}
				log.Printf("[PanRepair] kill-rogue-pi: PID %d owned by another user — ESCALATION: root must kill PID %d (rogue pi, %v)", r.PID, r.PID, r.Age.Round(time.Hour))
			} else {
				log.Printf("[PanRepair] kill-rogue-pi: kill PID %d failed: %v", r.PID, err)
			}
			if firstErr == nil {
				firstErr = fmt.Errorf("kill-rogue-pi: kill PID %d: %w", r.PID, err)
			}
			continue
		}
		log.Printf("[PanRepair] kill-rogue-pi: killed PID %d (%s)", r.PID, r.Cmdline)
		killed++
	}
	return killed, firstErr
}

// isKillPermissionDenied classifies EPERM from the kill syscall surfaces
// (os.Process.Signal returns OsError-wrapped syscall.EPERM; the kill(1)
// fallback exits 1 with "Operation not permitted" on stderr).
func isKillPermissionDenied(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, os.ErrPermission) || errors.Is(err, syscall.EPERM) {
		return true
	}
	return strings.Contains(err.Error(), "operation not permitted")
}

// isHydraServiceCgroup reports whether the /proc/<pid>/cgroup content places
// the process under hydra.service. Missing/unparseable content is NOT a
// match (a pi outside any cgroup we recognize is treated as rogue — the
// daemon's own pi workers are in hydra.service by construction).
func isHydraServiceCgroup(cgroupContent string) bool {
	return strings.Contains(cgroupContent, "hydra.service")
}

// basenameOf is filepath.Base without importing path semantics for argv[0]
// strings that may contain Windows-style separators in tests.
func basenameOf(p string) string {
	if i := strings.LastIndexByte(p, '/'); i >= 0 {
		return p[i+1:]
	}
	return p
}

// procStartAge computes the process age from /proc/<pid>/stat: field 22
// (1-indexed) is starttime in clock ticks since boot; /proc/stat btime is
// the boot epoch in seconds.
func procStartAge(procRoot, statPath string) (time.Duration, error) {
	raw, err := os.ReadFile(statPath)
	if err != nil {
		return 0, err
	}
	// The comm field can contain spaces; everything after the final ')' is
	// positional from field 3 onward.
	s := string(raw)
	idx := strings.LastIndexByte(s, ')')
	if idx < 0 {
		return 0, fmt.Errorf("procStartAge: no comm terminator in %s", statPath)
	}
	fields := strings.Fields(s[idx+1:])
	// starttime is field 22 overall; after comm (fields 1-2) it is index 19.
	if len(fields) < 20 {
		return 0, fmt.Errorf("procStartAge: short stat content in %s", statPath)
	}
	startTicks, err := strconv.ParseFloat(fields[19], 64)
	if err != nil {
		return 0, fmt.Errorf("procStartAge: parse starttime: %w", err)
	}
	btime, err := readBootTime(procRoot + "/stat")
	if err != nil {
		return 0, err
	}
	startEpoch := float64(btime) + startTicks/clockTicks
	age := time.Since(time.Unix(int64(startEpoch), 0))
	if age < 0 {
		age = 0
	}
	return age, nil
}

// readBootTime reads the btime line from /proc/stat (injectable path for
// tests).
func readBootTime(procStatPath string) (int64, error) {
	raw, err := os.ReadFile(procStatPath)
	if err != nil {
		return 0, err
	}
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.HasPrefix(line, "btime ") {
			return strconv.ParseInt(strings.TrimSpace(strings.TrimPrefix(line, "btime ")), 10, 64)
		}
	}
	return 0, fmt.Errorf("readBootTime: no btime in %s", procStatPath)
}
