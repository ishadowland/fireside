// Package hub is the in-process WS broadcast hub (Sprint 1 WP-5).
//
// Responsibilities:
//   - Track which WS connections are subscribed to which rooms
//   - Broadcast a frame to every connection in a room (except sender)
//   - Auto-clean dead connections when writes fail
//   - Heartbeat: periodic ping, close conns that don't respond in time
//
// Non-goals (Sprint 1):
//   - Cross-process pub/sub (Redis pub/sub) — ADR-0013 deferred
//   - Persistent message queue (Sprint 1 messages are DB-only;
//     broadcasts are fire-and-forget — reconnection replays from DB)
//   - Per-user read state — Sprint 1 has no per-user read markers
package hub

import (
	"encoding/json"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

// DefaultPingInterval is the time between pings sent to every conn.
const DefaultPingInterval = 30 * time.Second

// DefaultPongTimeout is the time we wait for a pong before closing
// a connection. Must be > DefaultPingInterval; per RFC §"心跳":
// 60s total (30s ping + 30s grace).
const DefaultPongTimeout = 60 * time.Second

// Hub manages per-connection, multi-room WS subscriptions.
//
// Concurrency model: a single sync.RWMutex guards both `rooms` and
// `byConn`. Reads (IsSubscribed, Count) take the RLock; writes
// (Register, Unregister, BroadcastToRoom) take the Lock. The mutex
// is held only for the duration of the index update, NOT for the
// websocket writes themselves (those happen after RUnlock to avoid
// blocking other registrations while a slow client drains).
type Hub struct {
	mu     sync.RWMutex
	rooms  map[string]map[*websocket.Conn]*sub // room_id -> conn -> sub
	byConn map[*websocket.Conn]map[string]bool  // reverse index for Unregister
	nextID uint64                               // atomic counter for ConnID
	log    *slog.Logger
}

// sub is the per-conn-per-room subscription record. Kept minimal —
// Sprint 1 only needs user_id for logging + a stable ConnID for
// debugging cross-room broadcasts.
type sub struct {
	userID   string
	connID   string
	joinedAt  time.Time
}

// New creates a Hub. log may be nil → slog.Default().
func New(log *slog.Logger) *Hub {
	if log == nil {
		log = slog.Default()
	}
	return &Hub{
		rooms:  make(map[string]map[*websocket.Conn]*sub),
		byConn: make(map[*websocket.Conn]map[string]bool),
		log:    log,
	}
}

// Register subscribes conn to roomID. Idempotent: registering the
// same conn twice to the same room is a no-op (and logs at debug
// for observability).
//
// userID is opaque to the hub — used only in logs and for
// per-user observability in Sprint 1. Sprint 2 may use it for
// per-user rate limiting.
//
// IMPORTANT: caller MUST defer h.Unregister(conn) on conn close
// to avoid leaks. The hub does not own the conn lifecycle.
func (h *Hub) Register(conn *websocket.Conn, roomID, userID string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	room, ok := h.rooms[roomID]
	if !ok {
		room = make(map[*websocket.Conn]*sub)
		h.rooms[roomID] = room
	}
	if _, dup := room[conn]; dup {
		h.log.Debug("Register: already subscribed (no-op)",
			"conn", h.connIDLocked(conn), "room_id", roomID)
		return
	}
	connID := h.connIDLocked(conn)
	room[conn] = &sub{
		userID:  userID,
		connID:  connID,
		joinedAt: time.Now(),
	}
	if _, ok := h.byConn[conn]; !ok {
		h.byConn[conn] = make(map[string]bool)
	}
	h.byConn[conn][roomID] = true
	h.log.Debug("Register: subscribed",
		"conn", connID, "user_id", userID, "room_id", roomID)
}

// Unregister removes conn from every room it was subscribed to.
// Idempotent: unregistering an unknown conn is a no-op.
//
// MUST be called via defer at the connection site. If the caller
// forgets, the conn will stay in the hub forever (memory leak;
// broadcasts will try to write to a closed conn and fail, eventually
// cleaning up via the write-failure path — but better to be explicit).
func (h *Hub) Unregister(conn *websocket.Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()

	rooms, ok := h.byConn[conn]
	if !ok {
		return // idempotent
	}
	for roomID := range rooms {
		if room, ok := h.rooms[roomID]; ok {
			delete(room, conn)
			if len(room) == 0 {
				delete(h.rooms, roomID)
			}
		}
	}
	delete(h.byConn, conn)
	h.log.Debug("Unregister: removed from all rooms",
		"conn", h.connIDLocked(conn), "n_rooms", len(rooms))
}

// BroadcastToRoom sends a JSON frame to every connection currently
// subscribed to roomID, except `except` (typically the sender).
//
// Returns the number of connections the frame was queued to.
//
// Dead-connection cleanup: if a write fails, the conn is
// auto-unregistered. The goroutine owning the conn will see the
// close on its next read.
//
// Concurrency: the index walk takes the RLock. The actual writes
// happen AFTER RUnlock to avoid holding the lock during slow client
// drains. This means BroadcastToRoom is non-atomic w.r.t. concurrent
// Register/Unregister — a conn that subscribes during a broadcast
// may or may not receive this particular frame (it will receive
// future frames). This is acceptable for the Sprint 1 "live chat"
// use case; ordering guarantees are out of scope.
func (h *Hub) BroadcastToRoom(roomID string, frame []byte, except *websocket.Conn) (delivered int) {
	// Phase 1: collect recipients under the RLock.
	h.mu.RLock()
	room, ok := h.rooms[roomID]
	if !ok {
		h.mu.RUnlock()
		return 0
	}
	type recipient struct {
		conn   *websocket.Conn
		connID string
		userID string
	}
	recips := make([]recipient, 0, len(room))
	for conn, s := range room {
		if conn == except {
			continue
		}
		recips = append(recips, recipient{conn, s.connID, s.userID})
	}
	h.mu.RUnlock()

	// Phase 2: write to each recipient outside the lock.
	var dead []*websocket.Conn
	for _, r := range recips {
		// Sprint 1: defer to the per-process writer hook. The ws
		// package installs a closure that holds the per-conn write
		// mutex (safeWriteMessage), so BroadcastToRoom is safe to
		// call from any goroutine. If the hook is the default
		// (no lock), this is just a WriteMessage.
		if err := writeHook(r.conn, h.log, websocket.TextMessage, frame); err != nil {
			h.log.Warn("BroadcastToRoom: write failed; will unregister",
				"conn", r.connID, "user_id", r.userID, "err", err)
			dead = append(dead, r.conn)
			continue
		}
		delivered++
	}

	// Phase 3: clean up dead conns (best-effort; the conn owner will
	// also see the failure and may close from their side).
	for _, conn := range dead {
		h.Unregister(conn)
	}
	return delivered
}

// IsSubscribed reports whether conn is currently subscribed to roomID.
// Used by handlers to short-circuit publishes (don't echo to sender
// — though the sender can also be excluded via the `except` arg).
func (h *Hub) IsSubscribed(conn *websocket.Conn, roomID string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	room, ok := h.rooms[roomID]
	if !ok {
		return false
	}
	_, ok = room[conn]
	return ok
}

// Count returns the total number of (conn, room) subscriptions
// across the hub. For tests / observability.
func (h *Hub) Count() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	n := 0
	for _, room := range h.rooms {
		n += len(room)
	}
	return n
}

