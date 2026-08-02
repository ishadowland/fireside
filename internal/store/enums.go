// Package store: typed enum constants + helpers for the PostgreSQL enums
// used across rooms / participants / messages.
//
// sqlc v1.31 cannot model PG enums (the column overrides for enum are
// not in sqlc.yaml), so the generated structs use sql.NullString for
// enum columns. These constants + helpers let callers compare without
// scattering `.String == "active"` literals through the codebase.
//
// When Sprint 2 adds a real enum override to sqlc.yaml, these constants
// stay (they're the canonical Go-side spelling); only the struct field
// type changes to a typed string alias.
package store

import "database/sql"

// RoomStatus values (room_status enum, db/migrations/0003_rooms.up.sql).
const (
	RoomStatusActive = "active"
	RoomStatusEnded  = "ended"
)

// StageState values (stage_state enum, db/migrations/0004_participants.up.sql).
const (
	StageStateOnStage  = "on_stage"
	StageStateOffStage = "off_stage"
)

// SenderKind values (sender_kind enum, db/migrations/0005_messages.up.sql).
const (
	SenderKindHuman  = "human"
	SenderKindAgent  = "agent"
	SenderKindSystem = "system"
)

// ContentType values (content_type enum, db/migrations/0005_messages.up.sql).
const (
	ContentTypeText     = "text"
	ContentTypeSystem   = "system"
	ContentTypeImage    = "image"
	ContentTypeQuestion = "question"
	ContentTypeAnswer   = "answer"
	ContentTypeProgress = "progress"
)

// IsRoomActive reports whether a rooms.status NullString is the active state.
// Safe to call on a zero-value NullString (returns false).
func IsRoomActive(status sql.NullString) bool {
	return status.Valid && status.String == RoomStatusActive
}

// IsOnStage reports whether a participants.stage_state NullString is on_stage.
func IsOnStage(state sql.NullString) bool {
	return state.Valid && state.String == StageStateOnStage
}