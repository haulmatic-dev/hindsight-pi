package api

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/haulmatic-dev/hydra/internal/registry"
)

// RegisterWorkerRegistryRoutes wires the worker-facing registry endpoints.
// getStore is resolved per request (the registry opens in the service
// starter, after the API server is built). Nil store (registry unavailable)
// answers 503 so workers fall back to pre-registry behavior.
func RegisterWorkerRegistryRoutes(v1 *gin.RouterGroup, getStore func() *registry.Store) {
	v1.POST("/workers/report", func(c *gin.Context) {
		store := getStore()
		if store == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "registry unavailable"})
			return
		}
		var rep registry.WorkerReport
		if err := c.ShouldBindJSON(&rep); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		if rep.BeadID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "bead_id required"})
			return
		}
		state := registry.WorkerState(rep.State)
		switch state {
		case registry.StateWorking, registry.StateParked, registry.StateHung:
		default:
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid state: " + rep.State})
			return
		}

		now := time.Now()
		rec := registry.WorkerRecord{
			BeadID:              rep.BeadID,
			PID:                 rep.PID,
			PGID:                rep.PGID,
			Project:             rep.Project,
			StartedAt:           now,
			LastHeartbeat:       now,
			State:               state,
			PiSessionID:         rep.PiSessionID,
			ErrStatus:           rep.ErrStatus,
			ErrType:             rep.ErrType,
			ErrMessage:          rep.ErrMessage,
			TokensSoFar:         rep.TokensSoFar,
			TokensFresh:         rep.TokensFresh,
			TokensCacheRead:     rep.TokensCacheRead,
			TokensCacheCreation: rep.TokensCacheCreation,
			TokensOutput:        rep.TokensOutput,
			ParkSince:           rep.ParkSince,
			ParkWakeAt:          rep.ParkWakeAt,
			ParkReason:          rep.ParkReason,
			NudgeCount:          rep.NudgeCount,
		}
		// Upsert preserves the original StartedAt for known workers.
		if prev, ok, err := store.GetWorker(rep.BeadID); err == nil && ok && !prev.StartedAt.IsZero() {
			rec.StartedAt = prev.StartedAt
		}
		if err := store.UpsertWorker(rec); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	v1.POST("/workers/completed", func(c *gin.Context) {
		store := getStore()
		if store == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "registry unavailable"})
			return
		}
		var rep registry.CompletionReport
		if err := c.ShouldBindJSON(&rep); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		if rep.BeadID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "bead_id required"})
			return
		}
		if err := store.MarkCompleted(rep.BeadID, rep.ExitCode); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	// Commands are consumed on read: a worker polling gets each command
	// exactly once.
	v1.GET("/workers/commands", func(c *gin.Context) {
		store := getStore()
		if store == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "registry unavailable"})
			return
		}
		beadID := c.Query("bead_id")
		if beadID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "bead_id required"})
			return
		}
		cmds, err := store.PendingCommands(beadID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		for _, cmd := range cmds {
			if err := store.ConsumeCommand(cmd.ID); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
		}
		c.JSON(http.StatusOK, gin.H{"commands": cmds})
	})
}
