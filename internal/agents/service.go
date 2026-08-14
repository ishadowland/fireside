// Package agents implements the server-side agent hook (方式1).
//
// Approach #1 (server-side hook): when a human message is persisted in
// a room, the messages.Service fires a registered hook. This package's
// TriggerRoom is that hook: it builds a chat context from the room's
// recent history, calls an OpenAI-compatible chat/completions endpoint,
// persists the reply as an agent message (sender_kind='agent'), and
// broadcasts msg.created over the hub — so every WS subscriber sees the
// AI reply like any other message.
//
// Invite-based trigger (owner decision, 2026-08-13): the AI is NOT
// auto-present in rooms. A room only gets AI replies after the HOST
// invites it (POST /v1/rooms/:id/agents), and the invitation carries
// the per-room system prompt. TriggerRoom stays silent for rooms with
// no room_agents row. The agent's own replies go through
// messages.CreateAgentMessage, which does NOT fire the hook, so there
// is no self-triggering loop.
//
// Connection config (endpoint URL / API key / model) is kept in memory
// and set through the loopback-only dashboard test interface
// (GET/POST /v1/dashboard/ai-config, POST .../ai-ping). It is
// intentionally NOT persisted: Redis/DB config is deferred (ADR-0013),
// and the test UI is the source of truth for the active session.
//
// Free-speech rounds (owner decision, 2026-08-13): with SetFreeSpeech,
// the HOST starts a time-boxed round (round_seconds, default 5 min)
// during which invited agents keep replying to each other. An agent's
// reply re-triggers the other slots; a slot that is still inside its
// cooldown window schedules a retrigger for when the window lapses, and
// a slot only replies when there is room content newer than its last
// reply (lastCount), so the dialogue paces itself and eventually stops
// when the round expires.
package agents

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ishadowland/fireside/internal/hub"
	"github.com/ishadowland/fireside/internal/messages"
	"github.com/ishadowland/fireside/internal/rooms"
	"github.com/ishadowland/fireside/internal/store"
)

// DefaultAgentID is the sender_id for the built-in agent (slot 1). There
// is no agents table yet (Sprint 2+, FK intentionally omitted — see
// db/migrations/0005), so a fixed CHAR(26) value serves as the well-known
// sender id (len == 26, asserted by a test).
const DefaultAgentID = "01AGT000000000000000000000"

// SecondAgentID is the sender_id for the second built-in agent (slot 2).
const SecondAgentID = "01AGT000000000000000000001"

// MaxSlots is how many AI assistants a room can host (the host-facing UI
// exposes slot 1 and slot 2).
const MaxSlots = 2

// MaxSystemPromptLen caps a room's agent system prompt. Mirrors the CHECK
// constraint in db/migrations/0009_room_agents.up.sql.
const MaxSystemPromptLen = 4000

// MaxCooldownSeconds caps the per-slot cooldown_seconds parameter.
const MaxCooldownSeconds = 3600

// cooldownJitterSeconds is the extra random 0-5s wait added on top of an
// agent's cooldown between its own replies (owner decision, 2026-08-13).
const cooldownJitterSeconds = 5

// DefaultRoundSeconds is the free-speech round length used when the host
// enables a round without specifying a duration (5 minutes).
const DefaultRoundSeconds = 300

// MaxRoundSeconds caps the free-speech round length.
const MaxRoundSeconds = 3600

// MaxMuteMinutes caps how long a host can temporarily ban an assistant.
const MaxMuteMinutes = 240

// defaultSystemPrompt is used when a room's invitation carries an empty
// system_prompt (host may leave it blank for the built-in personality).
const defaultSystemPrompt = "你是房间里的一个 AI 助手。请用中文、简洁地回答。你的回答会直接展示在聊天室里。"

// contextLimit is how many recent room messages are fed to the model as
// conversation context (newest N, oldest-first after reversal).
const contextLimit int32 = 10

// httpTimeout caps a single chat/completions call (ADR-0001: 60s).
const httpTimeout = 60 * time.Second

// Config describes an OpenAI-compatible chat/completions endpoint.
type Config struct {
	Kind    ProviderKind // provider protocol; empty → ProviderOpenAI
	BaseURL string       // e.g. https://api.openai.com/v1 (may omit /chat/completions)
	APIKey  string
	Model   string
	// AgentID is the backend agent name for openclaw/hermes (方式2). For
	// openclaw it selects openclaw/<agent>; for hermes it is the profile /
	// model name. Empty → cfg.Model is used verbatim.
	AgentID string
	// SessionKey is the stable backend session anchor (方式2). openclaw
	// sends it as the OpenAI `user` field ("conv:..."), hermes as the
	// X-Hermes-Session-Id header. Empty → stateless call.
	SessionKey string
}

// AgentStateView is what GET /v1/rooms/:id/agents/:slot returns.
type AgentStateView struct {
	Configured            bool
	AgentID               string
	SystemPrompt          string
	CooldownSeconds       int32
	Muted                 bool
	MutedRemainingSeconds int32
	PresetID              string
	PresetName            string
}

