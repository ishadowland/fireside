// Package ws — dispatch loop integration tests (Sprint 1 WP-6).
//
// Uses httptest.NewServer + gorilla websocket to bring up the full
// handler chain. The handler is configured to talk to a real
// PostgreSQL test DB (set FIRESIDE_TEST_DSN), so msg.send exercises
// the same store + service path as production.
//
// The 2-client "alice sends, bob receives" scenario is the
// canonical WP-6 acceptance test.

package ws

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/ishadowland/fireside/internal/hub"
	"github.com/ishadowland/fireside/internal/messages"
	"github.com/ishadowland/fireside/internal/participants"
	"github.com/ishadowland/fireside/internal/rooms"
	"github.com/ishadowland/fireside/internal/store"
)

const testDSNEnv = "FIRESIDE_TEST_DSN"

func testDSN() string { return os.Getenv(testDSNEnv) }

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := testDSN()
	if dsn == "" {
		t.Skipf("skipping: %s not set", testDSNEnv)
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	if err := db.PingContext(context.Background()); err != nil {
		t.Fatalf("db ping: %v", err)
	}
	return db
}

func truncate(t *testing.T, db *sql.DB) {
	t.Helper()
	if _, err := db.ExecContext(context.Background(),
		`TRUNCATE TABLE messages, participants, rooms, auth_tokens, users RESTART IDENTITY CASCADE`,
	); err != nil {
		t.Fatalf("truncate: %v", err)
	}
}

func seedUser(t *testing.T, db *sql.DB, phone string) (userID, tokenStr string) {
	t.Helper()
	// 26-char ULID: 21-char prefix + 5-char phone-tail.
	userID = "01HXY000000000000000" + phone[len(phone)-5:]
	if _, err := db.ExecContext(context.Background(),
		`INSERT INTO users (id, phone, display_name) VALUES ($1, $2, $3)`,
		userID, phone, "user-"+phone[len(phone)-3:]); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	jti := uuid.New().String()
	if _, err := db.ExecContext(context.Background(),
		`INSERT INTO auth_tokens (jti, user_id, expires_at) VALUES ($1, $2, NOW() + INTERVAL '1 hour')`,
		jti, userID); err != nil {
		t.Fatalf("seed token: %v", err)
	}
	return userID, jti
}

func seedRoom(t *testing.T, db *sql.DB, hostID string) string {
	t.Helper()
	// Room ID must fit in VARCHAR(26). Use a 22-char prefix + 4-char
	// suffix derived from the host's last 4 chars.
	id := "01HXYR0000000000000000" + hostID[len(hostID)-4:]
	if _, err := db.ExecContext(context.Background(),
		`INSERT INTO rooms (id, host_user_id, name, max_participants) VALUES ($1, $2, 'test-room', 8)`,
		id, hostID); err != nil {
		t.Fatalf("seed room: %v", err)
	}
	return id
}

// dialAndAuth brings up a WS server, dials as a client, sends
// auth.hello, and waits for auth.welcome. Returns the authenticated
// conn + the user_id it authenticated as.
func dialAndAuth(t *testing.T, server *httptest.Server, userID, jti string) *websocket.Conn {
	t.Helper()
	u, _ := url.Parse(server.URL)
	u.Scheme = "ws"

	conn, _, err := websocket.DefaultDialer.Dial(u.String()+"/ws/v1/connect", nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}

	// Build a minimal JWT (the test server uses cfg.JWTSecret; we
	// generate a JWT-shaped string here just to satisfy the auth
	// gate. The TokenLookup check needs the jti to match a row in
	// auth_tokens, which we seeded above.
	//
	// For the test, we don't need a real JWT signature — the test
	// server is configured with the same jti table, so as long as
	// the jti is in the table, the server's TokenLookup passes.
	// (We pass cfg.Tokens = store.Queries.)
	//
	// Actually: the WS handler also verifies the JWT signature.
	// We need a real HS256 token. Build one.
	tok := signTestToken(t, userID, jti)

	if err := conn.WriteJSON(map[string]string{
		"type":  FrameTypeAuthHello,
		"token": tok,
	}); err != nil {
		t.Fatalf("write auth.hello: %v", err)
	}

	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	_, raw, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read auth.welcome: %v", err)
	}
	var resp map[string]any
	_ = json.Unmarshal(raw, &resp)
	if resp["type"] != FrameTypeAuthWelcome {
		t.Fatalf("expected auth.welcome, got %s: %s", resp["type"], string(raw))
	}
	return conn
}

