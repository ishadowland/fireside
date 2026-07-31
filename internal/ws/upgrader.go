package ws

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"

	"github.com/ishadowland/fireside/internal/store"
)

// ClosePolicyViolation is the RFC 6455 close code for "policy
// violation". Per ADR-0007 (amended 2026-07-27), this is what Fireside
// uses for every auth failure / hello timeout — the original 4001 was
// rejected as an app-specific convention that breaks cross-client
// handling.
const ClosePolicyViolation = websocket.ClosePolicyViolation

// TokenLookup verifies that a JWT's jti was persisted by /v1/auth/login
// in this server's lifetime. Sprint 1-2 replay defense (ADR-0007 §Risks →
// "Replay"): even a perfectly-signed token is rejected if its jti is not
// in the table, so a stolen/replayed token can't be accepted.
//
// *store.Queries satisfies it directly. nil disables the check (test/legacy
// paths); main.go always wires the real store.
type TokenLookup interface {
	GetTokenByJTI(ctx context.Context, jti uuid.UUID) (store.AuthToken, error)
}

// Config wires the WS handler. JWTSecret and OnAuthenticated are
// required; HelloTimeout and CheckOrigin have sane defaults.
type Config struct {
	JWTSecret    []byte
	HelloTimeout time.Duration // default 5s if zero; ADR-0007 mandate

	// CheckOrigin runs on every upgrade. nil means reject all origins —
	// the dev default. Production sets this to a function that
	// allow-lists the public hostnames.
	CheckOrigin func(r *http.Request) bool

	// OnAuthenticated is called AFTER auth.welcome is sent. Sprint 0
	// stub: just log. Sprint 1+ will register the conn in the per-room
	// hub here.
	OnAuthenticated func(userID int64, jti string, conn *websocket.Conn)

	// Tokens enables jti replay defense. nil = check skipped (tests).
	Tokens TokenLookup
}

