-- 0008_refresh_tokens.up.sql
-- Sprint 1 WP-7.9 (issue #9): refresh token store.
--
-- The 0001_init.up.sql auth_tokens table tracks access-token jti
-- claims for replay defense (ADR-0007). Refresh tokens are a different
-- artifact: they are long-lived (7 days) bearer tokens that can be
-- exchanged for a fresh access token. They cannot reuse the access
-- token table because:
--
--   1. Refresh tokens need a longer TTL column (refresh_expires_at).
--   2. Refresh tokens must be ROTATED on each use (a used refresh
--      token is marked replaced_by_jti → prevents replay if it leaks).
--   3. The access-token owns its user_id directly; refresh tokens
--      also need a family_id so we can revoke an entire token chain
--      if one link is compromised.
--
-- Family revocation is implemented elsewhere as a periodic cleanup
-- (out of scope for Sprint 1; we just record the linkage).
--
-- All IDs are ULID-shaped (VARCHAR(26)) to match the schema post-0007.

CREATE TABLE refresh_tokens (
    jti                VARCHAR(26)  PRIMARY KEY,
    user_id            VARCHAR(26)  NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    family_id          VARCHAR(26)  NOT NULL,
    expires_at         TIMESTAMPTZ  NOT NULL,
    replaced_by_jti    VARCHAR(26)  NULL REFERENCES refresh_tokens(jti) ON DELETE SET NULL,
    created_at         TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_refresh_tokens_user_id    ON refresh_tokens(user_id);
CREATE INDEX idx_refresh_tokens_family_id  ON refresh_tokens(family_id);
CREATE INDEX idx_refresh_tokens_expires_at ON refresh_tokens(expires_at);
