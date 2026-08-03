package auth

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/ishadowland/fireside/internal/store"
)

// isULID matches the canonical 26-char Crockford ULID form.
var ulidPattern = regexp.MustCompile(`^[0123456789ABCDEFGHJKMNPQRSTVWXYZ]{26}$`)

func isULID(s string) bool { return ulidPattern.MatchString(s) }

func TestMain(m *testing.M) {
	gin.SetMode(gin.TestMode)
	os.Exit(m.Run())
}

// fakeStore is an in-memory auth.UserStore so handler tests don't need a DB.
type fakeStore struct {
	mu     sync.Mutex
	users  map[string]store.User
	tokens map[string]store.AuthToken // jti string -> row
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		users:  make(map[string]store.User),
		tokens: make(map[string]store.AuthToken),
	}
}

func (f *fakeStore) GetUserByPhone(_ context.Context, phone string) (store.User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	u, ok := f.users[phone]
	if !ok {
		return store.User{}, sql.ErrNoRows
	}
	return u, nil
}

func (f *fakeStore) InsertUser(_ context.Context, arg store.InsertUserParams) (store.User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	u := store.User{ID: arg.ID, Phone: arg.Phone}
	if existing, dup := f.users[arg.Phone]; dup {
		// Issue #21 fix support: the production code now catches
		// SQLSTATE 23505 and retries via GetUserByPhone. The
		// fakeStore simulates the same by returning a
		// pgconn.PgError(Code="23505") on duplicate phone, which
		// is exactly what the Postgres server returns.
		_ = existing
		return store.User{}, &pgconn.PgError{
			Code:    "23505",
			Message: "duplicate key value violates unique constraint \"idx_user_phone\"",
		}
	}
	f.users[arg.Phone] = u
	return u, nil
}

func (f *fakeStore) InsertToken(_ context.Context, arg store.InsertTokenParams) (store.AuthToken, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	tok := store.AuthToken{Jti: arg.Jti, UserID: arg.UserID, ExpiresAt: arg.ExpiresAt}
	f.tokens[arg.Jti.String()] = tok
	return tok, nil
}

// hasToken returns true if a jti was persisted (test helper).
func (f *fakeStore) hasToken(jti string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, ok := f.tokens[jti]
	return ok
}

func newTestEngine(t *testing.T) *gin.Engine {
	t.Helper()
	fs := newFakeStore()
	r := gin.New()
	Mount(r, Config{
		JWTSecret:      testSecret,
		AccessTokenTTL: 15 * time.Minute,
		Users:          fs,
		Tokens:         fs,
	})
	return r
}

func postJSON(t *testing.T, r *gin.Engine, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestLoginHandlerHappyPath(t *testing.T) {
	t.Parallel()
	r := newTestEngine(t)
	w := postJSON(t, r, "/v1/auth/login", `{"phone":"+8613800138000","code":"1234"}`)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200; body=%s", w.Code, w.Body.String())
	}

	var resp LoginResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Token == "" {
		t.Error("token empty")
	}
	if resp.ExpiresIn != 900 {
		t.Errorf("expires_in: got %d, want 900", resp.ExpiresIn)
	}

	// Token must roundtrip-validate against the same secret.
	claims, err := Validate(testSecret, resp.Token)
	if err != nil {
		t.Fatalf("issued token failed Validate: %v", err)
	}
	if claims.UserID == "" {
		t.Error("claims.UserID should be non-empty for a known phone")
	}
	if !isULID(claims.UserID) {
		t.Errorf("claims.UserID = %q, want valid 26-char ULID", claims.UserID)
	}
}

func TestLoginHandlerWrongCode(t *testing.T) {
	t.Parallel()
	r := newTestEngine(t)
	w := postJSON(t, r, "/v1/auth/login", `{"phone":"+8613800138000","code":"0000"}`)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status: got %d, want 401", w.Code)
	}
	var body map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["error"] != "invalid_credentials" {
		t.Errorf("error: got %q, want invalid_credentials", body["error"])
	}
}

