package api

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/haulmatic-dev/hydra/pkg/types"
)

// ErrorResponse represents an API error
type ErrorResponse struct {
	Error   string `json:"error"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

// BeadResponse represents a bead in API responses
type BeadResponse struct {
	ID           string    `json:"id"`
	Project      string    `json:"project"`
	Repo         string    `json:"repo,omitempty"`
	Number       int       `json:"number"`
	Title        string    `json:"title"`
	Type         string    `json:"type"`
	Status       string    `json:"status"`
	Dependencies []string  `json:"dependencies,omitempty"`
	Labels       []string  `json:"labels,omitempty"`
	Priority     int       `json:"priority"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// WorkerResponse represents a worker in API responses
type WorkerResponse struct {
	ID        string    `json:"id"`
	BeadID    string    `json:"bead_id"`
	AgentType string    `json:"agent_type"`
	PID       int       `json:"pid"`
	Status    string    `json:"status"`
	StartTime time.Time `json:"start_time"`
}

// ProjectResponse represents a project in API responses
type ProjectResponse struct {
	Name  string     `json:"name"`
	Path  string     `json:"path"`
	Type  string     `json:"type"`
	Repos []RepoInfo `json:"repos,omitempty"`
}

// RepoInfo represents repository info
type RepoInfo struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

// SystemStats represents system statistics
type SystemStats struct {
	ActiveWorkers   int       `json:"active_workers"`
	MaxWorkers      int       `json:"max_workers"`
	TotalProjects   int       `json:"total_projects"`
	Uptime          int       `json:"uptime_seconds"`
	OverseerRunning bool      `json:"overseer_running"`
	Timestamp       time.Time `json:"timestamp"`
}

// toBeadResponse converts a Bead to API response format
func toBeadResponse(bead types.Bead) BeadResponse {
	resp := BeadResponse{
		ID:        bead.ID.String(),
		Project:   bead.Project,
		Repo:      bead.Repo,
		Title:     bead.Title,
		Type:      bead.Type,
		Status:    string(bead.Status),
		Labels:    bead.Labels,
		Priority:  bead.Priority,
		CreatedAt: bead.CreatedAt,
		UpdatedAt: bead.UpdatedAt,
	}

	// Try to extract numeric ID for backward compatibility
	if num, err := bead.ID.Number(); err == nil {
		resp.Number = num
	}

	for _, dep := range bead.Dependencies {
		resp.Dependencies = append(resp.Dependencies, dep.String())
	}

	return resp
}

// toWorkerResponse converts a Worker to API response format
func toWorkerResponse(worker *types.Worker) WorkerResponse {
	return WorkerResponse{
		ID:        worker.ID,
		BeadID:    worker.BeadID.String(),
		AgentType: string(worker.AgentType),
		PID:       worker.PID,
		Status:    worker.Status,
		StartTime: worker.StartTime,
	}
}

// HealthHandler returns health status
func (s *Server) HealthHandler(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status":  "healthy",
		"version": "1.0.0",
	})
}

// GetStatsHandler returns system statistics
func (s *Server) GetStatsHandler(c *gin.Context) {
	stats := SystemStats{
		ActiveWorkers:   s.overseer.GetActiveWorkerCount(),
		MaxWorkers:      s.config.Overseer.MaxWorkers,
		TotalProjects:   len(s.overseer.GetProjects()),
		Uptime:          int(time.Since(time.Now().Add(-time.Hour)).Seconds()), // TODO: Track actual uptime
		OverseerRunning: s.overseer.IsRunning(),
		Timestamp:       time.Now(),
	}

	c.JSON(http.StatusOK, stats)
}

// GetProjectsHandler returns all projects
func (s *Server) GetProjectsHandler(c *gin.Context) {
	projects := s.overseer.GetProjects()
	response := make([]ProjectResponse, len(projects))

	for i, project := range projects {
		resp := ProjectResponse{
			Name: project.Name,
			Path: project.Path,
			Type: string(project.Type),
		}

		for _, repo := range project.Repos {
			resp.Repos = append(resp.Repos, RepoInfo{
				Name: repo.Name,
				Path: repo.Path,
			})
		}

		response[i] = resp
	}

	c.JSON(http.StatusOK, response)
}

// GetWorkersHandler returns active workers
func (s *Server) GetWorkersHandler(c *gin.Context) {
	workers := s.overseer.GetActiveWorkers()
	response := make([]WorkerResponse, len(workers))

	for i, worker := range workers {
		response[i] = toWorkerResponse(&worker)
	}

	c.JSON(http.StatusOK, response)
}

// PauseOverseerHandler pauses the overseer
func (s *Server) PauseOverseerHandler(c *gin.Context) {
	s.overseer.Pause()
	c.JSON(http.StatusOK, gin.H{"status": "paused"})
}

// ResumeOverseerHandler resumes the overseer
func (s *Server) ResumeOverseerHandler(c *gin.Context) {
	s.overseer.Resume()
	c.JSON(http.StatusOK, gin.H{"status": "resumed"})
}

// SetMaxWorkersHandler sets the maximum number of workers
func (s *Server) SetMaxWorkersHandler(c *gin.Context) {
	var req struct {
		Count int `json:"count" binding:"required,min=1,max=20"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error:   "invalid_request",
			Code:    "BAD_REQUEST",
			Message: err.Error(),
		})
		return
	}

	s.overseer.SetMaxWorkers(req.Count)
	c.JSON(http.StatusOK, gin.H{
		"max_workers": req.Count,
		"status":      "updated",
	})
}
