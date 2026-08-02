-- 0006_users_display_name.up.sql
-- Sprint 1 WP-1: add display_name to users.
--
-- Per RFC Q6: dashboard login flow shows a modal to fill display_name;
-- this column is the destination. Default '' so existing stub-registered
-- users (Sprint 1-1) are not broken.
--
-- Why ALTER (not recreate): ULID migration 0002 already shipped and we
-- have a stable, populated users table shape. ALTER is the cheapest,
-- safest change. (See ADR-0014.)
--
-- Sprint 1.5 follow-up (per WP-7.10): PATCH /v1/users/me sets this.
--
-- Not unique: per design/01 §User, display_name is presentation-only and
-- two humans with the same nickname are allowed (they are distinguished
-- by their ULID id).

ALTER TABLE users
    ADD COLUMN IF NOT EXISTS display_name VARCHAR(64) NOT NULL DEFAULT '';