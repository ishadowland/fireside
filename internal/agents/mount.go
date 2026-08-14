// Package agents — Mount registers the room-agents REST routes.
//
// Routes (under /v1/rooms/:id/agents/:slot, behind JWT auth middleware
// supplied by the caller as cfg.AuthMiddleware). Same conventions as
// messages.MountRoomMessages / rooms.Mount. slot is 1 or 2 (MaxSlots):
//
//	GET    /         getAgentHandler     system prompt + cooldown + invited flag + mute state (any authed user)
//	POST   /         inviteAgentHandler  invite the AI into the room  (host only)
//	DELETE /         removeAgentHandler  kick the AI out of the room   (host only)
//	POST   /mute     muteAgentHandler    temporarily ban / unban a slot (host only)
//
// Invite semantics (owner decision, 2026-08-13): an AI is only present
// in a room after the host invites it into a slot; the invitation carries
// the per-room system prompt and a per-slot cooldown (seconds). No
// invitation → no AI replies for that slot (the hook stays silent).
package agents

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/ishadowland/fireside/internal/auth"
	"github.com/ishadowland/fireside/internal/messages"
	"github.com/ishadowland/fireside/internal/rooms"
)

// MountConfig is what Mount needs from main.go.
type MountConfig struct {
	Service *Service
	// AuthMiddleware is auth.Middleware(secret); applied to the whole
	// /v1/rooms/:id/agents/:slot group.
	AuthMiddleware gin.HandlerFunc
}

// MountRoomAgents registers the room-agents routes on r.
//
// The caller (main.go) must mount this AFTER rooms.Mount — the paths
// share the /v1/rooms prefix.
func MountRoomAgents(r *gin.Engine, cfg MountConfig) {
	if cfg.Service == nil {
		panic("agents.MountRoomAgents: Service is required")
	}
	if cfg.AuthMiddleware == nil {
		panic("agents.MountRoomAgents: AuthMiddleware is required")
	}

	g := r.Group("/v1/rooms/:id/agents/:slot", cfg.AuthMiddleware)
	{
		g.GET("", getAgentHandler(cfg))
		g.POST("", inviteAgentHandler(cfg))
		g.DELETE("", removeAgentHandler(cfg))
		g.POST("/mute", muteAgentHandler(cfg))
	}

	// Free-speech round controls. The static "free-speech" path takes
	// precedence over the ":slot" param route above (gin httprouter
	// behavior), so /v1/rooms/:id/agents/free-speech never matches slot.
	m := r.Group("/v1/rooms/:id/agents", cfg.AuthMiddleware)
	{
		m.GET("/free-speech", freeSpeechGetHandler(cfg))
		m.POST("/free-speech", freeSpeechSetHandler(cfg))
	}
}

func actorID(c *gin.Context) string {
	return auth.UserIDFromContext(c)
}

func roomIDParam(c *gin.Context) string {
	return c.Param("id")
}

// slotParam parses + validates the :slot path segment (1..MaxSlots).
func slotParam(c *gin.Context) (int, error) {
	v := strings.TrimSpace(c.Param("slot"))
	if v == "" {
		return 0, errors.New("missing slot")
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, errors.New("invalid slot")
	}
	if err := validateSlot(n); err != nil {
		return 0, errors.New("invalid slot")
	}
	return n, nil
}

// mapAgentError maps service sentinels to HTTP statuses. Returns false
// when the error was unexpected (caller should 500 and log).
func mapAgentError(c *gin.Context, err error) bool {
	switch {
	case errors.Is(err, rooms.ErrRoomNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": "room_not_found"})
	case errors.Is(err, rooms.ErrNotHost):
		c.JSON(http.StatusForbidden, gin.H{"error": "not_host"})
	case errors.Is(err, messages.ErrRoomEnded):
		c.JSON(http.StatusConflict, gin.H{"error": "room_ended"})
	case errors.Is(err, messages.ErrInvalidArg):
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request"})
	default:
		return false
	}
	return true
}

func getAgentHandler(cfg MountConfig) gin.HandlerFunc {
	return func(c *gin.Context) {
		if actorID(c) == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}
		slot, err := slotParam(c)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request"})
			return
		}
		state, err := cfg.Service.AgentState(c.Request.Context(), roomIDParam(c), slot)
		if err != nil {
			if mapAgentError(c, err) {
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal_error"})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"configured":              state.Configured,
			"agent_id":                state.AgentID,
			"system_prompt":           state.SystemPrompt,
			"cooldown_seconds":        state.CooldownSeconds,
			"muted":                   state.Muted,
			"muted_remaining_seconds": state.MutedRemainingSeconds,
			"preset_id":               state.PresetID,
			"preset_name":             state.PresetName,
		})
	}
}

