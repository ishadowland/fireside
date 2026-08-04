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

// UserStore is the persistence surface the login handler needs.
//
// *store.Queries satisfies it directly (see internal/store/querier.go).
type UserStore interface {
	GetUserByPhone(ctx context.Context, phone string) (store.User, error)
	InsertUser(ctx context.Context, arg store.InsertUserParams) (store.User, error)
}

// TokenStore persists freshly-issued JWTs' jti so the WS layer can do
// replay defense (ADR-0007 §Risks → "Replay"). On login, the handler
// inserts the token's jti; on WS auth.hello, the ws package looks the
// jti up to confirm the token was actually issued by /v1/auth/login.
//
// *store.Queries satisfies it directly.
type TokenStore interface {
	InsertToken(ctx context.Context, arg store.InsertTokenParams) (store.AuthToken, error)

	// Refresh-token operations (issue #9 WP-7.9). *store.Queries
	// satisfies these directly via the refresh_tokens.go file.
	InsertRefreshToken(ctx context.Context, arg store.InsertRefreshTokenParams) (int64, error)
	GetRefreshToken(ctx context.Context, jti string) (store.RefreshToken, error)
	MarkRefreshTokenReplaced(ctx context.Context, jti, replacedBy string) (int64, error)
	DeleteRefreshToken(ctx context.Context, jti string) (int64, error)
	DeleteRefreshFamily(ctx context.Context, familyID string) (int64, error)
}

// LoginRequest is the JSON body of POST /v1/auth/login.
type LoginRequest struct {
	Phone string `json:"phone" binding:"required,e164"`
	Code  string `json:"code"  binding:"required,len=4"`
}

// LoginResponse is the 200 body.
type LoginResponse struct {
	Token        string `json:"token"`
	RefreshToken string `json:"refresh_token,omitempty"`
	ExpiresIn    int    `json:"expires_in"`
}

// StubCode is the default SMS code accepted when Config.StubCode is empty.
// Real SMS provider lands Sprint 1+ (see docs/handoff/sprint0/SUB-001-internal-auth.md §NOT
// in scope).
const StubCode = "1234"

// effectiveStubCode returns the configured stub code, falling back to StubCode.
func effectiveStubCode(cfg Config) string {
	if cfg.StubCode == "" {
		return StubCode
	}
	return cfg.StubCode
}

// LoginHandler returns the Gin handler for POST /v1/auth/login.
//
// On success: 200 with LoginResponse.
// On wrong code / unknown phone: 401 with {"error":"invalid_credentials"}.
// On malformed body: 400 with {"error":"invalid_request"}.
//
// Sprint 1-2: the freshly-issued JWT's jti is persisted in auth_tokens
// so the WS first-frame auth (ws.HandleConnect) can reject tokens that
// were never issued by /v1/auth/login (replay defense; ADR-0007 §Risks).
// If persistence fails, login returns 500 rather than issuing an
// untrackable token.
//
// Sprint 1-3 (ADR-0014): the user's id is a real ULID string (VARCHAR(26)
// post-migration 0007). Unknown phones are auto-registered with a fresh
// ulid.Make(); re-login for the same phone finds the existing row.
func LoginHandler(cfg Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req LoginRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request"})
			return
		}
		if req.Code != effectiveStubCode(cfg) {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid_credentials"})
			return
		}

		userID, err := resolveUserID(c.Request.Context(), cfg.Users, req.Phone)
		if err != nil {
			slog.Error("auth: failed to resolve user", "err", err, "phone", req.Phone)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal_error"})
			return
		}

		token, jti, err := Issue(cfg.JWTSecret, userID, cfg.AccessTokenTTL)
		if err != nil {
			slog.Error("auth: failed to sign jwt", "err", err, "jti", jti)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal_error"})
			return
		}

		if cfg.Tokens != nil {
			jtUUID, perr := uuid.Parse(jti)
			if perr != nil {
				slog.Error("auth: invalid jti from Issue", "err", perr)
				c.JSON(http.StatusInternalServerError, gin.H{"error": "internal_error"})
				return
			}
			if _, err := cfg.Tokens.InsertToken(c.Request.Context(), store.InsertTokenParams{
				Jti:       jtUUID,
				UserID:    userID,
				ExpiresAt: sql.NullTime{Time: time.Now().Add(cfg.AccessTokenTTL), Valid: true},
			}); err != nil {
				slog.Error("auth: failed to persist jti", "err", err, "jti", jti)
				c.JSON(http.StatusInternalServerError, gin.H{"error": "internal_error"})
				return
			}
		}

		resp := LoginResponse{
			Token:     token,
			ExpiresIn: int(cfg.AccessTokenTTL.Seconds()),
		}
		// Issue a refresh token alongside the access token (issue #9
		// WP-7.9). Errors are non-fatal — the access token alone is
		// still usable; the failure is logged for observability.
		if cfg.Tokens != nil {
			rt, rtErr := IssueRefreshToken(c.Request.Context(), cfg.Tokens, userID)
			if rtErr != nil {
				slog.Error("auth: failed to issue refresh token", "err", rtErr, "user_id", userID)
			} else {
				resp.RefreshToken = rt
			}
		}
		c.JSON(http.StatusOK, resp)
	}
}

// resolveUserID looks the phone up in the users table, auto-registering
// unknown phones with a fresh ULID. Returns the user's ULID string.
//
// Sprint 1-3 (ADR-0014): the inserted id is a real ulid.ULID (canonical
// 26-char lowercase form) generated from crypto/rand entropy. Re-login
// for the same phone finds the existing row and returns the same id;
// uniqueness is guaranteed by the phone UNIQUE index, not by ULID.
func resolveUserID(ctx context.Context, users UserStore, phone string) (string, error) {
	if users == nil {
		return newULID(), nil
	}
	user, err := users.GetUserByPhone(ctx, phone)
	if err == nil {
		return user.ID, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	inserted, err := users.InsertUser(ctx, store.InsertUserParams{
		ID:    newULID(),
		Phone: phone,
	})
	if err != nil {
		// Issue #21 fix: two simultaneous stub-login calls for a
		// brand-new phone both miss the lookup and both insert; the
		// loser hits idx_user_phone (SQLSTATE 23505). Treat as
		// success: re-GetUserByPhone and return that row. This makes
		// login idempotent under first-time concurrent load.
		if store.IsUniqueViolation(err) {
			user, lerr := users.GetUserByPhone(ctx, phone)
			if lerr == nil {
				return user.ID, nil
			}
			return "", lerr
		}
		return "", err
	}
	return inserted.ID, nil
}

// newULID returns a fresh canonical 26-char ULID. oklog/ulid v2 uses
// crypto/rand entropy by default.
func newULID() string {
	return ulid.Make().String()
}
