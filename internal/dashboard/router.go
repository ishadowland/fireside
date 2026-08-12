// Package dashboard serves a loopback-only web dashboard for local manual
// testing. It embeds a static HTML/JS page that mirrors the Android app's
// Sprint 0 flow: auto stub-login -> WS connect -> auth.hello -> welcome.
//
// Access is restricted to loopback addresses (127.0.0.1 / ::1) per ADR-0019.
package dashboard

import (
	"embed"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/ishadowland/fireside/internal/loopback"
)

//go:embed assets/*
var assets embed.FS

// Config carries what the dashboard needs to drive the auto-login flow.
type Config struct {
	StubCode string // SMS_STUB_CODE from config; falls back to "1234" in auth
}

// Mount registers the dashboard routes onto r.
func Mount(r *gin.Engine, cfg Config) {
	g := r.Group("/dashboard", loopback.Middleware())
	{
		g.GET("", serveAsset("assets/index.html"))
		g.GET("/", serveAsset("assets/index.html"))
		// WP-8: lobby + chat pages.
		g.GET("/rooms", serveAsset("assets/rooms.html"))
		g.GET("/rooms/:id", serveAsset("assets/room.html"))
		// Interface self-check page (end-to-end functional validation
		// of every currently-supported REST + WS interface).
		g.GET("/check", serveAsset("assets/check.html"))
		// Admin page (force-close / delete rooms; see internal/admin).
		g.GET("/admin", serveAsset("assets/admin.html"))
		g.GET("/static/:file", serveAssetDir())
	}

	cfgGroup := r.Group("/v1/dashboard", loopback.Middleware())
	{
		cfgGroup.GET("/config", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"stub_code": cfg.StubCode})
		})
	}
}

// serveAsset returns a handler that writes a single embedded file.
func serveAsset(name string) gin.HandlerFunc {
	return func(c *gin.Context) {
		serveEmbedded(c, name)
	}
}

// serveAssetDir serves files under assets/ via /dashboard/static/:file,
// guarding against path traversal.
func serveAssetDir() gin.HandlerFunc {
	return func(c *gin.Context) {
		file := c.Param("file")
		if file == "" || strings.Contains(file, "..") || strings.ContainsAny(file, "\\/") {
			c.AbortWithStatus(http.StatusNotFound)
			return
		}
		serveEmbedded(c, "assets/"+file)
	}
}

// serveEmbedded writes the named embedded asset with a content type inferred
// from its extension.
func serveEmbedded(c *gin.Context, name string) {
	data, err := assets.ReadFile(name)
	if err != nil {
		c.AbortWithStatus(http.StatusNotFound)
		return
	}
	c.Data(http.StatusOK, contentTypeFor(name), data)
}

func contentTypeFor(name string) string {
	switch {
	case strings.HasSuffix(name, ".html"):
		return "text/html; charset=utf-8"
	case strings.HasSuffix(name, ".css"):
		return "text/css; charset=utf-8"
	case strings.HasSuffix(name, ".js"):
		return "application/javascript; charset=utf-8"
	default:
		return "text/plain; charset=utf-8"
	}
}
