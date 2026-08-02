// Package participants — Mount registers the REST routes (Sprint 1 WP-4).
//
// Routes (under /v1/rooms/:id, all behind JWT auth middleware supplied
// as cfg.AuthMiddleware):
//
//	POST /:id/join    joinRoomHandler
//	POST /:id/leave   leaveRoomHandler
//
// Sprint 1 design notes:
//   - Routes live under /v1/rooms/:id/{join,leave} (participants are
//     operations on a room, not a top-level resource).
//   - Same AuthMiddleware pattern as rooms.Mount / messages.MountRoomMessages.
//   - GET endpoints (ListOnStageByRoom, ListOnStageByUser) are NOT
//     exposed as REST in Sprint 1 — they're consumed by the hub
//     (WP-5) and dashboard server-side. Sprint 2 may add public
//     GET /v1/rooms/:id/participants.
package participants

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/ishadowland/fireside/internal/auth"
	"github.com/ishadowland/fireside/internal/rooms"
)

// Config is what Mount needs from main.go.
type Config struct {
	Service        *Service
	AuthMiddleware gin.HandlerFunc
}

// MountRoomParticipants registers participant routes under
// /v1/rooms/:id.
func MountRoomParticipants(r *gin.Engine, cfg Config) {
	if cfg.Service == nil {
		panic("participants.MountRoomParticipants: Service is required")
	}
	if cfg.AuthMiddleware == nil {
		panic("participants.MountRoomParticipants: AuthMiddleware is required")
	}

	g := r.Group("/v1/rooms/:id", cfg.AuthMiddleware)
	{
		g.POST("/join", joinRoomHandler(cfg))
		g.POST("/leave", leaveRoomHandler(cfg))
	}
}

func joinRoomHandler(cfg Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		actorID := auth.UserIDFromContext(c)
		if actorID == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}
		roomID := c.Param("id")

		participant, err := cfg.Service.JoinRoom(c.Request.Context(), roomID, actorID)
		switch {
		case errors.Is(err, rooms.ErrRoomNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": "room_not_found"})
		case errors.Is(err, ErrRoomFull):
			c.JSON(http.StatusConflict, gin.H{"error": "room_full"})
		case errors.Is(err, ErrAlreadyOnStage):
			c.JSON(http.StatusConflict, gin.H{"error": "already_on_stage"})
		case err != nil:
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal_error"})
		default:
			c.JSON(http.StatusOK, JoinResponse{Participant: participant})
		}
	}
}

func leaveRoomHandler(cfg Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		actorID := auth.UserIDFromContext(c)
		if actorID == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}
		roomID := c.Param("id")

		participant, err := cfg.Service.LeaveRoom(c.Request.Context(), roomID, actorID)
		switch {
		case errors.Is(err, ErrNotOnStage):
			c.JSON(http.StatusConflict, gin.H{"error": "not_on_stage"})
		case err != nil:
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal_error"})
		default:
			c.JSON(http.StatusOK, LeaveResponse{Participant: participant})
		}
	}
}