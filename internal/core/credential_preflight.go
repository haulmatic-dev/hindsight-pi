package core

// Credential preflight (2026-10-01 fleet-grounding incident): the daemon's
// private ~/.git-credentials held a STALE token while the working PAT sat in
// the owning user's ~/.hydra/.github-token — publishes failed silently for
// 7h before a human noticed. Two failures it must catch:
//
//   - credential drift: token revoked / rotated elsewhere (401 on ls-remote)
//   - repo access revoked while the token itself is still valid (GitHub
//     returns 404 "Repository not found" for a fine-grained/classic PAT
//     without repo scope on that org) — an account-level /user probe would
//     return 200 and miss this; the probe is REPO-level by design.
//
// Run once at daemon start, per project repo: resolve the SAME publish
// identity the publish path will use (owner PAT / token owner user), then
// `git ls-remote` the origin. A stale result emits a typed
// `credential_stale{project}` event and AUTO-PARKS the project so beads stop
// entering the silent publish-failure loop. Probe errors (network, git
// missing) fail OPEN — no event, no park.

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"strings"
	"time"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// credentialProbeTimeout bounds one ls-remote.
const credentialProbeTimeout = 20 * time.Second

// credentialProbe is injectable for tests. Args: repoPath, resolved token.
// Returns nil on success; a non-nil error whose message classifies as stale
// (see classifyCredentialError) triggers the stale path.
var credentialProbe = func(repoPath, token string) error {
	url, err := gitOut(repoPath, "remote", "get-url", "origin")
	if err != nil {
		return fmt.Errorf("no origin remote: %w", err) // not a classifiable failure
	}
	target := authenticatedURL(strings.TrimSpace(url), token)
	if target == "" {
		return nil // local-only remote or no token — nothing to probe
	}
	ctx, cancel := context.WithTimeout(context.Background(), credentialProbeTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "ls-remote", "--heads", target)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ls-remote: %s", redactToken(err.Error()+" "+string(out), token))
	}
	return nil
}

// classifyCredentialError reports whether a probe error indicates a stale or
// unauthorized credential (fail-closed -> park), as opposed to transient
// network trouble (fail-open -> ignore).
func classifyCredentialError(msg string) bool {
	low := strings.ToLower(msg)
	for _, frag := range []string{
		"authentication failed", "could not read username", "401",
		"repository not found", "404", "permission denied", "not authorized",
		"terminal prompts disabled",
	} {
		if strings.Contains(low, frag) {
			return true
		}
	}
	return false
}

// RunCredentialPreflight probes every daemon-managed project repo's origin
// with the resolved publish identity. Stale credentials are surfaced with a
// typed event and an auto-park. Fail-open: never returns an error.
func (o *Overseer) RunCredentialPreflight() {
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
			o.preflightOneRepo(project, repoPath)
		}
	}
}

// credentialParkReasonPrefix marks parks this subsystem created; only those
// are eligible for automatic recovery (a human-set park reason stays put).
const credentialParkReasonPrefix = "auto-parked: GitHub credential"

// RunCredentialRecoverySweep re-probes ONLY projects currently parked by a
// credential preflight failure. A park with no paired recovery leaves the
// project grounded forever after the operator fixes the PAT (hydra itself
// sat parked 26h past credential recovery, 2026-10-06). Runs on an interval
// from service_starter; per-project cost is one ls-remote and only while
// parked. Fail-open throughout.
func (o *Overseer) RunCredentialRecoverySweep() {
	if o == nil || o.config == nil || o.registryStore == nil {
		return
	}
	for _, project := range o.GetProjects() {
		info, ok, err := o.registryStore.ProjectParkInfo(project.Name)
		if err != nil || !ok || !info.Parked {
			continue
		}
		if !strings.HasPrefix(info.Reason, credentialParkReasonPrefix) {
			continue
		}
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
			o.preflightOneRepo(project, repoPath)
		}
	}
}

// preflightOneRepo probes one repo dir. Identity resolution failure means
// the publish path would fail closed anyway — surface it as credential_stale.
func (o *Overseer) preflightOneRepo(project types.Project, repoPath string) {
	if !isGitWorktree(repoPath) {
		return // local-only or not a checkout; nothing to probe
	}
	id, err := resolvePublishIdentityFor(project.Path)
	if err != nil {
		o.emitCredentialStale(project, fmt.Sprintf("identity unresolved: %v", err))
		return
	}
	if err := credentialProbe(repoPath, id.Token); err != nil {
		if classifyCredentialError(err.Error()) {
			o.emitCredentialStale(project, redactToken(err.Error(), id.Token))
		} else {
			log.Printf("[CredentialPreflight] %s: probe failed open: %v", project.Name, err)
		}
		return
	}
	o.maybeUnparkCredentialRecovered(project)
	log.Printf("[CredentialPreflight] %s: origin probe OK (owner=%s source=%s)", project.Name, id.Owner, id.Source)
}

// maybeUnparkCredentialRecovered clears a park this subsystem set, once the
// same probe that parked the project passes again. Human-set parks (any
// other reason) are never touched. Fail-open: unpark failure only logs.
func (o *Overseer) maybeUnparkCredentialRecovered(project types.Project) {
	if o.registryStore == nil {
		return
	}
	info, ok, err := o.registryStore.ProjectParkInfo(project.Name)
	if err != nil || !ok || !info.Parked {
		return
	}
	if !strings.HasPrefix(info.Reason, credentialParkReasonPrefix) {
		return
	}
	if err := o.registryStore.SetProjectPark(project.Name, false,
		"auto-unparked: GitHub credential repo-level probe passed again"); err != nil {
		log.Printf("[CredentialPreflight] %s: auto-unpark failed: %v", project.Name, err)
		return
	}
	log.Printf("[CredentialPreflight] %s: credential RECOVERED — unparked project", project.Name)
	if o.eventSink != nil {
		o.eventSink.Emit(Event{
			Type:    "credential_recovered",
			Project: project.Name,
			Reason:  "preflight_repo_probe_passed",
		})
	}
}

// emitCredentialStale logs, emits the typed event, and auto-parks the
// project. Park failure is fail-open (event still emitted).
func (o *Overseer) emitCredentialStale(project types.Project, reason string) {
	log.Printf("[CredentialPreflight] ⚠️  %s: credential STALE — parking project: %s", project.Name, reason)
	if o.eventSink != nil {
		o.eventSink.Emit(Event{
			Type:    "credential_stale",
			Project: project.Name,
			Reason:  "preflight_repo_probe_failed",
			Detail:  reason,
		})
	}
	if o.registryStore == nil {
		log.Printf("[CredentialPreflight] %s: no registry store — cannot auto-park (loud event emitted)", project.Name)
		return
	}
	if err := o.registryStore.SetProjectPark(project.Name, true,
		"auto-parked: GitHub credential failed repo-level preflight probe (stale token or revoked repo access); fix the PAT and unpark"); err != nil {
		log.Printf("[CredentialPreflight] %s: auto-park failed: %v", project.Name, err)
	}
}
