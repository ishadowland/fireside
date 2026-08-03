package participants

import (
	"errors"

	"github.com/ishadowland/fireside/internal/rooms"
)

// Business errors returned by Service. Mount maps each to an HTTP status.
//
// ErrRoomNotFound is an alias for rooms.ErrRoomNotFound — same canonical
// sentinel as in the messages package (see the fix in commit 448d68c for
// the rationale). This avoids the "two equal-but-different errors.Is"
// failure mode where two packages declare their own
// `errors.New("... room not found")` and `errors.Is` fails to match.
var (
	// ErrAlreadyOnStage: JoinRoom when the user already has an on_stage
	// row in this room. DB enforces via the partial UNIQUE index
	// `uniq_participant_room_user_active`.
	ErrAlreadyOnStage = errors.New("participants: user is already on_stage in this room")

	// ErrNotOnStage: LeaveRoom when the user has no on_stage row in
	// this room.
	ErrNotOnStage = errors.New("participants: user is not on_stage in this room")

	// ErrRoomFull: JoinRoom when the room has hit its max_participants
	// capacity (Q7 default 8). Reserved for WP-4 — earlier WP-2 reserved
	// the symbol but no code path returned it.
	ErrRoomFull = errors.New("participants: room is full")

	// ErrRoomNotFound: alias to rooms.ErrRoomNotFound.
	ErrRoomNotFound = rooms.ErrRoomNotFound

	// ErrRoomEnded: alias to rooms.ErrRoomEnded. JoinRoom rejects new
	// joins to a room whose status != 'active' (same sentinel as the
	// messages package; see issue #22/#26 for the 404-vs-409 story).
	ErrRoomEnded = rooms.ErrRoomEnded
)