-- name: GetUserByPhone :one
-- Sprint 0 stub: returns the user row matching the E.164 phone number.
-- May return no rows for unknown phones (Sprint 1+ will reject at handler
-- level). Sprint 0 LoginHandler does NOT call this — it uses
-- deriveStubUserID. This query ships now so Sprint 1 has a starting point.

SELECT id, phone, created_at
FROM users
WHERE phone = $1;

-- name: InsertUser :one
-- Sprint 1+ will call this from a real SMS-provider path. Sprint 0
-- does not use it. id is provided by the caller (fnv64 stub today,
-- ULID in Sprint 1) — there is no DEFAULT so the stub's deterministic
-- int64 mapping is preserved.

INSERT INTO users (id, phone)
VALUES ($1, $2)
RETURNING id, phone, created_at;

-- name: InsertToken :one
-- Persist a freshly issued JWT's jti so the server can detect replays
-- within the access-token TTL window (ADR-0007 §Risks → "Replay").
-- Sprint 0's LoginHandler does not call this either — it's wired in
-- Sprint 1 alongside the real user lookup. Listing it here so Sprint 1
-- has a known query name to invoke.

INSERT INTO auth_tokens (jti, user_id, expires_at)
VALUES ($1, $2, $3)
RETURNING jti, user_id, expires_at, created_at;

-- name: DeleteExpiredTokens :execresult
-- Periodic cleanup goroutine (Sprint 1+) sweeps jti rows whose
-- expires_at is in the past. Without this the table grows unbounded.

DELETE FROM auth_tokens
WHERE expires_at < NOW();

-- name: GetTokenByJTI :one
-- Sprint 1-2 replay defense (ADR-0007 §Risks → "Replay"): the WS
-- first-frame auth looks this up to reject tokens whose jti was never
-- persisted (i.e. not issued by POST /v1/auth/login in this server's
-- lifetime), so a stolen/replayed token can't be accepted even if its
-- signature is valid.

SELECT jti, user_id, expires_at, created_at
FROM auth_tokens
WHERE jti = $1;