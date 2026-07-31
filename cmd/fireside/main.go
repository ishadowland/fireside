// Package main is the Fireside backend entry point.
//
// Sprint 0 wires:
//   - Gin HTTP server on :18080 (single port shared with WebSocket per ADR-0004)
//   - /healthz for liveness
//   - POST /v1/auth/login mounted by internal/auth (SUB-001)
//   - GET  /ws/v1/connect mounted by internal/ws (SUB-003)
//
// Each sub-package self-registers in its own Mount() helper so main.go
// never imports the package's internals.
package main

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	_ "github.com/jackc/pgx/v5/stdlib" // registers the "pgx" database/sql driver

	"github.com/ishadowland/fireside/internal/auth"
	"github.com/ishadowland/fireside/internal/config"
	"github.com/ishadowland/fireside/internal/dashboard"
	"github.com/ishadowland/fireside/internal/store"
	wspkg "github.com/ishadowland/fireside/internal/ws"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("config load failed", "err", err)
		os.Exit(1)
	}

	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel})))

	gin.SetMode(gin.ReleaseMode)
	engine := gin.New()
	engine.Use(gin.Recovery())
	engine.Use(requestLogger())

	engine.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	auth.Mount(engine, auth.Config{
		JWTSecret:      cfg.JWTSecret,
		AccessTokenTTL: cfg.JWTAccessTTL,
		StubCode:       cfg.SMSStubCode,
		Users:          newUserStore(cfg.PostgresDSN),
	})

	dashboard.Mount(engine, dashboard.Config{
		StubCode: cfg.SMSStubCode,
	})

	wspkg.Mount(engine, wspkg.Config{
		JWTSecret:    cfg.JWTSecret,
		HelloTimeout: 5 * time.Second, // ADR-0007 mandate (5s)
		CheckOrigin: func(r *http.Request) bool {
			// Sprint 0: allow any origin (dev). Production sets a strict
			// allow-list via CORS_ALLOWED_ORIGINS env (deferred).
			return true
		},
		OnAuthenticated: func(uid int64, jti string, _ *websocket.Conn) {
			slog.Info("ws authenticated", "user_id", uid, "jti", jti)
		},
	})

	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           engine,
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		slog.Info("fireside backend starting",
			"port", cfg.Port,
			"jwt_secret_bytes", len(cfg.JWTSecret),
			"access_token_ttl", cfg.JWTAccessTTL.String(),
		)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("server failed", "err", err)
			os.Exit(1)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	slog.Info("shutdown signal received")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("graceful shutdown failed", "err", err)
		os.Exit(1)
	}
	slog.Info("fireside backend stopped cleanly")
}

// requestLogger emits one structured log line per request, matching the
// log/slog convention mandated by docs/design/02-modules.md.
func requestLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		slog.Info("http",
			"method", c.Request.Method,
			"path", c.Request.URL.Path,
			"status", c.Writer.Status(),
			"dur_ms", time.Since(start).Milliseconds(),
			"client_ip", c.ClientIP(),
		)
	}
}

// newUserStore opens the Postgres connection and returns a store.Queries,
// which satisfies auth.UserStore. The server still boots when the DB is
// unreachable (healthz/dashboard stay up); auth/login then fails with 500
// until Postgres is reachable.
func newUserStore(dsn string) *store.Queries {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		slog.Error("open postgres failed", "err", err)
		os.Exit(1)
	}
	db.SetMaxOpenConns(5)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)

	pingCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(pingCtx); err != nil {
		slog.Warn("postgres not reachable yet; /v1/auth/login will 500 until it is up", "err", err)
	} else {
		slog.Info("postgres connected")
	}
	return store.New(db)
}
