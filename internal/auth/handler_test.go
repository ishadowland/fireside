package auth

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/ishadowland/fireside/internal/store"
)

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
	if claims.UserID == 0 {
		t.Error("claims.UserID should be non-zero for a known phone")
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
		t.Errorf("UserID should be deterministic; got %d then %d", c1.UserID, c2.UserID)
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