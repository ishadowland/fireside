// Package messages — Mount registers the REST routes (Sprint 1 WP-3).
//
// Routes (under /v1/rooms/:id/messages, all behind JWT auth middleware
// supplied as cfg.AuthMiddleware):
//
//	POST /               createMessageHandler   body: CreateMessageRequest
//	GET  /               listMessagesHandler    query: ?since=<id>&limit=N
//
// Sprint 1 design notes:
//   - Routes live under /v1/rooms/:id/messages (messages are children
//     of a room, not a top-level resource).
//   - The caller-supplied AuthMiddleware is applied to the whole group
//     (same pattern as rooms.Mount).
//   - The :id path param is the room_id. We don't bind to a typed
//     router; c.Param("id") reads it.
package messages

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/ishadowland/fireside/internal/auth"
)

// Config is what Mount needs from main.go.
type Config struct {
	Service        *Service
	AuthMiddleware gin.HandlerFunc
}

// MountRoomMessages registers messages routes under /v1/rooms/:id/messages.
//
// The caller (main.go) must register this AFTER the rooms.Mount group
// is set up so the path prefix /v1/rooms is shared. We don't depend on
// rooms.Mount directly — we just register a sub-route on r.
func MountRoomMessages(r *gin.Engine, cfg Config) {
	if cfg.Service == nil {
		panic("messages.MountRoomMessages: Service is required")
	}
	if cfg.AuthMiddleware == nil {
		panic("messages.MountRoomMessages: AuthMiddleware is required")
	}

	g := r.Group("/v1/rooms/:id/messages", cfg.AuthMiddleware)
	{
		g.POST("", createMessageHandler(cfg))
		g.GET("", listMessagesHandler(cfg))
	}
}

func createMessageHandler(cfg Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		actorID := auth.UserIDFromContext(c)
		if actorID == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}
		roomID := c.Param("id")

		var req CreateMessageRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request"})
			return
		}

		msg, err := cfg.Service.CreateMessage(c.Request.Context(), actorID, roomID, req)
		switch {
		case errors.Is(err, ErrRoomNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": "room_not_found"})
		case errors.Is(err, ErrNotOnStage):
			c.JSON(http.StatusForbidden, gin.H{"error": "not_on_stage"})
		case errors.Is(err, ErrInvalidArg):
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request"})
		case err != nil:
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal_error"})
		default:
			c.JSON(http.StatusOK, CreateMessageResponse{Message: msg})
		}
	}
}

func listMessagesHandler(cfg Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		actorID := auth.UserIDFromContext(c)
		if actorID == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}
		roomID := c.Param("id")

		// Parse ?limit (default DefaultPageSize, max MaxPageSize).
		limit := int32(0)
		if v := c.Query("limit"); v != "" {
			var parsed int
			if _, err := fmt.Sscan(v, &parsed); err == nil && parsed > 0 {
				limit = int32(parsed)
			}
		}

		// Parse ?since (cursor; pass-through).
		since := c.Query("since")

		msgs, nextBefore, err := cfg.Service.ListMessagesByRoom(
			c.Request.Context(), roomID, since, limit,
		)
		switch {
		case errors.Is(err, ErrRoomNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": "room_not_found"})
			return
		case err != nil:
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal_error"})
			return
		}
		c.JSON(http.StatusOK, ListMessagesResponse{
			Messages:   msgs,
			NextBefore: nextBefore,
		})
	}
}
