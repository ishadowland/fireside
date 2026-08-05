// Package users implements the user profile REST surface for the
// dashboard and Android UI. Sprint 1 scope (issue #9, WP-7.10):
//
//   - GET  /v1/users/me      — fetch the authenticated user, including
//                              display_name (used by the dashboard
//                              login flow to know whether to show the
//                              "enter your display_name" modal).
//   - PATCH /v1/users/me     — update display_name (max 64 chars).
//
// The underlying store layer already has UpdateUserDisplayName +
// GetDisplayName (see internal/store/users_display_name.go + the
// 0006_users_display_name migration). This package only adds the
// REST surface and the side-effects (validation, logging).
package users

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/gin-gonic/gin"

	"github.com/ishadowland/fireside/internal/auth"
	"github.com/ishadowland/fireside/internal/store"
)

// Service is the user-profile business layer.
type Service struct {
	q   *store.Queries
	log *slog.Logger
}

// NewService builds a Service.
func NewService(q *store.Queries, log *slog.Logger) *Service {
	if log == nil {
		log = slog.Default()
	}
	return &Service{q: q, log: log}
}

// Config carries what Mount needs.
type Config struct {
	Service        *Service
	AuthMiddleware gin.HandlerFunc
	// Log is the logger used by the HTTP handlers. Defaults to
	// slog.Default() if unset.
	Log *slog.Logger
}

// Mount registers /v1/users/me onto r.
func Mount(r *gin.Engine, cfg Config) {
	g := r.Group("/v1/users", cfg.AuthMiddleware)
	{
		g.GET("/me", getMeHandler(cfg))
		g.PATCH("/me", patchMeHandler(cfg))
	}
}

// UserView is the on-wire shape of /v1/users/me.
type UserView struct {
	ID          string `json:"id"`
	Phone       string `json:"phone"`
	DisplayName string `json:"display_name"`
}

// Error responses follow the rest of the API: { "error": "<code>" }.
const (
	errUnauthorized = "unauthorized"
	errInvalidArg   = "invalid_request"
	errInternal     = "internal_error"
)

func getMeHandler(cfg Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		uid := auth.UserIDFromContext(c)
		if uid == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": errUnauthorized})
			return
		}
		v, err := cfg.Service.GetByID(c.Request.Context(), uid)
		if err != nil {
			logf(cfg.Log, "users.getMe: lookup failed", "user_id", uid, "err", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": errInternal})
			return
		}
		c.JSON(http.StatusOK, v)
	}
}

// PatchMeRequest is the body of PATCH /v1/users/me.
type PatchMeRequest struct {
	DisplayName string `json:"display_name"`
}

// MaxDisplayNameLen caps the display_name field. RFC: 64 chars.
const MaxDisplayNameLen = 64

func patchMeHandler(cfg Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		uid := auth.UserIDFromContext(c)
		if uid == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": errUnauthorized})
			return
		}
		var req PatchMeRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": errInvalidArg})
			return
		}
		// Reject leading/trailing whitespace and control chars.
		name := strings.TrimSpace(req.DisplayName)
		// Count runes, not bytes: the RFC's 64-char limit and the DB's
		// VARCHAR(64) both count characters (Chinese names would be
		// rejected at ~21 chars under a byte count).
		if utf8.RuneCountInString(name) > MaxDisplayNameLen {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": errInvalidArg,
				"detail": fmt.Sprintf("display_name must be <= %d chars", MaxDisplayNameLen),
			})
			return
		}
		if err := cfg.Service.UpdateDisplayName(c.Request.Context(), uid, name); err != nil {
			logf(cfg.Log, "users.patchMe: update failed", "user_id", uid, "err", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": errInternal})
			return
		}
		c.JSON(http.StatusOK, gin.H{"display_name": name})
	}
}

// GetByID returns the public UserView for a user.
func (s *Service) GetByID(ctx context.Context, id string) (UserView, error) {
	u, err := s.q.GetUserByID(ctx, id)
	if err != nil {
		return UserView{}, err
	}
	return UserView{
		ID:          u.ID,
		Phone:       u.Phone,
		DisplayName: u.DisplayName,
	}, nil
}

// UpdateDisplayName sets the user's display_name.
func (s *Service) UpdateDisplayName(ctx context.Context, id, name string) error {
	_, err := s.q.UpdateUserDisplayName(ctx, store.UpdateUserDisplayNameParams{
		ID:          id,
		DisplayName: name,
	})
	return err
}

// logf is a tiny helper that resolves a nil logger to slog.Default.
func logf(log *slog.Logger, msg string, args ...any) {
	if log == nil {
		log = slog.Default()
	}
	log.Error(msg, args...)
}