func TestLoginHandlerBadPhone(t *testing.T) {
	t.Parallel()
	r := newTestEngine(t)
	w := postJSON(t, r, "/v1/auth/login", `{"phone":"not-a-phone","code":"1234"}`)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want 400; body=%s", w.Code, w.Body.String())
	}
	var body map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["error"] != "invalid_request" {
		t.Errorf("error: got %q, want invalid_request", body["error"])
	}
}

func TestLoginHandlerDeterministicUserID(t *testing.T) {
	t.Parallel()
	r := newTestEngine(t)

	w1 := postJSON(t, r, "/v1/auth/login", `{"phone":"+8613800138000","code":"1234"}`)
	w2 := postJSON(t, r, "/v1/auth/login", `{"phone":"+8613800138000","code":"1234"}`)
	if w1.Code != 200 || w2.Code != 200 {
		t.Fatalf("both calls should succeed; got %d, %d", w1.Code, w2.Code)
	}

	var r1, r2 LoginResponse
	if err := json.Unmarshal(w1.Body.Bytes(), &r1); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(w2.Body.Bytes(), &r2); err != nil {
		t.Fatal(err)
	}

	c1, err := Validate(testSecret, r1.Token)
	if err != nil {
		t.Fatal(err)
	}
	c2, err := Validate(testSecret, r2.Token)
	if err != nil {
		t.Fatal(err)
	}
	if c1.UserID != c2.UserID {
		t.Errorf("UserID should be deterministic; got %q then %q", c1.UserID, c2.UserID)
	}
}

// TestLoginHandlerAutoRegisters verifies the store is consulted: a first
// login persists the user (insert), a second login reuses the row (lookup).
func TestLoginHandlerAutoRegisters(t *testing.T) {
	t.Parallel()
	fs := newFakeStore()
	r := gin.New()
	Mount(r, Config{
		JWTSecret:      testSecret,
		AccessTokenTTL: 15 * time.Minute,
		Users:          fs,
	})

	const phone = "+8613900001111"
	w1 := postJSON(t, r, "/v1/auth/login", `{"phone":"`+phone+`","code":"1234"}`)
	if w1.Code != 200 {
		t.Fatalf("first login = %d, want 200; body=%s", w1.Code, w1.Body.String())
	}

	// The user row must now exist in the fake store (auto-registered).
	u, err := fs.GetUserByPhone(context.Background(), phone)
	if err != nil {
		t.Fatalf("user should have been auto-registered: %v", err)
	}
	if u.Phone != phone {
		t.Errorf("stored phone = %q, want %q", u.Phone, phone)
	}

	// A second login must find the existing row (insert count unchanged).
	var insertCount int
	for range fs.users {
		insertCount++
	}
	if insertCount != 1 {
		t.Errorf("expected exactly 1 user row, got %d", insertCount)
	}

	w2 := postJSON(t, r, "/v1/auth/login", `{"phone":"`+phone+`","code":"1234"}`)
	if w2.Code != 200 {
		t.Fatalf("second login = %d, want 200", w2.Code)
	}
}

// TestLoginHandlerPersistsJTI verifies Sprint 1-2 replay defense: each
// successful login inserts the JWT's jti into the token store so the WS
// first-frame auth can verify the token was actually issued.
func TestLoginHandlerPersistsJTI(t *testing.T) {
	t.Parallel()
	fs := newFakeStore()
	r := gin.New()
	Mount(r, Config{
		JWTSecret:      testSecret,
		AccessTokenTTL: 15 * time.Minute,
		Users:          fs,
		Tokens:         fs,
	})

	body := postJSON(t, r, "/v1/auth/login", `{"phone":"+8613900002222","code":"1234"}`)
	if body.Code != 200 {
		t.Fatalf("login = %d, want 200; body=%s", body.Code, body.Body.String())
	}

	var resp LoginResponse
	if err := json.Unmarshal(body.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}

	claims, err := Validate(testSecret, resp.Token)
	if err != nil {
		t.Fatalf("issued token failed Validate: %v", err)
	}
	if !fs.hasToken(claims.JTI) {
		t.Errorf("jti %q was not persisted; WS replay defense would reject it", claims.JTI)
	}
	if len(fs.tokens) != 1 {
		t.Errorf("expected 1 token row, got %d", len(fs.tokens))
	}
}

