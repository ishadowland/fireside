// Package messages — Service unit tests (Sprint 1 WP-3).
//
// Same test-DB convention as internal/rooms/service_test.go: requires
// FIRESIDE_TEST_DSN env var; skips otherwise. Each test TRUNCATEs
// relevant tables in TestMain-style setup (we use the same per-test
// truncate pattern as rooms for simplicity).
package messages

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/ishadowland/fireside/internal/rooms"
	"github.com/ishadowland/fireside/internal/store"

	"github.com/ishadowland/fireside/internal/testutil"
)

// agentsDefaultID mirrors agents.DefaultAgentID (well-known CHAR(26)
// sender id for the built-in agent; avoids an import cycle in tests).
const agentsDefaultID = "01AGT000000000000000000000"

func truncate(t *testing.T, db *sql.DB) {
	t.Helper()
	if _, err := db.ExecContext(context.Background(),
		`TRUNCATE TABLE messages, participants, rooms, auth_tokens, users RESTART IDENTITY CASCADE`,
	); err != nil {
		t.Fatalf("truncate: %v", err)
	}
}

// seedUser inserts a user row and returns the id (already ULID-shaped).
func seedUser(t *testing.T, db *sql.DB, phone string) string {
	t.Helper()
	id := "01HXY0000000000000000000" + phone[len(phone)-2:]
	if _, err := db.ExecContext(context.Background(),
		`INSERT INTO users (id, phone, display_name) VALUES ($1, $2, $3)`,
		id, phone, "user-"+phone[len(phone)-2:]); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	return id
}

// seedRoom inserts a room and returns the id. Uses VARCHAR(26) id
// (post migration 0007 — no Trim issues).
func seedRoom(t *testing.T, db *sql.DB, hostID string) string {
	t.Helper()
	id := "01HXY000000000000000000R0"
	if _, err := db.ExecContext(context.Background(),
		`INSERT INTO rooms (id, host_user_id, name, max_participants) VALUES ($1, $2, 'test-room', 8)`,
		id, hostID); err != nil {
		t.Fatalf("seed room: %v", err)
	}
	return id
}

// seedEndedRoom inserts a room and immediately marks it ended.
// Used to verify the issue #22 fix: ended rooms must return
// ErrRoomEnded (not ErrRoomNotFound) for both CreateMessage and
// CreateSystemMessage.
func seedEndedRoom(t *testing.T, db *sql.DB, hostID string) string {
	t.Helper()
	id := "01HXYENDED00000000000000R0"
	if _, err := db.ExecContext(context.Background(),
		`INSERT INTO rooms (id, host_user_id, name, max_participants, status, ended_at) VALUES ($1, $2, 'ended-test', 8, 'ended', NOW())`,
		id, hostID); err != nil {
		t.Fatalf("seed ended room: %v", err)
	}
	return id
}

// seedParticipant inserts an on_stage participant row.
func seedParticipant(t *testing.T, db *sql.DB, roomID, userID string) {
	t.Helper()
	if _, err := db.ExecContext(context.Background(),
		`INSERT INTO participants (id, room_id, user_id) VALUES ($1, $2, $3)`,
		"01HXY000000000000000000P"+userID[len(userID)-1:], roomID, userID); err != nil {
		t.Fatalf("seed participant: %v", err)
	}
}

func newTestService(t *testing.T, db *sql.DB) (*Service, *rooms.Service) {
	t.Helper()
	q := store.New(db)
	rs := rooms.NewService(q, nil)
	ms := NewService(q, rs, nil)
	return ms, rs
}

func TestService_CreateMessage_OK(t *testing.T) {
	db := testutil.OpenTestDB(t, "messages")
	defer func() { _ = db.Close() }()
	truncate(t, db)
	svc, _ := newTestService(t, db)

	ctx := context.Background()
	hostID := seedUser(t, db, "+8613800000001")
	userID := seedUser(t, db, "+8613800000002")
	roomID := seedRoom(t, db, hostID)
	seedParticipant(t, db, roomID, userID)

	got, err := svc.CreateMessage(ctx, userID, roomID, CreateMessageRequest{Content: "hello"})
	if err != nil {
		t.Fatalf("CreateMessage: %v", err)
	}
	if got.Content != "hello" {
		t.Errorf("Content = %q, want %q", got.Content, "hello")
	}
	if got.SenderKind != "human" {
		t.Errorf("SenderKind = %q, want %q", got.SenderKind, "human")
	}
	if got.SenderID == nil || *got.SenderID != userID {
		t.Errorf("SenderID = %v, want %q", got.SenderID, userID)
	}
	if got.ContentType != "text" {
		t.Errorf("ContentType = %q, want %q", got.ContentType, "text")
	}
	if got.RoomID != roomID {
		t.Errorf("RoomID = %q, want %q", got.RoomID, roomID)
	}
	if got.ID == "" {
		t.Error("ID is empty")
	}
	if len(got.ID) != 26 {
		t.Errorf("ID length = %d, want 26 (ULID)", len(got.ID))
	}
	if got.ReplyToID != nil {
		t.Errorf("ReplyToID = %v, want nil", got.ReplyToID)
	}
}

