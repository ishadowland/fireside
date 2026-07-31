package auth

import (
	"context"
	"database/sql"
	"errors"
	"hash/fnv"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

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
}

// LoginRequest is the JSON body of POST /v1/auth/login.
type LoginRequest struct {
	Phone string `json:"phone" binding:"required,e164"`
	Code  string `json:"code"  binding:"required,len=4"`
}

// LoginResponse is the 200 body.
type LoginResponse struct {
	Token     string `json:"token"`
	ExpiresIn int    `json:"expires_in"`
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

		c.JSON(http.StatusOK, LoginResponse{
			Token:     token,
			ExpiresIn: int(cfg.AccessTokenTTL.Seconds()),
		})
	}
}

// resolveUserID looks the phone up in the users table, auto-registering
// unknown phones. Returns the user's int64 id.
func resolveUserID(ctx context.Context, users UserStore, phone string) (int64, error) {
	if users == nil {
		return deriveStubUserID(phone), nil
	}
	user, err := users.GetUserByPhone(ctx, phone)
	if err == nil {
		return user.ID, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}
	inserted, err := users.InsertUser(ctx, store.InsertUserParams{
		ID:    deriveStubUserID(phone),
		Phone: phone,
	})
	if err != nil {
		return 0, err
	}
	return inserted.ID, nil
}

// deriveStubUserID returns a deterministic int64 from the E.164 phone.
//
// Sprint 1: used only as the id for auto-registered users (resolveUserID)
// and as the nil-Users fallback. Replaced by a ULID at Sprint 1+ per ADR-0014.
//
// We use FNV-64 because it is fast, allocation-free, and we don't care
// about cryptographic strength (the uid is internal; security comes from
// the JWT signature). Determinism matters so re-login returns the same
// uid — this is asserted by handler_test.go test #4.
func deriveStubUserID(phone string) int64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(phone))
	return int64(h.Sum64()) //nolint:gosec // stub only; uid wraps are acceptable
}
