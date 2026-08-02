// Package rooms — Service unit tests (Sprint 1 WP-2).
//
// Q18: integration tests use a local Postgres test DB (`fireside_test`),
// shared across tests; each test TRUNCATEs relevant tables in TestMain.
// We do NOT spin up testcontainers per test (too slow for Sprint 1).
//
// Required env:
//   FIRESIDE_TEST_DSN=postgres://fireside:devpassword@localhost:5432/fireside_test?sslmode=disable
// If unset, tests are skipped (so `go test ./...` works without Postgres).
package rooms

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib" // registers "pgx" database/sql driver

	"github.com/ishadowland/fireside/internal/store"
)

const testDSNEnv = "FIRESIDE_TEST_DSN"

// testDSN returns the test DSN from env. Empty if not set.
func testDSN() string {
	return os.Getenv(testDSNEnv)
}

// openTestDB opens the test DB or skips the test if DSN unset.
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

// truncate clears test data. CASCADE handles FK dependencies. We do NOT
// touch schema_migrations — migrations are applied out-of-band.
func truncate(t *testing.T, db *sql.DB) {
	t.Helper()
	if _, err := db.ExecContext(context.Background(),
		`TRUNCATE TABLE messages, participants, rooms, auth_tokens, users RESTART IDENTITY CASCADE`,
	); err != nil {
		t.Fatalf("truncate: %v", err)
	}
}

// newTestService builds a Service wired to the test DB. The caller is
// responsible for calling truncate between tests.
func newTestService(t *testing.T, db *sql.DB) (*Service, *store.Queries) {
	t.Helper()
	q := store.New(db)
	s := NewService(q, nil)
	return s, q
}

func TestService_CreateRoom_OK(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	truncate(t, db)
	svc, q := newTestService(t, db)

	// Need a host user (FK constraint).
	hostID := "01HXY0000000000000000000A"
	if _, err := db.ExecContext(context.Background(),
		`INSERT INTO users (id, phone, display_name) VALUES ($1, $2, 'Host')`,
		hostID, "+8613800000001",
	); err != nil {
		t.Fatalf("seed host user: %v", err)
	}

	ctx := context.Background()
	got, err := svc.CreateRoom(ctx, hostID, CreateRoomRequest{
		Name:              "Test Room",
		MaxParticipants:   8,
		KeepMessagesOnEnd: true,
	})
	if err != nil {
		t.Fatalf("CreateRoom: %v", err)
	}
	if got.Name != "Test Room" {
		t.Errorf("Name = %q, want %q", got.Name, "Test Room")
	}
	if got.MaxParticipants != 8 {
		t.Errorf("MaxParticipants = %d, want 8", got.MaxParticipants)
	}
	if got.Status != "active" {
		t.Errorf("Status = %q, want %q", got.Status, "active")
	}
	if !got.KeepMessagesOnEnd {
		t.Errorf("KeepMessagesOnEnd = false, want true")
	}
	if got.HostUserID != hostID {
		t.Errorf("HostUserID = %q, want %q", got.HostUserID, hostID)
	}
	if got.EndedAt != nil {
		t.Errorf("EndedAt = %v, want nil", got.EndedAt)
	}
	if got.Announcement != "" {
		t.Errorf("Announcement = %q, want \"\"", got.Announcement)
	}

	// Verify DB row.
	row, err := q.GetRoom(ctx, got.ID)
	if err != nil {
		t.Fatalf("GetRoom back: %v", err)
	}
	if row.Name != "Test Room" {
		t.Errorf("row.Name = %q, want %q", row.Name, "Test Room")
	}
}

func TestService_CreateRoom_EmptyName(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	truncate(t, db)
	svc, _ := newTestService(t, db)

	// Even without a host user (FK would catch it), empty-name validation
	// must trigger first and return a friendly error.
	_, err := svc.CreateRoom(context.Background(), "01HXY0000000000000000000HST",
		CreateRoomRequest{Name: "", MaxParticipants: 4})
	if err == nil {
		t.Fatal("expected error for empty name, got nil")
	}
}

func TestService_CreateRoom_MaxParticipantsOutOfRange(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	truncate(t, db)
	svc, _ := newTestService(t, db)

	for _, n := range []int32{0, 51} {
		_, err := svc.CreateRoom(context.Background(), "01HXY0000000000000000000HST",
			CreateRoomRequest{Name: "x", MaxParticipants: n})
		if err == nil {
			t.Errorf("expected error for max_participants=%d, got nil", n)
		}
	}
}