// Service triggers agent replies based on an in-memory AI config.
type Service struct {
	mu       sync.RWMutex
	cfg      *Config
	q        *store.Queries
	client   *http.Client
	messages *messages.Service
	rooms    *rooms.Service
	hub      *hub.Hub
	log      *slog.Logger

	// lastSpeak records when each (room, agent) last posted a reply, so a
	// slot's cooldown can gate the next one; lastCount records the room
	// message count at that last reply, so a slot only re-replies when
	// there is newer content. In-memory by design (same lifetime as the
	// AI config; ADR-0013).
	lastSpeakMu sync.Mutex
	lastSpeak   map[string]time.Time
	lastCount   map[string]int64

	// freeSpeech holds active free-speech rounds keyed by room id.
	freeSpeechMu sync.Mutex
	freeSpeech   map[string]freeSpeechState

	// muted holds temporary bans keyed by agentKey(room, agent); the
	// value is when the ban lapses. In-memory by design (same lifetime
	// as the cooldown/free-speech state; ADR-0013 spirit).
	mutedMu sync.Mutex
	muted   map[string]time.Time

	// presets is the persisted agent preset store (Agent 管理器). nil
	// until SetPresets is called; invitations referencing a preset then
	// fall back to the global config.
	presets *PresetStore
}

// freeSpeechState is one active free-speech round for a room.
type freeSpeechState struct {
	roundSeconds int32
	endAt        time.Time
}

// FreeSpeechView is what GET /v1/rooms/:id/agents/free-speech returns.
type FreeSpeechView struct {
	Enabled          bool
	RoundSeconds     int32
	RemainingSeconds int32
}

// NewService wires the agents hook. q, msgs and rooms are required for
// invite/remove/reply (nil q or rooms makes TriggerRoom and the invite
// endpoints no-ops). hub may be nil (then replies are persisted but not
// broadcast). log nil falls back to slog.Default().
func NewService(q *store.Queries, msgs *messages.Service, rms *rooms.Service, h *hub.Hub, log *slog.Logger) *Service {
	if log == nil {
		log = slog.Default()
	}
	return &Service{
		q:          q,
		messages:   msgs,
		rooms:      rms,
		hub:        h,
		log:        log,
		lastSpeak:  make(map[string]time.Time),
		lastCount:  make(map[string]int64),
		freeSpeech: make(map[string]freeSpeechState),
		muted:      make(map[string]time.Time),
		client:     &http.Client{Timeout: httpTimeout},
	}
}

// agentIDForSlot maps a UI slot (1..MaxSlots) to its well-known sender id.
func agentIDForSlot(slot int) string {
	if slot >= 2 {
		return SecondAgentID
	}
	return DefaultAgentID
}

// SetConfig installs the active AI config. Pass nil to disable.
func (s *Service) SetConfig(c *Config) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if c == nil {
		s.cfg = nil
		return
	}
	cp := *c
	s.cfg = &cp
}

// Config returns a copy of the active config, or nil when unset.
func (s *Service) Config() *Config {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.cfg == nil {
		return nil
	}
	cp := *s.cfg
	return &cp
}

// Configured reports whether a usable config is installed.
func (s *Service) Configured() bool {
	c := s.Config()
	return c != nil && strings.TrimSpace(c.BaseURL) != "" && strings.TrimSpace(c.Model) != ""
}

// AgentID returns the well-known sender id for the slot-1 agent.
func (s *Service) AgentID() string { return agentIDForSlot(1) }

// AgentIDForSlot returns the well-known sender id for a given slot.
func (s *Service) AgentIDForSlot(slot int) string { return agentIDForSlot(slot) }

// SetPresets installs the persisted agent preset store (Agent 管理器).
// Presets referenced by room invitations are resolved through it at reply
// time; without one, invitations fall back to the global in-memory config.
func (s *Service) SetPresets(ps *PresetStore) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.presets = ps
}

// PresetList returns masked preset views (never the api token).
func (s *Service) PresetList() ([]PresetView, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.presets == nil {
		return nil, errors.New("agents: preset store not wired")
	}
	return s.presets.List(), nil
}

// PresetGet returns a single masked preset view.
func (s *Service) PresetGet(id string) (PresetView, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.presets == nil {
		return PresetView{}, errors.New("agents: preset store not wired")
	}
	p, ok := s.presets.Get(id)
	if !ok {
		return PresetView{}, errors.New("preset: 未找到")
	}
	return presetView(p), nil
}

// PresetCreate validates + persists a new preset.
func (s *Service) PresetCreate(in PresetInput) (PresetView, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.presets == nil {
		return PresetView{}, errors.New("agents: preset store not wired")
	}
	p, err := s.presets.Create(in)
	if err != nil {
		return PresetView{}, err
	}
	return presetView(p), nil
}

// PresetUpdate validates + persists changes to an existing preset. An empty
// api_token keeps the stored token (write-only).
func (s *Service) PresetUpdate(id string, in PresetInput) (PresetView, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.presets == nil {
		return PresetView{}, errors.New("agents: preset store not wired")
	}
	p, err := s.presets.Update(id, in)
	if err != nil {
		return PresetView{}, err
	}
	return presetView(p), nil
}

// PresetDelete removes a preset by id.
func (s *Service) PresetDelete(id string) error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.presets == nil {
		return errors.New("agents: preset store not wired")
	}
	return s.presets.Delete(id)
}

