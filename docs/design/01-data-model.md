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
    WorkspaceEnabled  bool `json:"workspace_enabled"`    // 是否挂载共享 MD 工作区

    // === Workspace 相关 ===
    WorkspaceAutoMergeSeconds int    `json:"workspace_auto_merge_seconds"` // 0 = 仅手动;默认 30
    WorkspaceSummaryAgentID   string `json:"workspace_summary_agent_id"`    // 哪个 custom agent 生成 diff 摘要
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

// 作用域说明:本配置仅用于 "tool" 类型 agent。
// "lobster" 类型 agent 走 LobsterAgentConfig.BackendURL + internal/agents/lobster_driver.go,
// "custom" 类型 agent 走 LLM API 直调(internal/agents/custom_driver.go)。
// webhook 路径不会被 lobster/custom 使用。

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

## 8. Workspace（房间共享文档工作区）

> **新增 2026-07-26** — Agent 协作编辑 MD 文档的轻量方案。
> 后端实现:**`go-git/go-git/v5` 内嵌库**,不部署独立 git 服务。

### 设计原则
- **每房间可选挂载一个 workspace**(host 创建房间时决定)
- 每个被拉入的 agent 自动获得一个**独立 branch**(避免并发写冲突)
- 协作通过**手动触发合并**完成,合并后生成结构化 diff 摘要
- **MVP 不做实时协同**(类似 Google Docs),各 agent 在自己 branch 上独立编辑

### 数据模型

```go
type Workspace struct {
    ID              string    `json:"id"`            // ULID
    RoomID          string    `json:"room_id"`       // FK Room.ID (1:1, 一个房间最多一个工作区)
    RepoPath        string    `json:"repo_path"`     // 服务端文件系统路径 (e.g. /var/fireside/workspaces/<room_id>/.git)
    MainBranch      string    `json:"main_branch"`   // 默认 "main"
    CreatedAt       time.Time `json:"created_at"`
    LastMergeAt     *time.Time `json:"last_merge_at,omitempty"`
    LastMergeCommit string    `json:"last_merge_commit,omitempty"` // SHA
}

type WorkspaceBranch struct {
    ID             string    `json:"id"`             // ULID
    WorkspaceID    string    `json:"workspace_id"`   // FK Workspace.ID
    ParticipantID  string    `json:"participant_id"` // FK Participant.ID (持有该 branch 的 agent)
    BranchName     string    `json:"branch_name"`    // "agent/<agent_id>"
    LastCommitSHA  string    `json:"last_commit_sha"`
    HasUnmerged    bool      `json:"has_unmerged"`   // true = 有未合并的 commit
    CreatedAt      time.Time `json:"created_at"`
    UpdatedAt      time.Time `json:"updated_at"`
}

type WorkspaceMerge struct {
    ID             string         `json:"id"`              // ULID
    WorkspaceID    string         `json:"workspace_id"`    // FK Workspace.ID
    MergedBranches []string       `json:"merged_branches"` // branch_name 列表
    MergeCommitSHA string         `json:"merge_commit_sha"`
    DiffSummary    MergeDiffSummary `json:"diff_summary"`  // 结构化 diff
    HasConflicts   bool           `json:"has_conflicts"`
    ConflictFiles  []string       `json:"conflict_files,omitempty"`
    CreatedAt      time.Time      `json:"created_at"`
}

type MergeDiffSummary struct {
    TotalFilesChanged    int             `json:"total_files_changed"`
    TotalAdditions       int             `json:"total_additions"`
    TotalDeletions       int             `json:"total_deletions"`
    PerBranchCommits     map[string]int  `json:"per_branch_commits"`  // branch → commit count
    HumanReadable        string          `json:"human_readable"`       // AI 生成的摘要 (Phase 2)
    GeneratedByAgentID   string          `json:"generated_by_agent_id"` // 哪个 custom agent 生成的
    GeneratedAt          *time.Time      `json:"generated_at,omitempty"`
}
```

### 核心库依赖

```go
import (
    "github.com/go-git/go-git/v5"          // git 操作 (clone/commit/branch/merge)
    "github.com/sergi/go-diff/diffmatchpatch" // 文本 diff 算法
    "github.com/yuin/goldmark"             // Markdown 解析 (生成结构化 diff)
)
```

### 关键不变量

1. **一个房间最多一个 workspace**(1:1 约束)
2. **Branch 命名空间**:`agent/<agent_id>`(避免冲突)
3. **agent 下麦 = 不删 branch**(可在重新上麦后继续编辑)
4. **冲突时保留双侧**(git 标准冲突标记,不自动解决)
5. **每次合并写一条 system message 通知房间**

### 工作流

```
[Host 创建房间 + workspace=true]
   ↓
[Server: git init bare repo @ RepoPath]
   ↓
[Agent 上麦]
   ↓
[Server: 创建 agent/<id> branch (从 main checkout)]
   ↓
[Agent 通过 tool 调用: workspace.commit(file_path, content)]
   ↓
[Server: 写入 agent worktree + git add + commit]
   ↓
[Host 触发 workspace.merge]
   ↓
[Server:
   1. fetch 所有 unmerged branches
   2. 串行 git merge --no-ff (失败 → 标 conflicts)
   3. 生成 diff summary
   4. 写 WorkspaceMerge 记录
   5. 推一条 system message 到房间
]
```

### 与 Room 的关系

- `Room.Config.WorkspaceEnabled bool`(新增字段)
- host 创建房间时勾选,事后**不可改**(避免 workspace 数据迁移)
- room ended 时 workspace **保留为只读归档**(随 archive 一起保存)

### 已决议(2026-07-26)

- **W1 房间级 / 租户级** → **房间级**(每房间独立 repo,room ended 后归档)
- **W2 触发合并 UX** → **智能定时**(`workspace_auto_merge_seconds`,默认 30 秒)
- **W2.2 静默定义** → **Agent workspace 静默**(人类聊天不打断合并计时)
- **W3 Diff 摘要生成** → **复用 custom agent**(由 host 在 `workspace_summary_agent_id` 指定,**不维护 server 侧 LLM API key**)

### 待审阅的开放问题

(暂无)

---

## 待你审阅的开放问题

**Q1**: Tool agent 的 `Handler` 是直接 HTTP webhook,还是走消息总线?
- webhook 简单但耦合
- 消息总线解耦但多一层依赖

**已决议(2026-07-26)**: **HTTP webhook**(MVP 简单够用)。
- **作用域**:仅 `ToolAgentConfig.Handler` 字段
- **不影响** Lobster agent(它走自己的 `LobsterAgentConfig.BackendURL` + agent driver)
- **不影响** Custom agent(它走 LLM API 直调)

**Q2 ✅** Custom agent memory = 文件系统
**Q3**: RaiseHand 是否需要"超时自动拒绝"?
- 自动清:MVP 简化,大厅保持最简;但 host 失去"待办提醒"
- 不清:多一份维护负担

**已决议(2026-07-26)**: **文件系统**(路径 `/var/fireside/agents/<agent_id>/memory/`)
- 理由:lobster agent(OpenClaw/Hermes)原生读文件系统,零适配
- MVP 单租户,无多实例文件同步问题
- 文件结构:
  ```
  facts.json           # 长期事实(用户偏好 / 项目术语)
  conversations/       # 历史快照(每房间一份 markdown)
  index.json           # 索引(房间 → 文件映射)
  ```
- 由 Fireside 服务端管理目录生命周期(agent 创建/删除时建/清)

**Q3**: RaiseHand 是否需要"超时自动拒绝"?

回完这 3 个,我画下一篇 `02-modules.md`。