// Package messages is the messages service + REST layer (Sprint 1 WP-3).
//
// Layering mirrors internal/rooms:
//
//	internal/store         — SQL queries
//	internal/messages      — business logic + DTOs (this package)
//	internal/rooms         — cross-package dep (room existence check)
//	cmd/fireside/main.go   — wires Service into Gin via messages.Mount
//
// Sprint 1 scope:
//   - human text messages (sender_kind='human', content_type='text')
//   - system events (sender_kind='system', content_type='system',
//     sender_id=NULL)
//   - cursor pagination via ?since=<id>
//   - JSONB mentions default '[]' (Sprint 1 does not validate content)
//
// Out of scope:
//   - agent-originated messages (sender_kind='agent') — Sprint 2
//   - image / question / answer / progress content types — Sprint 2
//   - reply chains — Sprint 2
//   - real-time broadcast (the hub, WP-5) — POST messages REST writes
//     to DB; broadcast is the hub's job. REST POST → DB only.
package messages

import (
	"encoding/json"
	"time"

	"github.com/ishadowland/fireside/internal/store"
)

// CreateMessageRequest is the JSON body of POST /v1/rooms/:id/messages.
//
// Content max length is enforced server-side at 8192 chars (TEXT in DB
// is unbounded; the limit prevents accidental paste of a 1MB blob).
type CreateMessageRequest struct {
	Content    string `json:"content"           binding:"required,min=1,max=8192"`
	ReplyToID  string `json:"reply_to_id,omitempty"`
}

// CreateMessageResponse is the 200 body.
type CreateMessageResponse struct {
	Message MessageView `json:"message"`
}

// ListMessagesResponse is the 200 body of GET /v1/rooms/:id/messages.
//
// `next_before` is the cursor clients should pass as ?since on the next
// page request. Empty string means no more pages.
type ListMessagesResponse struct {
	Messages   []MessageView `json:"messages"`
	NextBefore string        `json:"next_before"`
}

// MessageView is the JSON-friendly view of a messages row.
//
// `mentions` is rendered as a JSON array (e.g. `[]` or `["01HXY..."]`).
// Sprint 1: handler-side default is `[]`. Sprint 2 may switch to typed
// []string via a marshaler.
type MessageView struct {
	ID          string    `json:"id"`
	RoomID      string    `json:"room_id"`
	SenderKind  string    `json:"sender_kind"`
	SenderID    *string   `json:"sender_id"`     // null for system messages
	ContentType string    `json:"content_type"`
	Content     string    `json:"content"`
	Mentions    []string  `json:"mentions"`      // JSON array (parsed from JSONB)
	ReplyToID   *string   `json:"reply_to_id,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}

// messageViewFromStore converts a store.Message to a MessageView.
//
// Sprint 1: VARCHAR(26) IDs (post 0007 migration) — no Trim needed.
// Sprint 2 follow-up if any older rows exist: see WP-2's audit comment.
//
// Mentions (JSONB []byte from pgx) is unmarshaled into []string. Empty
// bytes / invalid JSON yields an empty slice (never nil).
func messageViewFromStore(m store.Message) MessageView {
	v := MessageView{
		ID:          m.ID,
		RoomID:      m.RoomID,
		Content:     m.Content,
		Mentions:    unmarshalMentions(m.Mentions),
		CreatedAt:   m.CreatedAt,
	}
	if m.SenderKind.Valid {
		v.SenderKind = m.SenderKind.String
	}
	if m.SenderID.Valid {
		s := m.SenderID.String
		v.SenderID = &s
	}
	if m.ContentType.Valid {
		v.ContentType = m.ContentType.String
	}
	if m.ReplyToID.Valid {
		s := m.ReplyToID.String
		v.ReplyToID = &s
	}
	return v
}

// unmarshalMentions parses a JSONB []byte into []string. Returns an
// empty (non-nil) slice if the bytes are empty or invalid JSON.
func unmarshalMentions(b []byte) []string {
	if len(b) == 0 {
		return []string{}
	}
	var out []string
	if err := json.Unmarshal(b, &out); err != nil {
		return []string{}
	}
	if out == nil {
		return []string{}
	}
	return out
}

// messageViewsFromStore converts []store.Message to []MessageView.
func messageViewsFromStore(ms []store.Message) []MessageView {
	out := make([]MessageView, 0, len(ms))
	for _, m := range ms {
		out = append(out, messageViewFromStore(m))
	}
	return out
}