// PresetPing validates a preset's connection with a minimal chat call,
// returning the round-trip latency in milliseconds.
func (s *Service) PresetPing(ctx context.Context, id string) (int64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.presets == nil {
		return 0, errors.New("agents: preset store not wired")
	}
	p, ok := s.presets.Get(id)
	if !ok {
		return 0, errors.New("preset: 未找到")
	}
	cfg := Config{
		Kind:    p.Kind,
		BaseURL: p.Endpoint(),
		APIKey:  p.APIToken,
		Model:   p.Model,
		AgentID: p.AgentID,
	}
	start := time.Now()
	if _, err := s.chat(ctx, &cfg, []chatMessage{{Role: "user", Content: "你好"}}); err != nil {
		return 0, err
	}
	return time.Since(start).Milliseconds(), nil
}

// inviteKey / agentKey build the lastSpeak map key for one (room, agent).
func agentKey(roomID, agentID string) string { return roomID + "\x00" + agentID }

// validateSlot returns messages.ErrInvalidArg for out-of-range slots.
func validateSlot(slot int) error {
	if slot < 1 || slot > MaxSlots {
		return messages.ErrInvalidArg
	}
	return nil
}

// validateCooldown returns messages.ErrInvalidArg when cooldown is out of
// the accepted range [0, MaxCooldownSeconds].
func validateCooldown(cooldown int32) error {
	if cooldown < 0 || cooldown > MaxCooldownSeconds {
		return messages.ErrInvalidArg
	}
	return nil
}

// Invite is called by POST /v1/rooms/:id/agents/:slot. Host only; installs
// the agent's per-room system prompt (empty → built-in default at reply
// time) and cooldown. A fresh invitation resets the slot's cooldown clock.
//
// Returns rooms.ErrRoomNotFound / rooms.ErrNotHost / messages.ErrRoomEnded /
// messages.ErrInvalidArg (bad slot, prompt or cooldown too large).
func (s *Service) Invite(ctx context.Context, actorID, roomID string, slot int, systemPrompt string, cooldownSeconds int32) error {
	if err := validateSlot(slot); err != nil {
		return err
	}
	if err := validateCooldown(cooldownSeconds); err != nil {
		return err
	}
	if s.q == nil || s.rooms == nil {
		return errors.New("agents: store/rooms not wired")
	}
	room, _, err := s.rooms.GetRoom(ctx, roomID)
	if errors.Is(err, rooms.ErrRoomNotFound) {
		return rooms.ErrRoomNotFound
	}
	if err != nil {
		s.log.Error("agents.Invite: room lookup failed", "room_id", roomID, "err", err)
		return err
	}
	if room.HostUserID != actorID {
		return rooms.ErrNotHost
	}
	if room.Status != "active" {
		return messages.ErrRoomEnded
	}
	if len(systemPrompt) > MaxSystemPromptLen {
		return messages.ErrInvalidArg
	}
	return s.invite(ctx, actorID, roomID, slot, "", systemPrompt, cooldownSeconds)
}

// InviteWithPreset is called by POST /v1/rooms/:id/agents/:slot when the
// host picks a persisted agent preset (Agent 管理器, issue #38): the
// connection config and system prompt come from the preset, not the room.
// host only. Returns the same sentinels as Invite, plus
// messages.ErrInvalidArg when the preset id is unknown.
func (s *Service) InviteWithPreset(ctx context.Context, actorID, roomID string, slot int, presetID string, cooldownSeconds int32) error {
	if err := validateSlot(slot); err != nil {
		return err
	}
	if err := validateCooldown(cooldownSeconds); err != nil {
		return err
	}
	if presetID == "" {
		return messages.ErrInvalidArg
	}
	s.mu.RLock()
	ps := s.presets
	s.mu.RUnlock()
	if ps == nil {
		return messages.ErrInvalidArg
	}
	if _, ok := ps.Get(presetID); !ok {
		return messages.ErrInvalidArg
	}
	return s.invite(ctx, actorID, roomID, slot, presetID, "", cooldownSeconds)
}

// invite is the shared Invite / InviteWithPreset core. A preset invitation
// stores only the preset reference (the prompt resolves from the preset at
// reply time); a legacy invitation stores the prompt in-room.
func (s *Service) invite(ctx context.Context, actorID, roomID string, slot int, presetID, systemPrompt string, cooldownSeconds int32) error {
	if s.q == nil || s.rooms == nil {
		return errors.New("agents: store/rooms not wired")
	}
	room, _, err := s.rooms.GetRoom(ctx, roomID)
	if errors.Is(err, rooms.ErrRoomNotFound) {
		return rooms.ErrRoomNotFound
	}
	if err != nil {
		s.log.Error("agents.Invite: room lookup failed", "room_id", roomID, "err", err)
		return err
	}
	if room.HostUserID != actorID {
		return rooms.ErrNotHost
	}
	if room.Status != "active" {
		return messages.ErrRoomEnded
	}
	agentID := agentIDForSlot(slot)
	if err := s.q.UpsertRoomAgent(ctx, store.UpsertRoomAgentParams{
		RoomID:          roomID,
		AgentID:         agentID,
		SystemPrompt:    systemPrompt,
		CooldownSeconds: cooldownSeconds,
		AgentPresetID:   presetID,
	}); err != nil {
		s.log.Error("agents.Invite: upsert failed", "room_id", roomID, "err", err)
		return err
	}
	s.lastSpeakMu.Lock()
	delete(s.lastSpeak, agentKey(roomID, agentID))
	delete(s.lastCount, agentKey(roomID, agentID))
	s.lastSpeakMu.Unlock()
	s.mutedMu.Lock()
	delete(s.muted, agentKey(roomID, agentID))
	s.mutedMu.Unlock()
	s.log.Info("agents: invited into room", "room_id", roomID, "agent_id", agentID, "cooldown_seconds", cooldownSeconds, "preset_id", presetID)
	return nil
}

