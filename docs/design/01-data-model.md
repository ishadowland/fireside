# Data Model

> Entity definitions, relationships, and DB schema.
> 状态：🚧 Draft v0.1 — 待用户审阅后冻结。

## 实体总览

```
User ──┬── HostOf ────────────── Room
       │                         │
       ├── OnStage ──┐           ├── Config (JSONB)
       │             │           │
       └── RaiseHand │           ├── Participants ──┬── Human(User)
                     │           │                  ├── Agent(Custom)
Room ────────────────►│           │                  └── Agent(Lobster)
                     │           │
                     │           └── Messages
                     │                  ├── Text
                     │                  ├── Image
                     │                  └── System(状态事件)
                     │
Lobby ─────────────────────────► RaiseHandQueue
                                  │
                                  └── Pending(User + note)
```

## 1. User（人类用户）

```go
type User struct {
    ID            string    `json:"id"`             // ULID
    Phone         string    `json:"phone"`          // E.164, 唯一
    DisplayName   string    `json:"display_name"`
    AvatarURL     string    `json:"avatar_url,omitempty"`
    CreatedAt     time.Time `json:"created_at"`
    LastSeenAt    time.Time `json:"last_seen_at"`
    Status        UserStatus `json:"status"`        // active | suspended
}

type UserStatus string
const (
    UserActive    UserStatus = "active"
    UserSuspended UserStatus = "suspended"
)
```

**说明**:
- `Phone` 是唯一标识,登录用手机号验证码
- **MVP 单租户,不做密码/邮箱/第三方登录**
- 不存密码哈希(验证码登录无需)

## 2. Room（房间）

```go
type Room struct {
    ID           string      `json:"id"`            // ULID
    HostUserID   string      `json:"host_user_id"`  // FK User.ID, 房间所有者
    Name         string      `json:"name"`          // 用户自定义, e.g. "周会-2026-07-26"
    Status       RoomStatus  `json:"status"`        // active | ended
    Config       RoomConfig  `json:"config"`        // JSONB
    CreatedAt    time.Time   `json:"created_at"`
    EndedAt      *time.Time  `json:"ended_at,omitempty"`
    ArchivedAt   *time.Time  `json:"archived_at,omitempty"` // 归档触发时刻
    ArchiveRef   *string     `json:"archive_ref,omitempty"` // 归档 ID
}

type RoomStatus string
const (
    RoomActive RoomStatus = "active"
    RoomEnded  RoomStatus = "ended"
)

type RoomConfig struct {
    MaxParticipants   int  `json:"max_participants"`    // 默认 50
    AutoEndDays       int  `json:"auto_end_days"`        // 默认 0=不过期
    ArchiveOnEnd      bool `json:"archive_on_end"`       // 结束时拉纪要 agent
    DMInRoomAllowed   bool `json:"dm_in_room_allowed"`   // MVP=false
}
```

**说明**:
- 房间一旦创建,`HostUserID` **不可变**(只有转让才改)
- `Status=ended` 后,`EndedAt` 必填
- 归档是**异步操作**:`ArchiveRef` 在归档完成后填

### 3. Participant（房间参与者）

这是核心的"通用参与者"模型,**人类和 Agent 共用一张表**。
所有参与者都支持**热插拔**:运行时进出,无需重启服务。进出房间 = 上/下麦,二者是同一概念的两种表述。

```go
type Participant struct {
    ID             string            `json:"id"`              // ULID
    RoomID         string            `json:"room_id"`         // FK Room.ID
    Kind           ParticipantKind   `json:"kind"`            // human | agent
    StageState     StageState        `json:"stage_state"`     // on_stage(在房间里) | off_stage(已离房)
    JoinedAt       time.Time         `json:"joined_at"`       // 本次进房时刻
    LeftAt         *time.Time        `json:"left_at,omitempty"` // 本次离房时刻
    LastSeenAt     time.Time         `json:"last_seen_at"`    // 最近状态变化

    // === 人类专属 ===
    UserID         *string           `json:"user_id,omitempty"`        // FK User.ID
    OnStageAt      *time.Time        `json:"on_stage_at,omitempty"`    // = JoinedAt(语义保留)

    // === Agent 专属 ===
    AgentID        *string           `json:"agent_id,omitempty"`      // FK Agent.ID
    AgentConfig    *ParticipantAgentConfig `json:"agent_config,omitempty"`
}

type ParticipantKind string
const (
    KindHuman ParticipantKind = "human"
    KindAgent ParticipantKind = "agent"
)

type StageState string
const (
    StageOn   StageState = "on_stage"   // 在房间里,接收消息推送
    StageOff  StageState = "off_stage"  // 已离房,不再接收
)

type ParticipantAgentConfig struct {
    Mode         AgentMode `json:"mode"`           // silent | active
    ContextMode  ContextMode `json:"context_mode"` // full | incremental
}
```

