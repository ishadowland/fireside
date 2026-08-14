package agents

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ishadowland/fireside/internal/messages"
	"github.com/ishadowland/fireside/internal/rooms"
	"github.com/ishadowland/fireside/internal/store"
	"github.com/ishadowland/fireside/internal/testutil"
)

// TestDefaultAgentIDLength guards the CHAR(26) constraint on messages.sender_id.
func TestDefaultAgentIDLength(t *testing.T) {
	if len(DefaultAgentID) != 26 {
		t.Fatalf("DefaultAgentID length = %d, want 26 (sender_id CHAR(26))", len(DefaultAgentID))
	}
}

// TestSecondAgentID guards the CHAR(26) constraint and ensures the two
// slots use distinct sender ids.
func TestSecondAgentID(t *testing.T) {
	if len(SecondAgentID) != 26 {
		t.Fatalf("SecondAgentID length = %d, want 26 (sender_id CHAR(26))", len(SecondAgentID))
	}
	if SecondAgentID == DefaultAgentID {
		t.Fatal("SecondAgentID must differ from DefaultAgentID")
	}
	if agentIDForSlot(1) != DefaultAgentID || agentIDForSlot(2) != SecondAgentID {
		t.Errorf("agentIDForSlot mapping wrong: 1=%q 2=%q", agentIDForSlot(1), agentIDForSlot(2))
	}
}

func TestInvalidSlotAndCooldown(t *testing.T) {
	db := truncatedDB(t, "agents")
	defer func() { _ = db.Close() }()
	svc, _, _ := newWiredService(t, db)

	ctx := context.Background()
	hostID := seedUser(t, db, "+8613800000011")
	roomID := seedRoom(t, db, hostID)

	for _, slot := range []int{0, 3, -1} {
		if err := svc.Invite(ctx, hostID, roomID, slot, "p", 0); !errors.Is(err, messages.ErrInvalidArg) {
			t.Errorf("Invite slot %d err = %v, want messages.ErrInvalidArg", slot, err)
		}
	}
	if err := svc.Invite(ctx, hostID, roomID, 1, "p", MaxCooldownSeconds+1); !errors.Is(err, messages.ErrInvalidArg) {
		t.Errorf("Invite cooldown too large err = %v, want messages.ErrInvalidArg", err)
	}
	if err := svc.Remove(ctx, hostID, roomID, 0); !errors.Is(err, messages.ErrInvalidArg) {
		t.Errorf("Remove slot 0 err = %v, want messages.ErrInvalidArg", err)
	}
	if _, err := svc.AgentState(ctx, roomID, 4); !errors.Is(err, messages.ErrInvalidArg) {
		t.Errorf("AgentState slot 4 err = %v, want messages.ErrInvalidArg", err)
	}
}

func TestConfigSetGet(t *testing.T) {
	s := NewService(nil, nil, nil, nil, nil)
	if s.Configured() {
		t.Error("Configured() = true before SetConfig")
	}
	if c := s.Config(); c != nil {
		t.Errorf("Config() = %+v, want nil before SetConfig", c)
	}

	s.SetConfig(&Config{BaseURL: "https://api.example.com/v1", APIKey: "sk-x", Model: "gpt-4o-mini"})
	if !s.Configured() {
		t.Error("Configured() = false after SetConfig")
	}
	c := s.Config()
	if c.BaseURL != "https://api.example.com/v1" || c.Model != "gpt-4o-mini" || c.APIKey != "sk-x" {
		t.Errorf("Config() = %+v, want saved values", c)
	}

	// Config returns a copy — mutating it must not affect the service.
	c.APIKey = "tampered"
	if got := s.Config().APIKey; got != "sk-x" {
		t.Errorf("Config() returned non-copy (APIKey = %q)", got)
	}

	s.SetConfig(nil)
	if s.Configured() {
		t.Error("Configured() = true after SetConfig(nil)")
	}
}

