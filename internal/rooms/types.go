// Package rooms is the rooms service + REST layer (Sprint 1 WP-2).
//
// Layering:
//
//	internal/store         — SQL queries (auto/hand generated)
//	internal/rooms         — business logic + DTOs (this package)
//	cmd/fireside/main.go   — wires Service into Gin via rooms.Mount
//
// The package keeps the SQL row struct (store.Room) and the JSON DTO
// (RoomView) separate so internal columns can evolve without breaking
// the API surface. Sprint 1 only exposes status / announcement via
// read paths — the PATCH /v1/rooms/:id/announcement endpoint (D29) is
// Sprint 2 work.
package rooms

import (
	"time"

	"github.com/ishadowland/fireside/internal/store"
)

// CreateRoomRequest is the JSON body of POST /v1/rooms (RFC §4 WP-7.1).
type CreateRoomRequest struct {
	Name              string `json:"name"               binding:"required,min=1,max=128"`
	MaxParticipants   int32  `json:"max_participants"   binding:"required,min=1,max=50"`
	KeepMessagesOnEnd bool   `json:"keep_messages_on_end"`
}

// CreateRoomResponse is the 200 body of POST /v1/rooms.
type CreateRoomResponse struct {
	Room RoomView `json:"room"`
}

// ListActiveResponse is the 200 body of GET /v1/rooms.
type ListActiveResponse struct {
	Rooms []RoomView `json:"rooms"`
}

// GetRoomResponse is the 200 body of GET /v1/rooms/:id.
type GetRoomResponse struct {
	Room         RoomView          `json:"room"`
	Participants []ParticipantView `json:"participants"`
}

// EndRoomResponse is the 200 body of POST /v1/rooms/:id/end.
type EndRoomResponse struct {
	RoomID string `json:"room_id"`
	Status string `json:"status"`
}

// RoomView is the JSON-friendly view of a Room row.
//
// Status is a plain string ("active" / "ended") for client readability —
// the underlying store.Room stores it as sql.NullString since sqlc v1.31
// cannot model PG enums directly.
//
// Announcement may be empty string. The 500-char limit is enforced by
// the DB CHECK constraint; the handler does not validate client-side.
type RoomView struct {
	ID                string     `json:"id"`
	HostUserID        string     `json:"host_user_id"`
	Name              string     `json:"name"`
	MaxParticipants   int32      `json:"max_participants"`
	KeepMessagesOnEnd bool       `json:"keep_messages_on_end"`
	Status            string     `json:"status"`
	Announcement      string     `json:"announcement"`
	CreatedAt         time.Time  `json:"created_at"`
	EndedAt           *time.Time `json:"ended_at,omitempty"`
}

// ParticipantView is the JSON-friendly view of a Participant row.
type ParticipantView struct {
	ID         string     `json:"id"`
	RoomID     string     `json:"room_id"`
	UserID     string     `json:"user_id"`
	StageState string     `json:"stage_state"`
	JoinedAt   time.Time  `json:"joined_at"`
	LeftAt     *time.Time `json:"left_at,omitempty"`
}

// viewFromStore converts a store.Room to a RoomView.
//
// status: store stores it as sql.NullString. We always Valid (DB column
// is NOT NULL with default 'active'), so a zero value indicates a
// programming error; we still defensively render as "" rather than
// panicking.
func viewFromStore(r store.Room) RoomView {
	v := RoomView{
		ID:                r.ID,
		HostUserID:        r.HostUserID,
		Name:              r.Name,
		MaxParticipants:   r.MaxParticipants,
		KeepMessagesOnEnd: r.KeepMessagesOnEnd,
		Announcement:      r.Announcement,
		CreatedAt:         r.CreatedAt,
	}
	if r.Status.Valid {
		v.Status = r.Status.String
	}
	if r.EndedAt.Valid {
		t := r.EndedAt.Time
		v.EndedAt = &t
	}
	return v
}

// participantViewsFromStore converts []store.Participant to []ParticipantView.
func participantViewsFromStore(ps []store.Participant) []ParticipantView {
	out := make([]ParticipantView, 0, len(ps))
	for _, p := range ps {
		v := ParticipantView{
			ID:       p.ID,
			RoomID:   p.RoomID,
			UserID:   p.UserID,
			JoinedAt: p.JoinedAt,
		}
		if p.StageState.Valid {
			v.StageState = p.StageState.String
		}
		if p.LeftAt.Valid {
			t := p.LeftAt.Time
			v.LeftAt = &t
		}
		out = append(out, v)
	}
	return out
}