// Package auth — refresh token rotation (issue #9 WP-7.9).
//
// Refresh flow:
//   1. Client POSTs /v1/auth/refresh with the refresh token (the
//      jti we issued at login).
//   2. Server looks up the jti in refresh_tokens. If not found,
//      expired, or already replaced → 401.
//   3. Server marks the used refresh token replaced_by_jti = new_jti
//      and issues a new access token + new refresh token.
//   4. If the used token's mark request returns 0 rows affected
//      (because replaced_by_jti was already set), treat as replay
//      and revoke the entire family.
//
// Sprint 1 simplifications:
//   - Family revocation deletes all refresh tokens in the family,
//     not just the chain. This is a coarse hammer but blocks any
//     future use of the leaked credential.
//   - The cleanup cron for expired tokens is post-Sprint 1.
package auth

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/oklog/ulid/v2"

	"github.com/ishadowland/fireside/internal/store"
)

// RefreshTokenTTL is the lifetime of a refresh token. RFC Q16 says
// 7 days; we honor that.
const RefreshTokenTTL = 7 * 24 * time.Hour

// refreshRequest is the body of POST /v1/auth/refresh.
type refreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

// refreshResponse is the 200 body of POST /v1/auth/refresh.
type refreshResponse struct {
	Token        string `json:"token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
}

// RefreshHandler returns the gin handler for POST /v1/auth/refresh.
func RefreshHandler(cfg Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req refreshRequest
		if err := c.ShouldBindJSON(&req); err != nil || req.RefreshToken == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request"})
			return
		}

		row, err := cfg.Tokens.GetRefreshToken(c.Request.Context(), req.RefreshToken)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid_refresh_token"})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal_error"})
			return
		}

		// Expiry check.
		if !row.ExpiresAt.Valid || row.ExpiresAt.Time.Before(time.Now()) {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "refresh_token_expired"})
			return
		}

		// Issue new tokens. Family stays the same.
		accessToken, accessJTI, err := Issue(cfg.JWTSecret, row.UserID, cfg.AccessTokenTTL)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal_error"})
			return
		}
		newJTI := ulid.Make().String()
		newExpires := time.Now().Add(RefreshTokenTTL)
		if _, err := cfg.Tokens.InsertRefreshToken(c.Request.Context(), store.InsertRefreshTokenParams{
			JTI:       newJTI,
			UserID:    row.UserID,
			FamilyID:  row.FamilyID,
			ExpiresAt: sql.NullTime{Time: newExpires, Valid: true},
		}); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal_error"})
			return
		}
		// Mark the old token as replaced. If 0 rows affected (already
		// replaced), this is a replay — kill the family.
		affected, err := cfg.Tokens.MarkRefreshTokenReplaced(c.Request.Context(), row.JTI, newJTI)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal_error"})
			return
		}
		if affected == 0 {
			// Replay defense: kill the entire family. The access token
			// minted above is never handed out and its jti is not
			// persisted; the just-inserted refresh token is deleted by
			// the family revocation below.
			if _, err := cfg.Tokens.DeleteRefreshFamily(c.Request.Context(), row.FamilyID); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "internal_error"})
				return
			}
			c.JSON(http.StatusUnauthorized, gin.H{"error": "refresh_token_replayed"})
			return
		}

		// Persist the new access token's jti for ADR-0007 replay defense,
		// mirroring the login path. Done only after a successful rotation
		// so a replayed request never leaves a live access token behind.
		jtUUID, perr := uuid.Parse(accessJTI)
		if perr != nil {
			slog.Error("auth: refresh: invalid jti from Issue", "err", perr)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal_error"})
			return
		}
		if _, err := cfg.Tokens.InsertToken(c.Request.Context(), store.InsertTokenParams{
			Jti:       jtUUID,
			UserID:    row.UserID,
			ExpiresAt: sql.NullTime{Time: time.Now().Add(cfg.AccessTokenTTL), Valid: true},
		}); err != nil {
			slog.Error("auth: refresh: failed to persist access token jti", "err", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal_error"})
			return
		}

		c.JSON(http.StatusOK, refreshResponse{
			Token:        accessToken,
			RefreshToken: newJTI,
			ExpiresIn:    int(cfg.AccessTokenTTL.Seconds()),
		})
	}
}

// IssueRefreshToken creates a refresh token for the user. Used by
// the login handler to mint the first refresh token in a new
// family. The first token's jti is also its family id.
func IssueRefreshToken(ctx context.Context, tokens TokenStore, userID string) (string, error) {
	if tokens == nil {
		return "", errors.New("auth: tokens store not configured")
	}
	jti := ulid.Make().String()
	expires := time.Now().Add(RefreshTokenTTL)
	if _, err := tokens.InsertRefreshToken(ctx, store.InsertRefreshTokenParams{
		JTI:       jti,
		UserID:    userID,
		FamilyID:  jti,
		ExpiresAt: sql.NullTime{Time: expires, Valid: true},
	}); err != nil {
		return "", err
	}
	return jti, nil
}
