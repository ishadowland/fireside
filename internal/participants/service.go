// Package participants — Service (business logic) for Sprint 1 WP-4.
package participants

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/oklog/ulid/v2"

	"github.com/ishadowland/fireside/internal/messages"
	"github.com/ishadowland/fireside/internal/rooms"
	"github.com/ishadowland/fireside/internal/store"
)

// Service is the participants business-logic layer.
//
// Cross-package deps:
//   - rooms: room existence + capacity count
//   - messages: system event log (JoinRoom / LeaveRoom write a
//     sender_kind='system' message with sender_id=NULL).
type Service struct {
	q        *store.Queries
	rooms    *rooms.Service
	messages *messages.Service
	log      *slog.Logger
}

// NewService builds a Service.
func NewService(q *store.Queries, rooms *rooms.Service, messages *messages.Service, log *slog.Logger) *Service {
	if log == nil {
		log = slog.Default()
	}
	return &Service{q: q, rooms: rooms, messages: messages, log: log}
}

// JoinRoom creates a new on_stage participant row in the given room
// for the given user. Triggers a system message
// `{"event":"participant.joined","user_id":"<id>"}` so the hub (WP-5)
// can broadcast.
//
// Preconditions:
//   - room exists and status='active'
//   - room.participant_count < room.max_participants
//   - user does not already have an on_stage row in this room
//
// Returns ErrRoomNotFound / ErrRoomFull / ErrAlreadyOnStage.
func (s *Service) JoinRoom(ctx context.Context, roomID, userID string) (ParticipantView, error) {
	// 1) Room must exist and be active.
	room, _, err := s.rooms.GetRoom(ctx, roomID)
	if err != nil {
		// rooms.GetRoom returns either rooms.ErrRoomNotFound (mapped via
		// our alias) or a wrapped DB error.
		return ParticipantView{}, err
	}
	if room.Status != store.RoomStatusActive {
		return ParticipantView{}, ErrRoomNotFound
	}

	// 2) Capacity check (Q7). max_participants range is [1, 50] at the
	// DB layer; we honor that.
	count, err := s.q.CountOnStageParticipants(ctx, roomID)
	if err != nil {
		s.log.Error("JoinRoom: count failed", "room_id", roomID, "err", err)
		return ParticipantView{}, err
	}
	if int(count) >= int(room.MaxParticipants) {
		return ParticipantView{}, fmt.Errorf("%w (have %d / max %d)", ErrRoomFull, count, room.MaxParticipants)
	}

	// 3) Persist. UNIQUE (room_id, user_id) WHERE stage='on_stage'
	// blocks double-join; pgx surfaces it as a *pgconn.PgError with
	// Code="23505" unique_violation. We do not import pgconn here to
	// keep this package pgx-version-agnostic; fall back to the simpler
	// "Insert + check GetOnStageParticipant" — if we just inserted,
	// the row must exist; if the row already existed pre-call, that
	// means a stale on_stage from a prior session, which we treat as
	// ErrAlreadyOnStage.
	id := ulid.Make().String()
	row, err := s.q.JoinRoom(ctx, store.JoinRoomParams{
		ID:     id,
		RoomID: roomID,
		UserID: userID,
	})
	if err != nil {
		// Most likely a unique_violation. Check if the row already
		// exists; if so, return ErrAlreadyOnStage.
		if existing, gerr := s.q.GetOnStageParticipant(ctx, store.GetOnStageParticipantParams{
			RoomID: roomID,
			UserID: userID,
		}); gerr == nil && existing.ID != "" {
			return ParticipantView{}, ErrAlreadyOnStage
		}
		s.log.Error("JoinRoom: insert failed", "room_id", roomID, "user_id", userID, "err", err)
		return ParticipantView{}, err
	}

	// 4) System message (best-effort: log on failure but don't fail
	// the join — the user IS on stage, the event log is a side
	// effect).
	payload, _ := json.Marshal(map[string]string{
		"event":   "participant.joined",
		"user_id": userID,
	})
	if merr := s.messages.CreateSystemMessage(ctx, roomID, string(payload)); merr != nil {
		s.log.Warn("JoinRoom: system message failed (join succeeded)",
			"room_id", roomID, "user_id", userID, "err", merr)
	}

	return participantViewFromStore(row), nil
}

