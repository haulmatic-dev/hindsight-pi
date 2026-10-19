package api

// Operator Park/Quarantine API (FR-033, autonomous-resolution-layer PRD).
//
// Until now the only way for an operator to park a project campaign or clear
// quarantine labels was direct sqlite manipulation of registry.db or hand-run
// `br label remove` calls. These endpoints expose the same operations through
// the daemon, which is the single writer by contract, and emit a typed audit
// event on the shepherd stream for every mutation (NFR-002).
//
// Endpoints (all under /api/v1):
//   POST /projects/:name/park    {"reason": "..."} — park a project campaign
//   POST /projects/:name/unpark  {"reason": "..."} — release the park
//   GET  /quarantine             — list quarantined beads and branches
//   POST /quarantine/:bead/clear {"project": "...", "labels": [...]} — clear
//                                   quarantine labels from a bead
//
// All endpoints answer 503 when the worker registry is unavailable (nil
// store), matching the worker registry routes' fail behavior.

import (
	"net/http"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/haulmatic-dev/hydra/internal/core"
	"github.com/haulmatic-dev/hydra/internal/registry"
	"github.com/haulmatic-dev/hydra/pkg/types"
)

// quarantineLabels are the bead labels treated as quarantine state by the
// operator API. They mirror the hard-park and stranded-work labels the
// scheduler and stranded-work detector key on.
var quarantineLabels = []string{
	"stranded-work",
	"stranded-conflict",
	"integration-unverified",
	"integration-exhausted",
	"worktree-unavailable",
}

// RegisterOperatorRoutes wires the operator park/unpark/quarantine endpoints
// into the v1 route group. getStore is resolved per request because the
// registry opens after the API server is built.
func RegisterOperatorRoutes(v1 *gin.RouterGroup, o *core.Overseer, getStore func() *registry.Store) {
	v1.POST("/projects/:name/park", func(c *gin.Context) { handlePark(c, o, getStore, true) })
	v1.POST("/projects/:name/unpark", func(c *gin.Context) { handlePark(c, o, getStore, false) })
	v1.GET("/quarantine", func(c *gin.Context) { handleListQuarantine(c, o) })
	v1.POST("/quarantine/:bead/clear", func(c *gin.Context) { handleClearQuarantine(c, o) })
}

func handlePark(c *gin.Context, o *core.Overseer, getStore func() *registry.Store, parked bool) {
	store := getStore()
	if store == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "registry unavailable"})
		return
	}
	var body struct {
		Reason string `json:"reason"`
	}
	_ = c.ShouldBindJSON(&body) // body optional; empty reason allowed

	name := c.Param("name")
	// hydra-6tfb: store the park under the SAME namespace the scheduler reads.
	// campaignParked() looks up projectKey(projectPath), which user-namespaces
	// home/<user>/projects/<name> to <user>/<name> — storing under the raw
	// config name made parks silently ignored (2026-10-19: a parked campaign
	// kept spawning for 10+ minutes with the API answering 200 parked:true).
	// Accept the config name OR an already-namespaced key; resolve to the key.
	parkKey := name
	if o != nil {
		for _, p := range o.GetProjects() {
			if p.Name == name {
				parkKey = core.ProjectKey(p.Path)
				break
			}
		}
	}
	if err := store.SetProjectPark(parkKey, parked, body.Reason); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	action := "project_unparked"
	verb := "unparked"
	if parked {
		action = "project_parked"
		verb = "parked"
	}
	// Audit event: every operator mutation lands on the shepherd stream.
	o.EventSink().Emit(core.Event{
		Type:    action,
		Project: name,
		Reason:  "operator_api",
		Detail:  "operator " + verb + " project campaign via API",
		Fields:  map[string]any{"reason": body.Reason},
	})
	c.JSON(http.StatusOK, gin.H{"ok": true, "project": name, "parked": parked})
}