func TestService_CreateMessage_RoomNotFound(t *testing.T) {
	db := testutil.OpenTestDB(t, "messages")
	defer func() { _ = db.Close() }()
	truncate(t, db)
	svc, _ := newTestService(t, db)

	_, err := svc.CreateMessage(context.Background(), "01HXY0000000000000000000Z",
		"01HXY000000000000000000XXX", CreateMessageRequest{Content: "x"})
	if !errors.Is(err, ErrRoomNotFound) {
		t.Errorf("err = %v, want ErrRoomNotFound", err)
	}
}

func TestService_CreateMessage_NotOnStage(t *testing.T) {
	db := testutil.OpenTestDB(t, "messages")
	defer func() { _ = db.Close() }()
	truncate(t, db)
	svc, _ := newTestService(t, db)

	ctx := context.Background()
	hostID := seedUser(t, db, "+8613800000003")
	userID := seedUser(t, db, "+8613800000004")
	roomID := seedRoom(t, db, hostID)
	// Note: do NOT seed participant for userID — user is off_stage.

	_, err := svc.CreateMessage(ctx, userID, roomID, CreateMessageRequest{Content: "x"})
	if !errors.Is(err, ErrNotOnStage) {
		t.Errorf("err = %v, want ErrNotOnStage", err)
	}
}

func TestService_CreateMessage_EmptyContent(t *testing.T) {
	db := testutil.OpenTestDB(t, "messages")
	defer func() { _ = db.Close() }()
	truncate(t, db)
	svc, _ := newTestService(t, db)

	ctx := context.Background()
	hostID := seedUser(t, db, "+8613800000005")
	userID := seedUser(t, db, "+8613800000006")
	roomID := seedRoom(t, db, hostID)
	seedParticipant(t, db, roomID, userID)

	_, err := svc.CreateMessage(ctx, userID, roomID, CreateMessageRequest{Content: ""})
	if !errors.Is(err, ErrInvalidArg) {
		t.Errorf("err = %v, want ErrInvalidArg", err)
	}
}

func TestService_CreateSystemMessage_OK(t *testing.T) {
	db := testutil.OpenTestDB(t, "messages")
	defer func() { _ = db.Close() }()
	truncate(t, db)
	svc, _ := newTestService(t, db)

	ctx := context.Background()
	hostID := seedUser(t, db, "+8613800000007")
	roomID := seedRoom(t, db, hostID)

	err := svc.CreateSystemMessage(ctx, roomID, `{"event":"x"}`)
	if err != nil {
		t.Fatalf("CreateSystemMessage: %v", err)
	}
	// Verify via ListMessages.
	views, _, err := svc.ListMessagesByRoom(ctx, roomID, "", 10)
	if err != nil {
		t.Fatalf("ListMessagesByRoom: %v", err)
	}
	if len(views) != 1 {
		t.Fatalf("len(views) = %d, want 1", len(views))
	}
	v := views[0]
	if v.SenderKind != "system" {
		t.Errorf("SenderKind = %q, want %q", v.SenderKind, "system")
	}
	if v.SenderID != nil {
		t.Errorf("SenderID = %v, want nil (system must have NULL sender_id per CHECK)", v.SenderID)
	}
	if v.ContentType != "system" {
		t.Errorf("ContentType = %q, want %q", v.ContentType, "system")
	}
	if v.Content != `{"event":"x"}` {
		t.Errorf("Content = %q, want %q", v.Content, `{"event":"x"}`)
	}
}

