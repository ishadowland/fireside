package auth

import (
	"time"

	"github.com/gin-gonic/gin"
)

// Config configures the auth package. All fields are required at Mount()
// time; the handler will refuse to operate if JWTSecret is empty.
type Config struct {
	JWTSecret      []byte        // required; HS256 secret, ≥32 bytes in prod
	AccessTokenTTL time.Duration // required; RFC says 15 min
	StubCode       string        // optional; SMS code accepted in Sprint 0, defaults to "1234"
	Users          UserStore     // required; user lookup/auto-register for login
	Tokens         TokenStore    // required; persists jti for replay defense (Sprint 1-2)
}

// Mount registers the auth REST surface:
//   POST /v1/auth/login    — issue access token (and refresh token)
//   POST /v1/auth/refresh  — rotate refresh token + issue new access token
func Mount(r *gin.Engine, cfg Config) {
	r.POST("/v1/auth/login", LoginHandler(cfg))
	if cfg.Tokens != nil {
		// Refresh handler depends on the token store (issue #9 WP-7.9).
		r.POST("/v1/auth/refresh", RefreshHandler(cfg))
	}
}
