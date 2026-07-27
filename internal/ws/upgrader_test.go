package ws

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"

	"github.com/ishadowland/fireside/internal/auth"
)

var (
	testSecret = []byte("test-secret-not-for-production-use-only")
	testTTL    = 5 * time.Minute
)

func TestMain(m *testing.M) {
	gin.SetMode(gin.TestMode)
	m.Run()
}

func newTestServer(t *testing.T, cfg Config) *httptest.Server {
	t.Helper()
	r := gin.New()
	Mount(r, cfg)
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return srv
}

func dial(t *testing.T, url string) *websocket.Conn {
	t.Helper()
	wsURL := "ws" + strings.TrimPrefix(url, "http") + "/ws/v1/connect"
	c, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func TestHandleConnectHappyPath(t *testing.T) {
	t.Parallel()

	var (
		gotUID int64
		gotJTI string
		called = make(chan struct{}, 1)
	)
	srv := newTestServer(t, Config{
		JWTSecret:    testSecret,
		HelloTimeout: 5 * time.Second,
		OnAuthenticated: func(uid int64, jti string, _ *websocket.Conn) {
			gotUID = uid
			gotJTI = jti
			select {
			case called <- struct{}{}:
			default:
			}
		},
	})

	tok, jti, err := auth.Issue(testSecret, 42, testTTL)
	if err != nil {
		t.Fatal(err)
	}

	conn := dial(t, srv.URL)
	if err := conn.WriteJSON(AuthHello{Type: FrameTypeAuthHello, Token: tok}); err != nil {
		t.Fatalf("write hello: %v", err)
	}

	var welcome AuthWelcome
	if err := conn.ReadJSON(&welcome); err != nil {
		t.Fatalf("read welcome: %v", err)
	}
	if welcome.Type != FrameTypeAuthWelcome {
		t.Errorf("welcome.Type: got %q, want %q", welcome.Type, FrameTypeAuthWelcome)
	}
	if welcome.UserID != 42 {
		t.Errorf("welcome.UserID: got %d, want 42", welcome.UserID)
	}
	if welcome.JTI != jti {
		t.Errorf("welcome.JTI: got %q, want %q", welcome.JTI, jti)
	}
	if welcome.ServerTime == 0 {
		t.Error("welcome.ServerTime should be non-zero")
	}

	select {
	case <-called:
		if gotUID != 42 || gotJTI != jti {
			t.Errorf("OnAuthenticated got uid=%d jti=%q, want uid=42 jti=%q", gotUID, gotJTI, jti)
		}
	case <-time.After(time.Second):
		t.Error("OnAuthenticated was not called")
	}
}

func TestHandleConnectHelloTimeout(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t, Config{
		JWTSecret:    testSecret,
		HelloTimeout: 100 * time.Millisecond,
	})

	// Dial but do NOT send any frame.
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws/v1/connect"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	defer func() { _ = conn.Close() }()

	// After ~150ms, the server should have timed out and closed with 1008.
	// Reading from the closed conn surfaces *websocket.CloseError.
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))

	for {
		if _, _, err := conn.NextReader(); err != nil {
			ce, ok := err.(*websocket.CloseError)
			if !ok {
				t.Fatalf("expected CloseError, got %T: %v", err, err)
			}
			if ce.Code != websocket.ClosePolicyViolation {
				t.Errorf("close code: got %d, want %d (1008)", ce.Code, websocket.ClosePolicyViolation)
			}
			return
		}
	}
}

func TestHandleConnectInvalidToken(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t, Config{
		JWTSecret:    testSecret,
		HelloTimeout: 5 * time.Second,
	})

	conn := dial(t, srv.URL)
	if err := conn.WriteJSON(AuthHello{Type: FrameTypeAuthHello, Token: "garbage"}); err != nil {
		t.Fatalf("write hello: %v", err)
	}

	// Server should write AuthError{Code: CodeInvalidToken} then close 1008.
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))

	var got AuthError
	if err := conn.ReadJSON(&got); err != nil {
		t.Fatalf("read auth.error: %v", err)
	}
	if got.Type != FrameTypeAuthError {
		t.Errorf("type: got %q, want %q", got.Type, FrameTypeAuthError)
	}
	if got.Code != CodeInvalidToken {
		t.Errorf("code: got %q, want %q", got.Code, CodeInvalidToken)
	}

	if _, _, err := conn.NextReader(); err != nil {
		ce, ok := err.(*websocket.CloseError)
		if !ok {
			t.Fatalf("expected CloseError after error frame, got %T: %v", err, err)
		}
		if ce.Code != websocket.ClosePolicyViolation {
			t.Errorf("close code: got %d, want %d", ce.Code, websocket.ClosePolicyViolation)
		}
	} else {
		t.Error("expected close after auth.error frame")
	}
}

func TestHandleConnectBadFrame(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t, Config{
		JWTSecret:    testSecret,
		HelloTimeout: 5 * time.Second,
	})

	conn := dial(t, srv.URL)
	if err := conn.WriteJSON(map[string]string{"type": "system.ping", "foo": "bar"}); err != nil {
		t.Fatalf("write bad frame: %v", err)
	}

	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))

	var got AuthError
	if err := conn.ReadJSON(&got); err != nil {
		t.Fatalf("read auth.error: %v", err)
	}
	if got.Code != CodeBadFrame {
		t.Errorf("code: got %q, want %q", got.Code, CodeBadFrame)
	}
}

// Sanity: ensure Mount does not panic with a basic engine.
func TestMountRegisters(t *testing.T) {
	t.Parallel()
	r := gin.New()
	Mount(r, Config{JWTSecret: testSecret})
	// Just verify the route shows up in the engine's tree — internal
	// gin's API is stable enough that a 404-vs-handler check works.
	// The handler itself will log a warning (no WS headers) but should
	// not panic.
	req := httptest.NewRequest(http.MethodGet, "/ws/v1/connect", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code == http.StatusNotFound {
		t.Fatal("Mount did not register /ws/v1/connect")
	}
}