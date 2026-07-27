package auth

import (
	"errors"
	"hash/fnv"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10"
)

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

// StubCode is the only accepted SMS code in Sprint 0. Real SMS provider
// lands Sprint 1+ (see docs/handoff/sprint0/SUB-001-internal-auth.md §NOT
// in scope).
const StubCode = "1234"

// LoginHandler returns the Gin handler for POST /v1/auth/login.
//
// On success: 200 with LoginResponse.
// On wrong code / unknown phone: 401 with {"error":"invalid_credentials"}.
// On malformed body: 400 with {"error":"invalid_request"}.
//
// Sprint 0 does NOT verify the phone maps to an existing user — any E.164
// phone is accepted. deriveStubUserID returns a deterministic int64 from
// the phone so re-login yields the same uid (regression test #4).
func LoginHandler(cfg Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req LoginRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			var ve validator.ValidationErrors
			if errors.As(err, &ve) {
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request"})
				return
			}
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request"})
			return
		}
		if req.Code != StubCode {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid_credentials"})
			return
		}

		userID := deriveStubUserID(req.Phone)
		token, jti, err := Issue(cfg.JWTSecret, userID, cfg.AccessTokenTTL)
		if err != nil {
			slog.Error("auth: failed to sign jwt", "err", err, "jti", jti)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal_error"})
			return
		}

		c.JSON(http.StatusOK, LoginResponse{
			Token:     token,
			ExpiresIn: int(cfg.AccessTokenTTL.Seconds()),
		})
	}
}

// deriveStubUserID returns a deterministic int64 from the E.164 phone.
//
// stub: replace in Sprint 1 with real user lookup (db/queries/auth.sql :: GetUserByPhone).
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