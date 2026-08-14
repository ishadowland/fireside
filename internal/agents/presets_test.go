package agents

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ishadowland/fireside/internal/messages"
	"github.com/ishadowland/fireside/internal/rooms"
)

// TestParseOpenAIErrorWithoutErrorObject guards the nil-pointer fix
// (service.go parseOpenAIResponse): some providers (e.g. NVIDIA NIM)
// answer 429/5xx with a body that has no `error` object. Accessing
// cr.Error.Message used to panic and crash the whole server.
func TestParseOpenAIErrorWithoutErrorObject(t *testing.T) {
	_, err := parseOpenAIResponse(429, strings.NewReader(`{"message":"rate limit"}`))
	if err == nil || !strings.Contains(err.Error(), "HTTP 429") {
		t.Errorf("err = %v, want HTTP 429 fallback (no panic)", err)
	}
	_, err = parseOpenAIResponse(500, strings.NewReader(`{}`))
	if err == nil || !strings.Contains(err.Error(), "HTTP 500") {
		t.Errorf("err = %v, want HTTP 500 fallback (no panic)", err)
	}
	// With an error object, the provider message must be surfaced.
	_, err = parseOpenAIResponse(401, strings.NewReader(`{"error":{"message":"Invalid API key"}}`))
	if err == nil || !strings.Contains(err.Error(), "Invalid API key") {
		t.Errorf("err = %v, want upstream message", err)
	}
	// 2xx with empty choices must not panic either.
	if _, err = parseOpenAIResponse(200, strings.NewReader(`{}`)); err == nil {
		t.Error("200 with empty choices err = nil, want 模型返回了空回复")
	}
}

// presetInput is a valid baseline preset payload for tests.
func presetInput() PresetInput {
	return PresetInput{
		Name:         "本地测试",
		Kind:         ProviderOpenAI,
		BaseURL:      "http://localhost:8787/v1",
		APIToken:     "sk-secret",
		Model:        "gpt-4o-mini",
		SystemPrompt: "你是测试助手",
	}
}

func TestPresetStorePersistRoundtrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agents.local.json")

	s, err := NewPresetStore(path)
	if err != nil {
		t.Fatalf("NewPresetStore: %v", err)
	}
	created, err := s.Create(presetInput())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if len(created.ID) != 26 {
		t.Errorf("preset ID length = %d, want 26 (CHAR(26) column)", len(created.ID))
	}

	// Reload from disk — everything (incl. api token) must roundtrip so the
	// service can use the preset's connection config after a restart.
	loaded, err := NewPresetStore(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	got, ok := loaded.Get(created.ID)
	if !ok {
		t.Fatal("preset missing after reload")
	}
	if got.Name != "本地测试" || got.APIToken != "sk-secret" || got.Model != "gpt-4o-mini" ||
		got.SystemPrompt != "你是测试助手" || got.Kind != ProviderOpenAI {
		t.Errorf("reloaded preset = %+v, want saved values", got)
	}
	if len(loaded.List()) != 1 {
		t.Errorf("len(List()) = %d, want 1", len(loaded.List()))
	}

	// Config file must be 0600 (secrets live here).
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("preset file perms = %o, want 0600", perm)
	}
}

func TestPresetViewMasksToken(t *testing.T) {
	s, _ := NewPresetStore("")
	p, err := s.Create(presetInput())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	views := s.List()
	if len(views) != 1 || !views[0].HasToken {
		t.Fatalf("List() = %+v, want one preset with has_token=true", views)
	}
	raw, _ := s.Get(p.ID)
	if raw.APIToken != "sk-secret" {
		t.Errorf("store Get lost token: %q", raw.APIToken)
	}
}

func TestPresetValidation(t *testing.T) {
	base := presetInput()

	cases := []struct {
		name string
		mut  func(*PresetInput)
	}{
		{"empty name", func(in *PresetInput) { in.Name = "  " }},
		{"empty kind", func(in *PresetInput) { in.Kind = "" }},
		{"unknown kind", func(in *PresetInput) { in.Kind = "claude" }},
		{"no endpoint", func(in *PresetInput) { in.BaseURL = ""; in.Host = "" }},
		{"port too big", func(in *PresetInput) { in.Port = 70000 }},
		{"empty model", func(in *PresetInput) { in.Model = "" }},
		{"prompt too long", func(in *PresetInput) { in.SystemPrompt = strings.Repeat("x", MaxSystemPromptLen+1) }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			in := base
			c.mut(&in)
			if _, err := (&PresetStore{}).Create(in); err == nil {
				t.Errorf("Create(%s) err = nil, want error", c.name)
			}
		})
	}
}

