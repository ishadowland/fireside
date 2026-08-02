-- 0001_init.up.sql
-- Sprint 0 baseline schema. Only the two tables required by ADR-0007
-- (auth_tokens for jti replay defense) and the users table stub for
-- Sprint 1's real SMS provider lookup.
--
-- INT64 column type for users.id is INTENTIONAL per ADR-0014:
--   Sprint 0 keeps int64 (matches the SUB-001/SUB-003/ANDROID handoff
--   specs). Sprint 1 migrates this column to CHAR(26) ULID when the
--   real users lookup lands. Do NOT change this column type without
--   first reading ADR-0014 and updating the Sprint 1 migration plan.
--
-- Sprint 0 does NOT touch this table — auth.LoginHandler uses
-- deriveStubUserID (fnv64 of phone) and never inserts. Sprint 1+
-- inserts real users.

CREATE TABLE users (
    id         BIGINT       PRIMARY KEY,
    phone      VARCHAR(32)  NOT NULL,
    created_at TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX idx_user_phone ON users(phone);

-- auth_tokens: server tracks recently-seen JWT jti claims for replay
-- defense (per ADR-0007 §"Risks" → "Replay"). Sprint 0 stub does NOT
-- insert into this table either; the column type for user_id is BIGINT
-- for the same reason users.id is BIGINT (ADR-0014).
--
-- The expires_at index lets the server run a periodic cleanup query:
--   DELETE FROM auth_tokens WHERE expires_at < NOW();
-- (Sprint 1+ will add a cleanup cron / goroutine.)

CREATE TABLE auth_tokens (
    jti        UUID         PRIMARY KEY,
    user_id    BIGINT       NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    expires_at TIMESTAMPTZ  NOT NULL,
    created_at TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_auth_tokens_expires_at ON auth_tokens(expires_at);
CREATE INDEX idx_auth_tokens_user_id    ON auth_tokens(user_id);