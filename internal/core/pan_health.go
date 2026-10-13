package core

// Pan fleet health: the daemon-side collector that gives Pan eyes, plus the
// campaign-level repair verbs (park-campaign, log-improvement).
//
// Doctrine split: the collector is mechanical and runs every tick for free;
// Pan is only woken when a project carries an anomaly flag, so a healthy
// fleet costs zero MiniMax calls. The 2026-09-13→16 silence proved the need:
// every bead-level candidate class is failure-residue (retry exhaustion,
// spawn failure, hard-park labels), so gate execution, close loops, and
// stranded worktrees produced zero candidates for three days while ~30M
// tokens burned into the integrate bug.

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/haulmatic-dev/hydra/internal/degrade"
	"github.com/haulmatic-dev/hydra/internal/pan"
	"github.com/haulmatic-dev/hydra/internal/registry"
	"github.com/haulmatic-dev/hydra/pkg/types"
)

// panHoldMarker identifies Pan-created hold gates so park-campaign reuses
// them idempotently instead of stacking a new gate per repair.
// projectKey derives a stable, collision-free display key for a scan target:
// /home/<user>/projects/<repo> → "<user>/<repo>" (multi-user same-repo), any
// other path → its basename. The key is the basis of the campaign-anomaly
// pseudo ID and park-campaign's project resolution.
// unpublishedWedgeAge is how long locally-merged work may sit unpublished
// before the health digest flags a publisher wedge (Pan-detectable).
const unpublishedWedgeAge = 15 * time.Minute

func projectKey(path string) string {
	parts := strings.Split(filepath.Clean(path), string(filepath.Separator))
	for i := 0; i+2 < len(parts); i++ {
		if parts[i] == "home" && parts[i+1] != "" && parts[i+2] == "projects" && i+3 < len(parts)+1 {
			if i+3 <= len(parts)-1 {
				return parts[i+1] + "/" + strings.Join(parts[i+3:], "/")
			}
		}
	}
	return filepath.Base(path)
}

