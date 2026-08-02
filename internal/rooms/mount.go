// Package rooms — Mount registers the REST routes (Sprint 1 WP-2).
//
// Routes (under /v1/rooms, all behind JWT auth middleware supplied by
// the caller as cfg.AuthMiddleware):
//
//	POST   /              createRoomHandler   body: CreateRoomRequest
//	GET    /              listActiveHandler   query: ?limit=N
//	GET    /:id           getRoomHandler
//	POST   /:id/end       endRoomHandler      host only
package rooms

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/ishadowland/fireside/internal/auth"
)

// Config is what Mount needs from main.go.
type Config struct {
	Service *Service
	// AuthMiddleware should be auth.Middleware(secret). Mount applies it
	// to the whole /v1/rooms group; downstream handlers pull the
	// authenticated user_id via auth.UserIDFromContext(c).
	AuthMiddleware gin.HandlerFunc
}

// Mount registers the rooms REST routes on r.
func Mount(r *gin.Engine, cfg Config) {
	if cfg.Service == nil {
		panic("rooms.Mount: Service is required")
	}
	if cfg.AuthMiddleware == nil {
		panic("rooms.Mount: AuthMiddleware is required")
	}

	g := r.Group("/v1/rooms", cfg.AuthMiddleware)
	{
		g.POST("", createRoomHandler(cfg))
		g.GET("", listActiveHandler(cfg))
		g.GET("/:id", getRoomHandler(cfg))
		g.POST("/:id/end", endRoomHandler(cfg))
	}
}

func createRoomHandler(cfg Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req CreateRoomRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request"})
			return
		}
		hostID := auth.UserIDFromContext(c)
		if hostID == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}

		room, err := cfg.Service.CreateRoom(c.Request.Context(), hostID, req)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal_error"})
			return
		}
		c.JSON(http.StatusOK, CreateRoomResponse{Room: room})
	}
}

func listActiveHandler(cfg Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		// DefaultListLimit applies when ?limit is missing or unparseable.
		limit := int32(0)
		if v := c.Query("limit"); v != "" {
			var parsed int
			if _, err := fmt.Sscan(v, &parsed); err == nil && parsed > 0 {
				limit = int32(parsed)
			}
		}
		rooms, err := cfg.Service.ListActive(c.Request.Context(), limit)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal_error"})
			return
		}
		c.JSON(http.StatusOK, ListActiveResponse{Rooms: rooms})
	}
}

func getRoomHandler(cfg Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		roomID := c.Param("id")
		room, parts, err := cfg.Service.GetRoom(c.Request.Context(), roomID)
		if errors.Is(err, ErrRoomNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "room_not_found"})
			return
		}
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal_error"})
			return
		}
		c.JSON(http.StatusOK, GetRoomResponse{Room: room, Participants: parts})
	}
}

func endRoomHandler(cfg Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		actorID := auth.UserIDFromContext(c)
		if actorID == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}
		roomID := c.Param("id")
		err := cfg.Service.EndRoom(c.Request.Context(), actorID, roomID)
		switch {
		case errors.Is(err, ErrRoomNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": "room_not_found"})
		case errors.Is(err, ErrNotHost):
			c.JSON(http.StatusForbidden, gin.H{"error": "not_host"})
		case errors.Is(err, ErrRoomEnded):
			c.JSON(http.StatusConflict, gin.H{"error": "room_already_ended"})
		case err != nil:
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal_error"})
		default:
			c.JSON(http.StatusOK, EndRoomResponse{RoomID: roomID, Status: "ended"})
		}
	}
}