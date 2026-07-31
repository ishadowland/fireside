# SUB-003: `internal/ws/` — WebSocket upgrader + first-frame router

- **Subcontract ID**: SUB-003
- **Parent RFC**: `docs/rfc/phase-1-mvp.md` §Sprint 0 task breakdown (Afternoon slot, WS half)
- **Out scope for**: A single Go developer (or AI coding agent) with Gin + WebSocket experience.
- **Depends on** (must already exist in `main`):
  - `go.mod` with `github.com/ishadowland/fireside` module path
  - `github.com/gin-gonic/gin` v1.10+
  - `github.com/gorilla/websocket` v1.5+
  - **`internal/auth/` from SUB-001** — uses `auth.Validate(secret, token)` to verify `auth.hello` JWT
- **NOT in scope**: broadcast hub, room registry, message routing, agent dispatch. SUB-003 only proves the WS handshake + first-frame auth path. Full broadcast hub is Sprint 1+.

## What you deliver

Five files, all under `internal/ws/`:

1. `upgrader.go` — `HandleConnect` Gin handler that does the HTTP→WS upgrade
2. `first_frame.go` — reads `auth.hello`, validates JWT, replies `auth.welcome` (or close)
3. `protocol.go` — frame types (`auth.hello`, `auth.welcome`, `auth.error`) as Go structs
4. `upgrader_test.go` — end-to-end test using `httptest` + a real `gorilla/websocket` client
5. `router.go` — `Mount(r *gin.Engine, cfg Config)` helper

Plus one consumer site:
6. `cmd/fireside/main.go` — call `ws.Mount(engine, ws.Config{JWTSecret: ...})` (only if main.go exists; otherwise skip)

## Interface contract (locked — do not deviate)

```go
// internal/ws/protocol.go
package ws

// Frames the server reads from the client (Sprint 0 only):
const FrameTypeAuthHello = "auth.hello"

// Frames the server sends back (Sprint 0 only):
const (
    FrameTypeAuthWelcome = "auth.welcome"
    FrameTypeAuthError   = "auth.error"
)

// AuthHello is the only client frame the server reads in Sprint 0.
// Clients must send this within HelloTimeout of the WS upgrade succeeding.
type AuthHello struct {
    Type  string `json:"type"`            // must equal FrameTypeAuthHello
    Token string `json:"token"`           // HS256 JWT issued by SUB-001
}

// AuthWelcome is sent on successful auth.hello.
type AuthWelcome struct {
    Type     string `json:"type"`         // FrameTypeAuthWelcome
    UserID   int64  `json:"user_id"`
    JTI      string `json:"jti"`
    ServerTime int64 `json:"server_time"` // unix seconds; helps clients detect skew
}

// AuthError is sent before close on failed auth.
type AuthError struct {
    Type  string `json:"type"`  // FrameTypeAuthError
    Code  string `json:"code"`  // "invalid_token" | "hello_timeout" | "bad_frame" | "internal_error"
    Error string `json:"error"` // human-readable
}

// Error codes (exported so callers can match):
const (
    CodeInvalidToken = "invalid_token"
    CodeHelloTimeout = "hello_timeout"
    CodeBadFrame     = "bad_frame"
    CodeInternal     = "internal_error"
)
```

```go
// internal/ws/upgrader.go
package ws

import (
    "time"

    "github.com/gorilla/websocket"
)

type Config struct {
    JWTSecret      []byte
    HelloTimeout   time.Duration  // default 5s if zero; ADR-0007 mandate
    CheckOrigin    func(r *http.Request) bool // nil means reject all origins (dev); 
                                              // production uses CorsAllowedOrigins from cfg
    // OnAuthenticated is called AFTER auth.welcome is sent. Sprint 0 stub: just log.
    // Sprint 1+ will register the conn in the per-room hub here.
    OnAuthenticated func(userID int64, jti string, conn *websocket.Conn)
}

// HandleConnect is the Gin handler for GET /ws/v1/connect.
// Performs WS upgrade, reads auth.hello within HelloTimeout, validates JWT, sends auth.welcome.
// On any failure: writes an auth.error frame and closes with code 1008 (policy violation).
// On success: invokes cfg.OnAuthenticated and leaves the connection open (Sprint 1+ takes over).
func HandleConnect(cfg Config) gin.HandlerFunc
```