// collectHealthDigest builds the fleet glance. Every failure is fail-open:
// one unreadable store or repo must not blind the whole scan.
func (o *Overseer) collectHealthDigest() pan.HealthDigest {
	digest := pan.HealthDigest{}
	if o.beadsClient == nil {
		return digest
	}

	// Live worker rows once for the whole scan. A row whose heartbeat is
	// fresh counts as live regardless of state: a worker finalizing gates
	// after its row was prematurely marked completed still heartbeats (30s
	// cadence), and counting that bead as an orphan claim made Pan file a
	// twin P0 report bead every judgment round (2026-10-02: hydra-l867 row
	// completed at 02:52:38 while the worker PID ran on until 03:1x). The
	// threshold mirrors Charon's heartbeatHungAfter (5m).
	liveByKey := map[string][]registry.WorkerRecord{}
	completedByBead := map[string]bool{}
	if o.registryStore != nil {
		if workers, err := o.registryStore.ListWorkers(registry.StateWorking, registry.StateParked, registry.StateHung, registry.StateCompleted); err == nil {
			for _, w := range workers {
				if w.State == registry.StateCompleted {
					completedByBead[w.BeadID] = true
					if time.Since(w.LastHeartbeat) >= 5*time.Minute {
						continue // genuinely finished; not live
					}
				}
				// WorkerRecord.Project is the registered project name; match
				// by name or path basename (multi-user discovery registers
				// "<user>/<repo>" while workers carry the plain name).
				liveByKey[w.Project] = append(liveByKey[w.Project], w)
			}
		}
	}

	for _, project := range o.projects {
		for _, target := range repoScanTargets(project) {
			key := projectKey(target)
			ph := pan.ProjectHealth{Key: key}

			degrade.RecordAttempt("pan-store-scan")
			beads, err := o.beadsClient.ListBeads(target, "")
			if err != nil {
				log.Printf("[PanHealth] store scan failed for %s (fail open): %v", target, err)
				continue // fail open per store
			}
			degrade.RecordSuccess("pan-store-scan")
			statusByID := make(map[string]types.BeadStatus, len(beads))
			depsByID := make(map[string][]string, len(beads))
			claimsByID := make(map[string]time.Time, len(beads))
			labelsByID := make(map[string][]string, len(beads))
			for _, b := range beads {
				id := b.ID.String()
				statusByID[id] = b.Status
				claimsByID[id] = b.UpdatedAt
				labelsByID[id] = b.Labels
				if b.Status.IsTerminal() {
					continue
				}
				switch b.Status {
				case types.StatusOpen:
					ph.Open++
				case types.StatusInProgress:
					// Pan-filed meta beads are filed for human review and never
					// get a worker; counting them as in_progress inflates
					// inprog vs workers, which Pan infers as "orphaned claims"
					// — and each judgment files another pan-filed twin about
					// the phantom anomaly (self-referential loop, 2026-10-02:
					// 8 twins on one day). Excluded from the inprog count.
					if !slices.Contains(b.Labels, pan.FiledLabel) {
						ph.InProgress++
					}
				}
				for _, d := range b.Dependencies {
					depsByID[id] = append(depsByID[id], d.String())
				}
			}
			// Dep-blocked: non-terminal bead with at least one unmet dep.
			for _, deps := range depsByID {
				for _, d := range deps {
					if st, ok := statusByID[d]; ok && !st.IsTerminal() {
						ph.DepBlocked++
						break
					}
				}
			}

			// Live workers attributed to this target.
			var live []registry.WorkerRecord
			for projName, rows := range liveByKey {
				if projName == project.Name || projName == filepath.Base(target) || projName == key {
					live = append(live, rows...)
				}
			}
			ph.LiveWorkers = len(live)

			// P0 campaign anomaly: in_progress beads with no live registry
			// worker (fms 2026-10-01: 6 orphaned claims wedged a 17-bead DAG
			// ladder for 1h+ with zero live workers and zero progress — same
			// class as the tb3j fleet-stall rule). Charon's orphan recovery is
			// the mechanical fix; this anomaly makes the state visible to Pan
			// so a judgment fires instead of the campaign sitting silent — but
			// the
			// worker has claimed but not yet registered (spawn → status update →
			// registry upsert is not atomic). Counting fresh claims turned a
			// spawn wave into a self-perpetuating filing loop: each campaign-
			// anomaly bead Pan filed became itself a new in_progress claim with
			// no worker yet, re-firing the anomaly and re-filing (hydra
			// 2026-10-02 02:34–02:48: ctix→qmf7→t2e1→yz8z in 13 min, every one
			// a false alarm — all claims had live workers on recheck). A claim
			// within orphanClaimDigestGrace is skipped; Charon's recovery already
			// applies the same 5m grace before any destructive reopen.
			const orphanClaimDigestGrace = 5 * time.Minute
			for id, st := range statusByID {
				if st != types.StatusInProgress {
					continue
				}
				if age := time.Since(claimsByID[id]); age >= 0 && age < orphanClaimDigestGrace {
					continue // fresh claim: worker may not have registered yet
				}
				// Human-escalated beads (verified-close rejected 3/3 → escalated
				// to human, bead left in_progress) never get a worker until a
				// human or a Pan clear-escalation repair acts. Counting them as
				// orphaned claims re-fired the P0 anomaly for exactly the beads
				// the escalation gate already owns (hydra 2026-10-02 07:5x:
				// bxzr/1h4v/wiu6/cxpf — tdd-validation-failed escalations,
				// workers had completed normally). Excluded; escalation_ttl
				// (overseer.escalation_ttl) owns the aging.
				if slices.Contains(labelsByID[id], "escalated") {
					continue
				}
				// Integration-pending beads (work complete, integrate failed,
				// parked in place for the retest stage) sit in_progress with no
				// worker by design — counting them re-fired the P0 orphan-claim
				// anomaly every digest round while the retest stage owned them.
				if slices.Contains(labelsByID[id], integrationPendingLabel) {
					continue
				}
				// Pan-filed beads are campaign/triage filings, not pipeline work:
				// each filing is itself an in_progress claim that may sit waiting
				// for spawn or for its close gates long past the 5m grace. Counting
				// them as orphaned claims re-fired the P0 anomaly for the filings
				// the anomaly itself created (hydra 2026-10-06: 9 twin filings in
				// 25 min, all counting the previous filings' claims), a
				// self-referential loop on top of the 2026-10-02 inprog twin loop.
				// Excluded; the file-bead cap owns their lifecycle.
				if slices.Contains(labelsByID[id], pan.FiledLabel) {
					continue
				}
				orphaned := true
				for _, w := range live {
					if w.BeadID == id {
						orphaned = false
						break
					}
				}
				// A bead whose close gates are running is not an orphan: the
				// worker finished and the close path (build/lint/test gates,
				// verified-close judge) is in flight. Counting it re-fired the
				// P0 campaign anomaly across sweeps while the DAG was actually
				// progressing (hydra 2026-10-06: 6 beads mid-close, workers
				// exited 0 minutes earlier, Charon correctly left them — the
				// digest kept filing orphan twins). Same probe Charon uses.
				if orphaned && o.gateRoot != "" && GateCloseInProgress(o.gateRoot, project.Name, id) {
					continue
				}
				// A bead whose registry row is state=completed is not an orphan
				// even after the heartbeat goes stale: the worker exited through
				// the normal completion path (gates finished, verdict recorded)
				// and the scheduler's respawn backoff owns the requeue. Counting
				// the bead during that backoff window re-fired the P0 campaign
				// anomaly every sweep while Charon and the retry queue were
				// already cycling the ladder (hydra 2026-10-06 13:4x: 15 beads
				// between attempts — gate-reject churn + red/discovery chains —
				// every one with a completed row, pan filed a twin per round).
				// A genuine orphan is a bead whose row is still working/hung with
				// a stale heartbeat (worker died mid-run) — that case stays.
				if orphaned && completedByBead[id] {
					continue
				}
				if orphaned {
					ph.OrphanClaims++
				}
			}
			if ph.OrphanClaims > 0 {
				ph.Anomalies = append(ph.Anomalies, fmt.Sprintf(
					"P0 campaign anomaly: %d in_progress bead(s) with no live registry worker (orphaned claims wedging the DAG ladder)", ph.OrphanClaims))
			}

			// Gate deaths with no escalation (ee5r/lkbo/ejsk 2026-10-04
			// 15:45-16:17 UTC: three beads died at daemon gates in one window
			// and nobody was alerted). Two views, one rule — a gate death must
			// reach Pan within a digest tick:
			//   gatefailed: non-terminal beads carrying gate-failed that never
			//     reached the escalation ladder (no escalated/needs-human label) —
			//     a worker died or stalled before the retry cap could fire.
			//   closedfail: beads closed-failed inside closedFailedDigestWindow —
			//     terminal, so every other counter skips them, yet the death is
			//     recent and unaudited.
			const closedFailedDigestWindow = 4 * time.Hour
			for id, labels := range labelsByID {
				escalatedOrHuman := slices.Contains(labels, "escalated") || slices.Contains(labels, "needs-human")
				if !escalatedOrHuman && slices.Contains(labels, "gate-failed") {
					if st := statusByID[id]; !st.IsTerminal() {
						ph.GateFailedOpen++
					}
				}
				if slices.Contains(labels, "closed-failed") {
					if age := time.Since(claimsByID[id]); age >= 0 && age < closedFailedDigestWindow {
						ph.ClosedFailedRecent++
					}
				}
			}
			if ph.GateFailedOpen > 0 {
				ph.Anomalies = append(ph.Anomalies, fmt.Sprintf(
					"P0 campaign anomaly: %d bead(s) marked gate-failed with no escalation — gate deaths sitting silent (worker never re-attempted close)", ph.GateFailedOpen))
			}
			if ph.ClosedFailedRecent > 0 {
				ph.Anomalies = append(ph.Anomalies, fmt.Sprintf(
					"P0 campaign anomaly: %d bead(s) closed-failed in the last 4h — recent gate deaths, no escalation fired", ph.ClosedFailedRecent))
			}

			// Chained-parallel: live-worker pairs whose beads are dep-related
			// (either direction). The c36 blast signature.
			for i := 0; i < len(live); i++ {
				for j := i + 1; j < len(live); j++ {
					a, b := live[i].BeadID, live[j].BeadID
					if depRelated(depsByID, a, b) {
						ph.ChainedParallel++
					}
				}
			}

			ph.StrandedWorktrees = countStrandedWorktrees(target, statusByID)
			ph.UnintegratedBranches = countWTBranches(target)

			if ph.ChainedParallel > 0 {
				ph.Anomalies = append(ph.Anomalies, fmt.Sprintf(
					"%d dep-chained bead pairs have live workers — chain discipline violated (gate executed, dep broken, or double release)", ph.ChainedParallel))
			}
			if ph.StrandedWorktrees > 0 && ph.LiveWorkers == 0 {
				ph.Anomalies = append(ph.Anomalies, fmt.Sprintf(
					"%d stranded worktrees with no live workers — completed beads' work unswept", ph.StrandedWorktrees))
			}
			// Publisher wedge: locally-merged work that has not reached origin
			// for longer than unpublishedWedgeAge. Pan-detectable: the digest
			// anomaly wakes Pan even when no single bead looks blocked.
			for _, pp := range o.PushPending() {
				if pp.ProjectPath != target {
					continue
				}
				if age := time.Since(pp.Since); age > unpublishedWedgeAge {
					ph.Anomalies = append(ph.Anomalies, fmt.Sprintf(
						"publisher wedge: %s holds locally-merged work unpublished for %s (%s)",
						pp.ProjectPath, age.Round(time.Minute), pp.Reason))
				}
			}
			digest.Projects = append(digest.Projects, ph)
		}
	}
	sort.Slice(digest.Projects, func(i, j int) bool { return digest.Projects[i].Key < digest.Projects[j].Key })
	collectDiskWatermark(&digest, o.diskStatPath, o.diskWatermarkPct)
	if digest.DiskUsedPct > 0 {
		path := o.diskStatPath
		if path == "" {
			path = "/"
		}
		wm := o.diskWatermarkPct
		if wm <= 0 {
			wm = defaultDiskWatermarkPct
		}
		if o.diskCleanup == nil {
			o.diskCleanup = &diskCleanupGuard{}
		}
		o.diskCleanup.maybeCleanup(path, digest.DiskUsedPct, wm)
	}
	// FR-032: snapshot fail-open degradation counters; degraded subsystems
	// (0% success over the window) surface in the digest like host anomalies.
	for _, st := range degrade.Snapshot() {
		if st.Degraded() {
			digest.Degradations = append(digest.Degradations, st.String())
		}
	}
	// Stamp freshness for the file-bead premise gate (jm2 class): a filing
	// decision built on a digest older than panFilingDigestStaleAge is
	// refused at filing time.
	o.panDigestAt.Store(time.Now().UnixNano())
	return digest
}