// HandleConnect returns the Gin handler for GET /ws/v1/connect.
//
// Performs WS upgrade, reads auth.hello within HelloTimeout, validates
// JWT, sends auth.welcome. On any failure: writes an auth.error frame
// and closes with code 1008 (RFC 6455 policy violation; per ADR-0007
// amended 2026-07-27).
func HandleConnect(cfg Config) gin.HandlerFunc {
	if cfg.JWTSecret == nil {
		panic("ws.HandleConnect: JWTSecret is required")
	}
	if cfg.OnAuthenticated == nil {
		cfg.OnAuthenticated = func(uid int64, jti string, _ *websocket.Conn) {
			slog.Info("ws authenticated (no callback wired)", "user_id", uid, "jti", jti)
		}
	}

	upgrader := websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		CheckOrigin:     cfg.CheckOrigin, // nil-safe: gorilla rejects all when nil
	}

	timeout := effectiveHelloTimeout(cfg.HelloTimeout)

	return func(c *gin.Context) {
		conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
		if err != nil {
			// gin already wrote the 400-class response; just log.
			slog.Warn("ws upgrade failed", "err", err, "client_ip", c.ClientIP())
			return
		}
		defer func() { _ = conn.Close() }()

		if err := conn.SetReadDeadline(time.Now().Add(timeout)); err != nil {
			slog.Warn("ws set read deadline failed", "err", err)
			sendPolicyViolationClose(conn)
			return
		}
		claims, code, err := processHello(conn, cfg.JWTSecret)
		if err != nil {
			// processHello returned a network error (timeout / read failure).
			// code is still set (e.g. CodeHelloTimeout). Try to write an
			// auth.error frame + 1008 close so clients see a structured
			// reason. If WriteJSON fails the peer is gone and we just exit.
			slog.Warn("ws processHello network error", "code", code, "err", err)
			if werr := conn.WriteJSON(AuthError{
				Type:  FrameTypeAuthError,
				Code:  code,
				Error: humanize(code),
			}); werr != nil {
				slog.Debug("ws auth.error write after net err failed", "err", werr)
				return
			}
			sendPolicyViolationClose(conn)
			return
		}
		if code != "" {
			// try to write an auth.error frame so clients see a structured
			// reason before the close. If WriteJSON fails, the conn is
			// already gone — log and exit.
			if werr := conn.WriteJSON(AuthError{
				Type:  FrameTypeAuthError,
				Code:  code,
				Error: humanize(code),
			}); werr != nil {
				slog.Warn("ws auth.error write failed", "code", code, "err", werr)
				return
			}
			sendPolicyViolationClose(conn)
			return
		}

		// Sprint 1-2: replay defense (ADR-0007 §Risks). Even a perfectly
		// signed token is rejected if its jti is not in auth_tokens.
		if cfg.Tokens != nil {
			jtUUID, perr := uuid.Parse(claims.JTI)
			if perr != nil {
				slog.Warn("ws jti not a uuid", "jti", claims.JTI)
				writeAuthErrorAndClose(conn, CodeInvalidToken)
				return
			}
			if _, err := cfg.Tokens.GetTokenByJTI(c.Request.Context(), jtUUID); err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					slog.Info("ws rejected: jti not persisted", "jti", claims.JTI, "user_id", claims.UserID)
				} else {
					slog.Warn("ws jti lookup failed", "err", err, "jti", claims.JTI)
				}
				writeAuthErrorAndClose(conn, CodeInvalidToken)
				return
			}
		}

		welcome := AuthWelcome{
			Type:       FrameTypeAuthWelcome,
			UserID:     claims.UserID,
			JTI:        claims.JTI,
			ServerTime: time.Now().Unix(),
		}
		if werr := conn.WriteJSON(welcome); werr != nil {
			slog.Warn("ws auth.welcome write failed", "err", werr, "user_id", claims.UserID)
			return
		}

		cfg.OnAuthenticated(claims.UserID, claims.JTI, conn)

		// Sprint 0: just block until the client disconnects. We do NOT
		// install ping/pong handlers or a read loop with intent — the
		// gorilla default read deadline expires after the auth.hello
		// deadline; we clear it so the connection can stay open for
		// future Sprint 1+ traffic without immediate close. errcheck
		// (errcheck) insists we look at the return; a clear failure
		// here is fatal but unrecoverable, so we just log.
		if err := conn.SetReadDeadline(time.Time{}); err != nil {
			slog.Warn("ws clear read deadline failed", "err", err)
		}
		// Block in a discard read loop until the peer closes.
		for {
			if _, _, rerr := conn.NextReader(); rerr != nil {
				return
			}
		}
	}
}

// sendPolicyViolationClose writes a Close frame with code 1008 (RFC 6455
// policy violation) and then closes the underlying TCP conn. gorilla
// v1.5.x's Conn.Close() takes no args, so we use WriteControl for the
// Close frame and then call Close() to release the fd.
func sendPolicyViolationClose(conn *websocket.Conn) {
	deadline := time.Now().Add(time.Second)
	msg := websocket.FormatCloseMessage(ClosePolicyViolation, "policy violation")
	if err := conn.WriteControl(websocket.CloseMessage, msg, deadline); err != nil {
		// Peer may already be gone — fall through to plain Close.
		slog.Debug("ws write control close failed (peer likely gone)", "err", err)
	}
	if err := conn.Close(); err != nil {
		slog.Debug("ws plain close failed", "err", err)
	}
}

// writeAuthErrorAndClose writes an auth.error frame and then closes the
// connection with code 1008. Used by the replay-defense check (Sprint 1-2)
// and any other post-hello validation failure path.
func writeAuthErrorAndClose(conn *websocket.Conn, code string) {
	if werr := conn.WriteJSON(AuthError{
		Type:  FrameTypeAuthError,
		Code:  code,
		Error: humanize(code),
	}); werr != nil {
		slog.Debug("ws auth.error write failed", "code", code, "err", werr)
		return
	}
	sendPolicyViolationClose(conn)
}

// humanize maps error codes to human-readable strings for the AuthError
// payload. Kept tiny — clients can display this directly.
func humanize(code string) string {
	switch code {
	case CodeInvalidToken:
		return "JWT expired or malformed"
	case CodeHelloTimeout:
		return "auth.hello not received within timeout"
	case CodeBadFrame:
		return "first frame was not a valid auth.hello"
	case CodeInternal:
		return "internal server error"
	default:
		return "unknown"
	}
}