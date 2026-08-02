-- 0002_users_ulid.sql
-- Sprint 1-3 ULID migration (ADR-0014). Replaces the BIGINT fnv-derived
-- user_id with CHAR(26) ULID per the locked design doc
-- (docs/design/01-data-model.md, 03-protocol.md §auth.welcome).
--
-- Per ADR-0014: Sprint 0/1-1/1-2 stored BIGINT ids that were never
-- production-meaningful, so we DROP rather than attempt a lossy
-- conversion to ULID. This is safe: the table is dev-only and the
-- only writes so far are auto-registered stub users.
--
-- Drops happen in dependency order: auth_tokens.user_id REFERENCES
-- users(id), so users must be dropped last (or both at once via
-- CASCADE; we do it explicitly to keep the schema readable).
--
-- Idempotency: DROP TABLE IF EXISTS so re-running on a fresh DB is fine.

-- +migrate Up
DROP TABLE IF EXISTS auth_tokens;
DROP TABLE IF EXISTS users;

CREATE TABLE users (
    id         CHAR(26)     PRIMARY KEY,  -- ulid.ULID.String(); 26 chars
    phone      VARCHAR(32)  NOT NULL,
    created_at TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX idx_user_phone ON users(phone);

CREATE TABLE auth_tokens (
    jti        UUID         PRIMARY KEY,
    user_id    CHAR(26)     NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    expires_at TIMESTAMPTZ  NOT NULL,
    created_at TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_auth_tokens_expires_at ON auth_tokens(expires_at);
CREATE INDEX idx_auth_tokens_user_id    ON auth_tokens(user_id);

-- +migrate Down
-- Reverse to the 0001 (BIGINT) schema. Used by CI's `migrate ... down 1`
-- round-trip; kept simple because BIGINT data was throwaway per ADR-0014.
-- A real downgrade in production would also need to convert existing
-- CHAR(26) ULID ids back to BIGINT, which is not lossless — that's
-- why this migration is documented as effectively one-way.
CREATE TABLE users (
    id         BIGINT       PRIMARY KEY,
    phone      VARCHAR(32)  NOT NULL,
    created_at TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX idx_user_phone ON users(phone);

CREATE TABLE auth_tokens (
    jti        UUID         PRIMARY KEY,
    user_id    BIGINT       NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    expires_at TIMESTAMPTZ  NOT NULL,
    created_at TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_auth_tokens_expires_at ON auth_tokens(expires_at);
CREATE INDEX idx_auth_tokens_user_id ON auth_tokens(user_id);