// TestLoginHandlerNilTokensKeepsLegacyBehavior: with Tokens=nil the
// handler must still issue a token (preserves Sprint 0 contract for
// callers that haven't wired the store yet).
func TestLoginHandlerNilTokensKeepsLegacyBehavior(t *testing.T) {
	t.Parallel()
	r := gin.New()
	Mount(r, Config{
		JWTSecret:      testSecret,
		AccessTokenTTL: 15 * time.Minute,
		Users:          newFakeStore(),
		// Tokens deliberately nil.
	})

	body := postJSON(t, r, "/v1/auth/login", `{"phone":"+8613900003333","code":"1234"}`)
	if body.Code != 200 {
		t.Fatalf("login = %d, want 200; body=%s", body.Code, body.Body.String())
	}
}
// TestLoginHandler_ConcurrentFirstLogin_Idempotent (issue #21 fix) verifies
// that two simultaneous logins for the same brand-new phone both succeed
// and return the SAME user_id.
//
// The test does NOT exercise a real DB race; it uses a fakeStore
// configured to simulate SQLSTATE 23505 on duplicate phone inserts —
// the production code's pgerrcode.IsUniqueViolation catch then retries
// via GetUserByPhone, which returns the existing row.
//
// A real-DB integration version is in
// internal/auth/handler_race_test.go (skipped when FIRESIDE_TEST_DSN is
// unset).
func TestLoginHandler_ConcurrentFirstLogin_Idempotent(t *testing.T) {
	t.Parallel()
	fs := newFakeStore()
	r := gin.New()
	Mount(r, Config{
		JWTSecret:      testSecret,
		AccessTokenTTL: 15 * time.Minute,
		Users:          fs,
	})

	const phone = "+8613800009999"

	// Fire 20 simultaneous login requests for the same new phone.
	// Only the first should win the INSERT; the rest must see a
	// unique violation and recover via GetUserByPhone. All 20 must
	// succeed and produce the same user_id (decoded from the JWT).
	const N = 20
	var wg sync.WaitGroup
	userIDs := make([]string, N)
	codes := make([]int, N)
	wg.Add(N)
	for i := 0; i < N; i++ {
		i := i
		go func() {
			defer wg.Done()
			body := postJSON(t, r, "/v1/auth/login",
				`{"phone":"`+phone+`","code":"1234"}`)
			codes[i] = body.Code
			var resp LoginResponse
			if err := json.Unmarshal(body.Body.Bytes(), &resp); err == nil {
				userIDs[i] = userIDFromToken(t, resp.Token)
			}
		}()
	}
	wg.Wait()

	for i := 0; i < N; i++ {
		if codes[i] != http.StatusOK {
			t.Errorf("goroutine %d: status = %d, want 200", i, codes[i])
		}
		if userIDs[i] == "" {
			t.Errorf("goroutine %d: user_id is empty (login failed?)", i)
		}
	}
	// All results must be the same user_id (idempotent under race).
	first := userIDs[0]
	for i := 1; i < N; i++ {
		if userIDs[i] != first {
			t.Errorf("goroutine %d: user_id = %q, want %q (race not handled — issue #21 not fixed)", i, userIDs[i], first)
		}
	}
}

// userIDFromToken parses the JWT (signed with testSecret in this
// package) and returns the uid claim. Used by race tests.
func userIDFromToken(t *testing.T, tokenStr string) string {
	t.Helper()
	claims, err := Validate(testSecret, tokenStr)
	if err != nil {
		t.Fatalf("validate test token: %v", err)
	}
	return claims.UserID
}
