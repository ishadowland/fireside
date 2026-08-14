// Agent presets (Agent 管理器, issue #38).
//
// A preset is a persisted AI connection configuration: provider kind
// (openai / simple / openclaw), endpoint (host:port or base_url), api
// token, model and default system prompt. Presets live in a LOCAL file on
// disk — never in Postgres and never synced to GitHub (gitignored) — so
// api tokens stay on the operator's machine. Room invitations reference a
// preset by id instead of carrying the prompt/key in-room.
//
// Security contract:
//   - The api token is write-only: no server method returns it. PresetView
//     exposes only `has_token`.
//   - Management is loopback-only (dashboard, ADR-0019); room-facing APIs
//     never return preset details.
//   - The config file is created with 0600 perms.
package agents

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"
)

// ProviderKind identifies one AI backend protocol.
type ProviderKind string

const (
	// ProviderOpenAI is an OpenAI-compatible /chat/completions endpoint
	// (the historical default).
	ProviderOpenAI ProviderKind = "openai"
	// ProviderSimple is a bare chat endpoint using the same JSON shape,
	// called verbatim (no /chat/completions appended).
	ProviderSimple ProviderKind = "simple"
	// ProviderOpenClaw is an OpenClaw-compatible /api/chat endpoint;
	// request {model, messages}, response {reply}.
	ProviderOpenClaw ProviderKind = "openclaw"
)

// ValidProviderKinds lists the selectable kinds in UI order.
var ValidProviderKinds = []ProviderKind{ProviderOpenAI, ProviderSimple, ProviderOpenClaw}

// validProvider reports whether kind is a known provider kind.
func validProvider(k ProviderKind) bool {
	for _, v := range ValidProviderKinds {
		if k == v {
			return true
		}
	}
	return false
}

// Preset is one persisted agent connection configuration.
type Preset struct {
	ID           string       `json:"id"`
	Name         string       `json:"name"`
	Kind         ProviderKind `json:"kind"`
	Host         string       `json:"host,omitempty"`
	Port         int          `json:"port,omitempty"`
	BaseURL      string       `json:"base_url,omitempty"`
	APIToken     string       `json:"api_token"`
	Model        string       `json:"model"`
	SystemPrompt string       `json:"system_prompt,omitempty"`
	CreatedAt    time.Time    `json:"created_at"`
}

// Endpoint returns the resolved base URL for this preset (host:port when
// no explicit base_url is set).
func (p *Preset) Endpoint() string {
	if b := strings.TrimSpace(p.BaseURL); b != "" {
		return strings.TrimRight(b, "/")
	}
	host := strings.TrimSpace(p.Host)
	if host == "" {
		return ""
	}
	host = strings.TrimPrefix(host, "http://")
	host = strings.TrimPrefix(host, "https://")
	if p.Port > 0 {
		return "http://" + host + ":" + strconv.Itoa(p.Port)
	}
	return "http://" + host
}

// PresetInput is the write body for create/update.
type PresetInput struct {
	Name         string       `json:"name"`
	Kind         ProviderKind `json:"kind"`
	Host         string       `json:"host"`
	Port         int          `json:"port"`
	BaseURL      string       `json:"base_url"`
	APIToken     string       `json:"api_token"`
	Model        string       `json:"model"`
	SystemPrompt string       `json:"system_prompt"`
}

// validate performs input validation, mirroring the invite prompt cap.
func (in *PresetInput) validate() error {
	if strings.TrimSpace(in.Name) == "" {
		return errors.New("preset: 名称不能为空")
	}
	if in.Kind == "" {
		return errors.New("preset: 接入方式不能为空")
	}
	if !validProvider(in.Kind) {
		return fmt.Errorf("preset: 不支持的接入方式 %q", in.Kind)
	}
	if strings.TrimSpace(in.BaseURL) == "" && strings.TrimSpace(in.Host) == "" {
		return errors.New("preset: 需要 host:端口 或 base_url")
	}
	if in.Port < 0 || in.Port > 65535 {
		return errors.New("preset: 端口范围 0-65535")
	}
	if strings.TrimSpace(in.Model) == "" {
		return errors.New("preset: 模型不能为空")
	}
	if len(in.SystemPrompt) > MaxSystemPromptLen {
		return fmt.Errorf("preset: 提示词超过 %d 字符上限", MaxSystemPromptLen)
	}
	return nil
}

// PresetView is the masked view of a Preset — never carries the api token.
type PresetView struct {
	ID           string       `json:"id"`
	Name         string       `json:"name"`
	Kind         ProviderKind `json:"kind"`
	Endpoint     string       `json:"endpoint"`
	HasToken     bool         `json:"has_token"`
	Model        string       `json:"model"`
	SystemPrompt string       `json:"system_prompt"`
	CreatedAt    time.Time    `json:"created_at"`
}

