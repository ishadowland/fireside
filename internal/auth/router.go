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
}

// Mount registers POST /v1/auth/login onto r.
func Mount(r *gin.Engine, cfg Config) {
	r.POST("/v1/auth/login", LoginHandler(cfg))
}