func TestPresetUpdateKeepsTokenOnEmpty(t *testing.T) {
	s, _ := NewPresetStore("")
	p, _ := s.Create(presetInput())

	in := presetInput()
	in.APIToken = "" // keep existing token
	in.Name = "改名后"
	upd, err := s.Update(p.ID, in)
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if upd.APIToken != "sk-secret" {
		t.Errorf("Update with empty token lost token, got %q", upd.APIToken)
	}
	if upd.Name != "改名后" {
		t.Errorf("Update name = %q, want 改名后", upd.Name)
	}

	in.APIToken = "sk-new"
	upd, err = s.Update(p.ID, in)
	if err != nil {
		t.Fatalf("Update #2: %v", err)
	}
	if upd.APIToken != "sk-new" {
		t.Errorf("Update with new token = %q, want sk-new", upd.APIToken)
	}
}

func TestPresetDelete(t *testing.T) {
	s, _ := NewPresetStore("")
	p, _ := s.Create(presetInput())
	if err := s.Delete(p.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, ok := s.Get(p.ID); ok {
		t.Error("preset still present after Delete")
	}
	if err := s.Delete(p.ID); err == nil {
		t.Error("Delete missing preset err = nil, want error")
	}
}

func TestPresetEndpointResolution(t *testing.T) {
	cases := []struct {
		in   Preset
		want string
	}{
		{Preset{BaseURL: "http://localhost:8080/v1/"}, "http://localhost:8080/v1"},
		{Preset{Host: "localhost", Port: 8787}, "http://localhost:8787"},
		{Preset{Host: "http://10.0.0.2:9000"}, "http://10.0.0.2:9000"},
		{Preset{}, ""},
	}
	for _, c := range cases {
		if got := c.in.Endpoint(); got != c.want {
			t.Errorf("Endpoint(%+v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestProviderEndpoint(t *testing.T) {
	if got := providerEndpoint(ProviderSimple, "http://h:1/"); got != "http://h:1" {
		t.Errorf("simple = %q, want http://h:1", got)
	}
	if got := providerEndpoint(ProviderOpenClaw, "http://h:8080"); got != "http://h:8080/api/chat" {
		t.Errorf("openclaw = %q, want http://h:8080/api/chat", got)
	}
	if got := providerEndpoint(ProviderOpenClaw, "http://h:8080/api/chat"); got != "http://h:8080/api/chat" {
		t.Errorf("openclaw idempotent = %q, want verbatim", got)
	}
	if got := providerEndpoint(ProviderOpenAI, "http://h/v1"); got != "http://h/v1/chat/completions" {
		t.Errorf("openai = %q, want /chat/completions", got)
	}
}

// ---- preset invitations (DB-backed) ---------------------------------------

func TestInviteWithPresetPersistsAndState(t *testing.T) {
	db := truncatedDB(t, "agents")
	defer func() { _ = db.Close() }()
	svc, _, _ := newWiredService(t, db)
	ps, _ := NewPresetStore("")
	svc.SetPresets(ps)
	p, _ := ps.Create(presetInput())

	ctx := context.Background()
	hostID := seedUser(t, db, "+8613800000050")
	roomID := seedRoom(t, db, hostID)

	if err := svc.InviteWithPreset(ctx, hostID, roomID, 1, p.ID, 25); err != nil {
		t.Fatalf("InviteWithPreset: %v", err)
	}

	state, err := svc.AgentState(ctx, roomID, 1)
	if err != nil {
		t.Fatalf("AgentState: %v", err)
	}
	if !state.Configured {
		t.Error("AgentState.Configured = false, want true")
	}
	if state.PresetID != p.ID {
		t.Errorf("AgentState.PresetID = %q, want %q", state.PresetID, p.ID)
	}
	if state.PresetName != "本地测试" {
		t.Errorf("AgentState.PresetName = %q, want 本地测试", state.PresetName)
	}
	if state.SystemPrompt != "你是测试助手" {
		t.Errorf("AgentState.SystemPrompt = %q, want preset prompt", state.SystemPrompt)
	}
	if state.CooldownSeconds != 25 {
		t.Errorf("AgentState.CooldownSeconds = %d, want 25", state.CooldownSeconds)
	}

	// The DB row must store the preset reference (and no in-room prompt).
	row, err := svc.q.GetRoomAgent(ctx, roomID, DefaultAgentID)
	if err != nil {
		t.Fatalf("GetRoomAgent: %v", err)
	}
	if row.AgentPresetID != p.ID {
		t.Errorf("room_agents.agent_preset_id = %q, want %q", row.AgentPresetID, p.ID)
	}
	if row.SystemPrompt != "" {
		t.Errorf("room_agents.system_prompt = %q, want empty for preset invite", row.SystemPrompt)
	}
}

func TestInviteWithPresetUnknownPreset(t *testing.T) {
	db := truncatedDB(t, "agents")
	defer func() { _ = db.Close() }()
	svc, _, _ := newWiredService(t, db)
	ps, _ := NewPresetStore("")
	svc.SetPresets(ps)

	ctx := context.Background()
	hostID := seedUser(t, db, "+8613800000051")
	roomID := seedRoom(t, db, hostID)

	if err := svc.InviteWithPreset(ctx, hostID, roomID, 1, "01HXY00000000000000000XXXX", 0); !errors.Is(err, messages.ErrInvalidArg) {
		t.Errorf("InviteWithPreset unknown err = %v, want messages.ErrInvalidArg", err)
	}
	if err := svc.InviteWithPreset(ctx, hostID, roomID, 1, "", 0); !errors.Is(err, messages.ErrInvalidArg) {
		t.Errorf("InviteWithPreset empty err = %v, want messages.ErrInvalidArg", err)
	}
}

func TestInviteWithPresetUnwiredStore(t *testing.T) {
	db := truncatedDB(t, "agents")
	defer func() { _ = db.Close() }()
	svc, _, _ := newWiredService(t, db)
	// NOTE: SetPresets never called.

	ctx := context.Background()
	hostID := seedUser(t, db, "+8613800000052")
	roomID := seedRoom(t, db, hostID)

	if err := svc.InviteWithPreset(ctx, hostID, roomID, 1, "01HXY000000000000000000000", 0); !errors.Is(err, messages.ErrInvalidArg) {
		t.Errorf("InviteWithPreset unwired err = %v, want messages.ErrInvalidArg", err)
	}
}

func TestInviteWithPresetHostChecks(t *testing.T) {
	db := truncatedDB(t, "agents")
	defer func() { _ = db.Close() }()
	svc, _, _ := newWiredService(t, db)
	ps, _ := NewPresetStore("")
	svc.SetPresets(ps)
	p, _ := ps.Create(presetInput())

	ctx := context.Background()
	hostID := seedUser(t, db, "+8613800000053")
	otherID := seedUser(t, db, "+8613800000054")
	roomID := seedRoom(t, db, hostID)

	if err := svc.InviteWithPreset(ctx, otherID, roomID, 1, p.ID, 0); !errors.Is(err, rooms.ErrNotHost) {
		t.Errorf("non-host err = %v, want rooms.ErrNotHost", err)
	}
	ended := seedEndedRoom(t, db, hostID)
	if err := svc.InviteWithPreset(ctx, hostID, ended, 1, p.ID, 0); !errors.Is(err, messages.ErrRoomEnded) {
		t.Errorf("ended room err = %v, want messages.ErrRoomEnded", err)
	}
}

// TestReplyToRoomUsesPresetConfig proves a preset invitation drives the
// model call with the preset's own endpoint/token/model/prompt — the fake
// server asserts the token, and the reply echoes the preset prompt.
func TestReplyToRoomUsesPresetConfig(t *testing.T) {
	db := truncatedDB(t, "agents")
	defer func() { _ = db.Close() }()
	svc, ms, _ := newWiredService(t, db)

	srv := replyToServer(t) // asserts Bearer sk-test, echoes system msg
	defer srv.Close()

	ps, _ := NewPresetStore("")
	svc.SetPresets(ps)
	p, err := ps.Create(PresetInput{
		Name:         "预置助手",
		Kind:         ProviderOpenAI,
		BaseURL:      srv.URL + "/v1",
		APIToken:     "sk-test",
		Model:        "gpt-4o-mini",
		SystemPrompt: "预置系统提示",
	})
	if err != nil {
		t.Fatalf("Create preset: %v", err)
	}

	ctx := context.Background()
	hostID := seedUser(t, db, "+8613800000055")
	roomID := seedRoom(t, db, hostID)
	seedParticipant(t, db, roomID, hostID)
	if err := svc.InviteWithPreset(ctx, hostID, roomID, 1, p.ID, 0); err != nil {
		t.Fatalf("InviteWithPreset: %v", err)
	}
	if _, err := ms.CreateMessage(ctx, hostID, roomID, messages.CreateMessageRequest{Content: "你好"}); err != nil {
		t.Fatalf("CreateMessage: %v", err)
	}

	svc.replyToRoom(roomID, 1)

	views, _, _ := ms.ListMessagesByRoom(ctx, roomID, "", 10)
	reply := views[0]
	if reply.SenderKind != "agent" {
		t.Fatalf("reply SenderKind = %q, want agent", reply.SenderKind)
	}
	if !strings.Contains(reply.Content, "预置系统提示") {
		t.Errorf("agent reply = %q, want it to echo the preset prompt", reply.Content)
	}
}

// TestReplyToRoomPresetDeletedFallsBackToGlobal: deleting the preset after
// an invitation must not break the reply — it falls back to the global
// in-memory config with the room's (empty) prompt → default personality.
func TestReplyToRoomPresetDeletedFallsBackToGlobal(t *testing.T) {
	db := truncatedDB(t, "agents")
	defer func() { _ = db.Close() }()
	svc, ms, _ := newWiredService(t, db)

	srv := replyToServer(t)
	defer srv.Close()
	svc.SetConfig(&Config{BaseURL: srv.URL + "/v1", APIKey: "sk-test", Model: "gpt-4o-mini"})

	ps, _ := NewPresetStore("")
	svc.SetPresets(ps)
	p, _ := ps.Create(presetInput())

	ctx := context.Background()
	hostID := seedUser(t, db, "+8613800000056")
	roomID := seedRoom(t, db, hostID)
	seedParticipant(t, db, roomID, hostID)
	if err := svc.InviteWithPreset(ctx, hostID, roomID, 1, p.ID, 0); err != nil {
		t.Fatalf("InviteWithPreset: %v", err)
	}
	if err := ps.Delete(p.ID); err != nil {
		t.Fatalf("Delete preset: %v", err)
	}
	if _, err := ms.CreateMessage(ctx, hostID, roomID, messages.CreateMessageRequest{Content: "你好"}); err != nil {
		t.Fatalf("CreateMessage: %v", err)
	}

	svc.replyToRoom(roomID, 1)

	views, _, _ := ms.ListMessagesByRoom(ctx, roomID, "", 10)
	if len(views) != 2 {
		t.Fatalf("len(views) = %d, want 2 (fallback reply expected)", len(views))
	}
	if views[0].SenderKind != "agent" {
		t.Errorf("reply SenderKind = %q, want agent", views[0].SenderKind)
	}
	// Preset was deleted → default prompt applied (fake echoes system msg).
	if !strings.Contains(views[0].Content, "AI 助手") {
		t.Errorf("reply = %q, want default prompt echoed", views[0].Content)
	}
}

func TestPresetPing(t *testing.T) {
	srv := fakeChatServer(t)
	defer srv.Close()

	svc := NewService(nil, nil, nil, nil, nil)
	ps, _ := NewPresetStore("")
	svc.SetPresets(ps)
	p, _ := ps.Create(PresetInput{
		Name:     "ping-me",
		Kind:     ProviderOpenAI,
		BaseURL:  srv.URL + "/v1",
		APIToken: "sk-test",
		Model:    "gpt-4o-mini",
	})

	latency, err := svc.PresetPing(context.Background(), p.ID)
	if err != nil {
		t.Fatalf("PresetPing: %v", err)
	}
	if latency < 0 {
		t.Errorf("latency = %d, want >= 0", latency)
	}

	if _, err := svc.PresetPing(context.Background(), "01HXY00000000000000000XXXX"); err == nil {
		t.Error("PresetPing unknown id err = nil, want error")
	}
}
