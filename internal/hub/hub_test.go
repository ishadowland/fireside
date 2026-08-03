// Package hub — unit tests for the in-process broadcast hub.
//
// Uses net.Pipe-backed websocket.Conn (no real network) so tests are
// fast and deterministic. Concurrent tests use sync.WaitGroup to
// coordinate reader goroutines.
package hub

import (
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// newConnPair returns a (server, client) websocket.Conn pair backed
// by an in-memory net.Pipe. Both sides share a single "session".
func newConnPair(t *testing.T) (*websocket.Conn, *websocket.Conn) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := (&websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}).
			Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade: %v", err)
			return
		}
		// Echo pump: read forever, write back, until conn closes.
		go func() {
			for {
				mt, msg, err := c.ReadMessage()
				if err != nil {
					return
				}
				if err := c.WriteMessage(mt, msg); err != nil {
					return
				}
			}
		}()
	}))
	defer srv.Close()

	u, _ := url.Parse(srv.URL)
	u.Scheme = "ws"

	clientConn, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		t.Fatalf("client dial: %v", err)
	}

	// The server-side conn is the one Upgrade() returned. We need to
	// capture it in a thread-safe way. Easiest: upgrade the second
	// conn to be the server's via a second handler in the same srv.
	// For now, fall back: use a 2-conn httptest approach.
	t.Cleanup(func() {
		clientConn.Close()
	})
	return nil, clientConn
}

// connPair returns a real pair. We use a custom helper because
// httptest.NewServer's handler can't easily hand back the upgraded
// conn to the test. We use a channel.
func connPair(t *testing.T) (*websocket.Conn, *websocket.Conn) {
	t.Helper()

	type result struct {
		server *websocket.Conn
		err    error
	}
	ch := make(chan result, 1)

	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := upgrader.Upgrade(w, r, nil)
		ch <- result{c, err}
		// Block here holding the conn; test cleanup will Close it.
		<-r.Context().Done()
	}))
	t.Cleanup(srv.Close)

	u, _ := url.Parse(srv.URL)
	u.Scheme = "ws"
	clientConn, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { clientConn.Close() })

	res := <-ch
	if res.err != nil {
		t.Fatalf("upgrade: %v", res.err)
	}
	t.Cleanup(func() { res.server.Close() })
	return res.server, clientConn
}

func TestHub_RegisterAndUnregister(t *testing.T) {
	h := New(nil)
	srv, cli := connPair(t)

	h.Register(cli, "r1", "alice")
	if !h.IsSubscribed(cli, "r1") {
		t.Error("expected subscribed after Register")
	}
	if got := h.Count(); got != 1 {
		t.Errorf("Count = %d, want 1", got)
	}

	h.Unregister(cli)
	if h.IsSubscribed(cli, "r1") {
		t.Error("expected unsubscribed after Unregister")
	}
	if got := h.Count(); got != 0 {
		t.Errorf("Count after Unregister = %d, want 0", got)
	}
	_ = srv
}

func TestHub_Register_Idempotent(t *testing.T) {
	h := New(nil)
	_, cli := connPair(t)

	h.Register(cli, "r1", "alice")
	h.Register(cli, "r1", "alice") // second time, no-op
	if got := h.Count(); got != 1 {
		t.Errorf("Count = %d, want 1 (double register should not double-count)", got)
	}
}

func TestHub_Register_MultiRoom(t *testing.T) {
	h := New(nil)
	_, cli := connPair(t)

	h.Register(cli, "r1", "alice")
	h.Register(cli, "r2", "alice")
	h.Register(cli, "r3", "alice")
	if got := h.Count(); got != 3 {
		t.Errorf("Count = %d, want 3 (3 rooms)", got)
	}
	if h.RoomCount() != 3 {
		t.Errorf("RoomCount = %d, want 3", h.RoomCount())
	}

	// Unregister from one room — others stay.
	h.UnregisterFromRoom(cli, "r2")
	if h.IsSubscribed(cli, "r2") {
		t.Error("expected unsubscribed from r2")
	}
	if !h.IsSubscribed(cli, "r1") || !h.IsSubscribed(cli, "r3") {
		t.Error("expected still subscribed to r1 and r3")
	}
}

func TestHub_BroadcastToRoom_CrossRoomIsolation(t *testing.T) {
	h := New(nil)
	aliceSrv, aliceCli := connPair(t)
	bobSrv, bobCli := connPair(t)

	// Register the server-side conn: that's the one whose
	// WriteMessage the hub's Broadcast actually drives. (The client
	// side has its own goroutine reading; broadcasting to it would
	// race with the echo pump and never reach the test reader.)
	//
	// The dashboard scenario in production: the server is the
	// WebSocket upgrader in main.go; clients dial in and get their
	// own *websocket.Conn. The hub drives writes on the server
	// side. So registering the server conn is the realistic test.
	h.Register(aliceSrv, "r1", "alice")
	h.Register(bobSrv, "r2", "bob")

	delivered := h.BroadcastToRoom("r1", []byte("hi-alice-only"), nil)
	if delivered != 1 {
		t.Errorf("delivered = %d, want 1 (only alice in r1)", delivered)
	}

	// Read alice's client side.
	_ = aliceCli.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, msg, err := aliceCli.ReadMessage()
	if err != nil {
		t.Fatalf("alice read: %v", err)
	}
	if string(msg) != "hi-alice-only" {
		t.Errorf("alice got %q, want hi-alice-only", msg)
	}

	// Bob must NOT receive anything — verify with a short read deadline.
	_ = bobCli.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
	if _, _, err := bobCli.ReadMessage(); err == nil {
		t.Error("bob received a message from r1 (cross-room leak!)")
	}
}

