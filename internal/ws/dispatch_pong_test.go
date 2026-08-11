// Regression test: HandleDispatch must reset its read deadline when a
// PONG arrives, otherwise idle conns are killed at the deadline even
// though the hub heartbeat pings them and the client pongs.
//
// gorilla/websocket v1.5.3's DEFAULT pong handler does nothing (it no
// longer resets the read deadline), so HandleDispatch must set its own
// SetPongHandler. Without it, the server closes every idle conn after
// ~60s and the client sees close 1006.
//
// This test is DB-free (unlike the other dispatch tests) so it runs on
// every CI even without FIRESIDE_TEST_DSN.
package ws

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/ishadowland/fireside/internal/hub"
)

func TestHandleDispatch_PongResetsReadDeadline(t *testing.T) {
	// Shrink HandleDispatch's read deadline so the test doesn't wait 60s.
	// HandleDispatch re-arms this on every frame/ping/pong.
	orig := dispatchReadDeadline
	dispatchReadDeadline = 500 * time.Millisecond

	deps := &DispatchDeps{
		Hub: hub.New(slog.Default()),
		Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	done := make(chan struct{})
	var client *websocket.Conn

	// Restore the deadline only after the server goroutine has fully
	// exited, so -race sees no concurrent access to the package var.
	t.Cleanup(func() {
		if client != nil {
			_ = client.Close()
		}
		<-done
		dispatchReadDeadline = orig
	})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		go func() {
			HandleDispatch(context.Background(), conn, "test-user", deps)
			close(done)
		}()
	}))
	defer srv.Close()

	u, _ := url.Parse(srv.URL)
	u.Scheme = "ws"
	var err error
	client, _, err = websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}

	// Keep sending pongs well inside the deadline. HandleDispatch's own
	// SetPongHandler resets the read deadline to now+deadline on each one;
	// pings/pongs are CONTROL frames so they bypass the per-message reset
	// that data frames trigger. If the handler is missing, the conn dies
	// at the very first deadline (~500ms) regardless of these pongs.
	for i := 0; i < 8; i++ {
		time.Sleep(150 * time.Millisecond)
		if err := client.WriteControl(websocket.PongMessage, []byte("x"),
			time.Now().Add(time.Second)); err != nil {
			t.Fatalf("write pong: %v", err)
		}
	}

	// 8 pongs * 150ms = 1.2s, well past the 500ms deadline. The conn must
	// still be alive because each pong re-armed the deadline.
	select {
	case <-done:
		t.Fatal("dispatch exited even though pongs arrived — read deadline was not reset")
	default:
	}

	// A heartbeat frame after the original deadline must still be
	// accepted (the read loop is alive). The server replies nothing
	// to a heartbeat, so a read that returns a NETWORK TIMEOUT means
	// the conn is alive and idle. A close/non-timeout error means the
	// server conn died at the deadline. The read deadline must be
	// SHORTER than the server's re-armed dispatch deadline (500ms),
	// otherwise the idle server closes the conn first and the client
	// sees 1006 instead of a timeout.
	_ = client.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
	if err := client.WriteJSON(map[string]string{"type": "heartbeat"}); err != nil {
		t.Fatalf("write heartbeat after deadline: %v (server conn dead)", err)
	}
	_, _, err = client.ReadMessage()
	if err != nil {
		var ne net.Error
		if errors.As(err, &ne) && ne.Timeout() {
			return // alive + idle: regression fixed
		}
		t.Fatalf("read after pong reset returned non-timeout err %v — conn should be alive", err)
	}
}