// diskWatermarkPct / diskStatPath: injection points for tests; production
// leaves them zero so collectDiskWatermark applies the defaults (85%, "/").

// SetDiskWatermark overrides the host disk watermark and statfs path (tests
// and deployments whose runtime state lives off the root filesystem).
func (o *Overseer) SetDiskWatermark(pct int, path string) {
	o.diskWatermarkPct = pct
	o.diskStatPath = path
}

// defaultDiskWatermarkPct: above this used-percentage of the filesystem
// holding the runtime state, the digest flags host-level disk pressure.
// The 2026-09-25 incident hit 100% with zero warning (br I/O error 28,
// bead creation failed fleet-wide).
const defaultDiskWatermarkPct = 85

// collectDiskWatermark stats the filesystem at path ("" → "/", which holds
// /tmp, $HYDRA_HOME and the working state) and attaches a host anomaly when
// used% >= watermark. Fail-open: a statfs error never blocks the digest.
func collectDiskWatermark(d *pan.HealthDigest, path string, watermark int) {
	if path == "" {
		path = "/"
	}
	if watermark <= 0 {
		watermark = defaultDiskWatermarkPct
	}
	var fs syscall.Statfs_t
	if err := syscall.Statfs(path, &fs); err != nil {
		return // fail open
	}
	// Used% is block-based: fs.Blocks and fs.Bfree are both in fs.Bsize blocks.
	// (The 2026-10-06 incident mixed a byte-scaled total with a block count and
	// reported a constant ~99% on every filesystem, re-firing the disk-99%
	// campaign anomaly fleet-wide.)
	if fs.Blocks == 0 {
		return
	}
	usedPct := int(((fs.Blocks - fs.Bfree) * 100) / fs.Blocks)
	d.DiskUsedPct = usedPct
	if usedPct >= watermark {
		d.HostAnomalies = append(d.HostAnomalies, fmt.Sprintf(
			"disk usage %d%% on %s exceeds watermark %d%% — run systemd-tmpfiles cleanup and cap caches before br starts failing with I/O error 28",
			usedPct, path, watermark))
	}
}