// Remove is called by DELETE /v1/rooms/:id/agents/:slot. Host only; kicks
// the agent out so it stops replying and clears its cooldown clock.
// Returns rooms.ErrRoomNotFound / rooms.ErrNotHost / messages.ErrRoomEnded /
// messages.ErrInvalidArg (bad slot).
func (s *Service) Remove(ctx context.Context, actorID, roomID string, slot int) error {
	if err := validateSlot(slot); err != nil {
		return err
	}
	if s.q == nil || s.rooms == nil {
		return errors.New("agents: store/rooms not wired")
	}
	room, _, err := s.rooms.GetRoom(ctx, roomID)
	if errors.Is(err, rooms.ErrRoomNotFound) {
		return rooms.ErrRoomNotFound
	}
	if err != nil {
		s.log.Error("agents.Remove: room lookup failed", "room_id", roomID, "err", err)
		return err
	}
	if room.HostUserID != actorID {
		return rooms.ErrNotHost
	}
	if room.Status != "active" {
		return messages.ErrRoomEnded
	}
	agentID := agentIDForSlot(slot)
	if _, err := s.q.DeleteRoomAgent(ctx, roomID, agentID); err != nil {
		s.log.Error("agents.Remove: delete failed", "room_id", roomID, "err", err)
		return err
	}
	s.lastSpeakMu.Lock()
	delete(s.lastSpeak, agentKey(roomID, agentID))
	delete(s.lastCount, agentKey(roomID, agentID))
	s.lastSpeakMu.Unlock()
	s.mutedMu.Lock()
	delete(s.muted, agentKey(roomID, agentID))
	s.mutedMu.Unlock()
	s.log.Info("agents: removed from room", "room_id", roomID, "agent_id", agentID)
	return nil
}

// SetMute is called by POST /v1/rooms/:id/agents/:slot/mute. Host only.
// enabled=true temporarily bans the assistant for `minutes` (it stops
// replying until the window lapses); enabled=false lifts the ban
// immediately. Returns rooms.ErrRoomNotFound / rooms.ErrNotHost /
// messages.ErrRoomEnded / messages.ErrInvalidArg (bad slot or duration).
func (s *Service) SetMute(ctx context.Context, actorID, roomID string, slot int, enabled bool, minutes int32) error {
	if err := validateSlot(slot); err != nil {
		return err
	}
	if enabled && (minutes < 1 || minutes > MaxMuteMinutes) {
		return messages.ErrInvalidArg
	}
	if s.q == nil || s.rooms == nil {
		return errors.New("agents: store/rooms not wired")
	}
	room, _, err := s.rooms.GetRoom(ctx, roomID)
	if errors.Is(err, rooms.ErrRoomNotFound) {
		return rooms.ErrRoomNotFound
	}
	if err != nil {
		s.log.Error("agents.SetMute: room lookup failed", "room_id", roomID, "err", err)
		return err
	}
	if room.HostUserID != actorID {
		return rooms.ErrNotHost
	}
	if room.Status != "active" {
		return messages.ErrRoomEnded
	}
	agentID := agentIDForSlot(slot)
	s.mutedMu.Lock()
	defer s.mutedMu.Unlock()
	if enabled {
		s.muted[agentKey(roomID, agentID)] = time.Now().Add(time.Duration(minutes) * time.Minute)
		s.log.Info("agents: temporarily banned", "room_id", roomID, "agent_id", agentID, "minutes", minutes)
	} else {
		delete(s.muted, agentKey(roomID, agentID))
		s.log.Info("agents: ban lifted", "room_id", roomID, "agent_id", agentID)
	}
	return nil
}

// muteInfo reports whether (room, agent) is temporarily banned and, if so,
// how many seconds remain (lazily expiring stale bans).
func (s *Service) muteInfo(roomID, agentID string) (bool, int32) {
	s.mutedMu.Lock()
	defer s.mutedMu.Unlock()
	until, ok := s.muted[agentKey(roomID, agentID)]
	if !ok {
		return false, 0
	}
	if time.Now().After(until) {
		delete(s.muted, agentKey(roomID, agentID))
		return false, 0
	}
	return true, int32(time.Until(until).Seconds())
}