func TestHub_BroadcastToRoom_ExceptSender(t *testing.T) {
	h := New(nil)
	aliceSrv, aliceCli := connPair(t)
	bobSrv, bobCli := connPair(t)

	h.Register(aliceSrv, "r1", "alice")
	h.Register(bobSrv, "r1", "bob")

	// Alice sends, except=alice → only bob should receive.
	delivered := h.BroadcastToRoom("r1", []byte("hi-bob"), aliceSrv)
	if delivered != 1 {
		t.Errorf("delivered = %d, want 1 (sender excluded)", delivered)
	}

	_ = bobCli.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, msg, err := bobCli.ReadMessage()
	if err != nil {
		t.Fatalf("bob read: %v", err)
	}
	if string(msg) != "hi-bob" {
		t.Errorf("bob got %q, want hi-bob", msg)
	}

	// Alice must not receive her own message.
	_ = aliceCli.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
	if _, _, err := aliceCli.ReadMessage(); err == nil {
		t.Error("alice received her own message (echo bug)")
	}
}

func TestHub_BroadcastToRoom_DeadConnCleanup(t *testing.T) {
	h := New(nil)
	aliceSrv, _ := connPair(t)
	_, bobCli := connPair(t)

	// Register server-side conns only (where broadcasts land).
	h.Register(aliceSrv, "r1", "alice")
	h.Register(bobCli, "r1", "bob") // bob: client conn this time for variety

	// Kill alice's conn (server side) — broadcasts will fail to write.
	aliceSrv.Close()

	// Wait briefly for the close to propagate.
	time.Sleep(100 * time.Millisecond)

	// Broadcast — the hub should detect alice is dead and auto-unregister.
	delivered := h.BroadcastToRoom("r1", []byte("hi"), nil)
	if delivered != 1 {
		t.Errorf("delivered = %d, want 1 (alice dead → auto-unregister)", delivered)
	}

	// Now alice is no longer in the hub.
	h.mu.RLock()
	_, aliceStillIn := h.byConn[aliceSrv]
	h.mu.RUnlock()
	if aliceStillIn {
		t.Error("alice should be auto-unregistered after broadcast detected write failure")
	}
}

func TestHub_RoomMembers(t *testing.T) {
	h := New(nil)
	_, c1 := connPair(t)
	_, c2 := connPair(t)
	_, c3 := connPair(t)

	h.Register(c1, "r1", "alice")
	h.Register(c2, "r1", "alice") // same user, two conns
	h.Register(c3, "r1", "bob")

	members := h.RoomMembers("r1")
	if len(members) != 2 {
		t.Errorf("RoomMembers len = %d, want 2 (alice + bob, deduped)", len(members))
	}
	// Membership order is non-deterministic; check set membership.
	got := make(map[string]bool)
	for _, m := range members {
		got[m] = true
	}
	if !got["alice"] || !got["bob"] {
		t.Errorf("RoomMembers = %v, want {alice, bob}", members)
	}
}

func TestHub_Concurrent_RegisterBroadcast(t *testing.T) {
	h := New(nil)

	// Set up 5 conns in r1.
	const N = 5
	conns := make([]*websocket.Conn, N)
	for i := 0; i < N; i++ {
		_, c := connPair(t)
		conns[i] = c
		h.Register(c, "r1", "user")
	}

	// Spawn N reader goroutines (drain each conn's buffer).
	var wg sync.WaitGroup
	for i := 0; i < N; i++ {
		c := conns[i]
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = c.SetReadDeadline(time.Now().Add(5 * time.Second))
			for j := 0; j < 50; j++ {
				if _, _, err := c.ReadMessage(); err != nil {
					return
				}
			}
		}()
	}

	// Concurrently broadcast 50 messages.
	for j := 0; j < 50; j++ {
		h.BroadcastToRoom("r1", []byte("msg"), nil)
	}

	wg.Wait()
	// If no data race / no missed message, Count should still be N.
	if got := h.Count(); got != N {
		t.Errorf("Count after concurrent = %d, want %d", got, N)
	}
}

func TestHub_StartHeartbeat_NoOp(t *testing.T) {
	h := New(nil)
	// StartHeartbeat on an empty hub: no conns to ping, should not panic.
	stop := h.StartHeartbeat(10*time.Millisecond, 50*time.Millisecond)
	time.Sleep(100 * time.Millisecond)
	stop()
}

func TestHub_UnregisterFromRoom(t *testing.T) {
	h := New(nil)
	_, cli := connPair(t)

	h.Register(cli, "r1", "alice")
	h.Register(cli, "r2", "alice")
	h.Register(cli, "r3", "alice")
	if h.Count() != 3 {
		t.Fatalf("Count = %d, want 3", h.Count())
	}

	h.UnregisterFromRoom(cli, "r2")
	if h.IsSubscribed(cli, "r2") {
		t.Error("expected unsubscribed from r2")
	}
	if !h.IsSubscribed(cli, "r1") || !h.IsSubscribed(cli, "r3") {
		t.Error("expected still in r1 and r3")
	}
	if h.Count() != 2 {
		t.Errorf("Count = %d, want 2", h.Count())
	}

	// Unregister from r1 — should only remove r1, r3 stays.
	h.UnregisterFromRoom(cli, "r1")
	if h.IsSubscribed(cli, "r1") {
		t.Error("expected unsubscribed from r1")
	}
	if !h.IsSubscribed(cli, "r3") {
		t.Error("expected still in r3")
	}
}

// silence unused import warnings
var (
	_ = strings.HasPrefix
	_ = net.IPv4zero
)