// LeaveRoom transitions the user's on_stage row in the room to
// off_stage, recording left_at. Triggers a system message
// `{"event":"participant.left","user_id":"<id>"}`.
//
// Returns ErrNotOnStage if the user is not currently on_stage in the
// room. ErrRoomNotFound is NOT checked (LeaveRoom is idempotent for
// ended rooms — you can leave a room that was ended while you were in it).
func (s *Service) LeaveRoom(ctx context.Context, roomID, userID string) (ParticipantView, error) {
	// 1) Persist. q.LeaveRoom returns rowsAffected; 0 means not on_stage.
	rowsAffected, err := s.q.LeaveRoom(ctx, store.LeaveRoomParams{
		RoomID: roomID,
		UserID: userID,
	})
	if err != nil {
		s.log.Error("LeaveRoom: update failed", "room_id", roomID, "user_id", userID, "err", err)
		return ParticipantView{}, err
	}
	if rowsAffected == 0 {
		return ParticipantView{}, ErrNotOnStage
	}

	// 2) Look up the off_stage row to return it (left_at is set).
	// We use ListByRoom and filter — GetOnStageParticipant would not
	// find it (it's now off_stage). For Sprint 1 we return the most
	// recent row for (room_id, user_id).
	rows, err := s.q.ListByRoom(ctx, roomID)
	if err != nil {
		s.log.Error("LeaveRoom: lookup failed", "room_id", roomID, "user_id", userID, "err", err)
		return ParticipantView{}, err
	}
	var updated store.Participant
	found := false
	for _, p := range rows {
		if p.UserID == userID {
			updated = p
			found = true
			break
		}
	}
	if !found {
		// Should never happen — we just updated a row.
		return ParticipantView{}, fmt.Errorf("post-leave row not found")
	}

	// 3) System message (best-effort).
	payload, _ := json.Marshal(map[string]string{
		"event":   "participant.left",
		"user_id": userID,
	})
	if merr := s.messages.CreateSystemMessage(ctx, roomID, string(payload)); merr != nil {
		s.log.Warn("LeaveRoom: system message failed (leave succeeded)",
			"room_id", roomID, "user_id", userID, "err", merr)
	}

	return participantViewFromStore(updated), nil
}

// ListOnStageByRoom returns all currently on_stage participants in a
// room, ordered by joined_at ASC.
func (s *Service) ListOnStageByRoom(ctx context.Context, roomID string) ([]ParticipantView, error) {
	rows, err := s.q.ListOnStageByRoom(ctx, roomID)
	if err != nil {
		s.log.Error("ListOnStageByRoom failed", "room_id", roomID, "err", err)
		return nil, err
	}
	return participantViewsFromStore(rows), nil
}

// ListOnStageByUser returns all rooms a user is currently on_stage in.
func (s *Service) ListOnStageByUser(ctx context.Context, userID string) ([]ParticipantView, error) {
	rows, err := s.q.ListOnStageByUser(ctx, userID)
	if err != nil {
		s.log.Error("ListOnStageByUser failed", "user_id", userID, "err", err)
		return nil, err
	}
	return participantViewsFromStore(rows), nil
}

// GetOnStageParticipant returns the active on_stage row for
// (roomID, userID), or ErrNotOnStage if none.
func (s *Service) GetOnStageParticipant(ctx context.Context, roomID, userID string) (ParticipantView, error) {
	row, err := s.q.GetOnStageParticipant(ctx, store.GetOnStageParticipantParams{
		RoomID: roomID,
		UserID: userID,
	})
	if errors.Is(err, sql.ErrNoRows) {
		return ParticipantView{}, ErrNotOnStage
	}
	if err != nil {
		s.log.Error("GetOnStageParticipant failed", "room_id", roomID, "user_id", userID, "err", err)
		return ParticipantView{}, err
	}
	return participantViewFromStore(row), nil
}