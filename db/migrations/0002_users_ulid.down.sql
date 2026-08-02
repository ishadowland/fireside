-- 0002_users_ulid.down.sql
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
CREATE INDEX idx_auth_tokens_user_id    ON auth_tokens(user_id);