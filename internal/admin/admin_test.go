// Package admin — integration tests for the loopback-only admin API.
//
// Same DB pattern as the other service packages: FIRESIDE_TEST_DSN
// gated (skipped without it), tables TRUNCATEd per test.
package admin

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/ishadowland/fireside/internal/hub"
	"github.com/ishadowland/fireside/internal/rooms"
	"github.com/ishadowland/fireside/internal/store"
	"github.com/ishadowland/fireside/internal/testutil"
)

func TestMain(m *testing.M) {
	gin.SetMode(gin.TestMode)
	os.Exit(m.Run())
}

// testID returns a 26-char id unique per prefix+n.
func testID(prefix string, n int) string {
	s := fmt.Sprintf("%s%02d", prefix, n)
	return s + strings.Repeat("0", 26-len(s))
}

func newTestRouter(t *testing.T, db *sql.DB) (*gin.Engine, *rooms.Service) {
	t.Helper()
	q := store.New(db)
	svc := rooms.NewService(q, slog.Default())
	r := gin.New()
	Mount(r, Config{RoomsService: svc, Hub: hub.New(slog.Default())})
	return r, svc
}

func truncate(t *testing.T, db *sql.DB) {
	t.Helper()
	if _, err := db.ExecContext(context.Background(),
		`TRUNCATE TABLE messages, participants, rooms, auth_tokens, users RESTART IDENTITY CASCADE`,
	); err != nil {
		t.Fatalf("truncate: %v", err)
	}
}

func seedUser(t *testing.T, db *sql.DB, id, phone string) {
	t.Helper()
	if _, err := db.ExecContext(context.Background(),
		`INSERT INTO users (id, phone, display_name) VALUES ($1, $2, 'T')`, id, phone,
	); err != nil {
		t.Fatalf("seed user: %v", err)
	}
}

func seedRoom(t *testing.T, db *sql.DB, roomID, hostID string) {
	t.Helper()
	if _, err := db.ExecContext(context.Background(),
		`INSERT INTO rooms (id, host_user_id, name, max_participants) VALUES ($1, $2, 'T', 4)`,
		roomID, hostID,
	); err != nil {
		t.Fatalf("seed room: %v", err)
	}
}

func seedParticipant(t *testing.T, db *sql.DB, roomID, userID string) {
	t.Helper()
	if _, err := db.ExecContext(context.Background(),
		`INSERT INTO participants (id, room_id, user_id) VALUES ($1, $2, $3)`,
		testID("01HXYPART", 1), roomID, userID,
	); err != nil {
		t.Fatalf("seed participant: %v", err)
	}
}

func seedMessage(t *testing.T, db *sql.DB, roomID, userID string) {
	t.Helper()
	if _, err := db.ExecContext(context.Background(),
		`INSERT INTO messages (id, room_id, sender_kind, sender_id, content_type, content)
		 VALUES ($1, $2, 'human', $3, 'text', 'hello')`,
		testID("01HXYMSG", 1), roomID, userID,
	); err != nil {
		t.Fatalf("seed message: %v", err)
	}
}

