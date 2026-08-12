// Package loopback provides the loopback-only middleware shared by
// the dashboard and admin routes (ADR-0019).
package loopback

import (
	"net"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// Middleware aborts with 404 for any non-loopback client. RemoteAddr is
// used directly (not gin's ClientIP) so X-Forwarded-For cannot spoof a
// loopback origin. Applied to /dashboard, /v1/dashboard and /v1/admin.
func Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		host, _, err := net.SplitHostPort(c.Request.RemoteAddr)
		if err != nil {
			host = strings.Trim(c.Request.RemoteAddr, "[]")
		}
		ip := net.ParseIP(host)
		if ip == nil || !ip.IsLoopback() {
			c.AbortWithStatus(http.StatusNotFound)
			return
		}
		c.Next()
	}
}