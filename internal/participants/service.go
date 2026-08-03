// Package participants — Service (business logic) for Sprint 1 WP-4.
package participants

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
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
//
// Sprint 1-3 fix (issue #15 L-1/L-2): the SQL is atomic — capacity
// check + UNIQUE enforcement in a single INSERT. 9 concurrent joins
// to an 8-cap room can no longer all pass the check. ErrNoRows is
// disambiguated via GetOnStageParticipant: if a row exists it was a
// duplicate (ErrAlreadyOnStage), else it was a capacity miss
// (ErrRoomFull).
func (s *Service) JoinRoom(ctx context.Context, roomID, userID string) (ParticipantView, error) {
	// 1) Room must exist and be active.
	room, _, err := s.rooms.GetRoom(ctx, roomID)
	if err != nil {
		return ParticipantView{}, err
	}
	if room.Status != store.RoomStatusActive {
		return ParticipantView{}, ErrRoomNotFound
	}

	// 2) Atomic INSERT. Capacity + UNIQUE enforced in the SQL itself.
	id := ulid.Make().String()
	row, err := s.q.JoinRoom(ctx, store.JoinRoomParams{
		ID:              id,
		RoomID:          roomID,
		UserID:          userID,
		MaxParticipants: room.MaxParticipants,
	})
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			s.log.Error("JoinRoom: insert failed", "room_id", roomID, "user_id", userID, "err", err)
			return ParticipantView{}, err
		}
		// ErrNoRows = either capacity miss or duplicate. Disambiguate.
		if existing, gerr := s.q.GetOnStageParticipant(ctx, store.GetOnStageParticipantParams{
			RoomID: roomID,
			UserID: userID,
		}); gerr == nil && existing.ID != "" {
			return ParticipantView{}, ErrAlreadyOnStage
		}
		return ParticipantView{}, ErrRoomFull
	}

	// 3) System message (best-effort).
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
//
// Sprint 1-3 fix (issue #15 L-3): store.LeaveRoom now uses RETURNING
// to return the updated row directly — single round trip, no follow-up
// ListByRoom-and-filter pass.
func (s *Service) LeaveRoom(ctx context.Context, roomID, userID string) (ParticipantView, error) {
	row, err := s.q.LeaveRoom(ctx, store.LeaveRoomParams{
		RoomID: roomID,
		UserID: userID,
	})
	if errors.Is(err, sql.ErrNoRows) {
		return ParticipantView{}, ErrNotOnStage
	}
	if err != nil {
		s.log.Error("LeaveRoom: update failed", "room_id", roomID, "user_id", userID, "err", err)
		return ParticipantView{}, err
	}

	payload, _ := json.Marshal(map[string]string{
		"event":   "participant.left",
		"user_id": userID,
	})
	if merr := s.messages.CreateSystemMessage(ctx, roomID, string(payload)); merr != nil {
		s.log.Warn("LeaveRoom: system message failed (leave succeeded)",
			"room_id", roomID, "user_id", userID, "err", merr)
	}

	return participantViewFromStore(row), nil
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