// TestDispatch_SubscribeAndSend is the WP-6 acceptance test.
//
// Scenario: alice and bob both join a room via REST. They both
// connect over WS, subscribe to the room, and alice sends a
// message. Verify: (a) msg.created is broadcast to both, (b) the
// message is persisted in DB, (c) the message_id matches.
func TestDispatch_SubscribeAndSend(t *testing.T) {
	db := openTestDB(t)
	defer func() { _ = db.Close() }()
	truncate(t, db)

	q := store.New(db)
	rs := rooms.NewService(q, nil)
	ms := messages.NewService(q, rs, nil)
	ps := participants.NewService(q, rs, ms, nil)
	wsHub := hub.New(nil)

	// Seed two users with tokens.
	aliceID, aliceJTI := seedUser(t, db, "+861380000001")
	bobID, bobJTI := seedUser(t, db, "+861380000002")
	roomID := seedRoom(t, db, aliceID)

	// REST-join both to the room.
	if _, err := ps.JoinRoom(context.Background(), roomID, aliceID); err != nil {
		t.Fatalf("join alice: %v", err)
	}
	if _, err := ps.JoinRoom(context.Background(), roomID, bobID); err != nil {
		t.Fatalf("join bob: %v", err)
	}

	// Build the WS server with dispatch wired.
	jwtSecret := []byte("test-secret-must-be-32-bytes-long-xxxx")
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	MountBusiness(engine, Config{
		JWTSecret:    jwtSecret,
		HelloTimeout: 3 * time.Second,
		CheckOrigin:  func(*http.Request) bool { return true },
		Tokens:       q,
		Hub:          wsHub,
		DispatchDeps: &DispatchDeps{
			Hub:                 wsHub,
			MessagesService:     ms,
			RoomsService:        rs,
			ParticipantsService: ps,
			Log:                 nil,
		},
	})
	server := httptest.NewServer(engine)
	defer server.Close()

	// Connect both clients.
	aliceConn := dialAndAuth(t, server, aliceID, aliceJTI)
	defer func() { _ = aliceConn.Close() }()
	bobConn := dialAndAuth(t, server, bobID, bobJTI)
	defer func() { _ = bobConn.Close() }()

	// Subscribe both to the room.
	aliceConn.WriteJSON(RoomSubscribe{Type: FrameTypeRoomSubscribe, RoomID: roomID})
	bobConn.WriteJSON(RoomSubscribe{Type: FrameTypeRoomSubscribe, RoomID: roomID})
	if !waitForFrame(t, aliceConn, FrameTypeRoomSubscribed, 2*time.Second) {
		t.Fatal("alice did not receive room.subscribed")
	}
	if !waitForFrame(t, bobConn, FrameTypeRoomSubscribed, 2*time.Second) {
		t.Fatal("bob did not receive room.subscribed")
	}

	// Alice sends a message.
	if err := aliceConn.WriteJSON(MsgSend{
		Type:    FrameTypeMsgSend,
		RoomID:  roomID,
		Content: "hello from alice",
	}); err != nil {
		t.Fatalf("alice write msg.send: %v", err)
	}

	// Both should receive msg.created.
	aliceCreated := waitForFrameData(t, aliceConn, FrameTypeMsgCreated, 2*time.Second)
	bobCreated := waitForFrameData(t, bobConn, FrameTypeMsgCreated, 2*time.Second)
	if aliceCreated == nil {
		t.Fatal("alice did not receive msg.created")
	}
	if bobCreated == nil {
		t.Fatal("bob did not receive msg.created")
	}

	// Both must have the same message id.
	aliceMsg := aliceCreated["message"].(map[string]any)
	bobMsg := bobCreated["message"].(map[string]any)
	if aliceMsg["id"] != bobMsg["id"] {
		t.Errorf("message_id mismatch: alice=%v bob=%v", aliceMsg["id"], bobMsg["id"])
	}
	if aliceMsg["content"] != "hello from alice" {
		t.Errorf("alice content = %v, want 'hello from alice'", aliceMsg["content"])
	}
	if aliceMsg["sender_kind"] != "human" {
		t.Errorf("alice sender_kind = %v, want human", aliceMsg["sender_kind"])
	}
	if aliceMsg["sender_id"] != aliceID {
		t.Errorf("alice sender_id = %v, want %v", aliceMsg["sender_id"], aliceID)
	}

	// DB must have the message.
	var dbContent string
	if err := db.QueryRowContext(context.Background(),
		`SELECT content FROM messages WHERE room_id = $1 AND sender_kind = 'human'`,
		roomID).Scan(&dbContent); err != nil {
		t.Fatalf("db lookup: %v", err)
	}
	if dbContent != "hello from alice" {
		t.Errorf("db content = %q, want 'hello from alice'", dbContent)
	}
}

