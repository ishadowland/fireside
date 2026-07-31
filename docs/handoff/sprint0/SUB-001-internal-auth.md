# SUB-001: `internal/auth/` — JWT issue/validate + `POST /v1/auth/login`

- **Subcontract ID**: SUB-001
- **Parent RFC**: `docs/rfc/phase-1-mvp.md` §Sprint 0 task breakdown (Midday slots)
- **Out scope for**: A single Go developer (or AI coding agent) who knows Gin + JWT basics. No Android, no Postgres setup required — just Go.
- **Depends on** (must already exist in `main`):
  - `go.mod` with `github.com/ishadowland/fireside` module path
  - `github.com/gin-gonic/gin` v1.10+
  - `github.com/golang-jwt/jwt/v5` v5.2+
  - `github.com/google/uuid` (for `jti`)
- **NOT in scope**: SMS provider, refresh tokens, real DB user lookup. Stub-accept any phone + code `1234`, fabricate a `user_id` for now. Sprint 1+ swaps in real store.

## What you deliver

Four files, all under `internal/auth/`:

1. `jwt.go` — token issue/validate (pure functions, no Gin)
2. `handler.go` — Gin handler for `POST /v1/auth/login`
3. `router.go` — `Mount(r *gin.Engine, cfg Config)` helper that wires the route
4. `jwt_test.go` + `handler_test.go` — unit + handler tests

Plus one consumer site:
5. `cmd/fireside/main.go` — call `auth.Mount(engine, cfg)` (only if main.go does not yet wire this; otherwise skip — see §"Integration points")

## Interface contract (locked — do not deviate)

```go
// internal/auth/jwt.go
package auth

import (
    "errors"
    "time"

    "github.com/golang-jwt/jwt/v5"
    "github.com/google/uuid"
)

var (
    ErrTokenExpired = errors.New("auth: token expired")
    ErrTokenInvalid = errors.New("auth: token invalid")
)

type Claims struct {
    UserID int64  `json:"uid"`
    JTI    string `json:"jti"`
    jwt.RegisteredClaims
}

// Issue signs a HS256 token. Returns (token, jti, error).
// jti is returned separately so the caller can persist it (for replay defense — see ADR-0007).
// ttl is the access-token lifetime (15 min per RFC).
func Issue(secret []byte, userID int64, ttl time.Duration) (string, string, error)

// Validate parses and verifies a HS256 token. Returns ErrTokenExpired for expired tokens,
// ErrTokenInvalid for any other parse/verification failure.
func Validate(secret []byte, tokenStr string) (*Claims, error)
```

```go
// internal/auth/handler.go
package auth

// LoginRequest is the JSON body of POST /v1/auth/login.
type LoginRequest struct {
    Phone string `json:"phone" binding:"required,e164"` // E.164, e.g. "+8613800138000"
    Code  string `json:"code"  binding:"required,len=4"`
}

type LoginResponse struct {
    Token     string `json:"token"`
    ExpiresIn int    `json:"expires_in"` // seconds until expiry
}

// StubCode is the only accepted SMS code in Sprint 0. Real SMS provider lands Sprint 1+.
const StubCode = "1234"

// LoginHandler returns the Gin handler for POST /v1/auth/login.
// On success: 200 with LoginResponse.
// On failure: 401 with {"error": "invalid_credentials"} (wrong code or malformed body).
// Note: Sprint 0 does NOT verify the phone maps to an existing user — any E.164 phone
// is accepted. Sprint 1+ will look up users.phone and reject unknowns.
func LoginHandler(cfg Config) gin.HandlerFunc
```

```go
// internal/auth/router.go
package auth

type Config struct {
    JWTSecret      []byte        // required; HS256 secret, ≥32 bytes in prod
    AccessTokenTTL time.Duration // required; RFC says 15 min
}

// Mount registers POST /v1/auth/login onto r.
func Mount(r *gin.Engine, cfg Config)
```

## Implementation plan (steps you follow)

### Step 1 — `jwt.go` (~30 min)

Implement `Issue` and `Validate`:

- Use `jwt.NewWithClaims(jwt.SigningMethodHS256, &Claims{...})`
- `RegisteredClaims.ExpiresAt` = `jwt.NewNumericDate(time.Now().Add(ttl))`
- `jti` = `uuid.NewString()`
- `Validate` uses `jwt.ParseWithClaims` with a custom key function that returns `secret`
- On `jwt.ErrTokenExpired` return `ErrTokenExpired`; on any other parse/validation error return `ErrTokenInvalid`

