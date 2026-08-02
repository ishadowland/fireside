// Package participants — Service unit tests (Sprint 1 WP-4).
//
// Same test-DB convention as internal/rooms / internal/messages:
// requires FIRESIDE_TEST_DSN env var; skips otherwise. Per-test
// truncate; rooms + messages + participants tests share `fireside_test`
// (use `go test -p 1` for full sweep).
package participants

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/ishadowland/fireside/internal/messages"
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

func seedRoom(t *testing.T, db *sql.DB, hostID string, max int32) string {
	t.Helper()
	id := "01HXY000000000000000000R0"
	if _, err := db.ExecContext(context.Background(),
		`INSERT INTO rooms (id, host_user_id, name, max_participants) VALUES ($1, $2, 'test-room', $3)`,
		id, hostID, max); err != nil {
		t.Fatalf("seed room: %v", err)
	}
	return id
}

func newTestService(t *testing.T, db *sql.DB) (*Service, *rooms.Service, *messages.Service) {
	t.Helper()
	q := store.New(db)
	rs := rooms.NewService(q, nil)
	ms := messages.NewService(q, rs, nil)
	ps := NewService(q, rs, ms, nil)
	return ps, rs, ms
}

func TestService_JoinRoom_OK(t *testing.T) {
	db := openTestDB(t)
	defer func() { _ = db.Close() }()
	truncate(t, db)
	ps, _, _ := newTestService(t, db)

	ctx := context.Background()
	hostID := seedUser(t, db, "+8613800000010")
	userID := seedUser(t, db, "+8613800000011")
	roomID := seedRoom(t, db, hostID, 8)

	got, err := ps.JoinRoom(ctx, roomID, userID)
	if err != nil {
		t.Fatalf("JoinRoom: %v", err)
	}
	if got.UserID != userID {
		t.Errorf("UserID = %q, want %q", got.UserID, userID)
	}
	if got.RoomID != roomID {
		t.Errorf("RoomID = %q, want %q", got.RoomID, roomID)
	}
	if got.StageState != "on_stage" {
		t.Errorf("StageState = %q, want on_stage", got.StageState)
	}
	if got.LeftAt != nil {
		t.Errorf("LeftAt = %v, want nil", got.LeftAt)
	}

	// Verify system message was written.
	var msgContent string
	if err := db.QueryRowContext(ctx,
		`SELECT content FROM messages WHERE room_id = $1 AND sender_kind = 'system'`,
		roomID).Scan(&msgContent); err != nil {
		t.Errorf("expected system message, got: %v", err)
	}
}

func TestService_JoinRoom_RoomNotFound(t *testing.T) {
	db := openTestDB(t)
	defer func() { _ = db.Close() }()
	truncate(t, db)
	ps, _, _ := newTestService(t, db)

	_, err := ps.JoinRoom(context.Background(),
		"01HXY000000000000000000XXX", "01HXY0000000000000000000Z")
	if !errors.Is(err, ErrRoomNotFound) {
		t.Errorf("err = %v, want ErrRoomNotFound", err)
	}
}

func TestService_JoinRoom_AlreadyOnStage(t *testing.T) {
	db := openTestDB(t)
	defer func() { _ = db.Close() }()
	truncate(t, db)
	ps, _, _ := newTestService(t, db)

	ctx := context.Background()
	hostID := seedUser(t, db, "+8613800000012")
	userID := seedUser(t, db, "+8613800000013")
	roomID := seedRoom(t, db, hostID, 8)

	if _, err := ps.JoinRoom(ctx, roomID, userID); err != nil {
		t.Fatalf("first JoinRoom: %v", err)
	}
	_, err := ps.JoinRoom(ctx, roomID, userID)
	if !errors.Is(err, ErrAlreadyOnStage) {
		t.Errorf("err = %v, want ErrAlreadyOnStage", err)
	}
}

func TestService_JoinRoom_RoomFull(t *testing.T) {
	db := openTestDB(t)
	defer func() { _ = db.Close() }()
	truncate(t, db)
	ps, _, _ := newTestService(t, db)

	ctx := context.Background()
	hostID := seedUser(t, db, "+8613800000014")
	roomID := seedRoom(t, db, hostID, 2) // capacity 2

	// Fill the room.
	for i := 0; i < 2; i++ {
		uid := seedUser(t, db, "+861380000001"+string(rune('5'+i)))
		if _, err := ps.JoinRoom(ctx, roomID, uid); err != nil {
			t.Fatalf("JoinRoom #%d: %v", i, err)
		}
	}

	// Third joiner should fail with ErrRoomFull.
	extra := seedUser(t, db, "+861380000001A")
	_, err := ps.JoinRoom(ctx, roomID, extra)
	if !errors.Is(err, ErrRoomFull) {
		t.Errorf("err = %v, want ErrRoomFull", err)
	}
}

