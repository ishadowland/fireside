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
// Concurrency (issue #19 fix):
//   - The capacity check used to be done in a single INSERT ... WHERE
//     count < max ON CONFLICT DO NOTHING. That works for SAME-user
//     double-join (ON CONFLICT blocks) but does NOT serialize
//     DISTINCT-user joins: 9 concurrent joins by different users to
//     an 8-cap room all see `count = 8 < 8 = max` (false) and all
//     insert (each ON CONFLICT only blocks their own row).
//   - This fix wraps the check + insert in a tx that first takes a
//     row-level lock on the rooms row (`SELECT ... FOR UPDATE`).
//     Postgres serializes all concurrent JoinRoom txs that try to
//     lock the same rooms.id. After the lock, the count + insert
//     run with serialized visibility.
//
// Returns ErrRoomNotFound / ErrRoomFull / ErrAlreadyOnStage.
func (s *Service) JoinRoom(ctx context.Context, roomID, userID string) (ParticipantView, error) {
	// 1) Room must exist and be active (separate query — outside the
	// tx — to short-circuit without taking a tx lock when the room
	// is missing/ended).
	room, _, err := s.rooms.GetRoom(ctx, roomID)
	if err != nil {
		return ParticipantView{}, err
	}
	if room.Status != store.RoomStatusActive {
		// Issue #26: a room that exists but is ended is not "not found".
		// Distinguish it so the mount can return 409 (matches the
		// messages package's ErrRoomEnded treatment from issue #22).
		return ParticipantView{}, ErrRoomEnded
	}

	// 2) Serialize the capacity check + insert via tx + row lock.
	id := ulid.Make().String()
	row, err := s.joinRoomSerialized(ctx, store.JoinRoomParams{
		ID:              id,
		RoomID:          roomID,
		UserID:          userID,
		MaxParticipants: room.MaxParticipants,
	})
	if err != nil {
		if errors.Is(err, store.ErrUniqueViolationRoomFull) {
			return ParticipantView{}, ErrRoomFull
		}
		if errors.Is(err, store.ErrUniqueViolationAlreadyOnStage) {
			return ParticipantView{}, ErrAlreadyOnStage
		}
		if errors.Is(err, sql.ErrNoRows) {
			// The store layer maps the atomic-INSERT no-row case to
			// ErrUniqueViolationRoomFull | ErrAlreadyOnStage via the
			// existing GetOnStageParticipant disambiguation; that path
			// should not fall through here. If it does, treat as
			// RoomFull (capacity miss) — defensive.
			return ParticipantView{}, ErrRoomFull
		}
		s.log.Error("JoinRoom: insert failed", "room_id", roomID, "user_id", userID, "err", err)
		return ParticipantView{}, err
	}

	// 3) System message (best-effort: log on failure but don't fail
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

// joinRoomSerialized wraps the capacity check + atomic INSERT in a
// tx. First it acquires a row-level lock on the rooms row
// (`SELECT ... FOR UPDATE`); the lock serializes any concurrent
// JoinRoom txs for the same room, so the count + insert see a
// consistent snapshot. Then it runs the atomic INSERT ... WHERE count
// < max ON CONFLICT DO NOTHING from store/participants.sql.
//
// Issue #19 fix: replaces the previous race-prone "atomic INSERT
// with WHERE clause" with a tx-wrapped + row-locked check. Postgres
// MVCC + READ COMMITTED isolation are sufficient — the row lock on
// rooms.id ensures serial execution; the ON CONFLICT on participants
// is now redundant but kept as a defense-in-depth (a missing row
// lock would still let ON CONFLICT block same-user double-join).
func (s *Service) joinRoomSerialized(ctx context.Context, p store.JoinRoomParams) (store.Participant, error) {
	// Begin a tx via store.Queries.BeginTx (added in issue #19
	// fix). The tx holds a row-level lock on the rooms row, which
	// serializes concurrent JoinRoom txs for the same room.
	tx, err := s.q.BeginTx(ctx, nil)
	if err != nil {
		return store.Participant{}, err
	}
	defer func() { _ = tx.Rollback() }()

	// 1) Row lock on the rooms row (serializes concurrent JoinRoom
	// txs for the same room).
	var lockedMax int32
	if err := tx.QueryRowContext(ctx,
		`SELECT max_participants FROM rooms WHERE id = $1 FOR UPDATE`,
		p.RoomID,
	).Scan(&lockedMax); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// Race: room was deleted between the GetRoom above and
			// the FOR UPDATE here. Treat as not-found.
			return store.Participant{}, rooms.ErrRoomNotFound
		}
		return store.Participant{}, err
	}
	// Trust the lock holder's view of max_participants. (The
	// earlier GetRoom saw room.Status; that's not changed by
	// concurrent tx because EndRoom takes the same row lock.)

	// 2) Count on_stage participants.
	var onStageCount int
	if err := tx.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM participants WHERE room_id = $1 AND stage_state = 'on_stage'`,
		p.RoomID,
	).Scan(&onStageCount); err != nil {
		return store.Participant{}, err
	}
	if int32(onStageCount) >= lockedMax {
		// Capacity miss. Return a typed sentinel so the caller maps
		// to ErrRoomFull. We use a unique sentinel (not
		// sql.ErrNoRows) so it's distinguishable from the
		// already-on-stage branch.
		return store.Participant{}, store.ErrUniqueViolationRoomFull
	}

	// 3) Already-on-stage check (race-tight: the partial UNIQUE
	// index `uniq_participant_room_user_active` is the source of
	// truth, but we check explicitly for a clearer error).
	var existingID string
	if err := tx.QueryRowContext(ctx,
		`SELECT id FROM participants WHERE room_id = $1 AND user_id = $2 AND stage_state = 'on_stage'`,
		p.RoomID, p.UserID,
	).Scan(&existingID); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return store.Participant{}, err
	}
	if existingID != "" {
		return store.Participant{}, store.ErrUniqueViolationAlreadyOnStage
	}

	// 4) Atomic INSERT with ON CONFLICT (defense in depth — if two
	// somehow slipped past the lock, the partial UNIQUE index still
	// blocks same-user double-join).
	var i store.Participant
	row := tx.QueryRowContext(ctx, joinRoomSQL,
		p.ID, p.RoomID, p.UserID, lockedMax,
	)
	if err := row.Scan(&i.ID, &i.RoomID, &i.UserID, &i.StageState, &i.JoinedAt, &i.LeftAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// Defensive: shouldn't happen with the row lock + count
			// check, but if the COUNT < max WHERE clause still
			// rejects, classify it.
			return store.Participant{}, store.ErrUniqueViolationRoomFull
		}
		if store.IsUniqueViolation(err) {
			// partial UNIQUE index caught a same-user double-join
			// that slipped past the explicit check.
			return store.Participant{}, store.ErrUniqueViolationAlreadyOnStage
		}
		return store.Participant{}, err
	}

	if err := tx.Commit(); err != nil {
		return store.Participant{}, err
	}
	return i, nil
}

// joinRoomSQL is the inline atomic INSERT statement. Mirrors
// db/queries/participants.sql (kept verbatim there for sqlc).
// Casts are ::VARCHAR(26) per issue #23 (migration 0007 converted
// the ID columns; CHAR(26) would pad short inputs with spaces).
const joinRoomSQL = `
INSERT INTO participants (id, room_id, user_id, stage_state, joined_at)
SELECT $1::VARCHAR(26),
       $2::VARCHAR(26),
       $3::VARCHAR(26),
       'on_stage'::stage_state,
       NOW()
WHERE (SELECT COUNT(*) FROM participants
       WHERE room_id = $2::VARCHAR(26)
         AND stage_state = 'on_stage'::stage_state) < $4::INT
ON CONFLICT (room_id, user_id) WHERE stage_state = 'on_stage' DO NOTHING
RETURNING id, room_id, user_id, stage_state, joined_at, left_at
`
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