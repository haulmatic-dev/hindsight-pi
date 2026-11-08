package core

// git_ownership_preflight.go — onboarding preflight (bead hydra-hlen, fmsfe
// wedge 2026-10): hydra-user git ops in another user's repo die with
// "detected dubious ownership" (git exit 128). Every spawn then hard-parked
// mid-campaign because detection happened only at spawn time.
//
// Two failure surfaces, one typed class:
//
//  1. Preflight (daemon start, before the project is enabled for work):
//     run `git -C <path> rev-parse` AS THE HYDRA USER (the daemon's own
//     identity). Exit 128 "dubious ownership" => typed
//     `git_ownership_unusable` event + auto-park, with the EXACT remediation
//     command in the message.
//
//  2. Spawn time: a worktree setup failure of the same class maps to the
//     distinct hard-park label `git-ownership` (NOT generic
//     worktree-unavailable) + typed `spawn_failed_git_ownership` event, so
//     Pan's bead filing carries the exact fix command.
//
// SCOPE: detection + remediation TEXT only. The daemon must NEVER auto-run
// `git config --global safe.directory` — granting git trust on another
// user's repository is a security posture change that stays a human/T3
// action.

import (
	"fmt"
	"log"
	"os/exec"
	"strings"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// gitOwnershipLabel is the durable hard-park marker for a bead whose spawn
// (or its project preflight) failed because git refused the repository on
// ownership grounds. Distinct from worktreeUnavailableLabel so triage, Pan
// candidacy, and the event detail can name the exact one-command fix.
const gitOwnershipLabel = "git-ownership"

// gitOwnershipRemediation returns the exact remediation command text for the
// failing repo path. TEXT ONLY: nothing here executes it.
func gitOwnershipRemediation(path string) string {
	return fmt.Sprintf("Run as the repo owner (human/T3 action; hydra must never set safe.directory itself): git config --global --add safe.directory %s", path)
}

// isDubiousOwnershipError reports whether a git error is the exit-128
// "detected dubious ownership" refusal.
func isDubiousOwnershipError(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(strings.ToLower(err.Error()), "dubious ownership")
}

// gitAccessProbe runs `git rev-parse --is-inside-work-tree` in the repo as
// the daemon's user (the hydra user in production). Injectable for tests.
var gitAccessProbe = func(repoPath string) error {
	cmd := exec.Command("git", "rev-parse", "--is-inside-work-tree")
	cmd.Dir = repoPath
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git rev-parse failed (%v): %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// RunGitOwnershipPreflight probes every daemon-managed project repo with the
// git-access-as-hydra check. A dubious-ownership refusal fails CLOSED (typed
// event + auto-park); probe errors of any other shape fail open (network,
// git missing) — same policy as the credential preflight.
func (o *Overseer) RunGitOwnershipPreflight() {
	if o == nil || o.config == nil {
		return
	}
	for _, project := range o.GetProjects() {
		repoPaths := []string{project.Path}
		if project.Type == types.RepoTypeMulti && len(project.Repos) > 0 {
			repoPaths = repoPaths[:0]
			for _, repo := range project.Repos {
				if p := resolveRepoPath(project, repo); p != "" {
					repoPaths = append(repoPaths, p)
				}
			}
		}
		for _, repoPath := range repoPaths {
			o.preflightGitOwnership(project, repoPath)
		}
	}
}

// preflightGitOwnership probes one repo dir as the hydra user.
func (o *Overseer) preflightGitOwnership(project types.Project, repoPath string) {
	if !isGitWorktree(repoPath) {
		return // not a checkout; nothing to probe
	}
	if err := gitAccessProbe(repoPath); err != nil {
		if isDubiousOwnershipError(err) {
			o.emitGitOwnershipUnusable(project, repoPath, err.Error())
			return
		}
		log.Printf("[GitOwnershipPreflight] %s: probe failed open: %v", project.Name, err)
		return
	}
	log.Printf("[GitOwnershipPreflight] %s: git access OK as daemon user", project.Name)
}

// emitGitOwnershipUnusable logs, emits the typed event (with the exact
// remediation command in the detail), and auto-parks the project.
// Park failure is fail-open (event still emitted).
func (o *Overseer) emitGitOwnershipUnusable(project types.Project, repoPath, probeErr string) {
	remediation := gitOwnershipRemediation(repoPath)
	log.Printf("[GitOwnershipPreflight] ⚠️  %s: git ownership rejected — failing closed and parking project: %v", project.Name, probeErr)
	if o.eventSink != nil {
		o.eventSink.Emit(Event{
			Type:    "git_ownership_unusable",
			Project: project.Name,
			Reason:  "git_access_denied_ownership",
			Detail:  fmt.Sprintf("%v. %s", probeErr, remediation),
		})
	}
	if o.registryStore == nil {
		log.Printf("[GitOwnershipPreflight] %s: no registry store — cannot auto-park (loud event emitted)", project.Name)
		return
	}
	if err := o.registryStore.SetProjectPark(project.Name, true,
		"auto-parked: git refused the repository as the hydra user (dubious ownership); "+remediation); err != nil {
		log.Printf("[GitOwnershipPreflight] %s: auto-park failed: %v", project.Name, err)
	}
}

// noteSpawnGitOwnership handles the spawn-time occurrence: hard-parks the
// bead with the distinct git-ownership label and emits the typed event so
// Pan's filing contains the exact fix command. Returns the labeled failure
// error for the caller to surface. eventSink/beadsClient are nil-safe.
func noteSpawnGitOwnership(eventSink *EventSink, beadsClient BeadsClient, projectPath string, bead types.Bead, cause error) error {
	beadIDStr := bead.ID.String()
	repoPath := projectPath
	if repoPath == "" {
		repoPath = "<project-path>"
	}
	remediation := gitOwnershipRemediation(repoPath)
	detail := fmt.Sprintf("spawn failed for %s: git ownership rejected (%v). %s", beadIDStr, cause, remediation)
	if eventSink != nil && projectPath != "" {
		eventSink.EmitRepo(projectPath, Event{
			Type:    "spawn_failed_git_ownership",
			Reason:  "git_access_denied_ownership",
			Project: bead.Project,
			Bead:    beadIDStr,
			Detail:  detail,
		})
	} else {
		log.Printf("[Spawn] spawn_failed_git_ownership: %s", detail)
	}
	if beadsClient != nil && projectPath != "" {
		if lblErr := beadsClient.UpdateBeadLabels(projectPath, "", bead.ID, []string{gitOwnershipLabel}); lblErr != nil {
			log.Printf("[Spawn] failed to hard-park %s with %q label: %v", beadIDStr, gitOwnershipLabel, lblErr)
		}
	}
	return fmt.Errorf("git-ownership: spawn failed for %s (hard-parked; %s): %w", beadIDStr, remediation, cause)
}
