// Package participants is the participants service + REST layer
// (Sprint 1 WP-4).
//
// Layering mirrors internal/rooms and internal/messages:
//
//	internal/store         — SQL queries
//	internal/participants  — business logic + DTOs (this package)
//	internal/rooms         — cross-package dep (room existence, capacity)
//	internal/messages      — cross-package dep (system event log)
//	cmd/fireside/main.go   — wires Service into Gin via participants.Mount
//
// Sprint 1 scope:
//   - JoinRoom: create on_stage participant + system message
//   - LeaveRoom: transition to off_stage + system message
//   - ListOnStageByRoom / ListOnStageByUser / GetOnStageParticipant
//   - capacity enforcement (Q7: max 8 per room)
//   - UNIQUE (room_id, user_id) WHERE stage='on_stage' — DB enforces
//
// Out of scope:
//   - RaiseHand (Q5 deferral — no lobby approval flow)
//   - kick / transfer / cooldown (Sprint 2+ per docs/design/04 §"待办")
//   - real-time broadcast on join/leave (WP-5 hub will read message
//     table or subscribe to a stream; we just write the system message
//     here).
package participants

import (
	"time"

	"github.com/ishadowland/fireside/internal/store"
)

// ParticipantView is the JSON-friendly view of a participants row.
type ParticipantView struct {
	ID         string     `json:"id"`
	RoomID     string     `json:"room_id"`
	UserID     string     `json:"user_id"`
	StageState string     `json:"stage_state"`
	JoinedAt   time.Time  `json:"joined_at"`
	LeftAt     *time.Time `json:"left_at,omitempty"`
}

// JoinResponse is the 200 body of POST /v1/rooms/:id/join.
type JoinResponse struct {
	Participant ParticipantView `json:"participant"`
}

// LeaveResponse is the 200 body of POST /v1/rooms/:id/leave.
type LeaveResponse struct {
	Participant ParticipantView `json:"participant"`
}

// participantViewFromStore converts a store.Participant to a
// ParticipantView. VARCHAR(26) IDs (post 0007 migration) need no Trim.
func participantViewFromStore(p store.Participant) ParticipantView {
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
	return v
}

// participantViewsFromStore converts []store.Participant to
// []ParticipantView.
func participantViewsFromStore(ps []store.Participant) []ParticipantView {
	out := make([]ParticipantView, 0, len(ps))
	for _, p := range ps {
		out = append(out, participantViewFromStore(p))
	}
	return out
}