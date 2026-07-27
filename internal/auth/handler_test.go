package auth

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestMain(m *testing.M) {
	gin.SetMode(gin.TestMode)
	m.Run()
}

func newTestEngine(t *testing.T) *gin.Engine {
	t.Helper()
	r := gin.New()
	Mount(r, Config{
		JWTSecret:      testSecret,
		AccessTokenTTL: 15 * time.Minute,
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