func presetView(p Preset) PresetView {
	return PresetView{
		ID:           p.ID,
		Name:         p.Name,
		Kind:         p.Kind,
		Endpoint:     p.Endpoint(),
		HasToken:     p.APIToken != "",
		Model:        p.Model,
		SystemPrompt: p.SystemPrompt,
		CreatedAt:    p.CreatedAt,
	}
}

// PresetStore persists presets to a local JSON file (0600). All mutating
// methods persist atomically (temp file + rename). An empty path keeps
// everything in memory (used by tests).
type PresetStore struct {
	mu      sync.Mutex
	path    string
	presets map[string]Preset
	order   []string
}

// NewPresetStore loads presets from path (creating the dir/file as
// needed). Missing file = empty store. A malformed file is an error so the
// operator notices instead of silently losing presets.
func NewPresetStore(path string) (*PresetStore, error) {
	s := &PresetStore{
		path:    path,
		presets: make(map[string]Preset),
	}
	if path == "" {
		return s, nil
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return s, nil
	}
	var doc struct {
		Presets []Preset `json:"presets"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("preset store %s: %w", path, err)
	}
	for _, p := range doc.Presets {
		if p.ID == "" {
			continue
		}
		s.presets[p.ID] = p
		s.order = append(s.order, p.ID)
	}
	sort.Slice(s.order, func(i, j int) bool {
		return s.presets[s.order[i]].CreatedAt.Before(s.presets[s.order[j]].CreatedAt)
	})
	return s, nil
}

// save atomically persists the store. No-op for in-memory stores.
func (s *PresetStore) save() error {
	if s.path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	doc := struct {
		Presets []Preset `json:"presets"`
	}{Presets: make([]Preset, 0, len(s.order))}
	for _, id := range s.order {
		doc.Presets = append(doc.Presets, s.presets[id])
	}
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	if err := os.Chmod(tmp, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// List returns masked preset views, oldest first.
func (s *PresetStore) List() []PresetView {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]PresetView, 0, len(s.order))
	for _, id := range s.order {
		out = append(out, presetView(s.presets[id]))
	}
	return out
}

// Get returns a copy of the preset (with token) + presence flag. Only the
// agents service uses this internally; never expose the copy to clients.
func (s *PresetStore) Get(id string) (Preset, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.presets[id]
	return p, ok
}

// Create validates and inserts a new preset.
func (s *PresetStore) Create(in PresetInput) (Preset, error) {
	if err := in.validate(); err != nil {
		return Preset{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	p := Preset{
		ID:           ulid.Make().String(),
		Name:         strings.TrimSpace(in.Name),
		Kind:         in.Kind,
		Host:         strings.TrimSpace(in.Host),
		Port:         in.Port,
		BaseURL:      strings.TrimSpace(in.BaseURL),
		APIToken:     in.APIToken,
		Model:        strings.TrimSpace(in.Model),
		SystemPrompt: in.SystemPrompt,
		CreatedAt:    time.Now().UTC(),
	}
	s.presets[p.ID] = p
	s.order = append(s.order, p.ID)
	if err := s.save(); err != nil {
		delete(s.presets, p.ID)
		s.order = s.order[:len(s.order)-1]
		return Preset{}, err
	}
	return p, nil
}

// Update replaces a preset's mutable fields. An empty api_token in the
// input keeps the existing token (write-only semantics).
func (s *PresetStore) Update(id string, in PresetInput) (Preset, error) {
	if err := in.validate(); err != nil {
		return Preset{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.presets[id]
	if !ok {
		return Preset{}, errors.New("preset: 未找到")
	}
	p.Name = strings.TrimSpace(in.Name)
	p.Kind = in.Kind
	p.Host = strings.TrimSpace(in.Host)
	p.Port = in.Port
	p.BaseURL = strings.TrimSpace(in.BaseURL)
	p.Model = strings.TrimSpace(in.Model)
	p.SystemPrompt = in.SystemPrompt
	if strings.TrimSpace(in.APIToken) != "" {
		p.APIToken = in.APIToken
	}
	s.presets[id] = p
	if err := s.save(); err != nil {
		s.presets[id] = p
		return Preset{}, err
	}
	return p, nil
}

// Delete removes a preset.
func (s *PresetStore) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.presets[id]; !ok {
		return errors.New("preset: 未找到")
	}
	delete(s.presets, id)
	for i, oid := range s.order {
		if oid == id {
			s.order = append(s.order[:i], s.order[i+1:]...)
			break
		}
	}
	return s.save()
}
