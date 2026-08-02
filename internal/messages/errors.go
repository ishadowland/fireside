package messages

import "errors"

// Business errors returned by Service. Mount maps each to an HTTP status.
//
// Cross-package errors:
//   - rooms.ErrRoomNotFound is re-exported as messages.ErrRoomNotFound
//     so handlers only need to import one package. The mapping is done
//     at Mount level via errors.Is + room lookup.
//
// Sprint 1 keeps the set small. Sprint 2 adds ErrReplyTargetNotFound
// (when reply_to_id points at a non-existent message) and possibly
// ErrRateLimited if a token-bucket lands.
var (
	// ErrMessageNotFound: GetMessage on a non-existent id.
	ErrMessageNotFound = errors.New("messages: message not found")

	// ErrRoomNotFound: target room doesn't exist or is ended.
	// Wrapped from internal/rooms.ErrRoomNotFound; see service.go.
	ErrRoomNotFound = errors.New("messages: room not found")

	// ErrNotOnStage: caller (sender_kind='human') is not currently
	// on_stage in the target room. Sprint 1 returns this even though
	// RFC §5 §8 documents that "anyone can post once subscribed"; we
	// tighten to on_stage-only because the hub broadcast (WP-5) only
	// reaches on_stage peers — off_stage participants wouldn't see
	// the message anyway. Sprint 2 may revisit when WP-5 ships.
	ErrNotOnStage = errors.New("messages: caller is not on_stage in this room")

	// ErrInvalidArg: validation failures that the handler / DTO
	// binding should have caught earlier. Returned only if the service
	// is called directly with bad input (e.g. empty content after
	// binding bypass).
	ErrInvalidArg = errors.New("messages: invalid argument")
)