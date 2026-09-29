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
	"sort"
	"strings"
	"syscall"
	"time"

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

	// Live worker rows once for the whole scan.
	liveByKey := map[string][]registry.WorkerRecord{}
	if o.registryStore != nil {
		if workers, err := o.registryStore.ListWorkers(registry.StateWorking, registry.StateParked); err == nil {
			for _, w := range workers {
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

			beads, err := o.beadsClient.ListBeads(target, "")
			if err != nil {
				continue // fail open per store
			}
			statusByID := make(map[string]types.BeadStatus, len(beads))
			depsByID := make(map[string][]string, len(beads))
			for _, b := range beads {
				id := b.ID.String()
				statusByID[id] = b.Status
				if b.Status.IsTerminal() {
					continue
				}
				switch b.Status {
				case types.StatusOpen:
					ph.Open++
				case types.StatusInProgress:
					ph.InProgress++
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
	total := fs.Blocks * uint64(fs.Bsize)
	if total == 0 {
		return
	}
	usedPct := int(((total - fs.Bfree) * 100) / total)
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

// filePanBead implements the FR-022 bead-filing actuator daemon-side: it
// enforces the per-project cap of MaxOpenFiledBeadsPerProject open
// "pan-filed" beads, then creates the improvement bead.
func (o *Overseer) filePanBead(projectPath, beadID, title, description string) error {
	if o.beadsClient == nil {
		return fmt.Errorf("file-bead %s: beads client unavailable", beadID)
	}
	beads, err := o.beadsClient.ListBeads(projectPath, "")
	if err != nil {
		return fmt.Errorf("file-bead %s: list beads: %w", beadID, err)
	}
	openFiled := 0
	for _, b := range beads {
		if b.Status != types.StatusOpen {
			continue
		}
		for _, lbl := range b.Labels {
			if lbl == pan.FiledLabel {
				openFiled++
				break
			}
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
	if o.eventSink != nil {
		o.eventSink.EmitRepo(projectPath, Event{
			Type:   "pan_bead_filed",
			Project: filepath.Base(projectPath),
			Bead:   string(newID),
			Reason: "pan_filed",
			Fields: map[string]any{"source": beadID},
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