func TestService_LeaveRoom_OK(t *testing.T) {
	db := openTestDB(t)
	defer func() { _ = db.Close() }()
	truncate(t, db)
	ps, _, _ := newTestService(t, db)

	ctx := context.Background()
	hostID := seedUser(t, db, "+861380000001B")
	userID := seedUser(t, db, "+861380000001C")
	roomID := seedRoom(t, db, hostID, 8)

	if _, err := ps.JoinRoom(ctx, roomID, userID); err != nil {
		t.Fatalf("JoinRoom: %v", err)
	}
	got, err := ps.LeaveRoom(ctx, roomID, userID)
	if err != nil {
		t.Fatalf("LeaveRoom: %v", err)
	}
	if got.StageState != "off_stage" {
		t.Errorf("StageState = %q, want off_stage", got.StageState)
	}
	if got.LeftAt == nil {
		t.Errorf("LeftAt is nil, want set")
	}

	// Verify system message (1 join + 1 leave = 2 system messages).
	var msgCount int
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM messages WHERE room_id = $1 AND sender_kind = 'system'`,
		roomID).Scan(&msgCount); err != nil {
		t.Fatalf("count messages: %v", err)
	}
	if msgCount != 2 {
		t.Errorf("system message count = %d, want 2 (join + leave)", msgCount)
	}
}

func TestService_LeaveRoom_NotOnStage(t *testing.T) {
	db := openTestDB(t)
	defer func() { _ = db.Close() }()
	truncate(t, db)
	ps, _, _ := newTestService(t, db)

	ctx := context.Background()
	hostID := seedUser(t, db, "+861380000001D")
	userID := seedUser(t, db, "+861380000001E")
	roomID := seedRoom(t, db, hostID, 8)

	_, err := ps.LeaveRoom(ctx, roomID, userID)
	if !errors.Is(err, ErrNotOnStage) {
		t.Errorf("err = %v, want ErrNotOnStage", err)
	}
}

func TestService_LeaveRoom_RejoinOK(t *testing.T) {
	db := openTestDB(t)
	defer func() { _ = db.Close() }()
	truncate(t, db)
	ps, _, _ := newTestService(t, db)

	ctx := context.Background()
	hostID := seedUser(t, db, "+861380000001F")
	userID := seedUser(t, db, "+8613800000020")
	roomID := seedRoom(t, db, hostID, 8)

	// join → leave → join
	if _, err := ps.JoinRoom(ctx, roomID, userID); err != nil {
		t.Fatalf("first join: %v", err)
	}
	if _, err := ps.LeaveRoom(ctx, roomID, userID); err != nil {
		t.Fatalf("leave: %v", err)
	}
	if _, err := ps.JoinRoom(ctx, roomID, userID); err != nil {
		t.Errorf("rejoin: %v", err)
	}
}

func TestService_ListOnStageByRoom(t *testing.T) {
	db := openTestDB(t)
	defer func() { _ = db.Close() }()
	truncate(t, db)
	ps, _, _ := newTestService(t, db)

	ctx := context.Background()
	hostID := seedUser(t, db, "+8613800000021")
	roomID := seedRoom(t, db, hostID, 8)
	u1 := seedUser(t, db, "+8613800000022")
	u2 := seedUser(t, db, "+8613800000023")
	u3 := seedUser(t, db, "+8613800000024")

	for _, uid := range []string{u1, u2, u3} {
		if _, err := ps.JoinRoom(ctx, roomID, uid); err != nil {
			t.Fatalf("JoinRoom %s: %v", uid, err)
		}
	}
	// u2 leaves.
	if _, err := ps.LeaveRoom(ctx, roomID, u2); err != nil {
		t.Fatalf("LeaveRoom: %v", err)
	}

	parts, err := ps.ListOnStageByRoom(ctx, roomID)
	if err != nil {
		t.Fatalf("ListOnStageByRoom: %v", err)
	}
	if len(parts) != 2 {
		t.Errorf("len = %d, want 2", len(parts))
	}
	for _, p := range parts {
		if p.StageState != "on_stage" {
			t.Errorf("participant %s stage = %q, want on_stage", p.UserID, p.StageState)
		}
	}
}

func TestService_GetOnStageParticipant(t *testing.T) {
	db := openTestDB(t)
	defer func() { _ = db.Close() }()
	truncate(t, db)
	ps, _, _ := newTestService(t, db)

	ctx := context.Background()
	hostID := seedUser(t, db, "+8613800000025")
	userID := seedUser(t, db, "+8613800000026")
	roomID := seedRoom(t, db, hostID, 8)

	if _, err := ps.JoinRoom(ctx, roomID, userID); err != nil {
		t.Fatalf("JoinRoom: %v", err)
	}
	got, err := ps.GetOnStageParticipant(ctx, roomID, userID)
	if err != nil {
		t.Fatalf("GetOnStageParticipant: %v", err)
	}
	if got.StageState != "on_stage" {
		t.Errorf("StageState = %q, want on_stage", got.StageState)
	}
}

func TestService_GetOnStageParticipant_NotFound(t *testing.T) {
	db := openTestDB(t)
	defer func() { _ = db.Close() }()
	truncate(t, db)
	ps, _, _ := newTestService(t, db)

	ctx := context.Background()
	hostID := seedUser(t, db, "+8613800000027")
	roomID := seedRoom(t, db, hostID, 8)

	_, err := ps.GetOnStageParticipant(ctx, roomID, "01HXY0000000000000000000Z")
	if !errors.Is(err, ErrNotOnStage) {
		t.Errorf("err = %v, want ErrNotOnStage", err)
	}
}