func doLoopback(r *gin.Engine, method, path string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	req.RemoteAddr = "127.0.0.1:54321"
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func doRemote(r *gin.Engine, method, path string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	req.RemoteAddr = "203.0.113.7:54321"
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func decodeBody(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &m); err != nil {
		t.Fatalf("decode: %v (body=%s)", err, w.Body.String())
	}
	return m
}

func TestAdminBlockedForRemote(t *testing.T) {
	db := testutil.OpenTestDB(t, "admin")
	defer func() { _ = db.Close() }()
	truncate(t, db)
	r, _ := newTestRouter(t, db)
	for _, path := range []string{
		"/v1/admin/rooms",
		"/v1/admin/rooms/01HXYROOM01000000000000000/close",
		"/v1/admin/rooms/01HXYROOM01000000000000000",
	} {
		w := doRemote(r, http.MethodGet, path)
		if w.Code != http.StatusNotFound {
			t.Errorf("remote %s = %d, want 404", path, w.Code)
		}
	}
	w := doRemote(r, http.MethodDelete, "/v1/admin/rooms")
	if w.Code != http.StatusNotFound {
		t.Errorf("remote DELETE /v1/admin/rooms = %d, want 404", w.Code)
	}
}

func TestAdminListRoomsWithStats(t *testing.T) {
	db := testutil.OpenTestDB(t, "admin")
	defer func() { _ = db.Close() }()
	truncate(t, db)
	r, _ := newTestRouter(t, db)

	host := testID("01HXYHOST", 1)
	user := testID("01HXYUSER", 1)
	roomID := testID("01HXYROOM", 1)
	seedUser(t, db, host, "+8613800000001")
	seedUser(t, db, user, "+8613800000002")
	seedRoom(t, db, roomID, host)
	seedParticipant(t, db, roomID, user)
	seedMessage(t, db, roomID, user)

	w := doLoopback(r, http.MethodGet, "/v1/admin/rooms")
	if w.Code != http.StatusOK {
		t.Fatalf("GET /v1/admin/rooms = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}
	body := decodeBody(t, w)
	roomsList, ok := body["rooms"].([]any)
	if !ok || len(roomsList) != 1 {
		t.Fatalf("rooms = %v, want 1 entry", body["rooms"])
	}
	rm := roomsList[0].(map[string]any)
	if rm["id"] != roomID {
		t.Errorf("id = %v, want %s", rm["id"], roomID)
	}
	if rm["status"] != "active" {
		t.Errorf("status = %v, want active", rm["status"])
	}
	if rm["participant_count"].(float64) != 1 {
		t.Errorf("participant_count = %v, want 1", rm["participant_count"])
	}
	if rm["message_count"].(float64) != 1 {
		t.Errorf("message_count = %v, want 1", rm["message_count"])
	}
}

func TestAdminCloseRoom(t *testing.T) {
	db := testutil.OpenTestDB(t, "admin")
	defer func() { _ = db.Close() }()
	truncate(t, db)
	r, svc := newTestRouter(t, db)

	host := testID("01HXYHOST", 1)
	roomID := testID("01HXYROOM", 1)
	seedUser(t, db, host, "+8613800000001")
	seedRoom(t, db, roomID, host)

	w := doLoopback(r, http.MethodPost, "/v1/admin/rooms/"+roomID+"/close")
	if w.Code != http.StatusOK {
		t.Fatalf("close = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}
	if body := decodeBody(t, w); body["status"] != "ended" {
		t.Errorf("status = %v, want ended", body["status"])
	}

	// Room must now be ended.
	room, _, err := svc.GetRoom(context.Background(), roomID)
	if err != nil {
		t.Fatalf("get room: %v", err)
	}
	if room.Status != "ended" {
		t.Errorf("room status = %q, want ended", room.Status)
	}

	// Force-close is idempotent on an already-ended room.
	w = doLoopback(r, http.MethodPost, "/v1/admin/rooms/"+roomID+"/close")
	if w.Code != http.StatusOK {
		t.Errorf("second close = %d, want 200", w.Code)
	}
}

func TestAdminCloseRoomNotFound(t *testing.T) {
	db := testutil.OpenTestDB(t, "admin")
	defer func() { _ = db.Close() }()
	truncate(t, db)
	r, _ := newTestRouter(t, db)

	w := doLoopback(r, http.MethodPost, "/v1/admin/rooms/01HXYROOM01000000000000000/close")
	if w.Code != http.StatusNotFound {
		t.Errorf("close missing room = %d, want 404", w.Code)
	}
}

func TestAdminDeleteRoomCascades(t *testing.T) {
	db := testutil.OpenTestDB(t, "admin")
	defer func() { _ = db.Close() }()
	truncate(t, db)
	r, svc := newTestRouter(t, db)

	host := testID("01HXYHOST", 1)
	user := testID("01HXYUSER", 1)
	roomID := testID("01HXYROOM", 1)
	seedUser(t, db, host, "+8613800000001")
	seedUser(t, db, user, "+8613800000002")
	seedRoom(t, db, roomID, host)
	seedParticipant(t, db, roomID, user)
	seedMessage(t, db, roomID, user)

	w := doLoopback(r, http.MethodDelete, "/v1/admin/rooms/"+roomID)
	if w.Code != http.StatusOK {
		t.Fatalf("delete = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}

	// Room gone.
	if _, _, err := svc.GetRoom(context.Background(), roomID); err != rooms.ErrRoomNotFound {
		t.Errorf("get deleted room err = %v, want ErrRoomNotFound", err)
	}
	// Cascade removed participants + messages.
	var n int
	if err := db.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM participants WHERE room_id=$1`, roomID).Scan(&n); err != nil {
		t.Fatalf("count participants: %v", err)
	}
	if n != 0 {
		t.Errorf("participants left = %d, want 0", n)
	}
	if err := db.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM messages WHERE room_id=$1`, roomID).Scan(&n); err != nil {
		t.Fatalf("count messages: %v", err)
	}
	if n != 0 {
		t.Errorf("messages left = %d, want 0", n)
	}
}

func TestAdminDeleteRoomNotFound(t *testing.T) {
	db := testutil.OpenTestDB(t, "admin")
	defer func() { _ = db.Close() }()
	truncate(t, db)
	r, _ := newTestRouter(t, db)

	w := doLoopback(r, http.MethodDelete, "/v1/admin/rooms/01HXYROOM01000000000000000")
	if w.Code != http.StatusNotFound {
		t.Errorf("delete missing room = %d, want 404", w.Code)
	}
}

func TestAdminDeleteAllRooms(t *testing.T) {
	db := testutil.OpenTestDB(t, "admin")
	defer func() { _ = db.Close() }()
	truncate(t, db)
	r, _ := newTestRouter(t, db)

	host := testID("01HXYHOST", 1)
	seedUser(t, db, host, "+8613800000001")
	for i := 1; i <= 2; i++ {
		seedRoom(t, db, testID("01HXYROOM", i), host)
	}

	w := doLoopback(r, http.MethodDelete, "/v1/admin/rooms")
	if w.Code != http.StatusOK {
		t.Fatalf("delete all = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}
	if body := decodeBody(t, w); body["deleted"].(float64) != 2 {
		t.Errorf("deleted = %v, want 2", body["deleted"])
	}

	w = doLoopback(r, http.MethodGet, "/v1/admin/rooms")
	body := decodeBody(t, w)
	if roomsList, ok := body["rooms"].([]any); !ok || len(roomsList) != 0 {
		t.Errorf("rooms after clear = %v, want empty", body["rooms"])
	}
}