**说明**:
- **`StageState` 是单一状态**:on_stage = 在房间里(接收推送);off_stage = 已离房(不再接收)
- **人类热插拔语义**:
  - 进房 = 创建 participant(`stage_state=on_stage`),默认接收消息
  - 下麦/离房 = `stage_state=off_stage`(同一次操作),记录保留用于历史
  - 重新进房 = host `participant.pull` → 创建**新一条** participant 记录(旧的 off_stage 记录存档保留)
- **Agent 热插拔语义**:同人类,driver 随上麦加载,随下麦(房间 ended)关闭
- `UserID` 和 `AgentID` **二选一**(CHECK 约束)
- Agent 的 `Mode` 和 `ContextMode` **只有 on_stage 时才生效**

## 4. Agent（AI 智能体）

服务端配置的 agent,运行时被拉入房间。

```go
type Agent struct {
    ID           string      `json:"id"`             // ULID
    Name         string      `json:"name"`           // "会议纪要员"
    Type         AgentType   `json:"type"`           // tool | custom | lobster
    Description  string      `json:"description"`
    AvatarURL    string      `json:"avatar_url,omitempty"`
    Multimodal   bool        `json:"multimodal"`     // 是否支持图片
    Config       AgentConfig `json:"config"`         // type-specific
    CreatedAt    time.Time   `json:"created_at"`
    UpdatedAt    time.Time   `json:"updated_at"`
}

type AgentType string
const (
    AgentTool    AgentType = "tool"      // 可调用函数,无独立人格
    AgentCustom  AgentType = "custom"    // 自定义人格 + memory
    AgentLobster AgentType = "lobster"   // 接入 Hermes/OpenClaw
)

// === Tool agent 配置 ===
type ToolAgentConfig struct {
    // 纯函数描述,被调用时执行
    ToolDefinition ToolDefinition `json:"tool_definition"`
}

type ToolDefinition struct {
    Name        string                 `json:"name"`         // "search_web"
    Description string                 `json:"description"`
    Parameters  map[string]any         `json:"parameters"`   // JSON Schema
    Handler     string                 `json:"handler"`      // HTTP webhook URL
}

// === Custom agent 配置 ===
type CustomAgentConfig struct {
    SystemPrompt   string        `json:"system_prompt"`    // 人格定义
    ModelProvider  string        `json:"model_provider"`   // "openai" | "anthropic" | ...
    ModelName      string        `json:"model_name"`
    Temperature    float64       `json:"temperature"`
    MemoryConfig   MemoryConfig  `json:"memory_config"`
}

type MemoryConfig struct {
    Persistent     bool   `json:"persistent"`       // 跨房间持久化
    MemoryRootPath string `json:"memory_root_path"` // 文件系统路径 (仅 lobster)
    MaxTokens      int    `json:"max_tokens"`       // 上下文上限
}

// === Lobster agent 配置 ===
type LobsterAgentConfig struct {
    BackendType   string `json:"backend_type"`     // "hermes" | "openclaw"
    BackendURL    string `json:"backend_url"`      // e.g. "http://localhost:8420"
    AuthToken     string `json:"auth_token"`       // 调用 backend 的 token
    SandboxPath   string `json:"sandbox_path"`     // 文件沙盒路径 (可选)
}
```

## 5. Message（消息）

```go
type Message struct {
    ID           string         `json:"id"`             // ULID
    RoomID       string         `json:"room_id"`        // FK Room.ID
    SenderKind   SenderKind     `json:"sender_kind"`    // human | agent | system
    SenderID     string         `json:"sender_id"`      // User.ID 或 Agent.ID
    ContentType  ContentType    `json:"content_type"`   // text | image | system
    Content      string         `json:"content"`        // 文本 / 图片 URL / 系统事件 JSON
    Mentions     []string       `json:"mentions"`       // @ 的 participant ID
    ReplyToID    *string        `json:"reply_to_id,omitempty"`
    CreatedAt    time.Time      `json:"created_at"`
}

type SenderKind string
const (
    SenderHuman  SenderKind = "human"
    SenderAgent  SenderKind = "agent"
    SenderSystem SenderKind = "system"
)

type ContentType string
const (
    ContentText   ContentType = "text"
    ContentImage  ContentType = "image"
    ContentSystem ContentType = "system"
)
```

**说明**:
- 系统消息示例: `"actor"上麦了`、`"actor"下麦了`、`房间已归档`
- 房间 `ended` 时,**消息记录物理删除**(或软删除 + 后台 GC)
- 归档时:纪要 agent 一次性读全量,产出归档记录,然后消息被清

## 6. RaiseHand（举手申请）