```go
// internal/ws/router.go
package ws

// Mount registers GET /ws/v1/connect onto r.
func Mount(r *gin.Engine, cfg Config)
```

## Implementation plan (steps you follow)

### Step 1 — `protocol.go` (~15 min)

Define all the types and constants above. Use `encoding/json` struct tags. Add a `Validate()` method on `AuthHello` that checks `Type == FrameTypeAuthHello` and `Token != ""` — returns `CodeBadFrame` error otherwise.

### Step 2 — `upgrader.go` (~45 min)

`HandleConnect` flow:

```
1. upgrader := websocket.Upgrader{
     ReadBufferSize:  1024,
     WriteBufferSize: 1024,
     CheckOrigin:     cfg.CheckOrigin, // nil-safe: if nil, reject all (dev default)
   }
2. conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
   if err != nil: return (Gin already wrote the response; log it)
3. defer conn.Close()
4. conn.SetReadLimit(8KB) // sanity cap
5. Set hello deadline:
     conn.SetReadDeadline(time.Now().Add(cfg.HelloTimeout))  // 5s default
6. Read one message (use ReadJSON into AuthHello{}):
     err := conn.ReadJSON(&hello)
     if err:
        if isTimeout(err): send AuthError{Code: CodeHelloTimeout}; return
        if isClose(err): return  // client closed cleanly
        send AuthError{Code: CodeBadFrame}; return
7. Validate frame:
     if hello.Validate() != nil: send AuthError{Code: CodeBadFrame}; return
8. Validate JWT:
     claims, err := auth.Validate(cfg.JWTSecret, hello.Token)
     if err == auth.ErrTokenExpired: send AuthError{Code: CodeInvalidToken, Error:"expired"}; return
     if err == auth.ErrTokenInvalid: send AuthError{Code: CodeInvalidToken}; return
     if err != nil: send AuthError{Code: CodeInternal}; return
9. Send auth.welcome:
     conn.WriteJSON(AuthWelcome{Type: FrameTypeAuthWelcome, UserID: claims.UserID, JTI: claims.JTI, ServerTime: time.Now().Unix()})
10. cfg.OnAuthenticated(claims.UserID, claims.JTI, conn)  // Sprint 0: log only
11. Leave connection open. (Sprint 0 just blocks here reading pings or blocks until client disconnects.)
```

Implementation pitfalls:

- **`ReadJSON` blocks forever if no deadline**: forgetting `SetReadDeadline` is the #1 way this code hangs. The 5s deadline is part of the acceptance test.
- **Write the error frame BEFORE Close**: `WriteJSON` then `Close()` with code 1008. If WriteJSON fails (client already gone), log and exit.
- **Don't write a Gin response after Upgrade succeeded**: by the time `Upgrade` returns, the HTTP 101 Switching Protocols is on the wire. Subsequent errors must go through the WS conn, not `c.JSON()`.

### Step 3 — `first_frame.go` (~10 min)

Helper extracted for testability:

```go
// processHello reads the auth.hello frame, validates it, returns claims or an error code.
// Pure function over (conn, deadline, secret) — no Gin.
func processHello(conn *websocket.Conn, deadline time.Time, secret []byte) (*auth.Claims, string, error)
// returns (claims, errorCode, err). errorCode is "" on success.
```

This lets the test inject a fake conn. Implementation reads one frame with `ReadJSON(&AuthHello{})`, validates, and calls `auth.Validate`.

### Step 4 — `router.go` (~5 min)

```go
func Mount(r *gin.Engine, cfg Config) {
    r.GET("/ws/v1/connect", HandleConnect(cfg))
}
```

### Step 5 — `upgrader_test.go` (~45 min)