func handleListQuarantine(c *gin.Context, o *core.Overseer) {
	type quarantinedBead struct {
		ID     string   `json:"id"`
		Title  string   `json:"title"`
		Status string   `json:"status"`
		Labels []string `json:"labels"`
	}
	type projectReport struct {
		Project  string            `json:"project"`
		Path     string            `json:"path"`
		Branches []string          `json:"branches"`
		Beads    []quarantinedBead `json:"beads"`
		Error    string            `json:"error,omitempty"`
	}

	reports := []projectReport{}
	for _, p := range o.Projects() {
		rep := projectReport{Project: p.Name, Path: p.Path, Branches: []string{}, Beads: []quarantinedBead{}}

		branches, err := core.ListQuarantineBranches(p.Path)
		if err != nil {
			rep.Error = err.Error()
		} else {
			rep.Branches = make([]string, 0, len(branches))
			for _, b := range branches {
				rep.Branches = append(rep.Branches, b.Name)
			}
		}

		repoName := ""
		if p.Type == types.RepoTypeMulti && len(p.Repos) > 0 {
			repoName = p.Repos[0].Name
		}
		beads, err := o.BeadsClient().ListBeads(p.Path, repoName)
		if err != nil {
			if rep.Error == "" {
				rep.Error = err.Error()
			}
		} else {
			for _, b := range beads {
				q := intersectLabels(b.Labels, quarantineLabels)
				if len(q) == 0 {
					continue
				}
				rep.Beads = append(rep.Beads, quarantinedBead{
					ID:     string(b.ID),
					Title:  b.Title,
					Status: string(b.Status),
					Labels: q,
				})
			}
		}
		reports = append(reports, rep)
	}
	c.JSON(http.StatusOK, gin.H{"projects": reports})
}

func handleClearQuarantine(c *gin.Context, o *core.Overseer) {
	beadID := types.BeadID(c.Param("bead"))
	var body struct {
		Project string   `json:"project"`
		Labels  []string `json:"labels"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || body.Project == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "project field required"})
		return
	}

	proj, ok := findProject(o.Projects(), body.Project)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "project not found: " + body.Project})
		return
	}

	repoName := ""
	if proj.Type == types.RepoTypeMulti && len(proj.Repos) > 0 {
		repoName = proj.Repos[0].Name
	}
	bead, err := o.BeadsClient().GetBead(proj.Path, repoName, beadID)
	if err != nil || bead == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "bead not found: " + string(beadID)})
		return
	}

	// Default: clear every quarantine label present. Explicit list narrows it
	// but is intersected with the quarantine set so the endpoint can never
	// remove arbitrary labels.
	want := quarantineLabels
	if len(body.Labels) > 0 {
		want = intersectLabels(body.Labels, quarantineLabels)
	}
	toRemove := intersectLabels(bead.Labels, want)
	if len(toRemove) == 0 {
		c.JSON(http.StatusOK, gin.H{"ok": true, "removed": []string{}})
		return
	}
	if err := o.BeadsClient().RemoveBeadLabels(proj.Path, repoName, beadID, toRemove); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	o.EventSink().EmitRepo(proj.Path, core.Event{
		Type:    "quarantine_cleared",
		Project: proj.Name,
		Bead:    string(beadID),
		Reason:  "operator_api",
		Detail:  "operator cleared quarantine labels via API",
		Fields:  map[string]any{"labels": toRemove},
	})
	c.JSON(http.StatusOK, gin.H{"ok": true, "removed": toRemove})
}

func findProject(projects []types.Project, name string) (types.Project, bool) {
	for _, p := range projects {
		if p.Name == name || filepath.Base(p.Path) == name {
			return p, true
		}
	}
	return types.Project{}, false
}

func intersectLabels(have, want []string) []string {
	out := []string{}
	for _, w := range want {
		for _, h := range have {
			if strings.EqualFold(w, h) {
				out = append(out, h)
				break
			}
		}
	}
	return out
}
