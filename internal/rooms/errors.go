package rooms

import "errors"

// Business errors returned by Service. Mount maps each to an HTTP status
// (see mount.go errorToStatus).
//
// Sprint 1 keeps the set small. Sprint 2 adds ErrAnnouncementTooLong
// (D29) and ErrAlreadyOnStage / ErrNotOnStage (cross-package with
// internal/participants).
var (
	// ErrRoomNotFound: GetRoom / EndRoom when the id has no row.
	ErrRoomNotFound = errors.New("rooms: room not found")

	// ErrRoomEnded: EndRoom called on a room whose status is already 'ended'.
	ErrRoomEnded = errors.New("rooms: room has ended")

	// ErrNotHost: EndRoom called by someone other than host_user_id.
	ErrNotHost = errors.New("rooms: only the host can perform this action")
)