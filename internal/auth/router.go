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
}

// Mount registers POST /v1/auth/login onto r.
func Mount(r *gin.Engine, cfg Config) {
	r.POST("/v1/auth/login", LoginHandler(cfg))
}