// TestDispatch_SendWithoutSubscribe verifies that msg.send without
// a prior room.subscribe is rejected with not_subscribed.
func TestDispatch_SendWithoutSubscribe(t *testing.T) {
	db := openTestDB(t)
	defer func() { _ = db.Close() }()
	truncate(t, db)

	q := store.New(db)
	rs := rooms.NewService(q, nil)
	ms := messages.NewService(q, rs, nil)
	ps := participants.NewService(q, rs, ms, nil)
	wsHub := hub.New(nil)

	aliceID, aliceJTI := seedUser(t, db, "+861380000010")
	bobID, bobJTI := seedUser(t, db, "+861380000011")
	roomID := seedRoom(t, db, aliceID)
	ps.JoinRoom(context.Background(), roomID, aliceID)
	ps.JoinRoom(context.Background(), roomID, bobID)

	jwtSecret := []byte("test-secret-must-be-32-bytes-long-xxxx")
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	MountBusiness(engine, Config{
		JWTSecret:    jwtSecret,
		HelloTimeout: 3 * time.Second,
		CheckOrigin:  func(*http.Request) bool { return true },
		Tokens:       q,
		Hub:          wsHub,
		DispatchDeps: &DispatchDeps{
			Hub:                 wsHub,
			MessagesService:     ms,
			RoomsService:        rs,
			ParticipantsService: ps,
			Log:                 nil,
		},
	})
	server := httptest.NewServer(engine)
	defer server.Close()

	aliceConn := dialAndAuth(t, server, aliceID, aliceJTI)
	defer aliceConn.Close()

	// Send msg.send without subscribing first.
	aliceConn.WriteJSON(MsgSend{
		Type:    FrameTypeMsgSend,
		RoomID:  roomID,
		Content: "should fail",
	})
	if !waitForErrorCode(t, aliceConn, CodeNotSubscribed, 2*time.Second) {
		t.Fatal("expected not_subscribed error")
	}
	_ = bobID
	_ = bobJTI
}

// TestDispatch_SubscribeUnknownRoom verifies the room_not_found path.
func TestDispatch_SubscribeUnknownRoom(t *testing.T) {
	db := openTestDB(t)
	defer func() { _ = db.Close() }()
	truncate(t, db)

	q := store.New(db)
	rs := rooms.NewService(q, nil)
	ms := messages.NewService(q, rs, nil)
	ps := participants.NewService(q, rs, ms, nil)
	wsHub := hub.New(nil)

	aliceID, aliceJTI := seedUser(t, db, "+861380000020")
	_ = aliceID
	_ = aliceJTI

	jwtSecret := []byte("test-secret-must-be-32-bytes-long-xxxx")
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	MountBusiness(engine, Config{
		JWTSecret:    jwtSecret,
		HelloTimeout: 3 * time.Second,
		CheckOrigin:  func(*http.Request) bool { return true },
		Tokens:       q,
		Hub:          wsHub,
		DispatchDeps: &DispatchDeps{
			Hub:                 wsHub,
			MessagesService:     ms,
			RoomsService:        rs,
			ParticipantsService: ps,
			Log:                 nil,
		},
	})
	server := httptest.NewServer(engine)
	defer server.Close()

	aliceConn := dialAndAuth(t, server, aliceID, aliceJTI)
	defer aliceConn.Close()

	aliceConn.WriteJSON(RoomSubscribe{
		Type:   FrameTypeRoomSubscribe,
		RoomID: "01HXY0000000000000000000Z", // doesn't exist
	})
	if !waitForErrorCode(t, aliceConn, CodeRoomNotFound, 2*time.Second) {
		t.Fatal("expected room_not_found error")
	}
}