// depRelated reports whether beads a and b reference each other directly.
func depRelated(depsByID map[string][]string, a, b string) bool {
	for _, d := range depsByID[a] {
		if d == b {
			return true
		}
	}
	for _, d := range depsByID[b] {
		if d == a {
			return true
		}
	}
	return false
}

// countStrandedWorktrees counts worktree dirs for a project whose bead is in
// a terminal state — work on disk that no live process will touch again.
func countStrandedWorktrees(projectPath string, statusByID map[string]types.BeadStatus) int {
	base := os.Getenv("HYDRA_HOME")
	if base == "" {
		base = os.TempDir()
	}
	dir := filepath.Join(base, ".hydra-worktrees", strings.ReplaceAll(projectPath, "/", "-"))
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	n := 0
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if st, ok := statusByID[e.Name()]; ok && st.IsTerminal() {
			n++
		}
	}
	return n
}

// countWTBranches counts hydra/wt-* branches in the project checkout.
// Informational only (work landed in worktree branches); fail-open.
func countWTBranches(projectPath string) int {
	out, err := exec.Command("git", "-C", projectPath, "branch", "--list", "hydra/wt-*").Output()
	if err != nil {
		return 0
	}
	n := 0
	for _, line := range strings.Split(string(out), "\n") {
		if strings.TrimSpace(line) != "" {
			n++
		}
	}
	return n
}

