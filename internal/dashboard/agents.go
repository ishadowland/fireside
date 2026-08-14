// Agent 管理器 endpoints (issue #38).
//
// Persisted agent presets (connection kind / endpoint / api token / model /
// prompt) are managed here, loopback-only (ADR-0019). The api token is
// write-only: GET never returns it, only `has_token`.
//
// Routes (all loopback-only, under /v1/dashboard):
//
//	GET    /agents            -> {agents: [PresetView]}
//	POST   /agents            -> create  (PresetInput)
//	POST   /agents/:id        -> update  (PresetInput; empty api_token keeps existing)
//	DELETE /agents/:id        -> delete
//	POST   /agents/:id/ping   -> {ok, latency_ms} | {error}
//
// Room invitations reference presets by id (POST /v1/rooms/:id/agents/:slot
// with agent_preset_id); the key never leaves the server.
package dashboard

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/ishadowland/fireside/internal/agents"
)

// registerAgentManagerRoutes adds the Agent 管理器 routes to cfgGroup.
// When svc is nil the routes return 503 (agents not wired).
func registerAgentManagerRoutes(g *gin.RouterGroup, svc *agents.Service) {
	g.GET("/agents", agentListHandler(svc))
	g.POST("/agents", agentCreateHandler(svc))
	g.POST("/agents/:id", agentUpdateHandler(svc))
	g.DELETE("/agents/:id", agentDeleteHandler(svc))
	g.POST("/agents/:id/ping", agentPingHandler(svc))
}

func agentManagerUnavailable(svc *agents.Service) bool {
	return svc == nil
}

func agentManagerErr(c *gin.Context, err error) {
	if err == nil {
		return
	}
	status := http.StatusInternalServerError
	msg := err.Error()
	if strings.HasPrefix(msg, "preset: 未找到") {
		status = http.StatusNotFound
	} else if strings.HasPrefix(msg, "preset:") {
		status = http.StatusBadRequest
	}
	c.JSON(status, gin.H{"error": msg})
}

func agentListHandler(svc *agents.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		if agentManagerUnavailable(svc) {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "agents_unavailable"})
			return
		}
		list, err := svc.PresetList()
		if err != nil {
			agentManagerErr(c, err)
			return
		}
		if list == nil {
			list = []agents.PresetView{}
		}
		c.JSON(http.StatusOK, gin.H{"agents": list})
	}
}

func bindPresetInput(c *gin.Context) (agents.PresetInput, bool) {
	var in agents.PresetInput
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request"})
		return in, false
	}
	return in, true
}

func agentCreateHandler(svc *agents.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		if agentManagerUnavailable(svc) {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "agents_unavailable"})
			return
		}
		in, ok := bindPresetInput(c)
		if !ok {
			return
		}
		view, err := svc.PresetCreate(in)
		if err != nil {
			agentManagerErr(c, err)
			return
		}
		c.JSON(http.StatusOK, view)
	}
}

func agentUpdateHandler(svc *agents.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		if agentManagerUnavailable(svc) {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "agents_unavailable"})
			return
		}
		in, ok := bindPresetInput(c)
		if !ok {
			return
		}
		view, err := svc.PresetUpdate(c.Param("id"), in)
		if err != nil {
			agentManagerErr(c, err)
			return
		}
		c.JSON(http.StatusOK, view)
	}
}

func agentDeleteHandler(svc *agents.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		if agentManagerUnavailable(svc) {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "agents_unavailable"})
			return
		}
		if err := svc.PresetDelete(c.Param("id")); err != nil {
			agentManagerErr(c, err)
			return
		}
		c.JSON(http.StatusOK, gin.H{"deleted": true})
	}
}

func agentPingHandler(svc *agents.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		if agentManagerUnavailable(svc) {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "agents_unavailable"})
			return
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), 65*time.Second)
		defer cancel()
		latencyMs, err := svc.PresetPing(ctx, c.Param("id"))
		if err != nil {
			agentManagerErr(c, err)
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true, "latency_ms": latencyMs})
	}
}