Three end-to-end tests using `httptest.NewServer` + `gorilla/websocket.Dialer`:

1. **Happy path**:
   - Spin up an `httptest.NewServer` with a Gin engine that mounts `HandleConnect` with `JWTSecret: testSecret, HelloTimeout: 5*time.Second`
   - `OnAuthenticated` records the `userID` for assertion
   - Issue a real JWT via `auth.Issue(testSecret, 42, 5*time.Minute)`
   - `websocket.Dial(url+"/ws/v1/connect", nil)` → expect dial error = nil
   - Send `{"type":"auth.hello","token":"<jwt>"}`
   - Read next frame → assert it's `AuthWelcome{UserID:42, JTI:<matches>}`
   - Assert `OnAuthenticated` was called with `(42, <jti>, conn)`

2. **Hello timeout**:
   - Same setup with `HelloTimeout: 100*time.Millisecond`
   - Dial, but DO NOT send any frame
   - After ~150ms, attempt to read → expect `*websocket.CloseError` with code **1008** (RFC 6455 "policy violation")
   - (The server writes an `auth.error` frame first, but the client doesn't read it before close — assert via the close code)
   - Note: per **ADR-0007 (amended 2026-07-27)**, the close code is 1008, not 4001. If you copy code from older WS auth examples, double-check the constant.

3. **Invalid token**:
   - Dial, send `{"type":"auth.hello","token":"garbage"}`
   - Read frame → assert it's `AuthError{Code: CodeInvalidToken}`
   - Next read returns close 1008 (same RFC 6455 code as hello-timeout — clients shouldn't differentiate)

Pitfall: gorilla's `ReadMessage` will return `*websocket.CloseError` when the server closes. Wrap reads in a helper that classifies the error.

### Step 6 — Wire into `cmd/fireside/main.go` (~5 min)

Only if main.go exists. Add:

```go
ws.Mount(engine, ws.Config{
    JWTSecret:    []byte(os.Getenv("JWT_SECRET")),
    HelloTimeout: 5 * time.Second,
    OnAuthenticated: func(uid int64, jti string, _ *websocket.Conn) {
        slog.Info("ws authenticated", "user_id", uid, "jti", jti)
    },
})
```

If main.go doesn't exist, skip this step.

## Acceptance criteria (binary pass/fail)

- [ ] `internal/ws/` contains exactly the 5 files listed above
- [ ] `go build ./...` exits 0
- [ ] `go test ./internal/ws/...` passes all three test cases (happy / timeout / invalid_token)
- [ ] `go test ./...` exits 0 (no regression in SUB-001 tests)
- [ ] Manual smoke:
  - Start backend (`make backend.run`)
  - Get a token: `curl -X POST localhost:18080/v1/auth/login -H 'Content-Type: application/json' -d '{"phone":"+8613800138000","code":"1234"}'`
  - Connect via wscat: `wscat -c "ws://localhost:18080/ws/v1/connect"`
  - First frame: `{"type":"auth.hello","token":"<token>"}`
  - Expected reply: `{"type":"auth.welcome","user_id":<number>,"jti":"...","server_time":<unix>}`
- [ ] No files modified outside `internal/ws/` and (optionally) `cmd/fireside/main.go`

## What you do NOT do

- ❌ Do not implement message broadcast, room registry, or agent routing — Sprint 1+
- ❌ Do not register connections in any global hub — `OnAuthenticated` is a callback, not a registry
- ❌ Do not implement heartbeat/ping-pong — `gorilla/websocket` has built-in defaults; do not override
- ❌ Do not implement reconnect logic
- ❌ Do not write the Android client — that is SUB-ANDROID

## Verification handoff

When you finish, post:

1. `git diff main -- internal/ws/ cmd/fireside/main.go` (if applicable)
2. `go test ./internal/ws/... -v` output
3. The wscat roundtrip transcript
4. Any deviations from this spec

## Estimated effort

90–150 minutes for a Go developer with Gin + WebSocket experience. Depends on SUB-001 being merged first.