// parkCampaignRepair freezes a project's campaign by setting the durable
// project_parks flag in the registry. The scheduler consults this flag before
// spawning any worker for the project, so no new work starts while Pan's
// containment is in effect. Reversible with repair:unpark-campaign.
func (o *Overseer) parkCampaignRepair(beadID string) error {
	key, ok := pan.ProjectKeyFromAnomalyID(beadID)
	if !ok {
		return fmt.Errorf("park-campaign targets a <project>-campaign-anomaly pseudo ID, got %q", beadID)
	}
	if _, found := o.scanTargetByKey(key); !found {
		return fmt.Errorf("no scan target for project key %q", key)
	}
	if o.registryStore == nil {
		return fmt.Errorf("park-campaign %s: registry store unavailable", key)
	}
	if err := o.registryStore.SetProjectPark(key, true, "campaign frozen by Pan; investigate before release"); err != nil {
		return fmt.Errorf("park-campaign %s: %w", key, err)
	}
	log.Printf("[Pan] park-campaign %s: campaign parked (scheduler will skip spawns)", key)
	return nil
}

// unparkCampaignRepair releases a previously parked campaign.
func (o *Overseer) unparkCampaignRepair(beadID string) error {
	key, ok := pan.ProjectKeyFromAnomalyID(beadID)
	if !ok {
		return fmt.Errorf("unpark-campaign targets a <project>-campaign-anomaly pseudo ID, got %q", beadID)
	}
	if _, found := o.scanTargetByKey(key); !found {
		return fmt.Errorf("no scan target for project key %q", key)
	}
	if o.registryStore == nil {
		return fmt.Errorf("unpark-campaign %s: registry store unavailable", key)
	}
	if err := o.registryStore.SetProjectPark(key, false, ""); err != nil {
		return fmt.Errorf("unpark-campaign %s: %w", key, err)
	}
	log.Printf("[Pan] unpark-campaign %s: campaign released", key)
	return nil
}

// scanTargetByKey finds the scan target path for a digest project key.
func (o *Overseer) scanTargetByKey(key string) (string, bool) {
	for _, project := range o.projects {
		for _, target := range repoScanTargets(project) {
			if projectKey(target) == key {
				return target, true
			}
		}
	}
	return "", false
}