func TestChatEndpoint(t *testing.T) {
	cases := []struct{ in, want string }{
		{"https://api.openai.com/v1", "https://api.openai.com/v1/chat/completions"},
		{"https://api.openai.com/v1/", "https://api.openai.com/v1/chat/completions"},
		{"https://api.openai.com", "https://api.openai.com/chat/completions"},
		{"https://api.openai.com/v1/chat/completions", "https://api.openai.com/v1/chat/completions"},
	}
	for _, c := range cases {
		if got := chatEndpoint(c.in); got != c.want {
			t.Errorf("chatEndpoint(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// fakeChatServer returns an OpenAI-compatible chat/completions endpoint.
// It echoes the first system message back as the reply, so tests can
// assert that the per-room prompt actually reached the model.
func fakeChatServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("path = %q, want /v1/chat/completions", r.URL.Path)
		}
		if auth := r.Header.Get("Authorization"); auth != "Bearer sk-test" {
			t.Errorf("Authorization = %q, want Bearer sk-test", auth)
		}
		var req chatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode request: %v", err)
		}
		system := ""
		for i := len(req.Messages) - 1; i >= 0; i-- {
			if req.Messages[i].Role == "system" {
				system = req.Messages[i].Content
				break
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{map[string]any{
				"message": map[string]any{"role": "assistant", "content": "回复:" + system},
			}},
		})
	}))
	return srv
}

func TestChatSuccess(t *testing.T) {
	srv := fakeChatServer(t)
	defer srv.Close()

	s := NewService(nil, nil, nil, nil, nil)
	s.SetConfig(&Config{BaseURL: srv.URL + "/v1", APIKey: "sk-test", Model: "gpt-4o-mini"})

	ctx := context.Background()
	reply, err := s.chat(ctx, s.Config(), []chatMessage{{Role: "user", Content: "你好"}})
	if err != nil {
		t.Fatalf("chat: %v", err)
	}
	if !strings.HasPrefix(reply, "回复:") {
		t.Errorf("reply = %q, want prefix 回复:", reply)
	}
}

func TestChatErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{"message": "Invalid API key"},
		})
	}))
	defer srv.Close()

	s := NewService(nil, nil, nil, nil, nil)
	s.SetConfig(&Config{BaseURL: srv.URL, APIKey: "bad", Model: "gpt-4o-mini"})

	_, err := s.chat(context.Background(), s.Config(), []chatMessage{{Role: "user", Content: "hi"}})
	if err == nil || !strings.Contains(err.Error(), "Invalid API key") {
		t.Errorf("err = %v, want Invalid API key message", err)
	}
}

func TestPing(t *testing.T) {
	srv := fakeChatServer(t)
	defer srv.Close()

	s := NewService(nil, nil, nil, nil, nil)
	s.SetConfig(&Config{BaseURL: srv.URL + "/v1", APIKey: "sk-test", Model: "gpt-4o-mini"})

	latency, err := s.Ping(context.Background())
	if err != nil {
		t.Fatalf("Ping: %v", err)
	}
	if latency < 0 {
		t.Errorf("latency = %d, want >= 0", latency)
	}
}

func TestPingUnconfigured(t *testing.T) {
	s := NewService(nil, nil, nil, nil, nil)
	if _, err := s.Ping(context.Background()); err == nil {
		t.Error("Ping() = nil error when unconfigured, want error")
	}
}