func TestService_ListMessagesByRoom_CursorPagination(t *testing.T) {
	db := testutil.OpenTestDB(t, "messages")
	defer func() { _ = db.Close() }()
	truncate(t, db)
	svc, _ := newTestService(t, db)

	ctx := context.Background()
	hostID := seedUser(t, db, "+8613800000008")
	userID := seedUser(t, db, "+8613800000009")
	roomID := seedRoom(t, db, hostID)
	seedParticipant(t, db, roomID, userID)

	// Insert 5 messages.
	for i := 0; i < 5; i++ {
		_, err := svc.CreateMessage(ctx, userID, roomID, CreateMessageRequest{Content: "msg"})
		if err != nil {
			t.Fatalf("seed msg %d: %v", i, err)
		}
	}

	// Page 1: limit 2.
	page1, next1, err := svc.ListMessagesByRoom(ctx, roomID, "", 2)
	if err != nil {
		t.Fatalf("page1: %v", err)
	}
	if len(page1) != 2 {
		t.Errorf("page1 len = %d, want 2", len(page1))
	}
	if next1 == "" {
		t.Errorf("page1 next_before empty, want non-empty (more pages)")
	}

	// Page 2: cursor = page1.next_before, limit 2 → should get next 2 newest.
	page2, next2, err := svc.ListMessagesByRoom(ctx, roomID, next1, 2)
	if err != nil {
		t.Fatalf("page2: %v", err)
	}
	if len(page2) != 2 {
		t.Errorf("page2 len = %d, want 2", len(page2))
	}
	// No overlap: page2 message IDs should all be < page1's smallest.
	for _, p2 := range page2 {
		for _, p1 := range page1 {
			if p2.ID == p1.ID {
				t.Errorf("overlap: message %s appears in both pages", p2.ID)
			}
		}
	}
	if next2 == "" {
		t.Errorf("page2 next_before empty, want non-empty (more pages)")
	}

	// Page 3: cursor = page2.next_before, limit 2 → only 1 remaining.
	page3, next3, err := svc.ListMessagesByRoom(ctx, roomID, next2, 2)
	if err != nil {
		t.Fatalf("page3: %v", err)
	}
	if len(page3) != 1 {
		t.Errorf("page3 len = %d, want 1", len(page3))
	}
	if next3 != "" {
		t.Errorf("page3 next_before = %q, want empty (end of results)", next3)
	}

	// Total: 5 messages across 3 pages.
	total := len(page1) + len(page2) + len(page3)
	if total != 5 {
		t.Errorf("total = %d, want 5", total)
	}
}