// panFilingDigestStaleAge caps how old the digest backing a file-bead
// decision may be at filing time. Pan judgments run seconds behind digest
// collection, so a much older digest means the finding may rest on stale
// state — and acting on stale state is exactly how ajantha-srv-jobfile-jm2
// got filed (2026-10-05 04:01Z) claiming "no worktree, no worker" when the
// worktree existed and the worker had been live minutes earlier.
const panFilingDigestStaleAge = 15 * time.Minute

// panDigestAt is the unix-nano timestamp of the last collectHealthDigest
// run (0 = never run / unknown → fail-open, no staleness gate).

// emitPanFilingSkipped records a typed event when a filing is refused on a
// re-verified stale premise. Fail-open: a broken sink never blocks filing.
func (o *Overseer) emitPanFilingSkipped(projectPath, beadID, reason string) {
	if o.eventSink == nil {
		return
	}
	o.eventSink.EmitRepo(projectPath, Event{
		Type:    "pan_bead_filed_skipped",
		Project: filepath.Base(projectPath),
		Bead:    beadID,
		Reason:  "premise_stale",
		Fields:  map[string]any{"reason": reason},
	})
}

// verifyPanFilingPremise re-probes live state before a wedge bead is filed.
// Returns a reason string ("" = premise holds, filing may proceed):
//   - digest_stale: the digest the judgment was built on is older than
//     panFilingDigestStaleAge (or never collected) — premises may be stale;
//   - source_resolved: the judgment source bead is now terminal — wedge gone;
//   - worker_live: the source bead has a live registry worker (fresh
//     heartbeat, <5m, same liveness rule as the digest) — a live worker is
//     mid-flight, not a silent wedge.
//
// Fail-open: any probe error means "cannot prove the premise false" and lets
// the filing proceed — a genuine wedge must stay visible.
func (o *Overseer) verifyPanFilingPremise(projectPath, beadID string) string {
	if at := o.panDigestAt.Load(); at != 0 {
		if age := time.Since(time.Unix(0, at)); age > panFilingDigestStaleAge {
			return "digest_stale"
		}
	}
	if pan.IsPseudoBeadID(beadID) {
		return "" // no real source bead to re-probe
	}
	if o.beadsClient == nil {
		return ""
	}
	bead, err := o.beadsClient.GetBead(projectPath, "", types.BeadID(beadID))
	if err != nil || bead == nil {
		return "" // fail-open: cannot prove resolved
	}
	if bead.Status.IsTerminal() {
		return "source_resolved"
	}
	if o.registryStore != nil {
		if rec, ok, err := o.registryStore.GetWorker(beadID); err == nil && ok {
			live := rec.State == registry.StateWorking || time.Since(rec.LastHeartbeat) < 5*time.Minute
			if live {
				return "worker_live"
			}
		}
	}
	return ""
}

// filePanBead implements the FR-022 bead-filing actuator daemon-side: it
// re-verifies live state (jm2 guard), then enforces the per-project cap of
// MaxOpenFiledBeadsPerProject non-terminal (open or in_progress) "pan-filed"
// beads, then creates the improvement bead. Counting only open beads let the
// cap be bypassed whenever a filed bead was claimed (goes straight to
// in_progress) before the next filing.