func TestBuildChat(t *testing.T) {
	// ListMessagesByRoom returns newest-first; buildChat reverses into
	// chronological order and drops system messages.
	hist := []struct {
		kind, content string
	}{
		{"human", "第二条"}, // newest
		{"system", `{"event":"join"}`},
		{"agent", "机器人回"},
		{"human", "第一条"}, // oldest
	}
	views := make([]messages.MessageView, 0, len(hist))
	for _, h := range hist {
		views = append(views, messages.MessageView{SenderKind: h.kind, Content: h.content})
	}

	got := buildChat("你是助手", views)
	// Expect: system prompt, then 第一条(human), 机器人回(agent), 第二条(human).
	want := []struct{ role, content string }{
		{"system", "你是助手"},
		{"user", "第一条"},
		{"assistant", "机器人回"},
		{"user", "第二条"},
	}
	if len(got) != len(want) {
		t.Fatalf("buildChat returned %d msgs, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i].Role != want[i].role || got[i].Content != want[i].content {
			t.Errorf("msg[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
}

// ---- DB-backed tests (invite → trigger, skip without FIRESIDE_TEST_DSN) --

func truncatedDB(t *testing.T, pkg string) *sql.DB {
	t.Helper()
	db := testutil.OpenTestDB(t, pkg)
	if _, err := db.ExecContext(context.Background(),
		`TRUNCATE TABLE messages, participants, room_agents, rooms, auth_tokens, users RESTART IDENTITY CASCADE`,
	); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	return db
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

func seedRoom(t *testing.T, db *sql.DB, hostID string) string {
	t.Helper()
	id := "01HXY000000000000000000R0"
	if _, err := db.ExecContext(context.Background(),
		`INSERT INTO rooms (id, host_user_id, name, max_participants) VALUES ($1, $2, 'test-room', 8)`,
		id, hostID); err != nil {
		t.Fatalf("seed room: %v", err)
	}
	return id
}

func seedEndedRoom(t *testing.T, db *sql.DB, hostID string) string {
	t.Helper()
	id := "01HXYENDED00000000000000R0"
	if _, err := db.ExecContext(context.Background(),
		`INSERT INTO rooms (id, host_user_id, name, max_participants, status, ended_at) VALUES ($1, $2, 'ended-test', 8, 'ended', NOW())`,
		id, hostID); err != nil {
		t.Fatalf("seed ended room: %v", err)
	}
	return id
}

func seedParticipant(t *testing.T, db *sql.DB, roomID, userID string) {
	t.Helper()
	if _, err := db.ExecContext(context.Background(),
		`INSERT INTO participants (id, room_id, user_id) VALUES ($1, $2, $3)`,
		"01HXY000000000000000000P0", roomID, userID); err != nil {
		t.Fatalf("seed participant: %v", err)
	}
}

func newWiredService(t *testing.T, db *sql.DB) (*Service, *messages.Service, *rooms.Service) {
	t.Helper()
	q := store.New(db)
	rs := rooms.NewService(q, nil)
	ms := messages.NewService(q, rs, nil)
	return NewService(q, ms, rs, nil, nil), ms, rs
}

func TestInviteSetsPromptAndState(t *testing.T) {
	db := truncatedDB(t, "agents")
	defer func() { _ = db.Close() }()
	svc, _, _ := newWiredService(t, db)

	ctx := context.Background()
	hostID := seedUser(t, db, "+8613800000001")
	roomID := seedRoom(t, db, hostID)

	prompt := "你是萤火助手，回答必须简短。"
	if err := svc.Invite(ctx, hostID, roomID, 1, prompt, 0); err != nil {
		t.Fatalf("Invite: %v", err)
	}

	state, err := svc.AgentState(ctx, roomID, 1)
	if err != nil {
		t.Fatalf("AgentState: %v", err)
	}
	if !state.Configured {
		t.Error("AgentState.Configured = false after invite, want true")
	}
	if state.AgentID != DefaultAgentID {
		t.Errorf("AgentState.AgentID = %q, want %q", state.AgentID, DefaultAgentID)
	}
	if state.SystemPrompt != prompt {
		t.Errorf("AgentState.SystemPrompt = %q, want %q", state.SystemPrompt, prompt)
	}
	if state.CooldownSeconds != 0 {
		t.Errorf("AgentState.CooldownSeconds = %d, want 0", state.CooldownSeconds)
	}

	// Re-invite replaces the prompt and cooldown.
	if err := svc.Invite(ctx, hostID, roomID, 1, "新提示词", 30); err != nil {
		t.Fatalf("re-Invite: %v", err)
	}
	state, _ = svc.AgentState(ctx, roomID, 1)
	if state.SystemPrompt != "新提示词" {
		t.Errorf("after re-invite SystemPrompt = %q, want %q", state.SystemPrompt, "新提示词")
	}
	if state.CooldownSeconds != 30 {
		t.Errorf("after re-invite CooldownSeconds = %d, want 30", state.CooldownSeconds)
	}
}

// TestInviteSecondSlot ensures slot 2 is an independent AI assistant with
// its own prompt + cooldown.
func TestInviteSecondSlot(t *testing.T) {
	db := truncatedDB(t, "agents")
	defer func() { _ = db.Close() }()
	svc, _, _ := newWiredService(t, db)

	ctx := context.Background()
	hostID := seedUser(t, db, "+8613800000012")
	roomID := seedRoom(t, db, hostID)

	prompt := "你是二号助手"
	if err := svc.Invite(ctx, hostID, roomID, 2, prompt, 15); err != nil {
		t.Fatalf("Invite slot 2: %v", err)
	}

	state, err := svc.AgentState(ctx, roomID, 2)
	if err != nil {
		t.Fatalf("AgentState slot 2: %v", err)
	}
	if !state.Configured {
		t.Error("AgentState(2).Configured = false, want true")
	}
	if state.AgentID != SecondAgentID {
		t.Errorf("AgentState(2).AgentID = %q, want %q", state.AgentID, SecondAgentID)
	}
	if state.SystemPrompt != prompt || state.CooldownSeconds != 15 {
		t.Errorf("AgentState(2) = %+v, want prompt %q cooldown 15", state, prompt)
	}

	// Slot 1 must be unaffected.
	if state1, err := svc.AgentState(ctx, roomID, 1); err != nil || state1.Configured {
		t.Errorf("AgentState(1) after slot-2 invite = %+v err=%v, want not configured", state1, err)
	}
}

func TestRemoveIsPerSlot(t *testing.T) {
	db := truncatedDB(t, "agents")
	defer func() { _ = db.Close() }()
	svc, _, _ := newWiredService(t, db)

	ctx := context.Background()
	hostID := seedUser(t, db, "+8613800000013")
	roomID := seedRoom(t, db, hostID)

	if err := svc.Invite(ctx, hostID, roomID, 1, "p1", 0); err != nil {
		t.Fatalf("Invite slot 1: %v", err)
	}
	if err := svc.Invite(ctx, hostID, roomID, 2, "p2", 0); err != nil {
		t.Fatalf("Invite slot 2: %v", err)
	}
	if err := svc.Remove(ctx, hostID, roomID, 2); err != nil {
		t.Fatalf("Remove slot 2: %v", err)
	}
	if state, _ := svc.AgentState(ctx, roomID, 2); state.Configured {
		t.Error("slot 2 still configured after Remove")
	}
	if state, _ := svc.AgentState(ctx, roomID, 1); !state.Configured {
		t.Error("slot 1 was removed too, want it kept")
	}
}

func TestAgentStateNoInvite(t *testing.T) {
	db := truncatedDB(t, "agents")
	defer func() { _ = db.Close() }()
	svc, _, _ := newWiredService(t, db)

	ctx := context.Background()
	hostID := seedUser(t, db, "+8613800000002")
	roomID := seedRoom(t, db, hostID)

	state, err := svc.AgentState(ctx, roomID, 1)
	if err != nil {
		t.Fatalf("AgentState (no invite): %v", err)
	}
	if state.Configured {
		t.Error("AgentState.Configured = true without invite, want false")
	}
}

func TestAgentStateRoomNotFound(t *testing.T) {
	db := truncatedDB(t, "agents")
	defer func() { _ = db.Close() }()
	svc, _, _ := newWiredService(t, db)

	_, err := svc.AgentState(context.Background(), "01HXYNOPE0000000000000000", 1)
	if !errors.Is(err, rooms.ErrRoomNotFound) {
		t.Errorf("err = %v, want rooms.ErrRoomNotFound", err)
	}
}

func TestInviteRequiresHost(t *testing.T) {
	db := truncatedDB(t, "agents")
	defer func() { _ = db.Close() }()
	svc, _, _ := newWiredService(t, db)

	ctx := context.Background()
	hostID := seedUser(t, db, "+8613800000003")
	otherID := seedUser(t, db, "+8613800000004")
	roomID := seedRoom(t, db, hostID)

	err := svc.Invite(ctx, otherID, roomID, 1, "prompt", 0)
	if !errors.Is(err, rooms.ErrNotHost) {
		t.Errorf("Invite non-host err = %v, want rooms.ErrNotHost", err)
	}
}

func TestInviteRoomNotFoundAndEnded(t *testing.T) {
	db := truncatedDB(t, "agents")
	defer func() { _ = db.Close() }()
	svc, _, _ := newWiredService(t, db)

	ctx := context.Background()
	hostID := seedUser(t, db, "+8613800000005")

	if err := svc.Invite(ctx, hostID, "01HXYNOPE0000000000000000", 1, "p", 0); !errors.Is(err, rooms.ErrRoomNotFound) {
		t.Errorf("Invite missing room err = %v, want rooms.ErrRoomNotFound", err)
	}

	endedRoom := seedEndedRoom(t, db, hostID)
	if err := svc.Invite(ctx, hostID, endedRoom, 1, "p", 0); !errors.Is(err, messages.ErrRoomEnded) {
		t.Errorf("Invite ended room err = %v, want messages.ErrRoomEnded", err)
	}
}

func TestInvitePromptTooLong(t *testing.T) {
	db := truncatedDB(t, "agents")
	defer func() { _ = db.Close() }()
	svc, _, _ := newWiredService(t, db)

	ctx := context.Background()
	hostID := seedUser(t, db, "+8613800000006")
	roomID := seedRoom(t, db, hostID)

	err := svc.Invite(ctx, hostID, roomID, 1, strings.Repeat("x", MaxSystemPromptLen+1), 0)
	if !errors.Is(err, messages.ErrInvalidArg) {
		t.Errorf("Invite long prompt err = %v, want messages.ErrInvalidArg", err)
	}
}

func TestRemove(t *testing.T) {
	db := truncatedDB(t, "agents")
	defer func() { _ = db.Close() }()
	svc, _, _ := newWiredService(t, db)

	ctx := context.Background()
	hostID := seedUser(t, db, "+8613800000007")
	roomID := seedRoom(t, db, hostID)
	if err := svc.Invite(ctx, hostID, roomID, 1, "p", 0); err != nil {
		t.Fatalf("Invite: %v", err)
	}
	if err := svc.Remove(ctx, hostID, roomID, 1); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	state, err := svc.AgentState(ctx, roomID, 1)
	if err != nil {
		t.Fatalf("AgentState after Remove: %v", err)
	}
	if state.Configured {
		t.Error("AgentState.Configured = true after Remove, want false")
	}
}

// replyToServer returns the first system message as the reply so tests
// can assert the per-room prompt actually reached the model call.
func replyToServer(t *testing.T) *httptest.Server {
	t.Helper()
	return fakeChatServer(t)
}

func TestReplyToRoomUsesInvitedPrompt(t *testing.T) {
	db := truncatedDB(t, "agents")
	defer func() { _ = db.Close() }()
	svc, ms, _ := newWiredService(t, db)

	srv := replyToServer(t)
	defer srv.Close()
	svc.SetConfig(&Config{BaseURL: srv.URL + "/v1", APIKey: "sk-test", Model: "gpt-4o-mini"})

	ctx := context.Background()
	hostID := seedUser(t, db, "+8613800000008")
	roomID := seedRoom(t, db, hostID)
	seedParticipant(t, db, roomID, hostID)
	prompt := "你是萤火助手"
	if err := svc.Invite(ctx, hostID, roomID, 1, prompt, 0); err != nil {
		t.Fatalf("Invite: %v", err)
	}
	if _, err := ms.CreateMessage(ctx, hostID, roomID, messages.CreateMessageRequest{Content: "你好"}); err != nil {
		t.Fatalf("CreateMessage: %v", err)
	}

	svc.replyToRoom(roomID, 1)

	views, _, err := ms.ListMessagesByRoom(ctx, roomID, "", 10)
	if err != nil {
		t.Fatalf("ListMessagesByRoom: %v", err)
	}
	if len(views) != 2 {
		t.Fatalf("len(views) = %d, want 2 (human + agent)", len(views))
	}
	reply := views[0]
	if reply.SenderKind != "agent" {
		t.Errorf("reply SenderKind = %q, want agent", reply.SenderKind)
	}
	// Fake server echoes the system message: the per-room prompt must
	// have reached the model call (方式1 redesign core).
	if !strings.Contains(reply.Content, prompt) {
		t.Errorf("agent reply content = %q, want it to contain the invited prompt %q", reply.Content, prompt)
	}
}

func TestReplyToRoomNoInviteIsSilent(t *testing.T) {
	db := truncatedDB(t, "agents")
	defer func() { _ = db.Close() }()
	svc, ms, _ := newWiredService(t, db)

	srv := replyToServer(t)
	defer srv.Close()
	svc.SetConfig(&Config{BaseURL: srv.URL + "/v1", APIKey: "sk-test", Model: "gpt-4o-mini"})

	ctx := context.Background()
	hostID := seedUser(t, db, "+8613800000009")
	roomID := seedRoom(t, db, hostID)
	seedParticipant(t, db, roomID, hostID)
	// NOTE: no Invite — the agent was never pulled into this room.
	if _, err := ms.CreateMessage(ctx, hostID, roomID, messages.CreateMessageRequest{Content: "你好"}); err != nil {
		t.Fatalf("CreateMessage: %v", err)
	}

	svc.replyToRoom(roomID, 1)

	views, _, err := ms.ListMessagesByRoom(ctx, roomID, "", 10)
	if err != nil {
		t.Fatalf("ListMessagesByRoom: %v", err)
	}
	for _, v := range views {
		if v.SenderKind == "agent" {
			t.Errorf("agent message created without invitation: %+v", v)
		}
	}
}

// TestReplyToRoomSecondSlot proves slot 2 uses its own prompt and sender
// id, independent of slot 1.
func TestReplyToRoomSecondSlot(t *testing.T) {
	db := truncatedDB(t, "agents")
	defer func() { _ = db.Close() }()
	svc, ms, _ := newWiredService(t, db)

	srv := replyToServer(t)
	defer srv.Close()
	svc.SetConfig(&Config{BaseURL: srv.URL + "/v1", APIKey: "sk-test", Model: "gpt-4o-mini"})

	ctx := context.Background()
	hostID := seedUser(t, db, "+8613800000010")
	roomID := seedRoom(t, db, hostID)
	seedParticipant(t, db, roomID, hostID)

	prompt2 := "你是二号萤火"
	if err := svc.Invite(ctx, hostID, roomID, 2, prompt2, 0); err != nil {
		t.Fatalf("Invite slot 2: %v", err)
	}
	if _, err := ms.CreateMessage(ctx, hostID, roomID, messages.CreateMessageRequest{Content: "你好"}); err != nil {
		t.Fatalf("CreateMessage: %v", err)
	}

	svc.replyToRoom(roomID, 2)

	views, _, err := ms.ListMessagesByRoom(ctx, roomID, "", 10)
	if err != nil {
		t.Fatalf("ListMessagesByRoom: %v", err)
	}
	if len(views) != 2 {
		t.Fatalf("len(views) = %d, want 2 (human + agent)", len(views))
	}
	reply := views[0]
	if reply.SenderKind != "agent" || reply.SenderID == nil || *reply.SenderID != SecondAgentID {
		sender := "(nil)"
		if reply.SenderID != nil {
			sender = *reply.SenderID
		}
		t.Errorf("reply = kind %q sender %q, want agent %q", reply.SenderKind, sender, SecondAgentID)
	}
	if !strings.Contains(reply.Content, prompt2) {
		t.Errorf("agent reply content = %q, want it to contain slot-2 prompt %q", reply.Content, prompt2)
	}
}

// TestCooldownGatesReply: after the agent posts one reply, a second turn
// in the same room must be gated while its cooldown (3600s + jitter) is
// still in effect.
func TestCooldownGatesReply(t *testing.T) {
	db := truncatedDB(t, "agents")
	defer func() { _ = db.Close() }()
	svc, ms, _ := newWiredService(t, db)

	srv := replyToServer(t)
	defer srv.Close()
	svc.SetConfig(&Config{BaseURL: srv.URL + "/v1", APIKey: "sk-test", Model: "gpt-4o-mini"})

	ctx := context.Background()
	hostID := seedUser(t, db, "+8613800000020")
	roomID := seedRoom(t, db, hostID)
	seedParticipant(t, db, roomID, hostID)

	if err := svc.Invite(ctx, hostID, roomID, 1, "你是限速助手", MaxCooldownSeconds); err != nil {
		t.Fatalf("Invite: %v", err)
	}
	if _, err := ms.CreateMessage(ctx, hostID, roomID, messages.CreateMessageRequest{Content: "你好"}); err != nil {
		t.Fatalf("CreateMessage: %v", err)
	}

	// First turn: new content + fresh cooldown clock → allowed, posts a reply.
	svc.replyToRoom(roomID, 1)
	views, _, _ := ms.ListMessagesByRoom(ctx, roomID, "", 10)
	if got := countAgents(views); got != 1 {
		t.Fatalf("agent messages after first turn = %d, want 1", got)
	}

	// Second human message brings new content, but the 1h cooldown is
	// still running → the second turn must be gated (no extra reply).
	if _, err := ms.CreateMessage(ctx, hostID, roomID, messages.CreateMessageRequest{Content: "再来一次"}); err != nil {
		t.Fatalf("CreateMessage #2: %v", err)
	}
	svc.replyToRoom(roomID, 1)
	views, _, _ = ms.ListMessagesByRoom(ctx, roomID, "", 10)
	if got := countAgents(views); got != 1 {
		t.Fatalf("agent messages after gated turn = %d, want still 1", got)
	}
}

// TestCooldownIsPerSlot: each (room, agent) carries its own cooldown
// clock — slot 1 being gated must not gate slot 2.
func TestCooldownIsPerSlot(t *testing.T) {
	svc := NewService(nil, nil, nil, nil, nil)
	cd := time.Duration(MaxCooldownSeconds) * time.Second

	if !svc.cooldownAllows("R", DefaultAgentID, cd) {
		t.Fatal("fresh slot 1 should be allowed")
	}
	svc.markSpoke("R", DefaultAgentID)
	if svc.cooldownAllows("R", DefaultAgentID, cd) {
		t.Error("slot 1 must be gated right after speaking (1h cooldown)")
	}
	if !svc.cooldownAllows("R", SecondAgentID, cd) {
		t.Error("slot 2 must not be gated by slot 1's cooldown clock")
	}
}

func countAgents(views []messages.MessageView) int {
	n := 0
	for _, v := range views {
		if v.SenderKind == "agent" {
			n++
		}
	}
	return n
}

func countAgentReplies(ms *messages.Service, ctx context.Context, roomID, agentID string) int {
	views, _, err := ms.ListMessagesByRoom(ctx, roomID, "", 200)
	if err != nil {
		return -1
	}
	n := 0
	for _, v := range views {
		if v.SenderKind == "agent" && v.SenderID != nil && *v.SenderID == agentID {
			n++
		}
	}
	return n
}

// TestMuteValidation covers host-only / room-state / duration rules.
func TestMuteValidation(t *testing.T) {
	db := truncatedDB(t, "agents")
	defer func() { _ = db.Close() }()
	svc, _, _ := newWiredService(t, db)

	ctx := context.Background()
	hostID := seedUser(t, db, "+8613800000040")
	otherID := seedUser(t, db, "+8613800000041")
	roomID := seedRoom(t, db, hostID)

	if err := svc.SetMute(ctx, otherID, roomID, 1, true, 30); !errors.Is(err, rooms.ErrNotHost) {
		t.Errorf("non-host err = %v, want rooms.ErrNotHost", err)
	}
	if err := svc.SetMute(ctx, hostID, "01HXYNOPE0000000000000000", 1, true, 30); !errors.Is(err, rooms.ErrRoomNotFound) {
		t.Errorf("missing room err = %v, want rooms.ErrRoomNotFound", err)
	}
	if err := svc.SetMute(ctx, hostID, roomID, 1, true, 0); !errors.Is(err, messages.ErrInvalidArg) {
		t.Errorf("minutes 0 err = %v, want messages.ErrInvalidArg", err)
	}
	if err := svc.SetMute(ctx, hostID, roomID, 1, true, MaxMuteMinutes+1); !errors.Is(err, messages.ErrInvalidArg) {
		t.Errorf("minutes too big err = %v, want messages.ErrInvalidArg", err)
	}
	if err := svc.SetMute(ctx, hostID, roomID, 0, true, 30); !errors.Is(err, messages.ErrInvalidArg) {
		t.Errorf("bad slot err = %v, want messages.ErrInvalidArg", err)
	}
	ended := seedEndedRoom(t, db, hostID)
	if err := svc.SetMute(ctx, hostID, ended, 1, true, 30); !errors.Is(err, messages.ErrRoomEnded) {
		t.Errorf("ended room err = %v, want messages.ErrRoomEnded", err)
	}
}

// TestMuteState verifies mute shows up in AgentState and clears on unmute.
func TestMuteState(t *testing.T) {
	db := truncatedDB(t, "agents")
	defer func() { _ = db.Close() }()
	svc, _, _ := newWiredService(t, db)

	ctx := context.Background()
	hostID := seedUser(t, db, "+8613800000042")
	roomID := seedRoom(t, db, hostID)
	if err := svc.Invite(ctx, hostID, roomID, 1, "p", 0); err != nil {
		t.Fatalf("Invite: %v", err)
	}

	if err := svc.SetMute(ctx, hostID, roomID, 1, true, 30); err != nil {
		t.Fatalf("SetMute: %v", err)
	}
	state, err := svc.AgentState(ctx, roomID, 1)
	if err != nil {
		t.Fatalf("AgentState: %v", err)
	}
	if !state.Muted {
		t.Errorf("AgentState.Muted = false after mute, want true")
	}
	if state.MutedRemainingSeconds <= 0 || state.MutedRemainingSeconds > 30*60 {
		t.Errorf("MutedRemainingSeconds = %d, want ~30*60", state.MutedRemainingSeconds)
	}

	if err := svc.SetMute(ctx, hostID, roomID, 1, false, 0); err != nil {
		t.Fatalf("unmute: %v", err)
	}
	state, _ = svc.AgentState(ctx, roomID, 1)
	if state.Muted {
		t.Errorf("AgentState.Muted = true after unmute, want false")
	}
}

// TestMuteExpiry verifies a ban lapses on its own once time passes.
func TestMuteExpiry(t *testing.T) {
	db := truncatedDB(t, "agents")
	defer func() { _ = db.Close() }()
	svc, _, _ := newWiredService(t, db)

	svc.mutedMu.Lock()
	svc.muted[agentKey("R", DefaultAgentID)] = time.Now().Add(-time.Second)
	svc.mutedMu.Unlock()

	muted, remaining := svc.muteInfo("R", DefaultAgentID)
	if muted {
		t.Errorf("muteInfo = muted after expiry, want false")
	}
	if remaining != 0 {
		t.Errorf("muteInfo remaining = %d, want 0", remaining)
	}
}

// TestMuteSuppressesReply proves a banned slot does not reply, and replies
// again once unmuted (new content required, mirroring the normal gate).
func TestMuteSuppressesReply(t *testing.T) {
	db := truncatedDB(t, "agents")
	defer func() { _ = db.Close() }()
	svc, ms, _ := newWiredService(t, db)

	srv := replyToServer(t)
	defer srv.Close()
	svc.SetConfig(&Config{BaseURL: srv.URL + "/v1", APIKey: "sk-test", Model: "gpt-4o-mini"})

	ctx := context.Background()
	hostID := seedUser(t, db, "+8613800000043")
	roomID := seedRoom(t, db, hostID)
	seedParticipant(t, db, roomID, hostID)
	if err := svc.Invite(ctx, hostID, roomID, 1, "你是壹号", 0); err != nil {
		t.Fatalf("Invite: %v", err)
	}
	if _, err := ms.CreateMessage(ctx, hostID, roomID, messages.CreateMessageRequest{Content: "hello"}); err != nil {
		t.Fatalf("CreateMessage: %v", err)
	}

	if err := svc.SetMute(ctx, hostID, roomID, 1, true, 1); err != nil {
		t.Fatalf("SetMute: %v", err)
	}
	svc.replyToRoom(roomID, 1)
	if n := countAgentReplies(ms, ctx, roomID, DefaultAgentID); n != 0 {
		t.Errorf("muted slot replied %d times, want 0", n)
	}

	if err := svc.SetMute(ctx, hostID, roomID, 1, false, 0); err != nil {
		t.Fatalf("unmute: %v", err)
	}
	if _, err := ms.CreateMessage(ctx, hostID, roomID, messages.CreateMessageRequest{Content: "hello again"}); err != nil {
		t.Fatalf("CreateMessage: %v", err)
	}
	svc.replyToRoom(roomID, 1)
	if n := countAgentReplies(ms, ctx, roomID, DefaultAgentID); n != 1 {
		t.Errorf("unmuted slot replied %d times, want 1", n)
	}
}
