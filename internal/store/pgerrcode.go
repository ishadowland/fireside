// Package store — low-level helpers that are not query-generated
// (sqlc would put these in a separate file). Kept here so any
// service package can import them.
package store

import (
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
)

// IsUniqueViolation reports whether err is a Postgres unique_violation
// (SQLSTATE 23505). Use this to make "INSERT; on conflict re-fetch"
// patterns resilient to concurrent inserts of the same logical row.
//
// Added in the issue #21 fix. Shared by:
//   - internal/auth/handler.go resolveUserID (concurrent first login)
//   - internal/participants/service.go JoinRoom capacity-race fix
//     (issue #19 follow-up) — same SQLSTATE class
//
// Requires importing jackc/pgx/v5/pgconn. We isolate the dep here
// so callers don't have to.
func IsUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

// IsForeignKeyViolation reports whether err is a Postgres
// foreign_key_violation (SQLSTATE 23503). Provided for symmetry
// with IsUniqueViolation; not currently used by Sprint 1.
func IsForeignKeyViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23503"
}

// Issue #19 fix: typed sentinels for the two distinct "rejection"
// branches of JoinRoom (capacity miss vs already-on-stage). The
// tx-wrapped JoinRoom in internal/participants/service.go returns
// one of these two sentinels instead of sql.ErrNoRows + post-check,
// because the post-check is no longer atomic with the count + insert.
var (
	ErrUniqueViolationRoomFull      = errors.New("store: capacity miss")
	ErrUniqueViolationAlreadyOnStage = errors.New("store: already on_stage")
)