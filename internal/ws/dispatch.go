// Sprint 1 WP-6: WS dispatch loop after auth.welcome.
//
// After the client authenticates (auth.hello → auth.welcome), we
// start a read loop that handles business frames:
//
//   room.subscribe     → register conn with hub for a room
//   room.unsubscribe   → unregister
//   msg.send           → persist via messages.Service + broadcast
//   heartbeat          → no-op (gorilla's own ping handles liveness)
//
// The dispatch loop runs until:
//
//   - client closes the connection (read returns err)
//   - server sends a fatal frame and closes
//   - hub heartbeat detects a dead conn and unregisters
//
// Sprint 1 simplification: no concurrent goroutines per conn for
// reads/writes — gorilla requires exclusive writes from one
// goroutine. We use the read loop as the single writer.

package ws

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"

	"github.com/ishadowland/fireside/internal/hub"
	"github.com/ishadowland/fireside/internal/messages"
	"github.com/ishadowland/fireside/internal/participants"
	"github.com/ishadowland/fireside/internal/rooms"
)

// DispatchDeps groups the services that the dispatch loop needs.
// Wiring in main.go:
//
//   ws.MountBusiness(engine, ws.DispatchDeps{
//       Hub:             wsHub,
//       MessagesService: messagesService,
//       RoomsService:    roomsService,
//       ParticipantsService: participantsService,
//       Log:             slog.Default(),
//   })
type DispatchDeps struct {
	Hub                 *hub.Hub
	MessagesService     *messages.Service
	RoomsService        *rooms.Service
	ParticipantsService *participants.Service
	Log                 *slog.Logger
}

// MountBusiness registers the post-auth-welcome dispatch loop as a
// sub-handler on the WS route. It runs AFTER the auth handshake.
//
// The handler is invoked from upgrader.HandleConnect after auth.welcome
// is sent; the deps are passed via a new Config field so the
// handshake path can call into this dispatch loop.
//
// (See upgrader.go for the integration point.)
func MountBusiness(r *gin.Engine, cfg Config) {
	if cfg.DispatchDeps == nil {
		// Fall back to the auth-only path so callers can opt out of
		// the dispatch loop in tests. nil deps is the original
		// Sprint 0 / test-only behavior.
		Mount(r, cfg)
		return
	}
	if cfg.DispatchDeps.Log == nil {
		cfg.DispatchDeps.Log = slog.Default()
	}
	// Install the safe-write hook on the hub so BroadcastToRoom's
	// per-recipient WriteMessage calls are serialized through the
	// per-conn mutex. Without this, hub writes race with the
	// per-conn dispatch-loop writes (acks, errors) and gorilla
	// panics with "concurrent write to websocket connection".
	if cfg.DispatchDeps.Hub != nil {
		hub.SetHubWriter(safeWriteMessageForHub)
	}
	// Wire deps into the upgrade config so HandleConnect can find them.
	r.GET("/ws/v1/connect", HandleConnect(cfg))
}