// AgentState reports whether an agent (slot) is invited into a room and
// with which prompt / cooldown (GET /v1/rooms/:id/agents/:slot).
// Configured=false when the room has no invitation for that slot. Returns
// rooms.ErrRoomNotFound when the room doesn't exist (so the REST handler
// can map it to 404), messages.ErrInvalidArg for a bad slot.
func (s *Service) AgentState(ctx context.Context, roomID string, slot int) (AgentStateView, error) {
	if err := validateSlot(slot); err != nil {
		return AgentStateView{}, err
	}
	if _, _, err := s.rooms.GetRoom(ctx, roomID); err != nil {
		return AgentStateView{}, err
	}
	row, err := s.q.GetRoomAgent(ctx, roomID, agentIDForSlot(slot))
	if errors.Is(err, sql.ErrNoRows) {
		muted, remaining := s.muteInfo(roomID, agentIDForSlot(slot))
		return AgentStateView{Configured: false, Muted: muted, MutedRemainingSeconds: remaining}, nil
	}
	if err != nil {
		s.log.Error("agents.AgentState: lookup failed", "room_id", roomID, "err", err)
		return AgentStateView{}, err
	}
	muted, remaining := s.muteInfo(roomID, row.AgentID)
	view := AgentStateView{
		Configured:            true,
		AgentID:               row.AgentID,
		SystemPrompt:          row.SystemPrompt,
		CooldownSeconds:       row.CooldownSeconds,
		Muted:                 muted,
		MutedRemainingSeconds: remaining,
		PresetID:              row.AgentPresetID,
	}
	if row.AgentPresetID != "" {
		s.mu.RLock()
		ps := s.presets
		s.mu.RUnlock()
		if ps != nil {
			if p, ok := ps.Get(row.AgentPresetID); ok {
				view.PresetName = p.Name
				view.SystemPrompt = p.SystemPrompt
			}
		}
	}
	return view, nil
}

// TriggerRoom is the messages.Service hook (方式1). It is synchronous and
// returns immediately; the LLM round-trips run in background goroutines so
// the sending client is never blocked (ADR-0006 spirit; no placeholder in
// this MVP — placeholder/deletion is ADR-0009 later). Every invited slot
// gets a chance to reply (each gated by its own cooldown).
func (s *Service) TriggerRoom(_ context.Context, roomID string, _ messages.MessageView) {
	if s.messages == nil || s.q == nil {
		return
	}
	for slot := 1; slot <= MaxSlots; slot++ {
		slot := slot
		go s.replyToRoom(roomID, slot)
	}
}

// resolveChatConfig picks the connection config + system prompt for one
// invitation. A preset reference wins (live config/prompt from the store);
// otherwise the stored in-room prompt + the global in-memory config. The
// returned Config.Kind defaults to ProviderOpenAI.
func (s *Service) resolveChatConfig(agent store.RoomAgent) (Config, string) {
	if agent.AgentPresetID != "" {
		s.mu.RLock()
		ps := s.presets
		s.mu.RUnlock()
		if ps != nil {
			if p, ok := ps.Get(agent.AgentPresetID); ok {
				prompt := strings.TrimSpace(p.SystemPrompt)
				if prompt == "" {
					prompt = defaultSystemPrompt
				}
				return Config{
					Kind:       p.Kind,
					BaseURL:    p.Endpoint(),
					APIKey:     p.APIToken,
					Model:      p.Model,
					AgentID:    p.AgentID,
					SessionKey: p.SessionKey,
				}, prompt
			}
		}
	}
	cfg := s.Config()
	var out Config
	if cfg != nil {
		out = *cfg
	}
	if out.Kind == "" {
		out.Kind = ProviderOpenAI
	}
	prompt := strings.TrimSpace(agent.SystemPrompt)
	if prompt == "" {
		prompt = defaultSystemPrompt
	}
	return out, prompt
}

// roomAgent returns the stored invitation for a (room, agent), or
// present=false when that agent was not invited. replyToRoom stays silent
// without an invitation.
func (s *Service) roomAgent(ctx context.Context, roomID, agentID string) (store.RoomAgent, bool, error) {
	row, err := s.q.GetRoomAgent(ctx, roomID, agentID)
	if errors.Is(err, sql.ErrNoRows) {
		return store.RoomAgent{}, false, nil
	}
	if err != nil {
		return store.RoomAgent{}, false, err
	}
	return row, true, nil
}

// cooldownAllows reports whether (room, agent) may speak now. The first
// reply is always allowed; afterwards the agent must wait
// cooldown + random 0..5s since its last reply.
func (s *Service) cooldownAllows(roomID, agentID string, cooldown time.Duration) bool {
	allowed, _ := s.cooldownRemaining(roomID, agentID, cooldown)
	return allowed
}

// cooldownRemaining is like cooldownAllows but also returns how long the
// caller must wait until the cooldown (+ jitter) window lapses.
func (s *Service) cooldownRemaining(roomID, agentID string, cooldown time.Duration) (bool, time.Duration) {
	key := agentKey(roomID, agentID)
	s.lastSpeakMu.Lock()
	defer s.lastSpeakMu.Unlock()
	last, ok := s.lastSpeak[key]
	if !ok {
		return true, 0
	}
	wait := cooldown + time.Duration(rand.Intn(cooldownJitterSeconds+1))*time.Second
	elapsed := time.Since(last)
	if elapsed >= wait {
		return true, 0
	}
	return false, wait - elapsed
}

