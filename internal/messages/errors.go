package messages

import (
	"errors"

	"github.com/ishadowland/fireside/internal/rooms"
)

// Business errors returned by Service. Mount maps each to an HTTP status.
//
// ErrRoomNotFound is an alias for rooms.ErrRoomNotFound — both packages
// surface the same underlying sentinel so a handler using
// `errors.Is(err, rooms.ErrRoomNotFound)` (or `messages.ErrRoomNotFound`,
// which is the same value) matches errors returned from either package.
// This is preferable to wrapping because:
//   - one canonical sentinel reduces risk of "two equal-but-different"
//     errors.Is failures (the original WP-3 bug fixed by this comment).
//   - messages.Service genuinely has nothing extra to add to the
//     rooms.ErrRoomNotFound message.
//
// Sprint 2 may add ErrReplyTargetNotFound (when reply_to_id points at
// a non-existent message) and possibly ErrRateLimited.
var (
	// ErrMessageNotFound: GetMessage on a non-existent id.
	ErrMessageNotFound = errors.New("messages: message not found")

	// ErrRoomNotFound: alias to rooms.ErrRoomNotFound. See package doc.
	ErrRoomNotFound = rooms.ErrRoomNotFound

	// ErrRoomEnded: room exists in the DB but status='ended'. Alias
	// to rooms.ErrRoomEnded. Sprint 1's CreateMessage previously
	// conflated this with ErrRoomNotFound (returning 404 for both),
	// which made the WS dispatch's rooms.ErrRoomEnded switch arm dead
	// code (issue #22). Both packages now share the canonical
	// sentinel so a handler can match on either side. REST maps to
	// 409, WS to CodeRoomEnded.
	ErrRoomEnded = rooms.ErrRoomEnded

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