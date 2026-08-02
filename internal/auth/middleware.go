package auth

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// ContextUserIDKey is the gin.Context key under which Middleware stores
// the authenticated user_id (ULID string). Handlers should call
// UserIDFromContext(c) instead of indexing the context directly.
const ContextUserIDKey = "auth.user_id"

// Middleware returns a gin.HandlerFunc that verifies a Bearer JWT in
// the Authorization header using JWTSecret, and stores the user_id claim
// in the gin context for downstream handlers.
//
// On failure:
//   - missing / malformed Authorization header → 401 invalid_request
//   - JWT signature/exp invalid                  → 401 invalid_token
//
// On success, the request proceeds with c.Set(ContextUserIDKey, ulid).
//
// Sprint 1 usage: Mount it on /v1/rooms and any other future
// authenticated group. Login (/v1/auth/login) stays public.
func Middleware(secret []byte) gin.HandlerFunc {
	return func(c *gin.Context) {
		raw := c.GetHeader("Authorization")
		if raw == "" || !strings.HasPrefix(raw, "Bearer ") {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid_request"})
			return
		}
		token := strings.TrimSpace(strings.TrimPrefix(raw, "Bearer "))
		if token == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid_request"})
			return
		}

		claims, err := Validate(secret, token)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid_token"})
			return
		}

		c.Set(ContextUserIDKey, claims.UserID)
		c.Next()
	}
}

// UserIDFromContext extracts the authenticated user_id (ULID string)
// that Middleware set. Returns empty string if not set or wrong type
// — callers should treat empty as "anonymous / not authenticated".
func UserIDFromContext(c *gin.Context) string {
	if v, ok := c.Get(ContextUserIDKey); ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}