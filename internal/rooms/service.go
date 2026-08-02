// Package rooms — Service (business logic) for Sprint 1 WP-2.
package rooms

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/oklog/ulid/v2"

	"github.com/ishadowland/fireside/internal/store"
)

// DefaultListLimit is the cap on GET /v1/rooms when no ?limit is given.
const DefaultListLimit int32 = 50

// MaxListLimit is the absolute ceiling (?limit cannot exceed).
const MaxListLimit int32 = 200

// Service is the rooms business-logic layer. It owns the *store.Queries
// handle and exposes only domain operations (Create / Get / List / End).
//
// All methods accept a context.Context for cancellation / tracing and
// return either a typed value (Room / Participant) or a package error
// (ErrRoomNotFound, ErrNotHost, ErrRoomEnded). SQL-layer errors from
// pgx are surfaced as-is for the handler to log.
type Service struct {
	q   *store.Queries
	log *slog.Logger
}

// NewService builds a Service. q is the sqlc-generated queries handle;
// log is the structured logger (handlers may use it for request-scoped
// logs, the Service uses it for unexpected errors).
func NewService(q *store.Queries, log *slog.Logger) *Service {
	if log == nil {
		log = slog.Default()
	}
	return &Service{q: q, log: log}
}

// CreateRoom inserts a new room row. hostUserID is taken from the
// authenticated context (auth.Middleware put it there).
//
// hostUserID is Trimmed defensively for the CHAR(26) trailing-space
// behavior (see types.go doc on viewFromStore).
//
// Sprint 1: status defaults to 'active' in DB; announcement defaults
// to ''. No capacity check at the room level (Sprint 2 adds per-host
// cap). Returns ErrRoomNotFound never — CreateRoom can't fail with
// "not found".
func (s *Service) CreateRoom(ctx context.Context, hostUserID string, req CreateRoomRequest) (RoomView, error) {
	hostUserID = strings.TrimSpace(hostUserID)
	if req.Name == "" {
		return RoomView{}, fmt.Errorf("name required")
	}
	if req.MaxParticipants < 1 || req.MaxParticipants > 50 {
		return RoomView{}, fmt.Errorf("max_participants out of range [1, 50]")
	}

	room, err := s.q.CreateRoom(ctx, store.CreateRoomParams{
		ID:                ulid.Make().String(),
		HostUserID:        hostUserID,
		Name:              req.Name,
		MaxParticipants:   req.MaxParticipants,
		KeepMessagesOnEnd: req.KeepMessagesOnEnd,
		// Status / Announcement / CreatedAt / EndedAt: use DB defaults
	})
	if err != nil {
		s.log.Error("CreateRoom failed", "host_user_id", hostUserID, "err", err)
		return RoomView{}, err
	}
	return viewFromStore(room), nil
}

// GetRoom returns a room + current on_stage participants.
//
// Returns ErrRoomNotFound if the id doesn't exist. Participants slice
// is empty (not nil) when the room has no one on stage yet.
//
// roomID is Trimmed for the CHAR(26) trailing-space behavior.
func (s *Service) GetRoom(ctx context.Context, roomID string) (RoomView, []ParticipantView, error) {
	roomID = strings.TrimSpace(roomID)
	room, err := s.q.GetRoom(ctx, roomID)
	if errors.Is(err, sql.ErrNoRows) {
		return RoomView{}, nil, ErrRoomNotFound
	}
	if err != nil {
		s.log.Error("GetRoom failed", "room_id", roomID, "err", err)
		return RoomView{}, nil, err
	}

	parts, err := s.q.ListOnStageByRoom(ctx, roomID)
	if err != nil {
		s.log.Error("GetRoom participants failed", "room_id", roomID, "err", err)
		return RoomView{}, nil, err
	}
	return viewFromStore(room), participantViewsFromStore(parts), nil
}

// ListActive returns up to limit active rooms (status='active'), newest
// first. limit is clamped to [1, MaxListLimit]; 0 means DefaultListLimit.
func (s *Service) ListActive(ctx context.Context, limit int32) ([]RoomView, error) {
	if limit <= 0 {
		limit = DefaultListLimit
	}
	if limit > MaxListLimit {
		limit = MaxListLimit
	}
	rows, err := s.q.ListActiveRooms(ctx, limit)
	if err != nil {
		s.log.Error("ListActive failed", "err", err)
		return nil, err
	}
	out := make([]RoomView, 0, len(rows))
	for _, r := range rows {
		out = append(out, viewFromStore(r))
	}
	return out, nil
}

// EndRoom marks the room as ended. Only the host may call this.
//
// actorUserID and roomID are Trimmed for the CHAR(26) trailing-space
// behavior.
//
// Returns:
//   - ErrRoomNotFound  : id has no row
//   - ErrNotHost       : actorUserID != room.host_user_id
//   - ErrRoomEnded     : already ended (status check)
//   - other (DB err)   : surfaced for handler logging
func (s *Service) EndRoom(ctx context.Context, actorUserID, roomID string) error {
	actorUserID = strings.TrimSpace(actorUserID)
	roomID = strings.TrimSpace(roomID)
	room, err := s.q.GetRoom(ctx, roomID)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrRoomNotFound
	}
	if err != nil {
		s.log.Error("EndRoom lookup failed", "room_id", roomID, "err", err)
		return err
	}
	if strings.TrimSpace(room.HostUserID) != actorUserID {
		return ErrNotHost
	}
	if !store.IsRoomActive(room.Status) {
		return ErrRoomEnded
	}
	rowsAffected, err := s.q.EndRoom(ctx, roomID)
	if err != nil {
		s.log.Error("EndRoom update failed", "room_id", roomID, "err", err)
		return err
	}
	if rowsAffected == 0 {
		// Race: room transitioned between GetRoom and EndRoom. Treat as
		// already-ended since that's the only state that can transition
		// out of active.
		return ErrRoomEnded
	}
	return nil
}