Pitfall: `jwt-go` v5 changed `Claims.Valid()` semantics. Use `jwt.WithExpirationRequired()` option in `ParseWithClaims` so a token without `exp` is rejected (defense in depth).

### Step 2 — `jwt_test.go` (~20 min)

Three test cases:

1. **Roundtrip**: `Issue` → `Validate` returns same `UserID` and `JTI`.
2. **Expired**: build a token with `ttl = 1 * time.Millisecond`, sleep 10ms, `Validate` returns `ErrTokenExpired`.
3. **Tampered**: flip one character in the signature, `Validate` returns `ErrTokenInvalid`.

Use table-driven `t.Run`. No external test fixtures needed.

### Step 3 — `handler.go` (~30 min)

`LoginHandler` flow:

```
bind JSON → if err: 400 with {"error":"invalid_request"}
if req.Code != StubCode: 401 with {"error":"invalid_credentials"}
// Stub: skip DB lookup. user_id = deterministic hash of phone (so re-login returns same id).
// Use fnv64 of phone string → cast to int64. Document why in a comment.
userID := deriveStubUserID(req.Phone)
token, jti, err := Issue(cfg.JWTSecret, userID, cfg.AccessTokenTTL)
if err != nil: 500 with {"error":"internal_error"} (and slog.Error)
return 200 with LoginResponse{Token: token, ExpiresIn: int(cfg.AccessTokenTTL.Seconds())}
```

`deriveStubUserID` lives in `handler.go`, marked with `// stub: replace in Sprint 1 with real user lookup`.

### Step 4 — `handler_test.go` (~30 min)

Use `httptest`:

1. **Happy path**: POST `{"phone":"+8613800138000","code":"1234"}` → 200, `token` non-empty, `expires_in == 900` (15 min).
2. **Wrong code**: POST `{"phone":"...","code":"0000"}` → 401, body `{"error":"invalid_credentials"}`.
3. **Bad phone**: POST `{"phone":"not-a-phone","code":"1234"}` → 400, body `{"error":"invalid_request"}`.
4. **Deterministic user_id**: log in twice with same phone, both tokens validate to same `Claims.UserID`.

### Step 5 — `router.go` (~10 min)

```go
func Mount(r *gin.Engine, cfg Config) {
    r.POST("/v1/auth/login", LoginHandler(cfg))
}
```

### Step 6 — Wire into `cmd/fireside/main.go` (~5 min)

If `cmd/fireside/main.go` already exists and boots Gin: add `auth.Mount(engine, auth.Config{JWTSecret: ..., AccessTokenTTL: 15*time.Minute})` after the `/healthz` route. Read `JWT_SECRET` and `JWT_ACCESS_TTL_MIN` from env (matches `.env.example`).

If `cmd/fireside/main.go` does not exist yet: skip step 6, leave `Mount` callable for whoever wires main.go.

## Acceptance criteria (binary pass/fail)

The contract owner (Hermes / project owner) verifies ALL of these before signing off:

- [ ] `internal/auth/` contains exactly the 4 files listed above
- [ ] `go build ./...` exits 0
- [ ] `go test ./internal/auth/...` passes all listed cases
- [ ] `golangci-lint run ./internal/auth/...` exits 0 (only if `.golangci.yml` exists; otherwise skip)
- [ ] Manual smoke: start the backend (`make backend.run`), then
  `curl -X POST localhost:18080/v1/auth/login -H 'Content-Type: application/json' -d '{"phone":"+8613800138000","code":"1234"}'`
  returns `{"token":"...","expires_in":900}` and HTTP 200
- [ ] Same `curl` with `code:0000` returns 401
- [ ] No other files modified outside `internal/auth/` and (optionally) `cmd/fireside/main.go`

## What you do NOT do

- ❌ Do not introduce a `users` table or any SQL
- ❌ Do not create `.env`, only read from `os.Getenv`
- ❌ Do not add `/v1/auth/refresh` — deferred to Sprint 1+
- ❌ Do not add rate limiting — deferred to Phase 2
- ❌ Do not modify `cmd/fireside/main.go` unless it already exists
- ❌ Do not write the WebSocket handler — that is SUB-003

## Verification handoff

When you finish, post a comment with:

1. The diff (`git diff main -- internal/auth/ cmd/fireside/main.go` if applicable)
2. `go test ./internal/auth/... -v` output
3. The two `curl` outputs (200 case + 401 case)
4. Any decisions you made that deviate from this spec (and why)

The contract owner will run the acceptance checklist against your handoff.

## Estimated effort

90–120 minutes for a Go developer with Gin + JWT familiarity. No external blockers if `go.mod` is already set up.