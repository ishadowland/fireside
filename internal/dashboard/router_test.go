package dashboard

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestMain(m *testing.M) {
	gin.SetMode(gin.TestMode)
	m.Run()
}

func newTestRouter() *gin.Engine {
	r := gin.New()
	Mount(r, Config{StubCode: "1234"})
	return r
}

// doLoopback performs a request pretending to come from a loopback address.
func doLoopback(r *gin.Engine, method, path string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	req.RemoteAddr = "127.0.0.1:54321"
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// doRemote performs a request pretending to come from a non-loopback address.
func doRemote(r *gin.Engine, method, path string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	req.RemoteAddr = "203.0.113.7:54321"
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestIndexServedOnLoopback(t *testing.T) {
	r := newTestRouter()
	w := doLoopback(r, http.MethodGet, "/dashboard/")
	if w.Code != http.StatusOK {
		t.Fatalf("GET /dashboard/ = %d, want 200", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("content type = %q, want text/html", ct)
	}
	if !strings.Contains(w.Body.String(), "Fireside Dashboard") {
		t.Error("index page missing expected heading")
	}
}

func TestIndexRedirectOnLoopback(t *testing.T) {
	r := newTestRouter()
	w := doLoopback(r, http.MethodGet, "/dashboard")
	if w.Code != http.StatusOK && w.Code != http.StatusMovedPermanently {
		t.Fatalf("GET /dashboard = %d, want 200 or 301", w.Code)
	}
}

func TestStaticAssetsOnLoopback(t *testing.T) {
	eng := newTestRouter()
	for _, path := range []string{"/dashboard/static/app.js", "/dashboard/static/style.css"} {
		w := doLoopback(eng, http.MethodGet, path)
		if w.Code != http.StatusOK {
			t.Errorf("GET %s = %d, want 200", path, w.Code)
		}
	}
}

func TestDashboardBlockedForRemote(t *testing.T) {
	r := newTestRouter()
	for _, path := range []string{"/dashboard/", "/dashboard/static/app.js", "/v1/dashboard/config"} {
		w := doRemote(r, http.MethodGet, path)
		if w.Code != http.StatusNotFound {
			t.Errorf("remote GET %s = %d, want 404", path, w.Code)
		}
	}
}

func TestConfigEndpoint(t *testing.T) {
	r := newTestRouter()
	w := doLoopback(r, http.MethodGet, "/v1/dashboard/config")
	if w.Code != http.StatusOK {
		t.Fatalf("GET /v1/dashboard/config = %d, want 200", w.Code)
	}
	var body struct {
		StubCode string `json:"stub_code"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode config: %v", err)
	}
	if body.StubCode != "1234" {
		t.Errorf("stub_code = %q, want 1234", body.StubCode)
	}
}
