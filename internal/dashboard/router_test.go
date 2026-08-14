package dashboard

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/ishadowland/fireside/internal/agents"
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

// doLoopbackBody is doLoopback with a JSON request body.
func doLoopbackBody(r *gin.Engine, method, path string, body []byte) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
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

// TestDashboardJSDecodesUidClaim guards the host-control bug: the JWT
// carries the user id in the `uid` claim (ADR-0014), while `sub` is the
// constant "fireside-user". If a page decodes `.sub` as the user id,
// hostUserId !== meId forever and host-only controls (在场 移除 AI 助手,
// agent invite/mute, free-speech) never render.
func TestDashboardJSDecodesUidClaim(t *testing.T) {
	r := newTestRouter()
	for _, asset := range []string{"room.js", "rooms.js", "check.js"} {
		w := doLoopback(r, http.MethodGet, "/dashboard/static/"+asset)
		if w.Code != http.StatusOK {
			t.Fatalf("GET %s = %d, want 200", asset, w.Code)
		}
		body := w.Body.String()
		if strings.Contains(body, "payload.user_id || payload.sub") {
			t.Errorf("%s still falls back to `sub` (constant fireside-user) — host checks will fail", asset)
		}
		switch asset {
		case "room.js", "rooms.js":
			if !strings.Contains(body, "payload.uid") {
				t.Errorf("%s must read the JWT `uid` claim (payload.uid)", asset)
			}
		case "check.js":
			if !strings.Contains(body, ".uid") {
				t.Errorf("check.js must decode the JWT `uid` claim")
			}
		}
	}
}

// ---- AI test-config endpoints (方式1 agents hook) --------------------------

func newAITestRouter(t *testing.T) (*gin.Engine, *agents.Service) {
	t.Helper()
	svc := agents.NewService(nil, nil, nil, nil, nil)
	r := gin.New()
	Mount(r, Config{StubCode: "1234", Agents: svc})
	return r, svc
}

