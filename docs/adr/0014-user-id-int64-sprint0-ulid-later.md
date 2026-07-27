# ADR-0014: `user_id` is `int64` in Sprint 0, migrates to ULID at Sprint 1

- **Status**: Accepted
- **Date**: 2026-07-27
- **Source**: docs/design/01-data-model.md, docs/design/03-protocol.md, docs/handoff/sprint0/

## Context

The locked design doc `docs/design/03-protocol.md` shows `auth.welcome.user_id` as a ULID string (`"01HXY..."`), and `docs/design/01-data-model.md` references `User.ID` as a string (`FK User.ID` throughout the schema). However, the Sprint 0 handoff specs (`docs/handoff/sprint0/SUB-001-internal-auth.md`, `SUB-003-internal-ws.md`) declare `Claims.UserID int64` and `AuthWelcome.UserID int64` — chosen because:

1. Sprint 0 stub `deriveStubUserID` uses `fnv64(phone)` to deterministically fabricate IDs from E.164 phone numbers; ULIDs would require a generation step the stub doesn't need.
2. The stub user_id has no production meaning — Sprint 1 will swap it for a real DB lookup that returns a ULID.
3. Keeping `int64` avoids pulling a `github.com/oklog/ulid` dependency into Sprint 0's "wiring exercise" scope.

This ADR records the Sprint 0 → Sprint 1 transition plan so the int64 typing in Sprint 0 code is not mistaken for a permanent design choice.

## Decision

**Sprint 0 uses `int64` for `user_id`** — in `auth.Claims.UserID`, `ws.AuthWelcome.UserID`, Android `WsEvent.Welcome.userId: Long`, and (when landed) `users.id BIGINT`.

**Sprint 1 migrates to ULID** (`string` / `github.com/oklog/ulid`):

- Schema: `users.id` becomes `CHAR(26)` storing ULID strings (zero-padded or lowercase canonical form).
- Go: `Claims.UserID` becomes `ulid.ULID` (or `string` if sqlc codegen is easier).
- Wire format: `auth.welcome.user_id` becomes a JSON string per `docs/design/03-protocol.md`.
- Android: `WsEvent.Welcome.userId: String`.
- Migration script: convert existing rows via `users.id::text` → `lpad(..., 26, '0')` (assuming any rows exist; Sprint 0 is dev-only so the migration is effectively `DROP TABLE users; CREATE TABLE users (id CHAR(26) PRIMARY KEY, ...)`).

The Sprint 1 trigger is the **first time a real `users` table is created** (the Sprint 0 stub uses `deriveStubUserID` in-memory and never touches the table). At that moment the table is created with the ULID schema; no in-place column type change is needed.

## Alternatives Considered

- **Use ULID from day 1**: rejected — Sprint 0 is a "wiring exercise" with stub user IDs; introducing ULID generation, sqlc UUID/ULID overrides, and Android string parsing adds 20–30 minutes of pure setup with zero functional benefit (no real users exist in Sprint 0).
- **Use UUID (`uuid.UUID`)** instead of ULID: rejected — `docs/design/03-protocol.md` locks ULID-style IDs (`01HXY...`), and UUIDs are 36 chars vs ULID's 26 chars, making URLs/IDs unnecessarily long.
- **Skip the ADR and let Sprint 1 deal with it**: rejected — Sprint 0 code is committed and read by other agents; without an explicit ADR-0014, future-you (or a Sprint 1 agent) might assume `int64` was deliberate and bolt it into the schema.

## Consequences

### Positive
- Sprint 0 ships faster — no ULID lib, no string parsing, no sqlc UUID override.
- Determinism: `fnv64(phone)` gives the same int64 every time, so re-login returns the same ID and tests are reproducible.
- Clear migration point — Sprint 1's "create real `users` table" is the natural trigger.

### Negative
- `int64` lives in 3 places (Go server × 2, Android client) and must be touched in 3 files when migrating to ULID.
- The Sprint 0 stub user IDs collide easily (fnv64 is a non-cryptographic hash). Acceptable in dev; would be unsafe in any pre-prod environment.
- The handoff specs and the design doc disagree. ADR-0014 resolves this in favor of "design doc is the truth, handoff specs are Sprint 0 shortcuts".

### Risks
- **Silent divergence**: a future contributor reading `int64` in `auth.Claims` might think it's the locked choice. Mitigation: this ADR is referenced from the type definition (a doc comment in `internal/auth/jwt.go`) so the next reader sees ADR-0014 first.
- **Sprint 1 forgets to migrate**: if Sprint 1 lands without checking this ADR, `int64` leaks into production. Mitigation: `STATUS.md` Sprint 1 entry must cite ADR-0014 explicitly.
- **Android backward compat**: if the Android APK from Sprint 0 is reused at Sprint 1 with `userId: Long`, the deserializer will crash on the new string field. Mitigation: Sprint 1 ships Android + server in the same release; there's no in-the-wild Sprint 0 APK to support.

## Related

- ADR-0007 — WS auth first-frame (JWT carries `uid` claim)
- docs/design/01-data-model.md §users (final ID type)
- docs/design/03-protocol.md §auth.welcome frame (final wire type)
- docs/handoff/sprint0/SUB-001-internal-auth.md §Interface contract (Sprint 0 int64)
- docs/handoff/sprint0/SUB-003-internal-ws.md §Interface contract (Sprint 0 int64)
- docs/handoff/sprint0/SUB-ANDROID-connect-activity.md §Interface contract (Sprint 0 Long)