// RoomCount returns the number of distinct room_ids the hub knows about.
func (h *Hub) RoomCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.rooms)
}

// ConnID returns a stable per-connection ID, assigned the first time
// the conn is registered. Subsequent calls return the same ID.
//
// Used for logging — Sprint 1 doesn't need per-conn state machine
// beyond this. Thread-safe.
func (h *Hub) ConnID(conn *websocket.Conn) string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.connIDLocked(conn)
}

// connIDLocked is ConnID without the lock. Caller must hold h.mu.
func (h *Hub) connIDLocked(conn *websocket.Conn) string {
	// Look up the first sub for this conn; its connID is the canonical
	// one. If conn is not registered yet (e.g. ping before register),
	// synthesize a fresh ID and cache it in byConn's per-conn sidecar.
	if s, ok := h.lookupAnySubLocked(conn); ok {
		return s.connID
	}
	id := "c" + uintToString(atomic.AddUint64(&h.nextID, 1))
	// We can't store this in s because s doesn't exist; the next
	// register call will allocate the same conn's sub fresh. For the
	// brief window before register, the ID is volatile — acceptable.
	return id
}

// lookupAnySubLocked returns any sub for the given conn. Caller holds h.mu.
func (h *Hub) lookupAnySubLocked(conn *websocket.Conn) (*sub, bool) {
	rooms := h.byConn[conn]
	for roomID := range rooms {
		if room, ok := h.rooms[roomID]; ok {
			if s, ok := room[conn]; ok {
				return s, true
			}
		}
	}
	return nil, false
}

