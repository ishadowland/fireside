// Package ws implements the WebSocket upgrader + first-frame
// auth.hello dispatcher per ADR-0007 and
// docs/handoff/sprint0/SUB-003-internal-ws.md.
//
// Sprint 0 only proves the handshake path; broadcast hub, room
// registry, and message routing land in Sprint 1+.
package ws

const (
	// FrameTypeAuthHello is the only client frame the server reads in
	// Sprint 0. Clients must send this within HelloTimeout (default 5s)
	// of the WS upgrade succeeding. Per ADR-0007 + the Sprint 0 scope
	// note in docs/handoff/sprint0/README.md, the payload is
	// {type, token} only; client_version/device_id are Sprint 1+.
	FrameTypeAuthHello = "auth.hello"

	// FrameTypeAuthWelcome is sent on successful auth.hello.
	FrameTypeAuthWelcome = "auth.welcome"

	// FrameTypeAuthError is sent before close on failed auth.
	FrameTypeAuthError = "auth.error"
)

// AuthHello is the only client frame the server reads in Sprint 0.
type AuthHello struct {
	Type  string `json:"type"`  // must equal FrameTypeAuthHello
	Token string `json:"token"` // HS256 JWT issued by SUB-001
}

// AuthWelcome is sent on successful auth.hello.
type AuthWelcome struct {
	Type       string `json:"type"`        // FrameTypeAuthWelcome
	UserID     int64  `json:"user_id"`     // Sprint 0 int64 per ADR-0014
	JTI        string `json:"jti"`         // the JWT's jti claim
	ServerTime int64  `json:"server_time"` // unix seconds; helps clients detect skew
}

// AuthError is sent before close on failed auth.
type AuthError struct {
	Type  string `json:"type"`  // FrameTypeAuthError
	Code  string `json:"code"`  // one of the Code* constants below
	Error string `json:"error"` // human-readable
}

// Error codes (exported so callers / tests can match):
const (
	CodeInvalidToken = "invalid_token"
	CodeHelloTimeout = "hello_timeout"
	CodeBadFrame     = "bad_frame"
	CodeInternal     = "internal_error"
)

// Validate checks that a decoded AuthHello has the expected shape.
// Returns CodeBadFrame if anything is wrong.
func (h *AuthHello) Validate() string {
	if h.Type != FrameTypeAuthHello {
		return CodeBadFrame
	}
	if h.Token == "" {
		return CodeBadFrame
	}
	return ""
}