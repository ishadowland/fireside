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
	"strings"
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
	mu            sync.Mutex
	users         map[string]store.User
	tokens        map[string]store.AuthToken // jti string -> row (access)
	refreshTokens map[string]store.InsertRefreshTokenParams
	replaced      map[string]string // refresh jti -> replacement jti (rotation tracking)
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		users:         make(map[string]store.User),
		tokens:        make(map[string]store.AuthToken),
		refreshTokens: make(map[string]store.InsertRefreshTokenParams),
		replaced:      make(map[string]string),
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

// newTestEngineWithStore mirrors newTestEngine but also returns the
// fakeStore so a test can pre-seed it (refresh tokens, etc.).
func newTestEngineWithStore(t *testing.T) (*gin.Engine, *fakeStore) {
	t.Helper()
	fs := newFakeStore()
	// Defensive: in case newFakeStore forgets to initialize the
	// refresh_tokens map, do it here so test seed code can
	// directly assign to it.
	fs.mu.Lock()
	if fs.refreshTokens == nil {
		fs.refreshTokens = map[string]store.InsertRefreshTokenParams{}
	}
	fs.mu.Unlock()
	r := gin.New()
	Mount(r, Config{
		JWTSecret:      testSecret,
		AccessTokenTTL: 15 * time.Minute,
		Users:          fs,
		Tokens:         fs,
	})
	return r, fs
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


// Refresh-token stubs (issue #9 WP-7.9). The fakeStore now simulates
// rotation tracking (replaced map) so the replay/revoke path in
// RefreshHandler is exercised: a token already in `replaced` behaves
// as if a previous rotation marked it used.
func (f *fakeStore) InsertRefreshToken(_ context.Context, arg store.InsertRefreshTokenParams) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.refreshTokens[arg.JTI] = arg
	return 1, nil
}

func (f *fakeStore) GetRefreshToken(_ context.Context, jti string) (store.RefreshToken, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	row, ok := f.refreshTokens[jti]
	if !ok {
		return store.RefreshToken{}, sql.ErrNoRows
	}
	replacedBy := sql.NullString{Valid: false}
	if repl, ok := f.replaced[jti]; ok {
		replacedBy = sql.NullString{String: repl, Valid: true}
	}
	return store.RefreshToken{
		JTI:           row.JTI,
		UserID:        row.UserID,
		FamilyID:      row.FamilyID,
		ExpiresAt:     row.ExpiresAt,
		ReplacedByJTI: replacedBy,
	}, nil
}

func (f *fakeStore) MarkRefreshTokenReplaced(_ context.Context, jti, replacedBy string) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	row, ok := f.refreshTokens[jti]
	if !ok {
		return 0, nil
	}
	if _, already := f.replaced[jti]; already {
		// Replay: a previous rotation already consumed this token.
		return 0, nil
	}
	f.replaced[jti] = replacedBy
	_ = row
	return 1, nil
}

func (f *fakeStore) DeleteRefreshToken(_ context.Context, jti string) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.refreshTokens[jti]; !ok {
		return 0, nil
	}
	delete(f.refreshTokens, jti)
	delete(f.replaced, jti)
	return 1, nil
}

func (f *fakeStore) DeleteRefreshFamily(_ context.Context, familyID string) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var deleted int64
	for jti, row := range f.refreshTokens {
		if row.FamilyID == familyID {
			delete(f.refreshTokens, jti)
			delete(f.replaced, jti)
			deleted++
		}
	}
	return deleted, nil
}