// markSpoke records that (room, agent) just posted a reply.
func (s *Service) markSpoke(roomID, agentID string) {
	s.lastSpeakMu.Lock()
	defer s.lastSpeakMu.Unlock()
	s.lastSpeak[agentKey(roomID, agentID)] = time.Now()
}

// hasNewContent reports whether the room has more messages than at this
// agent's last reply (guards against echo loops / duplicate replies).
func (s *Service) hasNewContent(roomID, agentID string, current int64) bool {
	s.lastSpeakMu.Lock()
	defer s.lastSpeakMu.Unlock()
	last, ok := s.lastCount[agentKey(roomID, agentID)]
	return !ok || current > last
}

// markReplied records the room message count at this agent's last reply.
func (s *Service) markReplied(roomID, agentID string, count int64) {
	s.lastSpeakMu.Lock()
	defer s.lastSpeakMu.Unlock()
	s.lastSpeak[agentKey(roomID, agentID)] = time.Now()
	s.lastCount[agentKey(roomID, agentID)] = count
}

// SetFreeSpeech enables or disables a free-speech round for a room (host
// only, active room). Enabling starts a round of roundSeconds during which
// invited agents keep replying to each other; the round expires on its own.
// roundSeconds is ignored when disabling. Returns rooms.ErrRoomNotFound /
// rooms.ErrNotHost / messages.ErrRoomEnded / messages.ErrInvalidArg.
func (s *Service) SetFreeSpeech(ctx context.Context, actorID, roomID string, enabled bool, roundSeconds int32) error {
	if s.rooms == nil {
		return errors.New("agents: rooms not wired")
	}
	room, _, err := s.rooms.GetRoom(ctx, roomID)
	if errors.Is(err, rooms.ErrRoomNotFound) {
		return rooms.ErrRoomNotFound
	}
	if err != nil {
		s.log.Error("agents.SetFreeSpeech: room lookup failed", "room_id", roomID, "err", err)
		return err
	}
	if room.HostUserID != actorID {
		return rooms.ErrNotHost
	}
	if room.Status != "active" {
		return messages.ErrRoomEnded
	}
	if !enabled {
		s.freeSpeechMu.Lock()
		delete(s.freeSpeech, roomID)
		s.freeSpeechMu.Unlock()
		s.log.Info("agents: free-speech round stopped", "room_id", roomID)
		return nil
	}
	if roundSeconds < 1 || roundSeconds > MaxRoundSeconds {
		return messages.ErrInvalidArg
	}
	s.freeSpeechMu.Lock()
	s.freeSpeech[roomID] = freeSpeechState{
		roundSeconds: roundSeconds,
		endAt:        time.Now().Add(time.Duration(roundSeconds) * time.Second),
	}
	s.freeSpeechMu.Unlock()
	s.log.Info("agents: free-speech round started", "room_id", roomID, "round_seconds", roundSeconds)
	return nil
}

// FreeSpeechState reports whether a free-speech round is running and how
// much time is left. Returns rooms.ErrRoomNotFound for unknown rooms.
func (s *Service) FreeSpeechState(ctx context.Context, roomID string) (FreeSpeechView, error) {
	if _, _, err := s.rooms.GetRoom(ctx, roomID); err != nil {
		return FreeSpeechView{}, err
	}
	s.freeSpeechMu.Lock()
	defer s.freeSpeechMu.Unlock()
	st, ok := s.freeSpeech[roomID]
	if !ok {
		return FreeSpeechView{}, nil
	}
	until := time.Until(st.endAt)
	if until <= 0 {
		delete(s.freeSpeech, roomID)
		return FreeSpeechView{RoundSeconds: st.roundSeconds}, nil
	}
	remaining := int32(until / time.Second)
	if until%time.Second != 0 {
		remaining++
	}
	return FreeSpeechView{
		Enabled:          true,
		RoundSeconds:     st.roundSeconds,
		RemainingSeconds: remaining,
	}, nil
}

// freeSpeechActive reports whether a free-speech round is currently
// running for a room (lazily expiring stale rounds).
func (s *Service) freeSpeechActive(roomID string) bool {
	s.freeSpeechMu.Lock()
	defer s.freeSpeechMu.Unlock()
	st, ok := s.freeSpeech[roomID]
	if !ok {
		return false
	}
	if time.Now().After(st.endAt) {
		delete(s.freeSpeech, roomID)
		return false
	}
	return true
}

// triggerOthers kicks off a turn for every invited slot except selfSlot.
// Only called after a successful agent reply while a free-speech round is
// active, so the assistants keep answering each other.
func (s *Service) triggerOthers(roomID string, selfSlot int) {
	for slot := 1; slot <= MaxSlots; slot++ {
		if slot == selfSlot {
			continue
		}
		slot := slot
		go s.replyToRoom(roomID, slot)
	}
}