// Helpers ---------------------------------------------------------------

func waitForFrame(t *testing.T, conn *websocket.Conn, frameType string, timeout time.Duration) bool {
	t.Helper()
	deadline := time.Now().Add(timeout)
	_ = conn.SetReadDeadline(deadline)
	for time.Now().Before(deadline) {
		_, raw, err := conn.ReadMessage()
		if err != nil {
			return false
		}
		var f map[string]any
		_ = json.Unmarshal(raw, &f)
		if f["type"] == frameType {
			return true
		}
	}
	return false
}

func waitForFrameData(t *testing.T, conn *websocket.Conn, frameType string, timeout time.Duration) map[string]any {
	t.Helper()
	deadline := time.Now().Add(timeout)
	_ = conn.SetReadDeadline(deadline)
	for time.Now().Before(deadline) {
		_, raw, err := conn.ReadMessage()
		if err != nil {
			return nil
		}
		var f map[string]any
		_ = json.Unmarshal(raw, &f)
		if f["type"] == frameType {
			return f
		}
	}
	return nil
}

func waitForErrorCode(t *testing.T, conn *websocket.Conn, code string, timeout time.Duration) bool {
	t.Helper()
	deadline := time.Now().Add(timeout)
	_ = conn.SetReadDeadline(deadline)
	for time.Now().Before(deadline) {
		_, raw, err := conn.ReadMessage()
		if err != nil {
			return false
		}
		var f map[string]any
		_ = json.Unmarshal(raw, &f)
		if f["type"] == "error" && f["code"] == code {
			return true
		}
	}
	return false
}

// signTestToken builds a real HS256 JWT for the dispatch tests. The
// server's TokenLookup is the auth_tokens table; we seed a row with
// the given jti so the WS handler accepts the token.
func signTestToken(t *testing.T, userID, jti string) string {
	t.Helper()
	// We can't import internal/auth here (would be a cycle) — use
	// the same lib the auth package uses.
	// internal/auth uses github.com/golang-jwt/jwt/v5; we can use it
	// directly. But simpler: use the auth package's signing helper.
	// However, that would create an import cycle (auth imports ws
	// for nothing — actually, no, auth doesn't import ws).
	// Actually: ws does not import auth. auth does not import ws.
	// So we can import auth.
	//
	// But to keep this test self-contained, we'll generate the
	// token using the same library directly.
	//
	// For brevity, use the auth package:
	tok, err := signHS256(userID, jti, []byte("test-secret-must-be-32-bytes-long-xxxx"))
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}
	return tok
}

// signHS256 wraps the actual JWT library to keep the test compact.
func signHS256(userID, jti string, secret []byte) (string, error) {
	// Imported in dispatch_test.go via the auth package's transitive
	// dep. We use the same lib directly:
	//   import "github.com/golang-jwt/jwt/v5"
	// (declared in business_frames.go via the auth package's
	// transitive deps — but the test needs its own import.)
	//
	// Inline implementation avoids re-importing:
	//   return auth.SignTestToken(userID, jti, secret)  // would need a test helper
	//
	// Simplest: just call the auth package's exported function.
	return authSign(userID, jti, secret)
}

// authSign signs an HS256 token directly. Mirrors internal/auth.Sign
// but kept inline so the test doesn't need to import internal/auth
// (which would pull in a transitive dep on store).
func authSign(userID, jti string, secret []byte) (string, error) {
	now := time.Now()
	claims := jwt.MapClaims{
		"uid": userID,
		"jti": jti,
		"exp": now.Add(15 * time.Minute).Unix(),
		"iat": now.Unix(),
		"nbf": now.Add(-1 * time.Minute).Unix(),
		"iss": "fireside-test",
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return tok.SignedString(secret)
}

// authSignHelper is an alias kept for readability.
func authSignHelper(userID, jti string, secret []byte) (string, error) {
	return authSign(userID, jti, secret)
}

// silence unused
var (
	_ = sync.Mutex{}
	_ = fmt.Sprintf
)