func TestAIConfigGetBeforeSet(t *testing.T) {
	r, _ := newAITestRouter(t)
	w := doLoopback(r, http.MethodGet, "/v1/dashboard/ai-config")
	if w.Code != http.StatusOK {
		t.Fatalf("GET ai-config = %d, want 200", w.Code)
	}
	var body struct {
		Configured bool   `json:"configured"`
		BaseURL    string `json:"base_url"`
		Model      string `json:"model"`
		HasKey     bool   `json:"has_key"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Configured {
		t.Error("configured = true before SetConfig")
	}
	if body.HasKey {
		t.Error("has_key = true before SetConfig")
	}
}

func TestAIConfigSetAndGet(t *testing.T) {
	r, _ := newAITestRouter(t)
	// Set.
	body, _ := json.Marshal(gin.H{
		"base_url": "https://api.openai.com/v1/chat/completions/",
		"api_key":  "sk-secret",
		"model":    "gpt-4o-mini",
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/dashboard/ai-config", bytes.NewReader(body))
	req.RemoteAddr = "127.0.0.1:54321"
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("POST ai-config = %d, want 200; body=%s", w.Code, w.Body.String())
	}

	// Get must reflect configured, base_url (normalized, no /chat/completions),
	// model, has_key — but NEVER the key itself.
	w2 := doLoopback(r, http.MethodGet, "/v1/dashboard/ai-config")
	if w2.Code != http.StatusOK {
		t.Fatalf("GET ai-config = %d, want 200", w2.Code)
	}
	var resp struct {
		Configured bool   `json:"configured"`
		BaseURL    string `json:"base_url"`
		Model      string `json:"model"`
		HasKey     bool   `json:"has_key"`
	}
	if err := json.Unmarshal(w2.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !resp.Configured {
		t.Error("configured = false after SetConfig")
	}
	if resp.BaseURL != "https://api.openai.com/v1" {
		t.Errorf("base_url = %q, want normalized https://api.openai.com/v1", resp.BaseURL)
	}
	if resp.Model != "gpt-4o-mini" {
		t.Errorf("model = %q, want gpt-4o-mini", resp.Model)
	}
	if !resp.HasKey {
		t.Error("has_key = false after SetConfig with key")
	}
	if strings.Contains(w2.Body.String(), "sk-secret") {
		t.Error("ai-config GET leaked the API key")
	}
}

func TestAIConfigSetInvalid(t *testing.T) {
	r, _ := newAITestRouter(t)
	body, _ := json.Marshal(gin.H{"base_url": "", "model": ""})
	req := httptest.NewRequest(http.MethodPost, "/v1/dashboard/ai-config", bytes.NewReader(body))
	req.RemoteAddr = "127.0.0.1:54321"
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("POST invalid ai-config = %d, want 400", w.Code)
	}
}

func TestAIPingUnconfigured(t *testing.T) {
	r, _ := newAITestRouter(t)
	w := doLoopback(r, http.MethodPost, "/v1/dashboard/ai-ping")
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("ai-ping unconfigured = %d, want 503", w.Code)
	}
}

func TestAIConfigEndpointsBlockedForRemote(t *testing.T) {
	r, _ := newAITestRouter(t)
	for _, p := range []string{
		"/v1/dashboard/ai-config",
		"/v1/dashboard/ai-ping",
	} {
		w := doRemote(r, http.MethodGet, p)
		if w.Code != http.StatusNotFound {
			t.Errorf("remote GET %s = %d, want 404", p, w.Code)
		}
	}
}

func TestAIConfigWithoutAgentsWiring(t *testing.T) {
	r := newTestRouter() // Config has no Agents
	w := doLoopback(r, http.MethodGet, "/v1/dashboard/ai-config")
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("GET ai-config without Agents = %d, want 503", w.Code)
	}
}

func TestAIPingEndToEnd(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`))
	}))
	defer srv.Close()

	svc := agents.NewService(nil, nil, nil, nil, nil)
	svc.SetConfig(&agents.Config{BaseURL: srv.URL, APIKey: "sk-test", Model: "gpt-4o-mini"})
	r := gin.New()
	Mount(r, Config{StubCode: "1234", Agents: svc})

	w := doLoopback(r, http.MethodPost, "/v1/dashboard/ai-ping")
	if w.Code != http.StatusOK {
		t.Fatalf("ai-ping = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		OK        bool  `json:"ok"`
		LatencyMs int64 `json:"latency_ms"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !resp.OK {
		t.Error("ok = false")
	}
}

// TestAgentManagerHermesRoundtrip guards 方式2: a hermes preset created via
// the loopback Agent 管理器 persists agent_id/session_key, and the list
// view never leaks the api token.
func TestAgentManagerHermesRoundtrip(t *testing.T) {
	r, svc := newAITestRouter(t)
	ps, err := agents.NewPresetStore("")
	if err != nil {
		t.Fatalf("NewPresetStore: %v", err)
	}
	svc.SetPresets(ps)
	body, _ := json.Marshal(gin.H{
		"name":        "Hermes 本地",
		"kind":        "hermes",
		"base_url":    "http://localhost:8642/v1",
		"api_token":   "sk-hermes",
		"model":       "hermes-agent",
		"agent_id":    "analyst",
		"session_key": "conv:roomA:1",
		"system_prompt": "你是测试助手",
	})
	w := doLoopbackBody(r, http.MethodPost, "/v1/dashboard/agents", body)
	if w.Code != http.StatusOK {
		t.Fatalf("POST /agents = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	var created agents.PresetView
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode created: %v", err)
	}
	if created.Kind != agents.ProviderHermes {
		t.Errorf("kind = %q, want hermes", created.Kind)
	}
	if created.AgentID != "analyst" {
		t.Errorf("agent_id = %q, want analyst", created.AgentID)
	}
	if created.SessionKey != "conv:roomA:1" {
		t.Errorf("session_key = %q, want conv:roomA:1", created.SessionKey)
	}
	if strings.Contains(w.Body.String(), "sk-hermes") {
		t.Error("create response leaked the api token")
	}

	// List view: same fields visible, token masked.
	w2 := doLoopback(r, http.MethodGet, "/v1/dashboard/agents")
	if w2.Code != http.StatusOK {
		t.Fatalf("GET /agents = %d, want 200", w2.Code)
	}
	if strings.Contains(w2.Body.String(), "sk-hermes") {
		t.Error("list leaked the api token")
	}
	if !strings.Contains(w2.Body.String(), "analyst") || !strings.Contains(w2.Body.String(), "conv:roomA:1") {
		t.Error("list missing agent_id/session_key")
	}

	// Invitation referencing the hermes preset resolves its config.
	if svc == nil {
		t.Fatal("svc nil")
	}
	view, err := svc.PresetGet(created.ID)
	if err != nil {
		t.Fatalf("PresetGet: %v", err)
	}
	if view.ID != created.ID {
		t.Errorf("PresetGet id = %q, want %q", view.ID, created.ID)
	}
}