// HandleDispatch is the per-conn post-auth read loop. It blocks
// until the conn closes or returns an error.
//
// The caller MUST have already sent auth.welcome and registered the
// conn with the hub (if applicable).
func HandleDispatch(
	ctx context.Context,
	conn *websocket.Conn,
	userID string,
	deps *DispatchDeps,
) {
	defer func() {
		// Always unregister from the hub on exit; idempotent.
		if deps != nil && deps.Hub != nil {
			deps.Hub.Unregister(conn)
		}
		// Drop the per-conn write mutex so writeMuMap doesn't grow
		// without bound across connect/disconnect cycles.
		releaseWriteMu(conn)
		_ = conn.Close()
	}()

	log := deps.Log.With("user_id", userID, "conn", deps.Hub.ConnID(conn))
	log.Debug("HandleDispatch: enter")

	// Reasonable read deadline — gorilla's ping handler resets this
	// on each pong. 60s matches DefaultPongTimeout.
	_ = conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	conn.SetPingHandler(func(appData string) error {
		_ = conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	for {
		// Check ctx — server shutdown signal.
		if err := ctx.Err(); err != nil {
			log.Debug("HandleDispatch: ctx done, exiting", "err", err)
			return
		}

		// Read next message (blocking). gorilla returns *websocket.CloseError
		// on close; we ignore other errors via log+continue.
		_, raw, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				log.Debug("HandleDispatch: client closed")
				return
			}
			// net.OpError or i/o timeout — treat as disconnect.
			log.Info("HandleDispatch: read err, exiting", "err", err)
			return
		}
		_ = conn.SetReadDeadline(time.Now().Add(60 * time.Second))

		// Peek at the frame type. We decode twice (once into a small
		// type-only struct, then into the real one) to avoid
		// reflection-heavy type switches over `any`.
		var peek struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(raw, &peek); err != nil {
			log.Warn("HandleDispatch: bad frame JSON", "err", err)
			writeBusinessError(conn, log, CodeBadFrame, "invalid JSON", "")
			continue
		}

		// Dispatch.
		switch peek.Type {
		case FrameTypeRoomSubscribe:
			var f RoomSubscribe
			if err := json.Unmarshal(raw, &f); err != nil {
				writeBusinessError(conn, log, CodeBadFrame, "room.subscribe bad shape", "")
				continue
			}
			if code := f.Validate(); code != "" {
				writeBusinessError(conn, log, code, "room.subscribe invalid", f.RoomID)
				continue
			}
			handleRoomSubscribe(ctx, conn, userID, f, deps)

		case FrameTypeRoomUnsubscribe:
			var f RoomUnsubscribe
			if err := json.Unmarshal(raw, &f); err != nil {
				writeBusinessError(conn, log, CodeBadFrame, "room.unsubscribe bad shape", "")
				continue
			}
			if code := f.Validate(); code != "" {
				writeBusinessError(conn, log, code, "room.unsubscribe invalid", f.RoomID)
				continue
			}
			handleRoomUnsubscribe(ctx, conn, userID, f, deps)

		case FrameTypeMsgSend:
			var f MsgSend
			if err := json.Unmarshal(raw, &f); err != nil {
				writeBusinessError(conn, log, CodeBadFrame, "msg.send bad shape", "")
				continue
			}
			if code := f.Validate(); code != "" {
				writeBusinessError(conn, log, code, "msg.send invalid", f.RoomID)
				continue
			}
			handleMsgSend(ctx, conn, userID, f, deps)

		case FrameTypeHeartbeat:
			// Gorilla's own ping already covers liveness. We accept
			// heartbeat frames and discard (Sprint 1).
			log.Debug("HandleDispatch: heartbeat (no-op)")

		default:
			log.Warn("HandleDispatch: unsupported frame", "type", peek.Type)
			writeBusinessError(conn, log, CodeBadFrame,
				"unsupported frame type: "+peek.Type, "")
		}
	}
}

// handleRoomSubscribe registers conn with the hub for the given
// room. We do NOT enforce any per-user rate limit in Sprint 1; the
// only gate is "the room exists and is active".
func handleRoomSubscribe(
	ctx context.Context,
	conn *websocket.Conn,
	userID string,
	f RoomSubscribe,
	deps *DispatchDeps,
) {
	log := deps.Log.With("user_id", userID, "room_id", f.RoomID,
		"conn", deps.Hub.ConnID(conn))

	// 1) Verify room exists + is active.
	room, _, err := deps.RoomsService.GetRoom(ctx, f.RoomID)
	if err != nil {
		// rooms.ErrRoomNotFound or wrapped
		if errors.Is(err, rooms.ErrRoomNotFound) {
			writeBusinessError(conn, log, CodeRoomNotFound, "room not found", f.RoomID)
			return
		}
		log.Error("handleRoomSubscribe: GetRoom failed", "err", err)
		writeBusinessError(conn, log, CodeInternal, "internal error", f.RoomID)
		return
	}
	if room.Status != "active" {
		writeBusinessError(conn, log, CodeRoomEnded, "room has ended", f.RoomID)
		return
	}

	// 2) Register with the hub. Idempotent.
	deps.Hub.Register(conn, f.RoomID, userID)

	// 3) Send ack.
	safeWriteJSON(conn, log, RoomSubscribed{
		Type:       FrameTypeRoomSubscribed,
		RoomID:     f.RoomID,
		ConnID:     deps.Hub.ConnID(conn),
		ServerTime: time.Now().Unix(),
	})
}

