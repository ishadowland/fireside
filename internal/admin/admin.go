// Package admin — loopback-only admin API for the local dev/test server.
//
// ADR-0019: every route here is restricted to loopback addresses, so
// there is no separate admin auth/role — the loopback check is the gate
// (same model as the dashboard). Routes:
//
//	GET    /v1/admin/rooms            list every room + participant/message counts
//	POST   /v1/admin/rooms/:id/close  force-close a room (any host, idempotent)
//	DELETE /v1/admin/rooms/:id        delete a room and its records (cascade)
//	DELETE /v1/admin/rooms            delete every room and its records
//
// Force-close and delete broadcast a WS room.ended frame to any
// subscribers first (same shape as the host end-room path), so connected
// clients learn about the change.
package admin

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/ishadowland/fireside/internal/hub"
	"github.com/ishadowland/fireside/internal/loopback"
	"github.com/ishadowland/fireside/internal/rooms"
)

// Config carries what the admin API needs.
type Config struct {
	RoomsService *rooms.Service
	Hub          *hub.Hub
}

// Mount registers the admin routes on r. Panics if RoomsService is nil.
func Mount(r *gin.Engine, cfg Config) {
	if cfg.RoomsService == nil {
		panic("admin.Mount: RoomsService is required")
	}
	g := r.Group("/v1/admin", loopback.Middleware())
	{
		g.GET("/rooms", listRoomsHandler(cfg))
		g.POST("/rooms/:id/close", closeRoomHandler(cfg))
		g.DELETE("/rooms/:id", deleteRoomHandler(cfg))
		g.DELETE("/rooms", deleteAllRoomsHandler(cfg))
	}
}

func listRoomsHandler(cfg Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		rooms, err := cfg.RoomsService.ListAllWithStats(c.Request.Context())
		if err != nil {
			slog.Error("admin: list rooms failed", "err", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal_error"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"rooms": rooms})
	}
}

func closeRoomHandler(cfg Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		roomID := c.Param("id")
		err := cfg.RoomsService.ForceCloseRoom(c.Request.Context(), roomID)
		switch {
		case errors.Is(err, rooms.ErrRoomNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": "room_not_found"})
			return
		case err != nil:
			slog.Error("admin: close room failed", "room_id", roomID, "err", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal_error"})
			return
		}
		broadcastRoomEnded(cfg, roomID)
		c.JSON(http.StatusOK, gin.H{"room_id": roomID, "status": "ended"})
	}
}

func deleteRoomHandler(cfg Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		roomID := c.Param("id")
		// Notify still-subscribed clients before the row (and its
		// cascade) disappears.
		if _, _, err := cfg.RoomsService.GetRoom(c.Request.Context(), roomID); err == nil {
			broadcastRoomEnded(cfg, roomID)
		}
		err := cfg.RoomsService.DeleteRoom(c.Request.Context(), roomID)
		switch {
		case errors.Is(err, rooms.ErrRoomNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": "room_not_found"})
			return
		case err != nil:
			slog.Error("admin: delete room failed", "room_id", roomID, "err", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal_error"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"room_id": roomID, "deleted": true})
	}
}

func deleteAllRoomsHandler(cfg Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Notify subscribers of every room before wiping the table.
		if all, err := cfg.RoomsService.ListAllWithStats(c.Request.Context()); err == nil {
			for _, r := range all {
				broadcastRoomEnded(cfg, r.ID)
			}
		}
		n, err := cfg.RoomsService.DeleteAllRooms(c.Request.Context())
		if err != nil {
			slog.Error("admin: delete all rooms failed", "err", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal_error"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"deleted": n})
	}
}

// broadcastRoomEnded sends a room.ended WS frame (same shape as the
// host end-room path) to every conn subscribed to the room. No-op when
// the hub is nil or the room has no subscribers.
func broadcastRoomEnded(cfg Config, roomID string) {
	if cfg.Hub == nil {
		return
	}
	frame, _ := json.Marshal(map[string]any{
		"type":        "room.ended",
		"room_id":     roomID,
		"ended_by":    "admin",
		"server_time": time.Now().Unix(),
	})
	cfg.Hub.BroadcastToRoom(roomID, frame, nil)
}