// replyToRoom runs one agent turn for a slot: check the invitation + new
// content + cooldown, fetch context, call the model, persist the reply as
// an agent message, broadcast msg.created. During a free-speech round a
// cooldown-gated turn schedules a retrigger for when the window lapses, and
// a successful reply re-triggers the other slots.
func (s *Service) replyToRoom(roomID string, slot int) {
	agentID := agentIDForSlot(slot)
	ctx, cancel := context.WithTimeout(context.Background(), httpTimeout)
	defer cancel()
	log := s.log.With("room_id", roomID, "agent_id", agentID, "slot", slot)

	// 0) Only reply in rooms where this agent was invited.
	agent, present, err := s.roomAgent(ctx, roomID, agentID)
	if err != nil {
		log.Warn("agents: room agent lookup failed", "err", err)
		return
	}
	if !present {
		return
	}

	// 0b) Temporarily banned — skip this turn (free-speech chains and
	// human-triggered turns alike stay silent until the ban lapses).
	if muted, _ := s.muteInfo(roomID, agentID); muted {
		log.Debug("agents: temporarily banned, skipping turn")
		return
	}

	// 1) Only reply when the room has content newer than this agent's
	// last reply (otherwise we would echo the same history forever).
	count, err := s.q.CountMessagesByRoom(ctx, roomID)
	if err != nil {
		log.Warn("agents: count context failed", "err", err)
		return
	}
	if !s.hasNewContent(roomID, agentID, count) {
		log.Debug("agents: no new content to reply to")
		return
	}

	// 2) Cooldown: wait cooldown_seconds (+ 0-5s jitter) between this
	// agent's own replies. In free-speech mode a gated turn is retried
	// once the window lapses.
	cooldown := time.Duration(agent.CooldownSeconds) * time.Second
	allowed, wait := s.cooldownRemaining(roomID, agentID, cooldown)
	if !allowed {
		if s.freeSpeechActive(roomID) {
			slot := slot
			go func() {
				time.Sleep(wait)
				s.replyToRoom(roomID, slot)
			}()
		}
		return
	}

	// 3) Room history (newest-first) as conversation context.
	hist, _, err := s.messages.ListMessagesByRoom(ctx, roomID, "", contextLimit)
	if err != nil {
		log.Warn("agents: list context failed", "err", err)
		return
	}

	// 4) Model call — config + system prompt come from the preset when the
	// invitation references one, else the stored prompt + global config.
	chatCfg, system := s.resolveChatConfig(agent)
	if chatCfg.BaseURL == "" || chatCfg.Model == "" {
		log.Debug("agents: no connection config (no preset, global unset)")
		return
	}
	// 方式2 session anchor: unless a preset fixed a session key, derive a
	// stable per-(room, slot) key so openclaw/hermes keep one backend
	// session per room slot (no cross-room memory bleed).
	if chatCfg.SessionKey == "" && (chatCfg.Kind == ProviderOpenClaw || chatCfg.Kind == ProviderHermes) {
		chatCfg.SessionKey = roomID + ":" + strconv.Itoa(slot)
	}
	reply, err := s.chat(ctx, &chatCfg, buildChat(system, hist))
	if err != nil {
		log.Warn("agents: chat failed", "err", err)
		return
	}

	// 5) Persist agent message (does NOT re-trigger the hook).
	view, err := s.messages.CreateAgentMessage(ctx, roomID, agentID, reply)
	if err != nil {
		log.Warn("agents: persist agent message failed", "err", err)
		return
	}
	s.markReplied(roomID, agentID, count)

	// 6) Broadcast msg.created to subscribers.
	if s.hub != nil {
		frame, _ := json.Marshal(map[string]any{
			"type":    "msg.created",
			"message": view,
		})
		delivered := s.hub.BroadcastToRoom(roomID, frame, nil)
		log.Debug("agents: reply broadcast", "msg_id", view.ID, "delivered", delivered)
	}
	log.Info("agents: reply posted", "msg_id", view.ID)

	// 7) Free-speech: this reply is new content for the other slots — keep
	// the round going.
	if s.freeSpeechActive(roomID) {
		s.triggerOthers(roomID, slot)
	}
}

// Ping validates the configured endpoint with a minimal call, returning the
// round-trip latency in milliseconds. Returns an error when unconfigured.
func (s *Service) Ping(ctx context.Context) (int64, error) {
	cfg := s.Config()
	if cfg == nil {
		return 0, errors.New("AI 未配置")
	}
	start := time.Now()
	if _, err := s.chat(ctx, cfg, []chatMessage{{Role: "user", Content: "你好"}}); err != nil {
		return 0, err
	}
	return time.Since(start).Milliseconds(), nil
}

// ---- OpenAI-compatible client -------------------------------------------

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model    string        `json:"model"`
	Messages []chatMessage `json:"messages"`
	User     string        `json:"user,omitempty"` // openclaw 方式2: stable session anchor "conv:<key>"
}

type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// buildChat converts MessageViews into OpenAI messages: human → user,
// agent → assistant, system → dropped (they are event blobs). Room history
// arrives newest-first, so it is reversed into chronological order.
func buildChat(system string, hist []messages.MessageView) []chatMessage {
	out := make([]chatMessage, 0, len(hist)+1)
	out = append(out, chatMessage{Role: "system", Content: system})
	for i := len(hist) - 1; i >= 0; i-- {
		m := hist[i]
		switch m.SenderKind {
		case "human":
			out = append(out, chatMessage{Role: "user", Content: m.Content})
		case "agent":
			out = append(out, chatMessage{Role: "assistant", Content: m.Content})
		}
	}
	return out
}