func TestService_ListMessagesByRoom_DefaultAndMaxLimits(t *testing.T) {
	db := testutil.OpenTestDB(t, "messages")
	defer func() { _ = db.Close() }()
	truncate(t, db)
	svc, _ := newTestService(t, db)

	ctx := context.Background()
	hostID := seedUser(t, db, "+86138000000A1")
	roomID := seedRoom(t, db, hostID)

	// limit=0 → DefaultPageSize (50); no rows yet → empty page.
	page, _, err := svc.ListMessagesByRoom(ctx, roomID, "", 0)
	if err != nil {
		t.Fatalf("page: %v", err)
	}
	if len(page) != 0 {
		t.Errorf("page len = %d, want 0", len(page))
	}

	// limit > MaxPageSize → clamped silently.
	// Insert one row so the function returns 1 row + valid cursor.
	userID := seedUser(t, db, "+86138000000A2")
	seedParticipant(t, db, roomID, userID)
	if _, err := svc.CreateMessage(ctx, userID, roomID, CreateMessageRequest{Content: "x"}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	page2, _, err := svc.ListMessagesByRoom(ctx, roomID, "", 9999)
	if err != nil {
		t.Fatalf("page2: %v", err)
	}
	if len(page2) != 1 {
		t.Errorf("page2 len = %d, want 1 (clamped to MaxPageSize, but only 1 row exists)", len(page2))
	}
}

func TestService_GetMessage_OK(t *testing.T) {
	db := testutil.OpenTestDB(t, "messages")
	defer func() { _ = db.Close() }()
	truncate(t, db)
	svc, _ := newTestService(t, db)

	ctx := context.Background()
	hostID := seedUser(t, db, "+86138000000B1")
	userID := seedUser(t, db, "+86138000000B2")
	roomID := seedRoom(t, db, hostID)
	seedParticipant(t, db, roomID, userID)

	created, err := svc.CreateMessage(ctx, userID, roomID, CreateMessageRequest{Content: "ping"})
	if err != nil {
		t.Fatalf("CreateMessage: %v", err)
	}

	got, err := svc.GetMessage(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetMessage: %v", err)
	}
	if got.ID != created.ID {
		t.Errorf("ID = %q, want %q", got.ID, created.ID)
	}
	if got.Content != "ping" {
		t.Errorf("Content = %q, want %q", got.Content, "ping")
	}
}

func TestService_GetMessage_NotFound(t *testing.T) {
	db := testutil.OpenTestDB(t, "messages")
	defer func() { _ = db.Close() }()
	truncate(t, db)
	svc, _ := newTestService(t, db)

	_, err := svc.GetMessage(context.Background(), "01HXY0000000000000000000ZZ")
	if !errors.Is(err, ErrMessageNotFound) {
		t.Errorf("err = %v, want ErrMessageNotFound", err)
	}
}

// TestService_CreateMessage_EndedRoom (issue #22 fix) verifies that
// posting to a room with status='ended' returns ErrRoomEnded (not
// ErrRoomNotFound). The REST mount maps ErrRoomEnded to 409; the WS
// dispatch maps it to CodeRoomEnded.
func TestService_CreateMessage_EndedRoom(t *testing.T) {
	db := testutil.OpenTestDB(t, "messages")
	defer func() { _ = db.Close() }()
	truncate(t, db)
	svc, _ := newTestService(t, db)
	ctx := context.Background()

	hostID := seedUser(t, db, "+861380000910")
	roomID := seedEndedRoom(t, db, hostID)

	_, err := svc.CreateMessage(ctx, hostID, roomID, CreateMessageRequest{
		Content: "after ended",
	})
	if !errors.Is(err, ErrRoomEnded) {
		t.Errorf("err = %v, want ErrRoomEnded", err)
	}
	if errors.Is(err, ErrRoomNotFound) {
		t.Errorf("err also matches ErrRoomNotFound (issue #22 not fixed)")
	}
}

// TestService_CreateSystemMessage_EndedRoom (issue #22 fix) verifies
// the system-message path also distinguishes ended from missing.
func TestService_CreateSystemMessage_EndedRoom(t *testing.T) {
	db := testutil.OpenTestDB(t, "messages")
	defer func() { _ = db.Close() }()
	truncate(t, db)
	svc, _ := newTestService(t, db)
	ctx := context.Background()

	hostID := seedUser(t, db, "+861380000911")
	roomID := seedEndedRoom(t, db, hostID)

	err := svc.CreateSystemMessage(ctx, roomID, `{"event":"system.after.ended"}`)
	if !errors.Is(err, ErrRoomEnded) {
		t.Errorf("err = %v, want ErrRoomEnded", err)
	}
	if errors.Is(err, ErrRoomNotFound) {
		t.Errorf("err also matches ErrRoomNotFound (issue #22 not fixed)")
	}
}

func TestService_CreateAgentMessage_OK(t *testing.T) {
	db := testutil.OpenTestDB(t, "messages")
	defer func() { _ = db.Close() }()
	truncate(t, db)
	svc, _ := newTestService(t, db)

	ctx := context.Background()
	hostID := seedUser(t, db, "+8613800000007")
	roomID := seedRoom(t, db, hostID)

	view, err := svc.CreateAgentMessage(ctx, roomID, agentsDefaultID, "你好，我是 AI 助手。")
	if err != nil {
		t.Fatalf("CreateAgentMessage: %v", err)
	}
	if view.SenderKind != "agent" {
		t.Errorf("SenderKind = %q, want %q", view.SenderKind, "agent")
	}
	if view.SenderID == nil || *view.SenderID != agentsDefaultID {
		t.Errorf("SenderID = %v, want %q", view.SenderID, agentsDefaultID)
	}
	if view.ContentType != "text" {
		t.Errorf("ContentType = %q, want %q", view.ContentType, "text")
	}
	if view.Content != "你好，我是 AI 助手。" {
		t.Errorf("Content = %q, want reply text", view.Content)
	}

	// Persisted: readable via ListMessagesByRoom.
	views, _, err := svc.ListMessagesByRoom(ctx, roomID, "", 10)
	if err != nil {
		t.Fatalf("ListMessagesByRoom: %v", err)
	}
	if len(views) != 1 {
		t.Fatalf("len(views) = %d, want 1", len(views))
	}
}

func TestService_CreateAgentMessage_EndedRoom(t *testing.T) {
	db := testutil.OpenTestDB(t, "messages")
	defer func() { _ = db.Close() }()
	truncate(t, db)
	svc, _ := newTestService(t, db)
	ctx := context.Background()

	hostID := seedUser(t, db, "+861380000911")
	roomID := seedEndedRoom(t, db, hostID)

	_, err := svc.CreateAgentMessage(ctx, roomID, agentsDefaultID, "hi")
	if !errors.Is(err, ErrRoomEnded) {
		t.Errorf("err = %v, want ErrRoomEnded", err)
	}
}

func TestService_CreateAgentMessage_EmptyContent(t *testing.T) {
	db := testutil.OpenTestDB(t, "messages")
	defer func() { _ = db.Close() }()
	truncate(t, db)
	svc, _ := newTestService(t, db)
	ctx := context.Background()

	hostID := seedUser(t, db, "+861380000912")
	roomID := seedRoom(t, db, hostID)

	_, err := svc.CreateAgentMessage(ctx, roomID, agentsDefaultID, "")
	if !errors.Is(err, ErrInvalidArg) {
		t.Errorf("err = %v, want ErrInvalidArg", err)
	}
}
