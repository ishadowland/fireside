package ws

import (
	"errors"
	"time"

	"github.com/gorilla/websocket"

	"github.com/ishadowland/fireside/internal/auth"
)

// validateToken wraps auth.Validate with the ws-package error contract.
// Both ErrTokenExpired and ErrTokenInvalid surface as CodeInvalidToken
// to the client (per SUB-003 protocol.go); this function preserves the
// error type so processHello can log the difference server-side.
func validateToken(secret []byte, tokenStr string) (*auth.Claims, error) {
	c, err := auth.Validate(secret, tokenStr)
	if err != nil {
		return nil, err
	}
	return c, nil
}

// processHello reads the auth.hello frame, validates it, and returns the
// claims on success.
//
// Pure function over (conn, deadline, secret) — no Gin, no logging —
// so it is directly testable without spinning up the whole handler.
//
// Returns:
//   - (*auth.Claims, "", nil)            on success
//   - (nil, errorCode, nil)              where errorCode ∈ {CodeBadFrame, CodeInvalidToken, CodeInternal}
//   - (nil, CodeHelloTimeout, err)       on read timeout
//
// The caller (HandleConnect) must set conn.SetReadDeadline(deadline)
// BEFORE invoking processHello.
func processHello(conn *websocket.Conn, secret []byte) (*auth.Claims, string, error) {
	conn.SetReadLimit(8 * 1024) // 8 KB sanity cap on the auth.hello payload

	var hello AuthHello
	if err := conn.ReadJSON(&hello); err != nil {
		if isReadTimeout(err) {
			return nil, CodeHelloTimeout, err
		}
		if isClose(err) {
			// client closed cleanly — treat as bad_frame with no err so
			// the caller doesn't try to write an error frame back
			return nil, CodeBadFrame, nil
		}
		return nil, CodeBadFrame, err
	}

	if code := hello.Validate(); code != "" {
		return nil, code, nil
	}

	c, err := validateToken(secret, hello.Token)
	if err != nil {
		if errors.Is(err, auth.ErrTokenExpired) {
			return nil, CodeInvalidToken, nil
		}
		if errors.Is(err, auth.ErrTokenInvalid) {
			return nil, CodeInvalidToken, nil
		}
		return nil, CodeInternal, err
	}
	return c, "", nil
}

// isReadTimeout checks whether the read error is a network timeout.
// gorilla/websocket surfaces the underlying net.Error; we use both
// errors.Is (preferred) and a string check as belt-and-braces for
// older library versions where errors.Is wiring may not be perfect.
func isReadTimeout(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, websocket.ErrCloseSent) {
		return false
	}
	var ne interface{ Timeout() bool }
	if errors.As(err, &ne) && ne.Timeout() {
		return true
	}
	// gorilla wraps net.Error values; check string as fallback.
	msg := err.Error()
	return msg == "i/o timeout" ||
		(msg != "" && containsAll(msg, "read tcp", "timeout"))
}

func isClose(err error) bool {
	return websocket.IsCloseError(err,
		websocket.CloseNormalClosure,
		websocket.CloseGoingAway,
		websocket.CloseNoStatusReceived,
	)
}

func containsAll(s string, subs ...string) bool {
	for _, sub := range subs {
		if !containsString(s, sub) {
			return false
		}
	}
	return true
}

func containsString(s, sub string) bool {
	if sub == "" {
		return true
	}
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// defaultHelloTimeout is the ADR-0007-mandated 5-second window for
// auth.hello. Config{} zero-value falls back to this.
const defaultHelloTimeout = 5 * time.Second

func effectiveHelloTimeout(t time.Duration) time.Duration {
	if t <= 0 {
		return defaultHelloTimeout
	}
	return t
}