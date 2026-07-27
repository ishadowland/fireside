package ws

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

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

		conn.SetReadDeadline(time.Now().Add(timeout))
		claims, code, err := processHello(conn, cfg.JWTSecret)
		if err != nil {
			// processHello returned a network error — conn may already be dead.
			slog.Warn("ws processHello error", "code", code, "err", err)
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
			if cerr := conn.Close(websocket.ClosePolicyViolation); cerr != nil {
				slog.Warn("ws close after auth.error failed", "code", code, "err", cerr)
			}
			return
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
		// future Sprint 1+ traffic without immediate close.
		_ = conn.SetReadDeadline(time.Time{})
		// Block in a discard read loop until the peer closes.
		for {
			if _, _, rerr := conn.NextReader(); rerr != nil {
				return
			}
		}
	}
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