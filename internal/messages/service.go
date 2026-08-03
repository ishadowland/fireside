// Package messages — Service (business logic) for Sprint 1 WP-3.
package messages

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"

	"github.com/oklog/ulid/v2"

	"github.com/ishadowland/fireside/internal/rooms"
	"github.com/ishadowland/fireside/internal/store"
)

// DefaultPageSize is the cap on a single GET /v1/rooms/:id/messages page.
const DefaultPageSize int32 = 50

// MaxPageSize is the absolute ceiling (?limit cannot exceed).
const MaxPageSize int32 = 200

// Service is the messages business-logic layer.
//
// Sprint 1: CreateMessage writes to DB only. Real-time broadcast
// (WP-5 hub) is wired separately: the handler can call cfg.Hub if
// non-nil, or the hub may poll a stream. For now this Service is
// storage-only.
type Service struct {
	q       *store.Queries
	rooms   *rooms.Service
	log     *slog.Logger
}

// NewService builds a Service. q is the sqlc-generated queries handle;
// rooms is the rooms Service (used for room existence + on_stage checks);
// log is structured logger (may be nil → uses slog.Default()).
func NewService(q *store.Queries, rooms *rooms.Service, log *slog.Logger) *Service {
	if log == nil {
		log = slog.Default()
	}
	return &Service{q: q, rooms: rooms, log: log}
}

// CreateMessage persists a new human text message.
//
// Sprint 1 preconditions:
//   - room must exist and be active
//   - actorUserID must currently be on_stage in the room
//   - content must be non-empty (handler binding should have caught this)
//
// On success returns the new MessageView (with ID generated server-side).
//
// Returns ErrRoomNotFound / ErrRoomEnded / ErrNotOnStage / ErrInvalidArg.
// ErrRoomEnded vs ErrRoomNotFound: a room that exists but is ended
// is *conflict* (409) not *not-found* (404). The split was added in
// the fix for issue #22 — prior to that, ended rooms were reported
// as 404, which (a) was wrong, and (b) made the WS dispatch's
// rooms.ErrRoomEnded branch dead code.
func (s *Service) CreateMessage(ctx context.Context, actorUserID, roomID string, req CreateMessageRequest) (MessageView, error) {
	// 1) Room must exist.
	room, parts, err := s.rooms.GetRoom(ctx, roomID)
	if errors.Is(err, rooms.ErrRoomNotFound) {
		return MessageView{}, ErrRoomNotFound
	}
	if err != nil {
		s.log.Error("CreateMessage: room lookup failed", "room_id", roomID, "err", err)
		return MessageView{}, err
	}
	// 1a) Room must be active. Returned as ErrRoomEnded so the
	// REST mount maps to 409 and the WS dispatch maps to
	// CodeRoomEnded. Previously this branch returned ErrRoomNotFound,
	// which collapsed two distinct conditions.
	if room.Status != store.RoomStatusActive {
		return MessageView{}, ErrRoomEnded
	}

	// 2) Actor must be on_stage.
	onStage := false
	for _, p := range parts {
		if p.UserID == actorUserID && p.StageState == store.StageStateOnStage {
			onStage = true
			break
		}
	}
	if !onStage {
		return MessageView{}, ErrNotOnStage
	}

	// 3) Validate content.
	if req.Content == "" {
		return MessageView{}, fmt.Errorf("%w: content required", ErrInvalidArg)
	}
	// (handler binding already enforced min=1,max=8192)

	// 4) Persist. mentions default '[]' (JSONB column default).
	mentions := []byte("[]")
	var replyToID sql.NullString
	if req.ReplyToID != "" {
		// Sprint 1: trust caller; Sprint 2 may validate reply_to_id
		// exists in the same room.
		replyToID = sql.NullString{String: req.ReplyToID, Valid: true}
	}

	id := ulid.Make().String()
	msg, err := s.q.CreateMessage(ctx, store.CreateMessageParams{
		ID:          id,
		RoomID:      roomID,
		SenderKind:  sql.NullString{String: store.SenderKindHuman, Valid: true},
		SenderID:    sql.NullString{String: actorUserID, Valid: true},
		ContentType: sql.NullString{String: store.ContentTypeText, Valid: true},
		Content:     req.Content,
		Mentions:    mentions,
		ReplyToID:   replyToID,
	})
	if err != nil {
		s.log.Error("CreateMessage: store insert failed", "room_id", roomID, "actor", actorUserID, "err", err)
		return MessageView{}, err
	}
	return messageViewFromStore(msg), nil
}

