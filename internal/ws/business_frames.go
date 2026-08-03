// Sprint 1 WP-6: business frame types (after auth.welcome).
//
// Client → Server:
//   - room.subscribe    — register conn for a room
//   - room.unsubscribe  — unregister conn from a room
//   - msg.send          — send a human message (must be on_stage)
//   - heartbeat         — optional explicit ping (Sprint 1 ignores)
//
// Server → Client (broadcast):
//   - msg.created       — every conn in the room (except sender)
//                         receives this; also sent to sender as ack
//                         (so they can confirm the message is in
//                         the timeline)
//   - room.ended        — sent when the room is ended by the host
//
// Server → Client (ack / error):
//   - room.subscribed   — ack of room.subscribe
//   - room.unsubscribed — ack of room.unsubscribe
//   - error             — protocol-level errors (see Code* constants
//                         in protocol.go + the business-specific
//                         CodeRoom* constants below)
//
// Sprint 1 simplification: msg.created is broadcast to EVERY conn
// in the room, including sender (sender doesn't need it echoed
// separately; Sprint 1 has no off-band ack channel).

package ws

// Frame types for business frames (after auth.welcome).
const (
	// Client → Server
	FrameTypeRoomSubscribe   = "room.subscribe"
	FrameTypeRoomUnsubscribe = "room.unsubscribe"
	FrameTypeMsgSend         = "msg.send"
	FrameTypeHeartbeat       = "heartbeat"

	// Server → Client (broadcast)
	FrameTypeMsgCreated = "msg.created"
	FrameTypeRoomEnded  = "room.ended"

	// Server → Client (ack)
	FrameTypeRoomSubscribed   = "room.subscribed"
	FrameTypeRoomUnsubscribed = "room.unsubscribed"
)

// Error codes for business-frame errors. Codes in protocol.go are
// for the auth handshake; these are for the dispatch loop.
const (
	CodeRoomNotFound   = "room_not_found"
	CodeRoomEnded      = "room_ended"
	CodeNotOnStage     = "not_on_stage"
	CodeAlreadyOnStage = "already_on_stage"
	CodeNotSubscribed  = "not_subscribed"
	CodeBadRequest     = "bad_request"
)

// RoomSubscribe is a client frame asking to subscribe the conn to a room.
type RoomSubscribe struct {
	Type   string `json:"type"`   // must equal FrameTypeRoomSubscribe
	RoomID string `json:"room_id"`
}

// RoomUnsubscribe is a client frame asking to unsubscribe.
type RoomUnsubscribe struct {
	Type   string `json:"type"`   // must equal FrameTypeRoomUnsubscribe
	RoomID string `json:"room_id"`
}

// MsgSend is a client frame asking the server to persist + broadcast
// a message. Sender must be on_stage in the room.
type MsgSend struct {
	Type   string `json:"type"`   // must equal FrameTypeMsgSend
	RoomID string `json:"room_id"`
	// Content is the raw text. Sprint 1: required, length-bounded by
	// the service layer (currently unbounded; Sprint 2 may add
	// MAX_CONTENT_BYTES).
	Content string `json:"content"`
}

// MsgSendAck is the server-side response to msg.send, sent ONLY to
// the sender (not broadcast). It contains the canonical MessageView
// (with ULID, server-assigned created_at, etc.) so the client can
// render the message without waiting for msg.created.
//
// Sprint 1: we send this AS the msg.created broadcast, not
// separately. So MsgSendAck is not currently used (kept as a Sprint 2
// hook if we need off-band ack).
type MsgSendAck struct {
	Type    string `json:"type"`     // FrameTypeMsgCreated
	Message any     `json:"message"` // messages.MessageView (intentionally `any` to avoid an import cycle; cast at the call site)
}

// MsgCreated is the broadcast frame sent to every conn in the room
// (including sender).
//
// Payload shape:
//   - type: "msg.created"
//   - message: messages.MessageView
//
// Why broadcast to sender too: simpler client logic (every message
// they receive appears in the timeline). If Sprint 2 adds optimistic
// UI, we can revisit and send only to non-senders.
type MsgCreated struct {
	Type    string `json:"type"`    // FrameTypeMsgCreated
	Message any    `json:"message"` // messages.MessageView
}

// RoomSubscribed is the ack to a successful room.subscribe.
type RoomSubscribed struct {
	Type       string `json:"type"`        // FrameTypeRoomSubscribed
	RoomID     string `json:"room_id"`
	ConnID     string `json:"conn_id"`     // hub.ConnID(conn) — stable per-conn ID for client-side correlation
	ServerTime int64  `json:"server_time"` // unix seconds
}

// RoomUnsubscribed is the ack to a successful room.unsubscribe.
type RoomUnsubscribed struct {
	Type   string `json:"type"`    // FrameTypeRoomUnsubscribed
	RoomID string `json:"room_id"`
}

// RoomEnded is broadcast when a room is ended by its host. Sent
// AFTER the DB has been updated.
type RoomEnded struct {
	Type       string `json:"type"`        // FrameTypeRoomEnded
	RoomID     string `json:"room_id"`
	EndedBy    string `json:"ended_by"`     // ULID of the host
	ServerTime int64  `json:"server_time"` // unix seconds
}

// BusinessError is the per-frame error response.
type BusinessError struct {
	Type    string `json:"type"`    // "error"
	Code    string `json:"code"`    // see Code* constants
	Message string `json:"message"` // human-readable
	RoomID  string `json:"room_id,omitempty"`
}

// Validate methods for client frames (mirror AuthHello.Validate).

// Validate checks that a decoded RoomSubscribe has the expected shape.
// Returns "" if valid, otherwise an error code (CodeBadRequest).
func (f *RoomSubscribe) Validate() string {
	if f.Type != FrameTypeRoomSubscribe {
		return CodeBadFrame
	}
	if f.RoomID == "" {
		return CodeBadRequest
	}
	return ""
}

// Validate for RoomUnsubscribe.
func (f *RoomUnsubscribe) Validate() string {
	if f.Type != FrameTypeRoomUnsubscribe {
		return CodeBadFrame
	}
	if f.RoomID == "" {
		return CodeBadRequest
	}
	return ""
}

// Validate for MsgSend.
func (f *MsgSend) Validate() string {
	if f.Type != FrameTypeMsgSend {
		return CodeBadFrame
	}
	if f.RoomID == "" {
		return CodeBadRequest
	}
	// Sprint 1: empty content allowed (system messages use a
	// different code path; not exposed over WS). For msg.send,
	// require non-empty content so we don't persist blanks.
	if f.Content == "" {
		return CodeBadRequest
	}
	return ""
}