// panFileSigTTL is the dedupe window for pan-filed wedge signatures: within
// it, one normalized-signature finding files exactly one bead per project,
// even if the earlier bead was closed or its cap slot freed. (PR #232)
var panFileSigTTL = time.Hour
func (o *Overseer) filePanBead(projectPath, beadID, title, description string) error {
	if reason := o.verifyPanFilingPremise(projectPath, beadID); reason != "" {
		o.emitPanFilingSkipped(projectPath, beadID, reason)
		return fmt.Errorf("file-bead %s: %w (%s)", beadID, pan.ErrFileBeadPremiseStale, reason)
	}
	if o.beadsClient == nil {
		return fmt.Errorf("file-bead %s: beads client unavailable", beadID)
	}
	normTitle := pan.NormalizeReason(strings.TrimSpace(title))
	sig := projectPath + "|" + normTitle
	// Time-window dedupe: a signature filed (or deduped) within
	// panFileSigTTL must not file again, even after the earlier wedge bead
	// was closed — N blocked beads with one root cause yield ONE wedge bead.
	now := time.Now()
	o.panSigMu.Lock()
	if o.recentPanSigs != nil {
		if seen, ok := o.recentPanSigs[sig]; ok && now.Sub(seen) < panFileSigTTL {
			o.panSigMu.Unlock()
			return fmt.Errorf("file-bead %s: %w (%s)", beadID, pan.ErrFileBeadDuplicate, sig)
		}
	}
	o.panSigMu.Unlock()
	beads, err := o.beadsClient.ListBeads(projectPath, "")
	if err != nil {
		return fmt.Errorf("file-bead %s: list beads: %w", beadID, err)
	}
	openFiled := 0
	for _, b := range beads {
		if b.Status.IsTerminal() {
			continue
		}
		isFiled := false
		for _, lbl := range b.Labels {
			if lbl == pan.FiledLabel {
				isFiled = true
				break
			}
		}
		if !isFiled {
			continue
		}
		openFiled++
		// Persistent dedupe: a non-terminal pan-filed bead with the same
		// normalized title already tracks this finding — skip instead of filing
		// a twin (session-level filedSigs dedupe is lost on daemon restart).
		if pan.NormalizeReason(strings.TrimSpace(b.Title)) == normTitle {
			o.panSigMu.Lock()
			if o.recentPanSigs == nil {
				o.recentPanSigs = make(map[string]time.Time)
			}
			o.recentPanSigs[sig] = time.Now()
			o.panSigMu.Unlock()
			return fmt.Errorf("file-bead %s: %w (%s)", beadID, pan.ErrFileBeadDuplicate, b.ID)
		}
	}
	if openFiled >= pan.MaxOpenFiledBeadsPerProject {
		return fmt.Errorf("file-bead %s: cap reached (%d open pan-filed beads in project)", beadID, openFiled)
	}
	req := types.CreateBeadRequest{
		Title:       title,
		Description: description,
		Type:        "task",
		Priority:    2,
		Labels:      []string{pan.FiledLabel},
	}
	newID, err := o.beadsClient.CreateBead(projectPath, req)
	if err != nil {
		return fmt.Errorf("file-bead %s: %w", beadID, err)
	}
	// Record the signature only on success (a filer error must leave the
	// window open so the retry can file).
	o.panSigMu.Lock()
	if o.recentPanSigs == nil {
		o.recentPanSigs = make(map[string]time.Time)
	}
	o.recentPanSigs[sig] = time.Now()
	o.panSigMu.Unlock()
	if o.eventSink != nil {
		o.eventSink.EmitRepo(projectPath, Event{
			Type:    "pan_bead_filed",
			Project: filepath.Base(projectPath),
			Bead:    string(newID),
			Reason:  "pan_filed",
			Fields:  map[string]any{"source": beadID},
		})
	}
	log.Printf("[PanRepair] file-bead %s: filed %s in %s", beadID, newID, projectPath)
	return nil
}

// logImprovementRepair appends a Pan observation to the improvements log.
// Proposals, never self-modification: a human reviews the file and decides
// what becomes a bead.
func (o *Overseer) logImprovementRepair(beadID, text string) error {
	path := o.panImprovementsPath()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open improvements log: %w", err)
	}
	defer f.Close()
	line := fmt.Sprintf("%s [%s] %s\n", time.Now().UTC().Format(time.RFC3339), beadID, strings.ReplaceAll(text, "\n", " "))
	if _, err := f.WriteString(line); err != nil {
		return fmt.Errorf("write improvement: %w", err)
	}
	return nil
}

// panImprovementsPath resolves the improvements log under HYDRA_HOME
// (test-pinned via TestMain) so it never lands inside a code checkout.
func (o *Overseer) panImprovementsPath() string {
	base := os.Getenv("HYDRA_HOME")
	if base == "" {
		base = os.TempDir()
	}
	return filepath.Join(base, "pan-improvements.log")
}