// CreateSystemMessage is a package-private helper used by other internal
// services (e.g. participants service writes "user joined" events
// during JoinRoom). NOT exposed to handlers.
//
// Returns ErrRoomNotFound / ErrRoomEnded / ErrInvalidArg. See
// CreateMessage for the distinction; CreateSystemMessage uses the
// same pair.
func (s *Service) CreateSystemMessage(ctx context.Context, roomID string, content string) error {
	// Validate room existence (no on_stage check for system messages).
	room, _, err := s.rooms.GetRoom(ctx, roomID)
	if errors.Is(err, rooms.ErrRoomNotFound) {
		return ErrRoomNotFound
	}
	if err != nil {
		return err
	}
	if room.Status != store.RoomStatusActive {
		return ErrRoomEnded
	}
	if content == "" {
		return fmt.Errorf("%w: content required", ErrInvalidArg)
	}

	// content is expected to be a JSON object string e.g.
	// `{"event":"participant.joined","user_id":"01HXY..."}`.
	// We don't parse-validate it; just store as-is.
	_, err = s.q.CreateMessage(ctx, store.CreateMessageParams{
		ID:          ulid.Make().String(),
		RoomID:      roomID,
		SenderKind:  sql.NullString{String: store.SenderKindSystem, Valid: true},
		SenderID:    sql.NullString{Valid: false}, // MUST be NULL for system per CHECK
		ContentType: sql.NullString{String: store.ContentTypeSystem, Valid: true},
		Content:     content,
		Mentions:    []byte("[]"),
		ReplyToID:   sql.NullString{Valid: false},
	})
	if err != nil {
		s.log.Error("CreateSystemMessage failed", "room_id", roomID, "err", err)
		return err
	}
	return nil
}

// ListMessagesByRoom returns up to limit messages in a room, newest
// first. If since (a message id cursor) is non-empty, only messages
// with id < since are returned — this gives stable cursor pagination.
//
// roomID must exist (active OR ended; both are visible in Sprint 1
// per Q3 / Q4 — no message cleanup yet).
//
// Returns the page slice + the id of the last message (caller passes
// this as `next_before` to the client). If the page is empty,
// next_before is "".
func (s *Service) ListMessagesByRoom(ctx context.Context, roomID, sinceID string, limit int32) ([]MessageView, string, error) {
	if limit <= 0 {
		limit = DefaultPageSize
	}
	if limit > MaxPageSize {
		limit = MaxPageSize
	}
	// Verify the room exists before the messages query so an unknown room
	// surfaces as ErrRoomNotFound (404) instead of an empty list
	// (which would mask a typo or a deleted room as "no messages").
	if _, _, err := s.rooms.GetRoom(ctx, roomID); err != nil {
		return nil, "", err
	}
	var since sql.NullString
	if sinceID != "" {
		since = sql.NullString{String: sinceID, Valid: true}
	}

	rows, err := s.q.ListMessagesByRoom(ctx, store.ListMessagesByRoomParams{
		RoomID: roomID,
		Before: since,
		Limit:  limit,
	})
	if err != nil {
		s.log.Error("ListMessagesByRoom failed", "room_id", roomID, "err", err)
		return nil, "", err
	}
	// Cursor semantics: if we got a full page, there might be more;
	// if we got less than limit, we're at the end.
	if len(rows) == 0 {
		return messageViewsFromStore(rows), "", nil
	}
	if int32(len(rows)) < limit {
		// Partial page → no more results. Empty cursor.
		return messageViewsFromStore(rows), "", nil
	}
	// Full page → emit cursor pointing at the oldest id on this page;
	// the next call will use "id < cursor" to fetch older rows.
	last := rows[len(rows)-1].ID
	return messageViewsFromStore(rows), last, nil
}

// GetMessage returns a single message by id.
func (s *Service) GetMessage(ctx context.Context, messageID string) (MessageView, error) {
	row, err := s.q.GetMessage(ctx, messageID)
	if errors.Is(err, sql.ErrNoRows) {
		return MessageView{}, ErrMessageNotFound
	}
	if err != nil {
		s.log.Error("GetMessage failed", "message_id", messageID, "err", err)
		return MessageView{}, err
	}
	return messageViewFromStore(row), nil
}
