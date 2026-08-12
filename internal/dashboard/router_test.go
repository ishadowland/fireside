package dashboard

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestMain(m *testing.M) {
	gin.SetMode(gin.TestMode)
	os.Exit(m.Run())
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

func TestRoomsPageServedOnLoopback(t *testing.T) {
	r := newTestRouter()
	w := doLoopback(r, http.MethodGet, "/dashboard/rooms")
	if w.Code != http.StatusOK {
		t.Fatalf("GET /dashboard/rooms = %d, want 200", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("content type = %q, want text/html", ct)
	}
	if !strings.Contains(w.Body.String(), "Fireside") {
		t.Error("rooms page missing brand title")
	}
	if !strings.Contains(w.Body.String(), "/dashboard/static/rooms.js") {
		t.Error("rooms page missing rooms.js script reference")
	}
}

func TestRoomPageServedOnLoopback(t *testing.T) {
	r := newTestRouter()
	w := doLoopback(r, http.MethodGet, "/dashboard/rooms/01ABC")
	if w.Code != http.StatusOK {
		t.Fatalf("GET /dashboard/rooms/01ABC = %d, want 200", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("content type = %q, want text/html", ct)
	}
	if !strings.Contains(w.Body.String(), "/dashboard/static/room.js") {
		t.Error("room page missing room.js script reference")
	}
	if !strings.Contains(w.Body.String(), "/dashboard/static/lib.js") {
		t.Error("room page missing lib.js script reference")
	}
}

func TestWP8StaticAssetsOnLoopback(t *testing.T) {
	r := newTestRouter()
	for _, p := range []string{
		"/dashboard/static/lib.js",
		"/dashboard/static/rooms.js",
		"/dashboard/static/room.js",
	} {
		w := doLoopback(r, http.MethodGet, p)
		if w.Code != http.StatusOK {
			t.Errorf("GET %s = %d, want 200", p, w.Code)
		}
	}
}

func TestWP8RoomPageBlockedForRemote(t *testing.T) {
	r := newTestRouter()
	for _, p := range []string{
		"/dashboard/rooms",
		"/dashboard/rooms/01ABC",
		"/dashboard/static/rooms.js",
	} {
		w := doRemote(r, http.MethodGet, p)
		if w.Code != http.StatusNotFound {
			t.Errorf("remote GET %s = %d, want 404", p, w.Code)
		}
	}
}

func TestCheckPageServedOnLoopback(t *testing.T) {
	r := newTestRouter()
	w := doLoopback(r, http.MethodGet, "/dashboard/check")
	if w.Code != http.StatusOK {
		t.Fatalf("GET /dashboard/check = %d, want 200", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("content type = %q, want text/html", ct)
	}
	if !strings.Contains(w.Body.String(), "接口自检") {
		t.Error("check page missing title")
	}
	if !strings.Contains(w.Body.String(), "/dashboard/static/check.js") {
		t.Error("check page missing check.js script reference")
	}
	if !strings.Contains(w.Body.String(), "/dashboard/static/lib.js") {
		t.Error("check page missing lib.js script reference")
	}
}

func TestCheckPageBlockedForRemote(t *testing.T) {
	r := newTestRouter()
	for _, p := range []string{"/dashboard/check", "/dashboard/static/check.js"} {
		w := doRemote(r, http.MethodGet, p)
		if w.Code != http.StatusNotFound {
			t.Errorf("remote GET %s = %d, want 404", p, w.Code)
		}
	}
}

func TestDashboardTestPhonesAreValidE164(t *testing.T) {
	// Regression: rooms.js/room.js once shipped a masked placeholder
	// "+861****8000", which fails LoginRequest's `e164` binding and makes
	// every dashboard login return 400. Every TEST_PHONE constant across
	// the embedded assets must be a valid E.164 number.
	e164 := regexp.MustCompile(`^\+[1-9][0-9]{6,14}$`)
	pat := regexp.MustCompile(`(?:TEST_PHONE|PHONE_A|PHONE_B)\s*=\s*"([^"]+)"`)
	for _, name := range []string{"assets/app.js", "assets/rooms.js", "assets/room.js", "assets/check.js"} {
		data, err := assets.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		ms := pat.FindAllStringSubmatch(string(data), -1)
		if len(ms) == 0 {
			t.Errorf("%s: no TEST_PHONE constant found", name)
			continue
		}
		for _, m := range ms {
			if !e164.MatchString(m[1]) {
				t.Errorf("%s: TEST_PHONE %q is not a valid E.164 number", name, m[1])
			}
		}
	}
}

func TestAdminPageServedOnLoopback(t *testing.T) {
	r := newTestRouter()
	w := doLoopback(r, http.MethodGet, "/dashboard/admin")
	if w.Code != http.StatusOK {
		t.Fatalf("GET /dashboard/admin = %d, want 200", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("content type = %q, want text/html", ct)
	}
	for _, want := range []string{"管理后台", "/dashboard/static/admin.js", "/dashboard/static/lib.js"} {
		if !strings.Contains(w.Body.String(), want) {
			t.Errorf("admin page missing %q", want)
		}
	}
}

func TestAdminStaticAndPageBlockedForRemote(t *testing.T) {
	r := newTestRouter()
	for _, p := range []string{"/dashboard/admin", "/dashboard/static/admin.js"} {
		w := doRemote(r, http.MethodGet, p)
		if w.Code != http.StatusNotFound {
			t.Errorf("remote GET %s = %d, want 404", p, w.Code)
		}
	}
	w := doLoopback(r, http.MethodGet, "/dashboard/static/admin.js")
	if w.Code != http.StatusOK {
		t.Errorf("GET admin.js = %d, want 200", w.Code)
	}
}

func TestLibJSExposesFiresideGlobal(t *testing.T) {
	r := newTestRouter()
	w := doLoopback(r, http.MethodGet, "/dashboard/static/lib.js")
	if w.Code != http.StatusOK {
		t.Fatalf("GET lib.js = %d, want 200", w.Code)
	}
	// Smoke check: ensure the file declares the Fireside helper module.
	required := []string{"Fireside", "openWS", "login", "jwtFetch", "escapeHtml"}
	for _, kw := range required {
		if !strings.Contains(w.Body.String(), kw) {
			t.Errorf("lib.js missing helper %q", kw)
		}
	}
}