```go
type RaiseHand struct {
    ID           string    `json:"id"`
    UserID       string    `json:"user_id"`      // FK User.ID
    Note         string    `json:"note"`         // "想讨论 X 话题"
    Status       HandStatus `json:"status"`      // pending | approved | rejected | cancelled
    CreatedAt    time.Time `json:"created_at"`
    ResolvedAt   *time.Time `json:"resolved_at,omitempty"`
    ResolvedBy   *string    `json:"resolved_by,omitempty"` // host user_id
}

type HandStatus string
const (
    HandPending   HandStatus = "pending"
    HandApproved  HandStatus = "approved"
    HandRejected  HandStatus = "rejected"
    HandCancelled HandStatus = "cancelled"
)
```

**说明**:
- 大厅里人类唯一可见的"待处理事项"
- **每个用户同时只能有 1 条 pending**(避免刷屏)
- 主持人批准后,自动创建 Participant(`on_stage=true`),用户从大厅消失、出现在房间

## 7. Archive（归档）

```go
type Archive struct {
    ID           string    `json:"id"`           // ULID
    RoomID       string    `json:"room_id"`      // FK Room.ID
    AgentID      string    `json:"agent_id"`     // 纪要 agent ID
    SummaryMD    string    `json:"summary_md"`   // 完整 Markdown 纪要
    Participants []string  `json:"participants"` // 当时所有 participant ID
    MessageRange TimeRange `json:"message_range"`
    CreatedAt    time.Time `json:"created_at"`
}

type TimeRange struct {
    Start time.Time `json:"start"`
    End   time.Time `json:"end"`
}
```

**说明**:
- **Archive 与 Room 解耦**:Room 清空后,Archive 仍可查
- 保留摘要 + 参与者名单 + 发言时段,**不保留原始消息内容**(防止泄漏)

## ER 关系图

```
User ─────────┬───────── Participant ─────────┐
              │           │                     │
              │           │                     │  Agent ──── (config by type)
              │           │                     │
              │       Room ◄── host_user_id     │
              │         │                       │
              │         │                       │
              │     Message ── room_id           │
              │     Archive ── room_id          │
              │                                 │
       RaiseHand                               (FK)
              │
              └── reviewed_by (User)

每个 Room 必须有 1 个 Host (User)
每个 Room 可有 0..N Participants (Human + Agent),每个 Participant 用 StageState 单一状态
每个 Room 可有 0..N Messages
每个 Room 可有 0..1 Archive (触发归档后)
每个 User 可在 Lobby 同时最多 1 个 pending RaiseHand
```

## 索引建议

```sql
-- User
CREATE UNIQUE INDEX idx_user_phone ON users(phone);

-- Room
CREATE INDEX idx_room_host_status ON rooms(host_user_id, status);
CREATE INDEX idx_room_status_created ON rooms(status, created_at DESC);

-- Participant
CREATE INDEX idx_participant_room ON participants(room_id);
CREATE INDEX idx_participant_user_rooms ON participants(user_id) WHERE user_id IS NOT NULL;
-- 同一 user 在同一 room 同一时刻只能有 1 条 on_stage 记录(允许历史有 off_stage 记录)
CREATE UNIQUE INDEX uniq_participant_room_user_active ON participants(room_id, user_id) WHERE user_id IS NOT NULL AND stage_state = 'on_stage';
CREATE UNIQUE INDEX uniq_participant_room_agent_active ON participants(room_id, agent_id) WHERE agent_id IS NOT NULL AND stage_state = 'on_stage';

-- Message
CREATE INDEX idx_message_room_created ON messages(room_id, created_at DESC);
CREATE INDEX idx_message_sender ON messages(sender_kind, sender_id);

-- RaiseHand
CREATE INDEX idx_hand_status_created ON raise_hands(status, created_at DESC);
CREATE UNIQUE INDEX uniq_hand_pending_per_user ON raise_hands(user_id) WHERE status = 'pending';

-- Archive
CREATE INDEX idx_archive_room ON archives(room_id);
CREATE INDEX idx_archive_created ON archives(created_at DESC);
```

## 不存什么

明确**不存**:
- 房间未结束时的"在线状态"(WebSocket 连接自带)
- 消息已读/未读(异步模式下无意义)
- 编辑历史(分布式消息流禁编辑)

---

## 待你审阅的开放问题

**Q1**: Tool agent 的 `Handler` 是直接 HTTP webhook,还是走消息总线?
- webhook 简单但耦合
- 消息总线解耦但多一层依赖

**Q2**: Custom agent 的 memory 存储位置?
- 数据库(简单,损失灵活性)
- 文件系统(灵活,lobster agent 可直接读)

**Q3**: RaiseHand 是否需要"超时自动拒绝"?

回完这 3 个,我画下一篇 `02-modules.md`。