// handleRoomUnsubscribe removes conn from the hub for the given
// room. Idempotent (hub.UnregisterFromRoom is a no-op for unknown
// conn/room pairs).
func handleRoomUnsubscribe(
	ctx context.Context,
	conn *websocket.Conn,
	userID string,
	f RoomUnsubscribe,
	deps *DispatchDeps,
) {
	log := deps.Log.With("user_id", userID, "room_id", f.RoomID,
		"conn", deps.Hub.ConnID(conn))

	deps.Hub.UnregisterFromRoom(conn, f.RoomID)

	safeWriteJSON(conn, log, RoomUnsubscribed{
		Type:   FrameTypeRoomUnsubscribed,
		RoomID: f.RoomID,
	})
}

// handleMsgSend persists a message and broadcasts it.
//
// Preconditions:
//   - conn must be subscribed to f.RoomID (so we know they're in
//     the room). We use hub.IsSubscribed as the gate; this is a
//     stronger check than the REST `must be on_stage` because it
//     proves the conn explicitly subscribed.
//
// Sprint 1 simplification: we do NOT separately check on_stage in
// the participants table. The hub subscription is the source of
// truth. This means a user who joins via REST and subscribes via
// WS is "on_stage" for msg.send purposes — which is exactly what
// we want.
func handleMsgSend(
	ctx context.Context,
	conn *websocket.Conn,
	userID string,
	f MsgSend,
	deps *DispatchDeps,
) {
	log := deps.Log.With("user_id", userID, "room_id", f.RoomID,
		"conn", deps.Hub.ConnID(conn))

	// 1) Subscription gate.
	if !deps.Hub.IsSubscribed(conn, f.RoomID) {
		writeBusinessError(conn, log, CodeNotSubscribed,
			"subscribe to the room first (room.subscribe)", f.RoomID)
		return
	}

	// 2) Persist.
	msg, err := deps.MessagesService.CreateMessage(ctx, userID, f.RoomID, messages.CreateMessageRequest{
		Content: f.Content,
	})
	if err != nil {
		// Map service errors to WS error codes.
		switch {
		case errors.Is(err, messages.ErrRoomNotFound),
			errors.Is(err, rooms.ErrRoomNotFound):
			writeBusinessError(conn, log, CodeRoomNotFound, "room not found", f.RoomID)
		case errors.Is(err, messages.ErrNotOnStage):
			writeBusinessError(conn, log, CodeNotOnStage, "must be on_stage to send", f.RoomID)
		case errors.Is(err, rooms.ErrRoomEnded):
			writeBusinessError(conn, log, CodeRoomEnded, "room has ended", f.RoomID)
		default:
			log.Error("handleMsgSend: CreateMessage failed", "err", err)
			writeBusinessError(conn, log, CodeInternal, "internal error", f.RoomID)
		}
		return
	}

	// 3) Broadcast (including sender — Sprint 1 simplification).
	frame := MsgCreated{
		Type:    FrameTypeMsgCreated,
		Message: msg,
	}
	rawFrame, _ := json.Marshal(frame)
	delivered := deps.Hub.BroadcastToRoom(f.RoomID, rawFrame, nil)
	log.Debug("handleMsgSend: broadcast",
		"msg_id", msg.ID, "delivered", delivered)
}

// writeBusinessError sends a business error frame to the conn.
// Best-effort: if the write fails we log and return.
//
// MUST serialize through the per-conn write mutex like every other
// post-auth write: hub broadcasts (safeWriteMessage) can be running
// on another goroutine, and writing here without the lock would race
// with them (gorilla panics on concurrent writes).
func writeBusinessError(conn *websocket.Conn, log *slog.Logger, code, message, roomID string) {
	safeWriteJSON(conn, log, BusinessError{
		Type:    "error",
		Code:    code,
		Message: message,
		RoomID:  roomID,
	})
}