func TestRefreshHandler_RejectsMissingToken(t *testing.T) {
	gin.SetMode(gin.TestMode)
	hr := newTestEngine(t)
	w := postJSON(t, hr, "/v1/auth/refresh", `{}`)
	if w.Code != 400 {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestRefreshHandler_RejectsUnknownToken(t *testing.T) {
	gin.SetMode(gin.TestMode)
	hr := newTestEngine(t)
	w := postJSON(t, hr, "/v1/auth/refresh", `{"refresh_token":"unknown"}`)
	if w.Code != 401 {
		t.Errorf("status = %d, want 401", w.Code)
	}
}

func TestRefreshHandler_RotatesToken(t *testing.T) {
	gin.SetMode(gin.TestMode)
	hr, fs := newTestEngineWithStore(t)
	userID := "01HREFRESH0000000000000000XX"
	familyID := "01HREFFAMILY00000000000000"
	jti := "01HREFRESHTOKEN00000000000AA"
	fs.refreshTokens[jti] = store.InsertRefreshTokenParams{
		JTI:       jti,
		UserID:    userID,
		FamilyID:  familyID,
		ExpiresAt: sql.NullTime{Time: time.Now().Add(7 * 24 * time.Hour), Valid: true},
	}
	w := postJSON(t, hr, "/v1/auth/refresh", `{"refresh_token":"`+jti+`"}`)
	if w.Code != 200 {
		t.Errorf("status = %d, want 200: %s", w.Code, w.Body.String())
	}
	var body struct {
		Token        string `json:"token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Token == "" {
		t.Error("access token empty")
	}
	if body.RefreshToken == "" || body.RefreshToken == jti {
		t.Errorf("refresh token not rotated: got %q", body.RefreshToken)
	}
	// The new refresh token should be inserted.
	if _, ok := fs.refreshTokens[body.RefreshToken]; !ok {
		t.Error("new refresh token not in store")
	}
	// The access token's jti must be persisted for ADR-0007 replay
	// defense, mirroring the login path (issue #33).
	claims, err := Validate(testSecret, body.Token)
	if err != nil {
		t.Fatalf("validate access token: %v", err)
	}
	if !fs.hasToken(claims.JTI) {
		t.Errorf("access token jti %q not persisted", claims.JTI)
	}
}

func TestRefreshHandler_ReplayRevokesFamily(t *testing.T) {
	gin.SetMode(gin.TestMode)
	hr, fs := newTestEngineWithStore(t)
	userID := "01HREFRESH0000000000000000XX"
	familyID := "01HREFFAMILY00000000000000"
	jti := "01HREFRESHTOKEN00000000000AA"
	fs.refreshTokens[jti] = store.InsertRefreshTokenParams{
		JTI:       jti,
		UserID:    userID,
		FamilyID:  familyID,
		ExpiresAt: sql.NullTime{Time: time.Now().Add(7 * 24 * time.Hour), Valid: true},
	}
	// Simulate a token that a previous rotation already consumed: the
	// mark step will report 0 rows affected, triggering family revoke.
	fs.replaced[jti] = "01HREFRESHTOKEN00000000000BB"

	w := postJSON(t, hr, "/v1/auth/refresh", `{"refresh_token":"`+jti+`"}`)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "refresh_token_replayed") {
		t.Errorf("body = %s, want refresh_token_replayed", w.Body.String())
	}
	// The whole family must be revoked (the replacement token the
	// handler inserted is deleted along with the original).
	fs.mu.Lock()
	defer fs.mu.Unlock()
	if len(fs.refreshTokens) != 0 {
		t.Errorf("family not revoked: refreshTokens = %v", fs.refreshTokens)
	}
	if len(fs.replaced) != 0 {
		t.Errorf("family revocation left replaced tracking: %v", fs.replaced)
	}
}

func TestRefreshHandler_RejectsExpiredToken(t *testing.T) {
	gin.SetMode(gin.TestMode)
	hr, fs := newTestEngineWithStore(t)
	userID := "01HREFRESH0000000000000000XX"
	jti := "01HEXPIREDREFRESH00000000000"
	fs.refreshTokens[jti] = store.InsertRefreshTokenParams{
		JTI:       jti,
		UserID:    userID,
		FamilyID:  jti,
		ExpiresAt: sql.NullTime{Time: time.Now().Add(-1 * time.Hour), Valid: true},
	}
	w := postJSON(t, hr, "/v1/auth/refresh", `{"refresh_token":"`+jti+`"}`)
	if w.Code != 401 {
		t.Errorf("status = %d, want 401", w.Code)
	}
}