func TestService_GetRoom_NotFound(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	truncate(t, db)
	svc, _ := newTestService(t, db)

	_, _, err := svc.GetRoom(context.Background(), "01HXY00000000000000000NONE")
	if !errors.Is(err, ErrRoomNotFound) {
		t.Errorf("err = %v, want ErrRoomNotFound", err)
	}
}

func TestService_EndRoom_NotHost(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	truncate(t, db)
	svc, _ := newTestService(t, db)

	ctx := context.Background()
	hostID := "01HXY0000000000000000000B"
	otherID := "01HXY0000000000000000000C"
	for _, id := range []string{hostID, otherID} {
		if _, err := db.ExecContext(ctx,
			`INSERT INTO users (id, phone, display_name) VALUES ($1, $2, $3)`,
			id, "+86"+id[len(id)-4:], "user-"+id[len(id)-4:]); err != nil {
			t.Fatalf("seed user %s: %v", id, err)
		}
	}
	room, err := svc.CreateRoom(ctx, hostID, CreateRoomRequest{Name: "r", MaxParticipants: 4})
	if err != nil {
		t.Fatalf("CreateRoom: %v", err)
	}
	err = svc.EndRoom(ctx, otherID, room.ID)
	if !errors.Is(err, ErrNotHost) {
		t.Errorf("err = %v, want ErrNotHost", err)
	}
}

func TestService_EndRoom_OK(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	truncate(t, db)
	svc, q := newTestService(t, db)

	ctx := context.Background()
	hostID := "01HXY0000000000000000000D"
	if _, err := db.ExecContext(ctx,
		`INSERT INTO users (id, phone, display_name) VALUES ($1, '+8613800000002', 'Host2')`,
		hostID); err != nil {
		t.Fatalf("seed: %v", err)
	}
	room, err := svc.CreateRoom(ctx, hostID, CreateRoomRequest{Name: "r2", MaxParticipants: 4})
	if err != nil {
		t.Fatalf("CreateRoom: %v", err)
	}
	if err := svc.EndRoom(ctx, hostID, room.ID); err != nil {
		t.Fatalf("EndRoom: %v", err)
	}

	// Verify status flipped.
	row, err := q.GetRoom(ctx, room.ID)
	if err != nil {
		t.Fatalf("GetRoom after end: %v", err)
	}
	if !row.Status.Valid || row.Status.String != "ended" {
		t.Errorf("Status = %v, want ended", row.Status)
	}
	if !row.EndedAt.Valid {
		t.Errorf("EndedAt not set after EndRoom")
	}

	// Double-end should fail.
	if err := svc.EndRoom(ctx, hostID, room.ID); !errors.Is(err, ErrRoomEnded) {
		t.Errorf("second EndRoom err = %v, want ErrRoomEnded", err)
	}
}

func TestService_ListActive(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	truncate(t, db)
	svc, _ := newTestService(t, db)

	ctx := context.Background()
	hostID := "01HXY0000000000000000000E"
	if _, err := db.ExecContext(ctx,
		`INSERT INTO users (id, phone, display_name) VALUES ($1, '+8613800000003', 'H3')`,
		hostID); err != nil {
		t.Fatalf("seed: %v", err)
	}
	for i := 0; i < 3; i++ {
		if _, err := svc.CreateRoom(ctx, hostID, CreateRoomRequest{
			Name: "room", MaxParticipants: 4,
		}); err != nil {
			t.Fatalf("CreateRoom #%d: %v", i, err)
		}
	}
	// End one room to verify ListActive filters it out.
	rooms, err := svc.ListActive(ctx, 0)
	if err != nil {
		t.Fatalf("ListActive: %v", err)
	}
	if len(rooms) != 3 {
		t.Errorf("len = %d, want 3", len(rooms))
	}
	// End the first one.
	if err := svc.EndRoom(ctx, hostID, rooms[0].ID); err != nil {
		t.Fatalf("EndRoom: %v", err)
	}
	rooms2, err := svc.ListActive(ctx, 0)
	if err != nil {
		t.Fatalf("ListActive after end: %v", err)
	}
	if len(rooms2) != 2 {
		t.Errorf("len after end = %d, want 2", len(rooms2))
	}
	for _, r := range rooms2 {
		if r.Status != "active" {
			t.Errorf("room %s status = %q, want active", r.ID, r.Status)
		}
	}
}