// chat calls the model endpoint for the configured provider kind. openai →
// /chat/completions, simple → verbatim endpoint, openclaw → /api/chat
// (legacy) or /v1/chat/completions (gateway, when the base already points
// at it), hermes → /chat/completions (方式2). For openclaw/hermes a
// non-empty SessionKey anchors the call to a stable backend session:
// openclaw sends it as the OpenAI `user` field ("conv:<key>") so the
// Gateway derives a stable session; hermes sends it as the
// X-Hermes-Session-Id header (only when an API key is configured, since
// the header is gated on auth there).
func (s *Service) chat(ctx context.Context, cfg *Config, msgs []chatMessage) (string, error) {
	kind := cfg.Kind
	if kind == "" {
		kind = ProviderOpenAI
	}
	endpoint := providerEndpoint(kind, cfg.BaseURL)
	if endpoint == "" {
		return "", errors.New("agents: missing base url")
	}

	reqBody := chatRequest{Model: backendModel(cfg), Messages: msgs}
	if kind == ProviderOpenClaw && cfg.SessionKey != "" {
		reqBody.User = "conv:" + cfg.SessionKey
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	}
	if kind == ProviderHermes && cfg.SessionKey != "" && cfg.APIKey != "" {
		req.Header.Set("X-Hermes-Session-Id", cfg.SessionKey)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	return parseResponse(kind, endpoint, resp.StatusCode, resp.Body)
}

// parseResponse picks the decoder for a provider kind. openclaw uses the
// legacy {reply} shape unless the endpoint is the OpenAI-compatible
// gateway surface (/v1/chat/completions), which answers
// choices[].message.content like every other kind.
func parseResponse(kind ProviderKind, endpoint string, status int, r io.Reader) (string, error) {
	if kind == ProviderOpenClaw && !strings.HasSuffix(strings.TrimRight(endpoint, "/"), "/chat/completions") {
		return parseOpenClawResponse(status, r)
	}
	return parseOpenAIResponse(status, r)
}

// backendModel resolves the effective model name for a call. For
// openclaw/hermes (方式2) an AgentID selects the backend agent: openclaw →
// "openclaw/<agent>", hermes → the agent/profile name. Empty AgentID falls
// back to cfg.Model verbatim.
func backendModel(cfg *Config) string {
	if cfg.AgentID == "" {
		return cfg.Model
	}
	switch cfg.Kind {
	case ProviderOpenClaw:
		return "openclaw/" + cfg.AgentID
	case ProviderHermes:
		return cfg.AgentID
	}
	return cfg.Model
}

// providerEndpoint resolves a base URL into the full chat endpoint for a
// provider kind. openai appends /chat/completions (chatEndpoint), simple
// uses the base verbatim, openclaw appends /api/chat (legacy) or keeps a
// base that already ends in /chat/completions (gateway), hermes appends
// /chat/completions like openai.
func providerEndpoint(kind ProviderKind, base string) string {
	switch kind {
	case ProviderSimple:
		return strings.TrimRight(base, "/")
	case ProviderOpenClaw:
		b := strings.TrimRight(base, "/")
		if strings.HasSuffix(b, "/api/chat") || strings.HasSuffix(b, "/chat/completions") {
			return b
		}
		return b + "/api/chat"
	case ProviderHermes:
		return chatEndpoint(base)
	default:
		return chatEndpoint(base)
	}
}

// parseOpenAIResponse decodes a chat/completions (openai/simple) response.
func parseOpenAIResponse(status int, r io.Reader) (string, error) {
	var cr chatResponse
	if err := json.NewDecoder(r).Decode(&cr); err != nil {
		return "", fmt.Errorf("decode chat response (status %d): %w", status, err)
	}
	if status >= 300 {
		var msg string
		if cr.Error != nil {
			msg = cr.Error.Message
		}
		if msg == "" {
			msg = fmt.Sprintf("HTTP %d", status)
		}
		return "", errors.New(msg)
	}
	if len(cr.Choices) == 0 || strings.TrimSpace(cr.Choices[0].Message.Content) == "" {
		return "", errors.New("模型返回了空回复")
	}
	return strings.TrimSpace(cr.Choices[0].Message.Content), nil
}

// parseOpenClawResponse decodes an OpenClaw /api/chat response ({reply}).
func parseOpenClawResponse(status int, r io.Reader) (string, error) {
	var oc struct {
		Reply string `json:"reply"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(r).Decode(&oc); err != nil {
		return "", fmt.Errorf("decode openclaw response (status %d): %w", status, err)
	}
	if status >= 300 {
		msg := ""
		if oc.Error != nil {
			msg = oc.Error.Message
		}
		if msg == "" {
			msg = fmt.Sprintf("HTTP %d", status)
		}
		return "", errors.New(msg)
	}
	if strings.TrimSpace(oc.Reply) == "" {
		return "", errors.New("模型返回了空回复")
	}
	return strings.TrimSpace(oc.Reply), nil
}

// chatEndpoint normalizes a base URL into a full chat/completions URL.
// Accepts ".../v1" or a bare host; if it already ends with
// "/chat/completions" it is used verbatim.
func chatEndpoint(base string) string {
	b := strings.TrimRight(base, "/")
	if strings.HasSuffix(b, "/chat/completions") {
		return b
	}
	return b + "/chat/completions"
}