// inviteRequest is the body of POST /v1/rooms/:id/agents/:slot.
type inviteRequest struct {
	// AgentPresetID picks a persisted agent preset (Agent 管理器): the
	// connection config + system prompt come from the preset. Empty =
	// legacy in-room system_prompt + global config.
	AgentPresetID   string `json:"agent_preset_id"`
	SystemPrompt    string `json:"system_prompt"`    // legacy; ignored when agent_preset_id set; empty → built-in default
	CooldownSeconds int32  `json:"cooldown_seconds"` // min seconds between this agent's own replies; 0 → none
}

func inviteAgentHandler(cfg MountConfig) gin.HandlerFunc {
	return func(c *gin.Context) {
		uid := actorID(c)
		if uid == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}
		slot, err := slotParam(c)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request"})
			return
		}
		var req inviteRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request"})
			return
		}
		if req.AgentPresetID != "" {
			err = cfg.Service.InviteWithPreset(c.Request.Context(), uid, roomIDParam(c), slot, req.AgentPresetID, req.CooldownSeconds)
		} else {
			err = cfg.Service.Invite(c.Request.Context(), uid, roomIDParam(c), slot, req.SystemPrompt, req.CooldownSeconds)
		}
		if err != nil {
			if mapAgentError(c, err) {
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal_error"})
			return
		}
		state, err := cfg.Service.AgentState(c.Request.Context(), roomIDParam(c), slot)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal_error"})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"configured":       true,
			"agent_id":         state.AgentID,
			"system_prompt":    state.SystemPrompt,
			"cooldown_seconds": state.CooldownSeconds,
			"preset_id":        state.PresetID,
			"preset_name":      state.PresetName,
		})
	}
}

func removeAgentHandler(cfg MountConfig) gin.HandlerFunc {
	return func(c *gin.Context) {
		uid := actorID(c)
		if uid == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}
		slot, err := slotParam(c)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request"})
			return
		}
		if err := cfg.Service.Remove(c.Request.Context(), uid, roomIDParam(c), slot); err != nil {
			if mapAgentError(c, err) {
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal_error"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"removed": true})
	}
}

// muteRequest is the body of POST /v1/rooms/:id/agents/:slot/mute.
type muteRequest struct {
	Enabled bool  `json:"enabled"`
	Minutes int32 `json:"minutes"` // 1..MaxMuteMinutes; required when enabled=true
}

func muteAgentHandler(cfg MountConfig) gin.HandlerFunc {
	return func(c *gin.Context) {
		uid := actorID(c)
		if uid == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}
		slot, err := slotParam(c)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request"})
			return
		}
		var req muteRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request"})
			return
		}
		if err := cfg.Service.SetMute(c.Request.Context(), uid, roomIDParam(c), slot, req.Enabled, req.Minutes); err != nil {
			if mapAgentError(c, err) {
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal_error"})
			return
		}
		state, err := cfg.Service.AgentState(c.Request.Context(), roomIDParam(c), slot)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal_error"})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"configured":              state.Configured,
			"agent_id":                state.AgentID,
			"muted":                   state.Muted,
			"muted_remaining_seconds": state.MutedRemainingSeconds,
		})
	}
}

// freeSpeechViewJSON renders a FreeSpeechView like GET /ai-config does.
func freeSpeechViewJSON(v FreeSpeechView) gin.H {
	return gin.H{
		"enabled":           v.Enabled,
		"round_seconds":     v.RoundSeconds,
		"remaining_seconds": v.RemainingSeconds,
	}
}

func freeSpeechGetHandler(cfg MountConfig) gin.HandlerFunc {
	return func(c *gin.Context) {
		if actorID(c) == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}
		view, err := cfg.Service.FreeSpeechState(c.Request.Context(), roomIDParam(c))
		if err != nil {
			if mapAgentError(c, err) {
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal_error"})
			return
		}
		c.JSON(http.StatusOK, freeSpeechViewJSON(view))
	}
}

// freeSpeechRequest is the body of POST /v1/rooms/:id/agents/free-speech.
type freeSpeechRequest struct {
	Enabled      bool  `json:"enabled"`
	RoundSeconds int32 `json:"round_seconds"` // ignored when enabled=false; default DefaultRoundSeconds
}

func freeSpeechSetHandler(cfg MountConfig) gin.HandlerFunc {
	return func(c *gin.Context) {
		uid := actorID(c)
		if uid == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}
		var req freeSpeechRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request"})
			return
		}
		round := req.RoundSeconds
		if req.Enabled && round <= 0 {
			round = DefaultRoundSeconds
		}
		if err := cfg.Service.SetFreeSpeech(c.Request.Context(), uid, roomIDParam(c), req.Enabled, round); err != nil {
			if mapAgentError(c, err) {
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal_error"})
			return
		}
		view, err := cfg.Service.FreeSpeechState(c.Request.Context(), roomIDParam(c))
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal_error"})
			return
		}
		c.JSON(http.StatusOK, freeSpeechViewJSON(view))
	}
}
