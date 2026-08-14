// AI test-config endpoints for the dashboard (loopback-only, ADR-0019).
//
// These let the operator type an OpenAI-compatible endpoint URL + API key
// into the interface self-check page (check.html) at test time, without
// putting the key in env / config files. The agents.Service holds it in
// memory for the process lifetime (ADR-0013: no Redis/DB persistence).
//
// Routes (all loopback-only, under /v1/dashboard):
//
//	GET  /ai-config             -> {configured, base_url, model, has_key}
//	POST /ai-config             -> {base_url, api_key, model}  (install)
//	POST /ai-ping               -> {ok, latency_ms} | {error}
//
// The API key is write-only: GET never returns it, only `has_key`.
package dashboard

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/ishadowland/fireside/internal/agents"
)

// registerAIConfigRoutes adds the AI test-config routes to cfgGroup. When
// svc is nil the routes return 503 (agent hook not wired).
func registerAIConfigRoutes(g *gin.RouterGroup, svc *agents.Service) {
	g.GET("/ai-config", aiConfigGetHandler(svc))
	g.POST("/ai-config", aiConfigSetHandler(svc))
	g.POST("/ai-ping", aiPingHandler(svc))
}

// aiConfigGetHandler reports the current AI config without exposing the
// API key.
func aiConfigGetHandler(svc *agents.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		if svc == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "ai_unavailable"})
			return
		}
		cfg := svc.Config()
		resp := gin.H{
			"configured": cfg != nil && cfg.BaseURL != "" && cfg.Model != "",
			"base_url":   "",
			"model":      "",
			"has_key":    false,
		}
		if cfg != nil {
			resp["base_url"] = cfg.BaseURL
			resp["model"] = cfg.Model
			resp["has_key"] = cfg.APIKey != ""
		}
		c.JSON(http.StatusOK, resp)
	}
}

// aiConfigRequest is the POST body for /v1/dashboard/ai-config.
type aiConfigRequest struct {
	BaseURL string `json:"base_url" binding:"required"`
	APIKey  string `json:"api_key"`
	Model   string `json:"model"    binding:"required"`
}

// aiConfigSetHandler installs a new AI config. base_url is normalized to
// drop a trailing "/chat/completions" so the model endpoint is derived
// consistently (agents.chatEndpoint re-appends it).
func aiConfigSetHandler(svc *agents.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		if svc == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "ai_unavailable"})
			return
		}
		var req aiConfigRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request"})
			return
		}
		base := strings.TrimSuffix(req.BaseURL, "/")
		if strings.HasSuffix(base, "/chat/completions") {
			base = strings.TrimSuffix(base, "/chat/completions")
		}
		svc.SetConfig(&agents.Config{BaseURL: base, APIKey: req.APIKey, Model: req.Model})
		c.JSON(http.StatusOK, gin.H{"configured": true})
	}
}

// aiPingHandler fires a minimal chat call against the configured endpoint
// to validate the URL + key. 503 when unconfigured, 502 when the call
// fails.
func aiPingHandler(svc *agents.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		if svc == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "ai_unavailable"})
			return
		}
		if !svc.Configured() {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "ai_not_configured"})
			return
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), 65*time.Second)
		defer cancel()
		latencyMs, err := svc.Ping(ctx)
		if err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true, "latency_ms": latencyMs})
	}
}