// hubWrite is removed; see the hub.SetHubWriter function (hub
// package). The dispatch loop calls hub.SetHubWriter at
// MountBusiness time to install a closure that wraps
// safeWriteMessage.

// SetHubWriter is exported for main.go / tests to install a locking
// write wrapper. See MountBusiness.
//
// (No-op stub here. The real SetHubWriter is in the hub package
// (hub.SetHubWriter) which the dispatch loop calls in MountBusiness
// to install safeWriteMessageForHub.)
//
// gorilla/websocket forbids concurrent Write* calls on the same
// conn, but our dispatch loop has two writers racing on the same
// conn: the per-conn read goroutine (sending acks, errors) and
// hub.BroadcastToRoom (writing a frame to every conn in the room,
// including the sender's own conn). These run on different
// goroutines and can collide.
//
// We protect each conn with a sync.Mutex keyed by the conn pointer.
// Hub.BroadcastToRoom looks up the same mutex when writing.
var (
	writeMuMap   = make(map[*websocket.Conn]*sync.Mutex)
	writeMuMapMu sync.Mutex
)

func writeMuFor(conn *websocket.Conn) *sync.Mutex {
	writeMuMapMu.Lock()
	defer writeMuMapMu.Unlock()
	mu, ok := writeMuMap[conn]
	if !ok {
		mu = &sync.Mutex{}
		writeMuMap[conn] = mu
	}
	return mu
}

func releaseWriteMu(conn *websocket.Conn) {
	writeMuMapMu.Lock()
	defer writeMuMapMu.Unlock()
	delete(writeMuMap, conn)
}

// safeWriteJSON is the ONLY way to write to a conn from the
// dispatch loop. It serializes all writes (acks, errors, broadcasts)
// on the same conn.
func safeWriteJSON(conn *websocket.Conn, log *slog.Logger, v any) {
	mu := writeMuFor(conn)
	mu.Lock()
	defer mu.Unlock()
	_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	if err := conn.WriteJSON(v); err != nil {
		log.Warn("safeWriteJSON: write failed", "err", err)
	}
}

// safeWriteMessage is the variant hub.BroadcastToRoom uses. The
// hub already pre-marshals frames; we wrap the raw WriteMessage.
func safeWriteMessage(conn *websocket.Conn, log *slog.Logger, messageType int, data []byte) error {
	mu := writeMuFor(conn)
	mu.Lock()
	defer mu.Unlock()
	_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	return conn.WriteMessage(messageType, data)
}

// safeWriteMessageForHub is the function MountBusiness installs
// on the hub via SetHubWriter. It adapts the hub's signature
// (which doesn't pass a logger) to safeWriteMessage's signature.
func safeWriteMessageForHub(conn *websocket.Conn, log *slog.Logger, messageType int, data []byte) error {
	return safeWriteMessage(conn, log, messageType, data)
}

// BroadcastRoomEnded is a helper for the (future) REST endpoint
// /v1/rooms/:id/end. It tells the hub to fan-out a room.ended
// frame to every conn subscribed to roomID.
//
// Sprint 1: not yet wired into a REST handler (WP-2's EndRoom is
// REST-only and doesn't notify WS subscribers yet). WP-6 exposes
// this for callers (e.g. WP-7 REST → WS glue) without forcing the
// caller to know about hub internals.
func BroadcastRoomEnded(ctx context.Context, deps *DispatchDeps, roomID, endedBy string) {
	if deps == nil || deps.Hub == nil {
		return
	}
	frame := RoomEnded{
		Type:       FrameTypeRoomEnded,
		RoomID:     roomID,
		EndedBy:    endedBy,
		ServerTime: time.Now().Unix(),
	}
	raw, _ := json.Marshal(frame)
	deps.Hub.BroadcastToRoom(roomID, raw, nil)
}