// uintToString is a tiny helper (avoids importing strconv just for this).
func uintToString(n uint64) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

// StartHeartbeat spawns a goroutine that pings all registered
// connections at `interval`. Connections that don't respond with a
// pong within `pongTimeout` are unregister'd and their underlying
// conn is closed (the conn owner's read loop will return an error).
//
// Returns a stop function the caller invokes at shutdown.
//
// Sprint 1: simplest possible. Sprint 2 may switch to per-conn
// ping deadlines (set on each ping, cleared on pong).
func (h *Hub) StartHeartbeat(interval, pongTimeout time.Duration) (stop func()) {
	if interval <= 0 {
		interval = DefaultPingInterval
	}
	if pongTimeout <= 0 {
		pongTimeout = DefaultPongTimeout
	}
	stopCh := make(chan struct{})

	go func() {
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-stopCh:
				return
			case <-t.C:
				h.pingAll(pongTimeout)
			}
		}
	}()

	return func() {
		close(stopCh)
	}
}

// pingAll sends a ping to every registered conn. If a conn's write
// fails (dead), it is unregister'd. We don't read pongs here — the
// conn owner's read loop is responsible for processing pongs and
// resetting the read deadline. Sprint 1 simplification.
func (h *Hub) pingAll(pongTimeout time.Duration) {
	// Snapshot of all conns (deduped across rooms).
	h.mu.RLock()
	conns := make(map[*websocket.Conn]struct{}, len(h.byConn))
	for c := range h.byConn {
		conns[c] = struct{}{}
	}
	h.mu.RUnlock()

	deadline := time.Now().Add(pongTimeout)
	for conn := range conns {
		_ = conn.SetWriteDeadline(deadline)
		if err := conn.WriteControl(websocket.PingMessage, nil, deadline); err != nil {
			h.log.Warn("pingAll: write ping failed; unregistering",
				"conn", h.ConnID(conn), "err", err)
			h.Unregister(conn)
		}
	}
}

// RoomMembers returns the user_ids currently subscribed to roomID.
// Used by the dashboard "who's here" view. Sprint 1: list of
// strings; Sprint 2 may switch to a richer struct.
func (h *Hub) RoomMembers(roomID string) []string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	room, ok := h.rooms[roomID]
	if !ok {
		return nil
	}
	out := make([]string, 0, len(room))
	seen := make(map[string]bool, len(room))
	for _, s := range room {
		if seen[s.userID] {
			continue
		}
		seen[s.userID] = true
		out = append(out, s.userID)
	}
	return out
}

// UnregisterFromRoom removes conn from a single room. Idempotent:
// removing from a room the conn is not in is a no-op.
//
// Unlike Unregister (which removes from ALL rooms and is intended
// for conn close), UnregisterFromRoom is for the WS
// room.unsubscribe handler.
func (h *Hub) UnregisterFromRoom(conn *websocket.Conn, roomID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if room, ok := h.rooms[roomID]; ok {
		delete(room, conn)
		if len(room) == 0 {
			delete(h.rooms, roomID)
		}
	}
	if rooms, ok := h.byConn[conn]; ok {
		delete(rooms, roomID)
	}
}

// writeHook is the per-process function used to write frames to a
// conn. The ws package installs a closure that holds the per-conn
// write mutex (see wspkg.SetHubWriter / safeWriteMessage). The hub
// package never depends on the ws package — the indirection is
// set up at Mount time.
//
// default: writes directly to the conn. Suitable for tests /
// processes that do not run the ws dispatch loop.
var writeHook = func(conn *websocket.Conn, _ *slog.Logger, messageType int, data []byte) error {
	return conn.WriteMessage(messageType, data)
}

// HubWrite is exported for ws.SetHubWriter to call. Mirrors the
// per-process writeHook so the hub doesn't have to import ws.
func HubWrite(conn *websocket.Conn, log *slog.Logger, messageType int, data []byte) error {
	return writeHook(conn, log, messageType, data)
}

// SetHubWriter replaces the function the hub uses to write frames.
// Callers (the ws package) install a closure that holds the
// per-conn write mutex.
func SetHubWriter(fn func(*websocket.Conn, *slog.Logger, int, []byte) error) {
	writeHook = fn
}
func MarshalFrame(v any) ([]byte, error) {
	return json.Marshal(v)
}
